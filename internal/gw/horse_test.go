package gw

import (
	"testing"

	"github.com/sandertv/gophertunnel/minecraft/nbt"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
	attach "github.com/tachyne/tachyne-common/attach"
)

// A horse's saddle and armour, a donkey's saddle and chest, a llama's
// carpet and chest each land where Bedrock's mount screen keeps them.
func TestHorseScreen(t *testing.T) {
	h := newWindowMirror(3, horseLayout(0, false))
	if j, ok := h.mapIn(protocol.ContainerHorseEquip, 1); !ok || j != 1 {
		t.Errorf("horse armour → %d %v", j, ok)
	}
	d := newWindowMirror(4, horseLayout(5, false))
	if len(d.layout) != 2+15 {
		t.Fatalf("donkey layout %d", len(d.layout))
	}
	if j, ok := d.mapIn(protocol.ContainerLevelEntity, 1); !ok || j != 2 {
		t.Errorf("donkey chest 0 → %d %v", j, ok)
	}
	if _, ok := d.mapIn(protocol.ContainerHorseEquip, horseUnmapped); ok {
		t.Error("a donkey's armour slot mapped")
	}
	l := newWindowMirror(5, horseLayout(2, true))
	if j, ok := l.mapIn(protocol.ContainerHorseEquip, 0); !ok || j != 1 {
		t.Errorf("llama carpet → %d %v", j, ok)
	}
	var c capture
	slots := make([]attach.ItemStack, 2+6+36)
	slots[1] = attach.ItemStack{ID: mappedItem(t, 0), Count: 1} // the carpet
	slots[2] = attach.ItemStack{ID: mappedItem(t, 1), Count: 3} // chest slot 0
	sendWindowItems(&c, l, slots)
	ic, ok := c.pkts[0].(*packet.InventoryContent)
	if !ok || ic.WindowID != 5 || len(ic.Content) != 7 || ic.Content[0].Stack.Count != 1 || ic.Content[1].Stack.Count != 3 {
		t.Errorf("llama content %+v", c.pkts[0])
	}
	eq := horseEquipPacket(5, 77, 2, true)
	var tag map[string]any
	if err := nbt.UnmarshalEncoding(eq.SerialisedInventoryData, &tag, nbt.NetworkLittleEndian); err != nil || eq.EntityUniqueID != 77 || eq.WindowType != byte(protocol.ContainerTypeHorse) {
		t.Fatalf("equip %+v %v", eq, err)
	}
	if sl, _ := tag["slots"].([]any); len(sl) != 1 || len(sl[0].(map[string]any)["acceptedItems"].([]any)) != 16 {
		t.Errorf("llama slots %v", tag)
	}
	if sl, _ := func() (map[string]any, error) {
		var m map[string]any
		return m, nbt.UnmarshalEncoding(horseEquipPacket(1, 1, 0, false).SerialisedInventoryData, &m, nbt.NetworkLittleEndian)
	}(); len(sl["slots"].([]any)) != 2 {
		t.Errorf("horse slots %v", sl)
	}
}
