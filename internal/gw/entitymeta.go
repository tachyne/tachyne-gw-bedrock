package gw

import (
	"bytes"
	"io"

	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
	tproto "github.com/tachyne/tachyne-common/protocol"
)

// Entity metadata. The world's canonical set_entity_data carries what a
// Java client renders; what Bedrock has a twin for is folded into a
// mobLook and rendered as actor data. Java's indices past the shared
// Entity/LivingEntity/Mob fields mean different things per mob (index 17
// is a sheep's fleece, a pet's sitting flag, a creeper's charge, a frog's
// variant…), so the mob's identity decides how an entry is read; the
// Bedrock keys and value numbering follow what the Geyser project
// established for each mob (MIT).

// metaEntry is one parsed canonical entry.
type metaEntry struct {
	idx  byte
	typ  int32
	val  int64    // byte/bool/varint/pose/optional-state payload
	vals [3]int64 // villager data: type, profession, level
	str  string   // an optional text component (custom name), "" = none
}

// Canonical (770) metadata value types this walker understands.
const (
	metaTypeByte        = 0
	metaTypeVarInt      = 1
	metaTypeFloat       = 3
	metaTypeOptText     = 6 // Optional<Component>: bool + network NBT
	metaTypeBool        = 8
	metaTypeBlockPos    = 10 // a packed long
	metaTypeOptBlockPos = 11 // bool + packed long
	metaTypeOptState    = 15 // Optional<BlockState>: one VarInt, 0 = empty
	metaTypeVillager    = tproto.VillagerDataSerializer770
	metaTypePose        = 21
	// The mob-variant registry holders (one VarInt registry id each).
	metaTypeFrogVariant    = tproto.FrogVariantSerializer770
	metaTypeWolfVariant    = tproto.WolfVariantSerializer770
	metaTypeCatVariant     = tproto.CatVariantSerializer770
	metaTypePigVariant     = tproto.PigVariantSerializer770
	metaTypeCowVariant     = tproto.CowVariantSerializer770
	metaTypeChickenVariant = tproto.ChickenVariantSerializer770
)

// Bedrock's VARIANT numbering for the holder-variant coats, by canonical
// registry name (Geyser's WolfVariant / CatVariant ordinals). The canonical
// wire id is the name's position in the registry list the Java gateways
// send, so the lookup goes id → name → Bedrock ordinal.
var (
	wolfVariantBedrock = map[string]int32{
		"minecraft:pale": 0, "minecraft:ashen": 1, "minecraft:black": 2, "minecraft:chestnut": 3,
		"minecraft:rusty": 4, "minecraft:snowy": 5, "minecraft:spotted": 6, "minecraft:striped": 7,
		"minecraft:woods": 8,
	}
	catVariantBedrock = map[string]int32{
		"minecraft:white": 0, "minecraft:black": 1, "minecraft:red": 2, "minecraft:siamese": 3,
		"minecraft:british_shorthair": 4, "minecraft:calico": 5, "minecraft:persian": 6,
		"minecraft:ragdoll": 7, "minecraft:tabby": 8, "minecraft:all_black": 9, "minecraft:jellie": 10,
	}
)

// registryEntryName resolves a canonical registry id to its entry name, ""
// when out of range.
func registryEntryName(regID string, id int64) string {
	for _, reg := range tproto.SyncedRegistries {
		if reg.ID == regID {
			if id >= 0 && int(id) < len(reg.Entries) {
				return reg.Entries[id]
			}
			return ""
		}
	}
	return ""
}

