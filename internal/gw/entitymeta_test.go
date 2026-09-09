package gw

import (
	"bytes"
	"testing"

	"github.com/sandertv/gophertunnel/minecraft/protocol"
	tproto "github.com/tachyne/tachyne-common/protocol"
)

func TestSimpleMetaParse(t *testing.T) {
	meta := []byte{0}
	meta = tproto.AppendVarInt(meta, 0) // byte flags
	meta = append(meta, 0x03)           // on fire + sneaking
	meta = append(meta, 16)
	meta = tproto.AppendVarInt(meta, 8) // bool baby
	meta = append(meta, 1)
	meta = append(meta, 0xff)
	st := &entState{ident: "minecraft:cow"}
	if !st.applyMeta(parseSimpleMeta(meta)) || !st.look.onFire || !st.look.sneaking || !st.look.baby {
		t.Errorf("flags not applied: %+v", st)
	}
	if pd := actorData(5, st); pd.EntityRuntimeID != rt(5) || len(pd.EntityMetadata) == 0 {
		t.Errorf("actor data %+v", pd)
	}
	// An unknown type stops the walk without touching what came before.
	meta2 := []byte{16}
	meta2 = tproto.AppendVarInt(meta2, 8)
	meta2 = append(meta2, 0)
	meta2 = append(meta2, 8)
	meta2 = tproto.AppendVarInt(meta2, 7) // a Slot: not walked
	meta2 = append(meta2, 1, 2, 3)
	st2 := &entState{ident: "minecraft:cow", look: mobLook{baby: true}}
	if !st2.applyMeta(parseSimpleMeta(meta2)) || st2.look.baby {
		t.Errorf("baby cleared before the unknown entry: %+v", st2)
	}
}

// entry builds one canonical metadata entry: index, type, then the payload.
func entry(idx byte, typ int32, payload ...byte) []byte {
	b := []byte{idx}
	b = tproto.AppendVarInt(b, typ)
	return append(b, payload...)
}

func varint(v int32) []byte { return tproto.AppendVarInt(nil, v) }

