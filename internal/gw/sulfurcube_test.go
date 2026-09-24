package gw

import (
	"testing"

	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
	"github.com/tachyne/tachyne-common/attach"
	tproto "github.com/tachyne/tachyne-common/protocol"
)

// A sulfur cube has a Bedrock actor of its own.
func TestSulfurCubeActor(t *testing.T) {
	if got := bedrockEntityIDs[tproto.CanonicalEntity("sulfur_cube")]; got != bedrockSulfurCube {
		t.Errorf("sulfur cube renders as %q, want %q", got, bedrockSulfurCube)
	}
}

// The swallowed block picks the archetype look from the 26.3 tags; an
// empty cube is "none".
func TestSulfurArchetypeIndex(t *testing.T) {
	for _, c := range []struct {
		item string
		want string
	}{
		{"tnt", "explosive"}, {"magma_block", "hot"}, {"dirt", "regular"},
		{"oak_planks", "bouncy"}, {"white_wool", "light"}, {"honeycomb_block", "sticky"},
		{"blue_ice", "fast_sliding"}, {"soul_sand", "high_resistance"},
	} {
		i := sulfurArchetypeIndex(tproto.CanonicalItem(c.item))
		if sulfurArchetypeEnum[i] != c.want {
			t.Errorf("%s: archetype %q, want %q", c.item, sulfurArchetypeEnum[i], c.want)
		}
	}
	if sulfurArchetypeEnum[sulfurArchetypeIndex(0)] != "none" {
		t.Error("an empty cube is not \"none\"")
	}
	if len(sulfurArchetypeByItem) < 300 {
		t.Errorf("only %d swallowable items mapped", len(sulfurArchetypeByItem))
	}
	pd := sulfurCubeProperty().PropertyData
	props := pd["properties"].([]map[string]any)
	if pd["type"] != bedrockSulfurCube || props[0]["name"] != "minecraft:sulfur_cube_archetype" {
		t.Errorf("property definition %v", pd)
	}
}

// The block goes in the cube's hand, and the look follows it.
func TestSulfurCubeBodyPackets(t *testing.T) {
	st := &entState{ident: bedrockSulfurCube}
	tnt := tproto.CanonicalItem("tnt")
	pks := sulfurBodyPackets(9, st, bedrockStack(attach.ItemStack{ID: tnt, Count: 1}), tnt)
	eq, ok := pks[0].(*packet.MobEquipment)
	if !ok || eq.EntityRuntimeID != rt(9) || eq.NewItem.Stack.Count != 1 {
		t.Fatalf("equipment %+v", pks[0])
	}
	ad, ok := pks[1].(*packet.SetActorData)
	if !ok {
		t.Fatalf("actor data %+v", pks[1])
	}
	props := ad.EntityProperties.IntegerProperties
	if len(props) != 1 || sulfurArchetypeEnum[props[0].Value] != "explosive" {
		t.Errorf("properties %+v, want the explosive look", props)
	}
}

// Its own fields ride at their 26.x indices: size (18) is no scale on
// Bedrock and a baby keeps scale 1; MAX_FUSE (19) is the fuse time once lit.
func TestSulfurCubeMeta(t *testing.T) {
	st := &entState{ident: bedrockSulfurCube}
	meta := entry(16, 8, 1)
	meta = append(meta, entry(18, 1, varint(1)...)...)
	meta = append(meta, entry(19, 1, varint(-1)...)...)
	meta = append(meta, entry(20, 8, 0)...)
	meta = append(meta, 0xff)
	st.applyMeta(parseSimpleMeta(meta))
	m := actorData(3, st).EntityMetadata
	if !st.look.baby || m[protocol.EntityDataKeyScale] != float32(1) {
		t.Errorf("baby %v scale %v, want a baby at scale 1", st.look.baby, m[protocol.EntityDataKeyScale])
	}
	if st.look.hasFuse {
		t.Error("an unlit cube carries a fuse")
	}
	lit := append(entry(19, 1, varint(37)...), 0xff)
	st.applyMeta(parseSimpleMeta(lit))
	if m := actorData(3, st).EntityMetadata; m[protocol.EntityDataKeyFuseTime] != int32(37) {
		t.Errorf("fuse %v, want 37", m[protocol.EntityDataKeyFuseTime])
	}
}