// parseSimpleMeta walks a canonical metadata list as far as the value
// types it knows reach; an entry of any other type ends the walk, since
// its size is unknown here. Text components are read only in the form the
// world emits (a bare NBT string).
func parseSimpleMeta(meta []byte) []metaEntry {
	var out []metaEntry
	r := bytes.NewReader(meta)
	for {
		idx, err := r.ReadByte()
		if err != nil || idx == 0xff {
			return out
		}
		typ, err := tproto.ReadVarInt(r)
		if err != nil {
			return out
		}
		e := metaEntry{idx: idx, typ: typ}
		switch typ {
		case metaTypeByte, metaTypeBool:
			b, err := r.ReadByte()
			if err != nil {
				return out
			}
			e.val = int64(b)
		case metaTypeVarInt, metaTypePose, metaTypeOptState, metaTypeFrogVariant, metaTypeWolfVariant,
			metaTypeCatVariant, metaTypePigVariant, metaTypeCowVariant, metaTypeChickenVariant:
			v, err := tproto.ReadVarInt(r)
			if err != nil {
				return out
			}
			e.val = int64(v)
		case metaTypeVillager:
			for i := range e.vals {
				v, err := tproto.ReadVarInt(r)
				if err != nil {
					return out
				}
				e.vals[i] = int64(v)
			}
		case metaTypeFloat:
			var f [4]byte
			if _, err := io.ReadFull(r, f[:]); err != nil {
				return out
			}
		case metaTypeBlockPos:
			var f [8]byte
			if _, err := io.ReadFull(r, f[:]); err != nil {
				return out
			}
		case metaTypeOptBlockPos:
			has, err := r.ReadByte()
			if err != nil {
				return out
			}
			if has != 0 {
				var f [8]byte
				if _, err := io.ReadFull(r, f[:]); err != nil {
					return out
				}
			}
			e.val = int64(has)
		case metaTypeOptText:
			has, err := r.ReadByte()
			if err != nil {
				return out
			}
			if has != 0 {
				var hdr [3]byte // TAG_String + big-endian length
				if _, err := io.ReadFull(r, hdr[:]); err != nil || hdr[0] != 0x08 {
					return out
				}
				buf := make([]byte, int(hdr[1])<<8|int(hdr[2]))
				if _, err := io.ReadFull(r, buf); err != nil {
					return out
				}
				e.str = string(buf)
			}
			e.val = int64(has)
		default:
			return out
		}
		out = append(out, e)
	}
}

// mobLook is what Bedrock renders of an entity's metadata.
type mobLook struct {
	// Shared entity flags and pose.
	onFire, sneaking, sprinting, invisible, gliding, swimming, sleeping bool
	baby                                                                bool
	// Per-mob looks; which apply depends on the mob.
	sheared, sitting, tamed, angry, powered, ignited, climbing, shaking bool
	bribed                                                              bool // the killer bunny's Bedrock flag
	dancing                                                             bool // the allay's jukebox dance
	sniffing, digging                                                   bool // the sniffer's poses
	hasColor                                                            bool
	color                                                               byte
	hasVariant, hasMark, hasTier, hasStrength                           bool
	variant, markVariant, tradeTier, strength                           int32
	scale                                                               float32 // 0 = the default
	hasCarry                                                            bool
	carry                                                               uint32 // canonical block state, 0 = none
	hasTarget                                                           bool
	target                                                              int32 // the entity id a guardian beam locks on
	hasName                                                             bool
	name                                                                string
	nameVisible                                                         bool
	hasFuse                                                             bool
	fuse                                                                int32
	hasClimate                                                          bool
	climate                                                             int32 // Bedrock climate_variant index: 0 temperate, 1 warm, 2 cold
}

// villagerProfessionBedrock and villagerTypeBedrock renumber the canonical
// VillagerData registries (professions: none, armorer, butcher,
// cartographer, cleric, farmer, fisherman, fletcher, leatherworker,
// librarian, mason, nitwit, shepherd, toolsmith, weaponsmith; types:
// desert, jungle, plains, savanna, snow, swamp, taiga) into Bedrock's.
var (
	villagerProfessionBedrock = [...]int32{0, 8, 11, 6, 7, 1, 2, 4, 12, 5, 13, 14, 3, 10, 9}
	villagerTypeBedrock       = [...]int32{1, 2, 0, 3, 4, 5, 6}
)

// applyMeta folds the entries Bedrock understands into the entity's look;
// returns whether anything it renders changed.
func (st *entState) applyMeta(entries []metaEntry) bool {
	before := st.look
	l := &st.look
	for _, e := range entries {
		switch {
		case e.idx == 0 && e.typ == metaTypeByte: // Entity flags byte
			l.onFire = e.val&0x01 != 0
			l.sneaking = e.val&0x02 != 0
			l.sprinting = e.val&0x08 != 0
			l.invisible = e.val&0x20 != 0
			l.gliding = e.val&0x80 != 0
		case e.idx == 2 && e.typ == metaTypeOptText: // custom name
			l.hasName, l.name = true, e.str
		case e.idx == 3 && e.typ == metaTypeBool:
			l.nameVisible = e.val != 0
		case e.idx == 6 && e.typ == metaTypePose:
			l.sneaking = e.val == 5
			l.gliding = e.val == 1
			l.sleeping = e.val == 2
			l.swimming = e.val == 3
			l.sniffing = e.val == 12 // Pose.SNIFFING (the same id on every served version)
			l.digging = e.val == 14  // Pose.DIGGING
		case e.idx == 16 && e.typ == metaTypeBool && st.ident != "minecraft:creeper": // ageable / zombie baby
			l.baby = e.val != 0
		default:
			st.applyMobMeta(e)
		}
	}
	return st.look != before
}