// The same index means different things per mob; each reads as its own
// look and renders to the Bedrock key Geyser established.
func TestMobLooks(t *testing.T) {
	type want struct {
		ident string
		meta  []byte
		check func(*testing.T, mobLook, protocol.EntityMetadata)
	}
	cases := []want{
		{"minecraft:sheep", entry(17, 0, 0x10|14), func(t *testing.T, l mobLook, m protocol.EntityMetadata) {
			if !l.sheared || !l.hasColor || l.color != 14 || m[protocol.EntityDataKeyColorIndex] != byte(14) {
				t.Errorf("sheep %+v %v", l, m[protocol.EntityDataKeyColorIndex])
			}
			if actorFlag(m, protocol.EntityDataFlagSheared) == false {
				t.Error("sheared flag")
			}
		}},
		{"minecraft:wolf", entry(17, 0, 0x05), func(t *testing.T, l mobLook, m protocol.EntityMetadata) {
			if !l.sitting || !l.tamed || l.angry || !actorFlag(m, protocol.EntityDataFlagSitting) {
				t.Errorf("wolf %+v", l)
			}
		}},
		{"minecraft:creeper", append(entry(16, 1, varint(1)...), entry(17, 8, 1)...), func(t *testing.T, l mobLook, m protocol.EntityMetadata) {
			if !l.ignited || !l.powered || l.baby || !actorFlag(m, protocol.EntityDataFlagPowered) {
				t.Errorf("creeper %+v", l)
			}
		}},
		{"minecraft:slime", entry(16, 1, varint(4)...), func(t *testing.T, l mobLook, m protocol.EntityMetadata) {
			if m[protocol.EntityDataKeyScale] != float32(4.1) {
				t.Errorf("slime scale %v", m[protocol.EntityDataKeyScale])
			}
		}},
		{"minecraft:villager_v2", entry(18, tproto.VillagerDataSerializer770, append(append(varint(0), varint(5)...), varint(3)...)...),
			func(t *testing.T, l mobLook, m protocol.EntityMetadata) {
				// a desert farmer at level 3: Bedrock farmer 1, desert 1, tier 2
				if m[protocol.EntityDataKeyVariant] != int32(1) || m[protocol.EntityDataKeyMarkVariant] != int32(1) || m[protocol.EntityDataKeyTradeTier] != int32(2) {
					t.Errorf("villager %v %v %v", m[protocol.EntityDataKeyVariant], m[protocol.EntityDataKeyMarkVariant], m[protocol.EntityDataKeyTradeTier])
				}
			}},
		{"minecraft:zombie_villager_v2", append(entry(19, 8, 1), entry(20, tproto.VillagerDataSerializer770, append(append(varint(2), varint(9)...), varint(1)...)...)...),
			func(t *testing.T, l mobLook, m protocol.EntityMetadata) {
				if !l.shaking || m[protocol.EntityDataKeyVariant] != int32(5) || m[protocol.EntityDataKeyMarkVariant] != int32(0) || m[protocol.EntityDataKeyTradeTier] != int32(0) {
					t.Errorf("zombie villager %+v", l)
				}
			}},
		{"minecraft:axolotl", entry(17, 1, varint(1)...), func(t *testing.T, l mobLook, m protocol.EntityMetadata) {
			if m[protocol.EntityDataKeyVariant] != int32(3) { // wild is Bedrock's 3
				t.Errorf("axolotl %v", m[protocol.EntityDataKeyVariant])
			}
		}},
		{"minecraft:frog", entry(17, tproto.FrogVariantSerializer770, varint(0)...), func(t *testing.T, l mobLook, m protocol.EntityMetadata) {
			if m[protocol.EntityDataKeyVariant] != int32(1) { // cold is Bedrock's 1
				t.Errorf("frog %v", m[protocol.EntityDataKeyVariant])
			}
		}},
		{"minecraft:enderman", entry(16, 15, varint(int32(uint32(1)))...), func(t *testing.T, l mobLook, m protocol.EntityMetadata) {
			if !l.hasCarry || m[protocol.EntityDataKeyCarryBlockRuntimeID] != int32(bedrockBlockRID(uint32(1))) {
				t.Errorf("enderman %+v %v", l, m[protocol.EntityDataKeyCarryBlockRuntimeID])
			}
		}},
		{"minecraft:guardian", entry(17, 1, varint(77)...), func(t *testing.T, l mobLook, m protocol.EntityMetadata) {
			if m[protocol.EntityDataKeyTarget] != int64(rt(77)) {
				t.Errorf("guardian target %v", m[protocol.EntityDataKeyTarget])
			}
		}},
		{"minecraft:cow", append(entry(2, 6, append([]byte{1, 0x08, 0, 5}, "Daisy"...)...), entry(3, 8, 1)...), func(t *testing.T, l mobLook, m protocol.EntityMetadata) {
			if m[protocol.EntityDataKeyName] != "Daisy" || m[protocol.EntityDataKeyAlwaysShowNameTag] != byte(1) {
				t.Errorf("name %v %v", m[protocol.EntityDataKeyName], m[protocol.EntityDataKeyAlwaysShowNameTag])
			}
		}},
		{"minecraft:tnt", entry(8, 1, varint(60)...), func(t *testing.T, l mobLook, m protocol.EntityMetadata) {
			if m[protocol.EntityDataKeyFuseTime] != int32(60) {
				t.Errorf("fuse %v", m[protocol.EntityDataKeyFuseTime])
			}
		}},
		{"minecraft:cow", entry(6, 21, varint(2)...), func(t *testing.T, l mobLook, m protocol.EntityMetadata) {
			if !l.sleeping || !actorFlag(m, protocol.EntityDataFlagSleeping) {
				t.Errorf("pose %+v", l)
			}
		}},
		// A wolf's coat is a wolf_variant holder at 22: the canonical id is the
		// registry position (ashen 0 … woods 8, alphabetical), Bedrock's the
		// Geyser ordinal (pale 0, ashen 1, black 2, chestnut 3, rusty 4, snowy
		// 5, spotted 6, striped 7, woods 8).
		{"minecraft:wolf", append(entry(17, 0, 0x04), entry(22, tproto.WolfVariantSerializer770, varint(canonicalRegistryID(t, "minecraft:wolf_variant", "minecraft:woods"))...)...),
			func(t *testing.T, l mobLook, m protocol.EntityMetadata) {
				if !l.tamed || m[protocol.EntityDataKeyVariant] != int32(8) {
					t.Errorf("woods wolf %+v %v", l, m[protocol.EntityDataKeyVariant])
				}
			}},
		{"minecraft:wolf", entry(22, tproto.WolfVariantSerializer770, varint(canonicalRegistryID(t, "minecraft:wolf_variant", "minecraft:pale"))...),
			func(t *testing.T, l mobLook, m protocol.EntityMetadata) {
				if m[protocol.EntityDataKeyVariant] != int32(0) {
					t.Errorf("pale wolf %v", m[protocol.EntityDataKeyVariant])
				}
			}},
		// A cat's coat is a cat_variant holder at 19 (all_black 0 … white 10
		// canonically; Bedrock white 0 … jellie 10).
		{"minecraft:cat", entry(19, tproto.CatVariantSerializer770, varint(canonicalRegistryID(t, "minecraft:cat_variant", "minecraft:jellie"))...),
			func(t *testing.T, l mobLook, m protocol.EntityMetadata) {
				if m[protocol.EntityDataKeyVariant] != int32(10) {
					t.Errorf("jellie cat %v", m[protocol.EntityDataKeyVariant])
				}
			}},
		{"minecraft:cat", entry(19, tproto.CatVariantSerializer770, varint(canonicalRegistryID(t, "minecraft:cat_variant", "minecraft:all_black"))...),
			func(t *testing.T, l mobLook, m protocol.EntityMetadata) {
				if m[protocol.EntityDataKeyVariant] != int32(9) {
					t.Errorf("all_black cat %v", m[protocol.EntityDataKeyVariant])
				}
			}},
		// A horse packs colour | markings<<8 into one INT at 18; Bedrock wants
		// VARIANT = colour and MARK_VARIANT = markings.
		{"minecraft:horse", entry(18, 1, varint(4|(3<<8))...), func(t *testing.T, l mobLook, m protocol.EntityMetadata) {
			if m[protocol.EntityDataKeyVariant] != int32(4) || m[protocol.EntityDataKeyMarkVariant] != int32(3) {
				t.Errorf("horse %v %v", m[protocol.EntityDataKeyVariant], m[protocol.EntityDataKeyMarkVariant])
			}
		}},
		// A llama (and a trader llama, the same Bedrock mob) carries strength at
		// 19 and its coat at 20, both INT.
		{"minecraft:llama", append(entry(19, 1, varint(4)...), entry(20, 1, varint(2)...)...), func(t *testing.T, l mobLook, m protocol.EntityMetadata) {
			if m[protocol.EntityDataKeyVariant] != int32(2) || m[protocol.EntityDataKeyStrength] != int32(4) {
				t.Errorf("llama %v %v", m[protocol.EntityDataKeyVariant], m[protocol.EntityDataKeyStrength])
			}
		}},
		{"minecraft:parrot", entry(19, 1, varint(3)...), func(t *testing.T, l mobLook, m protocol.EntityMetadata) {
			if m[protocol.EntityDataKeyVariant] != int32(3) {
				t.Errorf("parrot %v", m[protocol.EntityDataKeyVariant])
			}
		}},
		{"minecraft:rabbit", entry(17, 1, varint(5)...), func(t *testing.T, l mobLook, m protocol.EntityMetadata) {
			if m[protocol.EntityDataKeyVariant] != int32(5) || actorFlag(m, protocol.EntityDataFlagBribed) {
				t.Errorf("salt rabbit %v", m[protocol.EntityDataKeyVariant])
			}
		}},
		{"minecraft:rabbit", entry(17, 1, varint(99)...), func(t *testing.T, l mobLook, m protocol.EntityMetadata) {
			if m[protocol.EntityDataKeyVariant] != int32(1) || !actorFlag(m, protocol.EntityDataFlagBribed) {
				t.Errorf("killer bunny %v bribed=%v", m[protocol.EntityDataKeyVariant], l.bribed)
			}
		}},
		{"minecraft:fox", entry(17, 1, varint(1)...), func(t *testing.T, l mobLook, m protocol.EntityMetadata) {
			if m[protocol.EntityDataKeyVariant] != int32(1) {
				t.Errorf("snow fox %v", m[protocol.EntityDataKeyVariant])
			}
		}},
		{"minecraft:mooshroom", entry(17, 1, varint(1)...), func(t *testing.T, l mobLook, m protocol.EntityMetadata) {
			if m[protocol.EntityDataKeyVariant] != int32(1) {
				t.Errorf("brown mooshroom %v", m[protocol.EntityDataKeyVariant])
			}
		}},
	}
	for _, c := range cases {
		st := &entState{ident: c.ident}
		meta := append(append([]byte{}, c.meta...), 0xff)
		if !st.applyMeta(parseSimpleMeta(meta)) {
			t.Errorf("%s: nothing changed", c.ident)
			continue
		}
		c.check(t, st.look, actorData(1, st).EntityMetadata)
	}
	// A pig's temperature holder is an entity property on Bedrock, not actor
	// data: it is walked (a later entry still applies) and dropped.
	pig := &entState{ident: "minecraft:pig"}
	pigMeta := append(entry(18, tproto.PigVariantSerializer770, varint(2)...), entry(16, 8, 1)...)
	if !pig.applyMeta(parseSimpleMeta(append(pigMeta, 0xff))) || !pig.look.baby || pig.look.hasVariant {
		t.Errorf("pig temperature variant should be skipped, the baby flag after it kept: %+v", pig.look)
	}
	// A sheep's index 17 is not a pet's: no sitting flag from a fleece byte.
	st := &entState{ident: "minecraft:sheep"}
	st.applyMeta(parseSimpleMeta(append(entry(17, 0, 0x01), 0xff)))
	if st.look.sitting || st.look.color != 1 {
		t.Errorf("sheep read as a pet: %+v", st.look)
	}
}

