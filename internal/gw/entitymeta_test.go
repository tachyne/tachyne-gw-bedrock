package gw

import (
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
	// A sheep's index 17 is not a pet's: no sitting flag from a fleece byte.
	st := &entState{ident: "minecraft:sheep"}
	st.applyMeta(parseSimpleMeta(append(entry(17, 0, 0x01), 0xff)))
	if st.look.sitting || st.look.color != 1 {
		t.Errorf("sheep read as a pet: %+v", st.look)
	}
}