// applyMobMeta reads one entry of a mob's own fields, by the mob.
func (st *entState) applyMobMeta(e metaEntry) {
	l := &st.look
	switch st.ident {
	case "minecraft:sheep":
		if e.idx == 17 && e.typ == metaTypeByte { // fleece: colour bits + sheared 0x10
			l.hasColor, l.color = true, byte(e.val&0x0f)
			l.sheared = e.val&0x10 != 0
		}
	case "minecraft:allay":
		if e.idx == 16 && e.typ == metaTypeBool { // DATA_DANCING
			l.dancing = e.val != 0
		}
	case "minecraft:wolf", "minecraft:cat", "minecraft:parrot":
		switch {
		case e.idx == 17 && e.typ == metaTypeByte: // TamableAnimal flags
			l.sitting = e.val&0x01 != 0
			l.angry = e.val&0x02 != 0
			l.tamed = e.val&0x04 != 0
		case st.ident == "minecraft:wolf" && e.idx == 22 && e.typ == metaTypeWolfVariant: // coat: a wolf_variant holder
			if v, ok := wolfVariantBedrock[registryEntryName("minecraft:wolf_variant", e.val)]; ok {
				l.hasVariant, l.variant = true, v
			}
		case st.ident == "minecraft:cat" && e.idx == 19 && e.typ == metaTypeCatVariant: // coat: a cat_variant holder
			if v, ok := catVariantBedrock[registryEntryName("minecraft:cat_variant", e.val)]; ok {
				l.hasVariant, l.variant = true, v
			}
		case st.ident == "minecraft:wolf" && e.idx == 20 && e.typ == metaTypeVarInt, // DATA_COLLAR_COLOR: the dye ordinal
			st.ident == "minecraft:cat" && e.idx == 22 && e.typ == metaTypeVarInt:
			l.hasColor, l.color = true, byte(e.val&0x0f) // Bedrock's COLOR key, as Geyser sets it
		case st.ident == "minecraft:parrot" && e.idx == 19 && e.typ == metaTypeVarInt: // colour: the same five numbers
			l.hasVariant, l.variant = true, int32(clampIdx(e.val, 5))
		}
	case "minecraft:horse":
		if e.idx == 18 && e.typ == metaTypeVarInt { // colour | markings<<8: Bedrock splits them
			l.hasVariant, l.variant = true, int32(e.val&0xff)
			l.hasMark, l.markVariant = true, int32((e.val>>8)%5)
		}
	case "minecraft:llama": // the trader llama renders as a llama too
		switch {
		case e.idx == 19 && e.typ == metaTypeVarInt: // strength: chest columns
			l.hasStrength, l.strength = true, int32(e.val)
		case e.idx == 20 && e.typ == metaTypeVarInt: // coat: creamy, white, brown, gray on both editions
			l.hasVariant, l.variant = true, int32(clampIdx(e.val, 4))
		}
	case "minecraft:rabbit":
		if e.idx == 17 && e.typ == metaTypeVarInt { // type: the same six numbers; 99 (killer) is white + bribed
			l.hasVariant, l.variant = true, int32(e.val)
			l.bribed = false
			if e.val == 99 {
				l.variant, l.bribed = 1, true
			} else if e.val < 0 || e.val > 5 {
				l.variant = 0
			}
		}
	case "minecraft:fox", "minecraft:mooshroom":
		if e.idx == 17 && e.typ == metaTypeVarInt { // fox red/snow, mooshroom red/brown: the same two numbers
			l.hasVariant, l.variant = true, int32(clampIdx(e.val, 2))
		}
		if st.ident == "minecraft:fox" && e.idx == 18 && e.typ == metaTypeByte { // DATA_FLAGS: crouching 4, sleeping 32
			l.sneaking = e.val&0x04 != 0
			l.sleeping = e.val&0x20 != 0
		}
	case "minecraft:pig", "minecraft:cow", "minecraft:chicken":
		// The 1.21.5 temperature variants (cold/temperate/warm holders at pig
		// 18, cow 17, chicken 17) are the Bedrock ENTITY PROPERTY
		// minecraft:climate_variant (defined per type by climateProperty
		// after StartGame), carried on the actor as an int property rather
		// than actor data. Registry order cold, temperate, warm → the
		// property's enum order temperate, warm, cold.
		idx, typ := byte(17), int32(metaTypeCowVariant)
		switch st.ident {
		case "minecraft:pig":
			idx, typ = 18, metaTypePigVariant
		case "minecraft:chicken":
			typ = metaTypeChickenVariant
		}
		if e.idx == idx && e.typ == typ {
			l.hasClimate = true
			l.climate = [...]int32{2, 0, 1}[clampIdx(e.val, 3)]
		}
	case "minecraft:bee":
		switch {
		case e.idx == 17 && e.typ == metaTypeByte: // stung: Bedrock's mark variant
			l.hasMark = true
			l.markVariant = 0
			if e.val&0x04 != 0 {
				l.markVariant = 1
			}
		case e.idx == 18 && e.typ == metaTypeVarInt: // remaining anger
			l.angry = e.val > 0
		}
	case "minecraft:creeper":
		switch {
		case e.idx == 16 && e.typ == metaTypeVarInt: // swell direction: +1 is fusing
			l.ignited = e.val == 1
		case e.idx == 17 && e.typ == metaTypeBool:
			l.powered = e.val != 0
		}
	case "minecraft:slime", "minecraft:magma_cube":
		if e.idx == 16 && e.typ == metaTypeVarInt { // size 1/2/4: the cube's scale
			l.scale = 0.1 + float32(e.val)
		}
	case "minecraft:spider", "minecraft:cave_spider":
		if e.idx == 16 && e.typ == metaTypeByte {
			l.climbing = e.val&0x01 != 0
		}
	case "minecraft:zombie", "minecraft:husk", "minecraft:drowned":
		if e.idx == 18 && e.typ == metaTypeBool { // drowning conversion: shaking
			l.shaking = e.val != 0
		}
	case "minecraft:zombie_villager_v2":
		switch {
		case e.idx == 19 && e.typ == metaTypeBool: // curing
			l.shaking = e.val != 0
		case e.idx == 20 && e.typ == metaTypeVillager:
			l.setVillager(e.vals)
		}
	case "minecraft:villager_v2":
		if e.idx == 18 && e.typ == metaTypeVillager {
			l.setVillager(e.vals)
		}
	case "minecraft:enderman":
		if e.idx == 16 && e.typ == metaTypeOptState {
			l.hasCarry, l.carry = true, uint32(e.val)
		}
	case "minecraft:guardian", "minecraft:elder_guardian":
		if e.idx == 17 && e.typ == metaTypeVarInt {
			l.hasTarget, l.target = true, int32(e.val)
		}
	case "minecraft:frog":
		if e.idx == 17 && e.typ == metaTypeFrogVariant { // canonical cold 0, temperate 1, warm 2
			l.hasVariant = true
			l.variant = [...]int32{1, 0, 2}[clampIdx(e.val, 3)] // Bedrock temperate 0, cold 1, warm 2
		}
	case "minecraft:axolotl":
		if e.idx == 17 && e.typ == metaTypeVarInt { // wild and cyan swap places
			l.hasVariant = true
			l.variant = [...]int32{0, 3, 2, 1, 4}[clampIdx(e.val, 5)]
		}
	case "minecraft:tnt":
		if e.idx == 8 && e.typ == metaTypeVarInt {
			l.hasFuse, l.fuse = true, int32(e.val)
		}
	}
}