// canonicalRegistryID is an entry's wire id: its position in the registry
// list the Java gateways send.
func canonicalRegistryID(t *testing.T, regID, name string) int32 {
	t.Helper()
	for _, reg := range tproto.SyncedRegistries {
		if reg.ID != regID {
			continue
		}
		for i, e := range reg.Entries {
			if e == name {
				return int32(i)
			}
		}
	}
	t.Fatalf("%s has no %s", regID, name)
	return -1
}

// A pig's climate holder (registry order cold, temperate, warm) becomes the
// Bedrock climate_variant property index (temperate, warm, cold) on the actor
// data, and the type's property definition round-trips through the NBT
// encoder.
func TestClimateVariantProperty(t *testing.T) {
	for _, c := range []struct {
		ident     string
		idx       byte
		typ       int32
		val, want int32
	}{
		{"minecraft:pig", 18, metaTypePigVariant, 0, 2}, // cold
		{"minecraft:pig", 18, metaTypePigVariant, 1, 0}, // temperate
		{"minecraft:cow", 17, metaTypeCowVariant, 2, 1}, // warm
		{"minecraft:chicken", 17, metaTypeChickenVariant, 0, 2},
	} {
		meta := entry(c.idx, c.typ, tproto.AppendVarInt(nil, c.val)...)
		meta = append(meta, 0xff)
		st := &entState{ident: c.ident}
		if !st.applyMeta(parseSimpleMeta(meta)) || !st.look.hasClimate || st.look.climate != c.want {
			t.Errorf("%s holder %d: look %+v, want climate %d", c.ident, c.val, st.look, c.want)
			continue
		}
		pd := actorData(7, st)
		if len(pd.EntityProperties.IntegerProperties) != 1 || pd.EntityProperties.IntegerProperties[0].Value != c.want {
			t.Errorf("%s: properties %+v", c.ident, pd.EntityProperties)
		}
	}
	// A sheep carries no climate property.
	st := &entState{ident: "minecraft:sheep"}
	st.applyMeta(parseSimpleMeta(append(entry(17, metaTypeVarInt, 1), 0xff)))
	if pd := actorData(8, st); len(pd.EntityProperties.IntegerProperties) != 0 {
		t.Errorf("sheep carries properties %+v", pd.EntityProperties)
	}
	pk := climateProperty("minecraft:pig")
	buf := new(bytes.Buffer)
	w := protocol.NewWriter(buf, 0)
	pk.Marshal(w)
	if buf.Len() == 0 {
		t.Fatal("empty SyncActorProperty")
	}
	if props := pk.PropertyData["properties"].([]map[string]any); props[0]["name"] != "minecraft:climate_variant" || props[0]["type"] != int32(3) {
		t.Errorf("property definition %+v", props[0])
	}
}