func clampIdx(v int64, n int) int {
	if v < 0 || int(v) >= n {
		return 0
	}
	return int(v)
}

// setVillager takes canonical VillagerData (type, profession, level).
func (l *mobLook) setVillager(v [3]int64) {
	l.hasVariant, l.hasMark, l.hasTier = true, true, true
	l.variant = villagerProfessionBedrock[clampIdx(v[1], len(villagerProfessionBedrock))]
	l.markVariant = villagerTypeBedrock[clampIdx(v[0], len(villagerTypeBedrock))]
	l.tradeTier = int32(v[2]) - 1
	if l.tradeTier < 0 {
		l.tradeTier = 0
	}
}

// actorData is the SetActorData carrying the entity's current look.
func actorData(eid int32, st *entState) *packet.SetActorData {
	l := &st.look
	m := baseMetadata(0, 0)
	flag := func(on bool, f uint8) {
		if on {
			setActorFlag(m, f)
		}
	}
	flag(l.onFire, protocol.EntityDataFlagOnFire)
	flag(l.sneaking, protocol.EntityDataFlagSneaking)
	flag(l.sprinting, protocol.EntityDataFlagSprinting)
	flag(l.invisible, protocol.EntityDataFlagInvisible)
	flag(l.gliding, protocol.EntityDataFlagGliding)
	flag(l.swimming, protocol.EntityDataFlagSwimming)
	flag(l.sleeping, protocol.EntityDataFlagSleeping)
	flag(l.baby, protocol.EntityDataFlagBaby)
	flag(l.sheared, protocol.EntityDataFlagSheared)
	flag(l.sitting, protocol.EntityDataFlagSitting)
	flag(l.dancing, protocol.EntityDataFlagDancing)
	flag(l.sniffing, protocol.EntityDataFlagSniffing)
	flag(l.digging, protocol.EntityDataFlagDigging)
	flag(l.tamed, protocol.EntityDataFlagTamed)
	flag(l.angry, protocol.EntityDataFlagAngry)
	flag(l.powered, protocol.EntityDataFlagPowered)
	flag(l.ignited, protocol.EntityDataFlagIgnited)
	flag(l.climbing, protocol.EntityDataFlagWallClimbing)
	flag(l.shaking, protocol.EntityDataFlagShaking)
	flag(l.bribed, protocol.EntityDataFlagBribed)
	switch {
	case l.scale > 0:
		m[protocol.EntityDataKeyScale] = l.scale
	case l.baby:
		m[protocol.EntityDataKeyScale] = float32(0.5)
	default:
		m[protocol.EntityDataKeyScale] = float32(1)
	}
	if l.hasColor {
		m[protocol.EntityDataKeyColorIndex] = l.color
	}
	if l.hasVariant {
		m[protocol.EntityDataKeyVariant] = l.variant
	}
	if l.hasMark {
		m[protocol.EntityDataKeyMarkVariant] = l.markVariant
	}
	if l.hasTier {
		m[protocol.EntityDataKeyTradeTier] = l.tradeTier
	}
	if l.hasStrength {
		m[protocol.EntityDataKeyStrength] = l.strength
	}
	if l.hasCarry {
		var rid int32
		if l.carry != 0 {
			rid = int32(bedrockBlockRID(l.carry))
		}
		m[protocol.EntityDataKeyCarryBlockRuntimeID] = rid
	}
	if l.hasTarget {
		var t int64
		if l.target != 0 {
			t = int64(rt(l.target))
		}
		m[protocol.EntityDataKeyTarget] = t
	}
	if l.hasName && !st.player {
		m[protocol.EntityDataKeyName] = l.name
		var show byte
		if l.nameVisible && l.name != "" {
			show = 1
		}
		m[protocol.EntityDataKeyAlwaysShowNameTag] = show
	}
	if l.hasFuse {
		m[protocol.EntityDataKeyFuseTime] = l.fuse
	}
	pd := &packet.SetActorData{EntityRuntimeID: rt(eid), EntityMetadata: m}
	if l.hasClimate {
		pd.EntityProperties.IntegerProperties = []protocol.IntegerEntityProperty{{Index: 0, Value: l.climate}}
	}
	return pd
}

// setActorFlag sets one actor flag: the first 64 live in the flags word,
// the rest (sleeping, climbing… numbered from 64) in the second.
func setActorFlag(m protocol.EntityMetadata, f uint8) {
	if f < 64 {
		m.SetFlag(protocol.EntityDataKeyFlags, f)
		return
	}
	if _, ok := m[protocol.EntityDataKeyFlagsTwo]; !ok {
		m[protocol.EntityDataKeyFlagsTwo] = int64(0)
	}
	m.SetFlag(protocol.EntityDataKeyFlagsTwo, f-64)
}

// actorFlag reports one actor flag, wherever it lives.
func actorFlag(m protocol.EntityMetadata, f uint8) bool {
	if f < 64 {
		return m.Flag(protocol.EntityDataKeyFlags, f)
	}
	if _, ok := m[protocol.EntityDataKeyFlagsTwo]; !ok {
		return false
	}
	return m.Flag(protocol.EntityDataKeyFlagsTwo, f-64)
}
