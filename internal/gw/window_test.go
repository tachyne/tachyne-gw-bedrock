package gw

import (
	"testing"

	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
	attach "github.com/tachyne/tachyne-common/attach"
)

// A container window's slot map: the container's slots first, then the
// player's main inventory and hotbar, the cursor last — both ways.
func TestWindowMirrorSlotMap(t *testing.T) {
	m := newWindowMirror(3, chestLayout(27))
	if len(m.slots) != 27+36+1 || m.cursor != 63 {
		t.Fatalf("layout %d/%d", len(m.slots), m.cursor)
	}
	cases := []struct {
		container, slot byte
		java            int32
	}{
		{protocol.ContainerLevelEntity, 5, 5},
		{protocol.ContainerInventory, 0, 27 + 27}, // hotbar 0 → after the main inventory
		{protocol.ContainerInventory, 9, 27},      // main inventory's first
		{protocol.ContainerHotBar, 8, 27 + 27 + 8},
		{protocol.ContainerCursor, 0, 63},
	}
	for _, c := range cases {
		got, ok := m.mapIn(c.container, c.slot)
		if !ok || got != c.java {
			t.Errorf("mapIn(%d,%d) = %d %v, want %d", c.container, c.slot, got, ok, c.java)
		}
		cid, idx, ok := m.mapOut(c.java)
		if !ok {
			t.Errorf("mapOut(%d) failed", c.java)
		}
		back, ok2 := m.mapIn(cid, byte(idx))
		if !ok2 || back != c.java {
			t.Errorf("round trip %d → (%d,%d) → %d", c.java, cid, idx, back)
		}
	}
	if _, ok := m.mapIn(protocol.ContainerLevelEntity, 27); ok {
		t.Error("a slot past the container's size is refused")
	}
	// Clicks name the window and the mirror's player view lines up.
	m.set(27, attach.ItemStack{ID: 1, Count: 2})
	if v := m.playerView(); v[javaMainFirst].ID != 1 {
		t.Errorf("player view main[0] = %+v", v[javaMainFirst])
	}
	if e := m.clickFor([]int32{5}); e.ID != 3 {
		t.Errorf("click window id %d", e.ID)
	}
	if mw, ok := menuWindows[2]; !ok || len(mw.layout) != 27 || mw.ctype != protocol.ContainerTypeContainer {
		t.Error("generic_9x3 is a 27-slot container")
	}
}

// The furnace and brewing stand keep their slots under named containers;
// the brewing stand's order differs between the editions.
func TestFixedSlotWindows(t *testing.T) {
	f := newWindowMirror(4, furnaceLayout)
	if j, ok := f.mapIn(protocol.ContainerFurnaceResult, 2); !ok || j != 2 {
		t.Errorf("furnace result → %d %v", j, ok)
	}
	if j, ok := f.mapIn(protocol.ContainerHotBar, 0); !ok || j != 3+27 {
		t.Errorf("furnace hotbar 0 → %d %v", j, ok)
	}
	b := newWindowMirror(5, brewingLayout)
	for j, ws := range brewingLayout {
		c, idx, ok := b.mapOut(int32(j))
		if !ok || c != ws.container || idx != ws.idx {
			t.Errorf("brewing %d → (%d,%d) %v, want %+v", j, c, idx, ok, ws)
		}
		if back, ok := b.mapIn(ws.container, byte(ws.idx)); !ok || back != int32(j) {
			t.Errorf("brewing (%d,%d) → %d %v", ws.container, ws.idx, back, ok)
		}
	}
	if _, ok := b.mapIn(protocol.ContainerBrewingStandResult, 0); ok {
		t.Error("bottle index 0 is not a Bedrock bottle slot")
	}
	// Window items land at Bedrock's indices, not Java's: the ingredient
	// (Java 3) at 0, the first bottle (Java 0) at 1.
	var w capture
	sendWindowItems(&w, b, []attach.ItemStack{{ID: 1, Count: 1}, {ID: 2, Count: 1}, {ID: 3, Count: 1}, {ID: 4, Count: 1}, {ID: 5, Count: 1}})
	var content *packet.InventoryContent
	for _, pk := range w.pkts {
		if ic, ok := pk.(*packet.InventoryContent); ok && ic.WindowID == 5 {
			content = ic
		}
	}
	if content == nil || len(content.Content) != 5 {
		t.Fatalf("no 5-slot window content: %+v", content)
	}
	if content.Content[0].Stack.NetworkID != bedrockStack(attach.ItemStack{ID: 4, Count: 1}).Stack.NetworkID ||
		content.Content[1].Stack.NetworkID != bedrockStack(attach.ItemStack{ID: 1, Count: 1}).Stack.NetworkID {
		t.Errorf("brewing content order: %v", content.Content)
	}
	// Properties: furnace cook progress is the tick count; total time has no key.
	w.pkts = nil
	windowData(&w, f, protocol.ContainerTypeFurnace, 2, 77)
	windowData(&w, f, protocol.ContainerTypeFurnace, 3, 200)
	if len(w.pkts) != 1 {
		t.Fatalf("%d data packets", len(w.pkts))
	}
	if d := w.pkts[0].(*packet.ContainerSetData); d.Key != packet.ContainerDataFurnaceTickCount || d.Value != 77 || d.WindowID != 4 {
		t.Errorf("data %+v", d)
	}
}

// The crafter window keeps its grid in the window container, the player's
// inventory next, and the result preview as a tail slot (Java 45) at the
// crafter container's slot 50; its container properties become the block
// entity's disabled-slot mask; a Bedrock toggle becomes the world's
// SlotState.
func TestCrafterWindow(t *testing.T) {
	mw, ok := menuWindows[7]
	if !ok || mw.ctype != protocol.ContainerTypeCrafter || len(mw.layout) != 9 {
		t.Fatalf("crafter menu %+v %v", mw, ok)
	}
	m := newWindowMirror(6, mw.layout)
	m.setTail(crafterTail)
	if m.cursor != 46 || len(m.slots) != 47 {
		t.Fatalf("cursor %d slots %d", m.cursor, len(m.slots))
	}
	if c, idx, ok := m.mapOut(45); !ok || c != protocol.ContainerCrafterLevelEntity || idx != craftOutputSlot {
		t.Errorf("result maps to (%d,%d,%v)", c, idx, ok)
	}
	if j, ok := m.mapIn(protocol.ContainerHotBar, 0); !ok || j != 36 {
		t.Errorf("hotbar 0 → %d %v", j, ok)
	}
	if j, ok := m.mapIn(protocol.ContainerInventory, 9); !ok || j != 9 {
		t.Errorf("main 0 → %d %v", j, ok)
	}
	if j, ok := m.mapIn(protocol.ContainerLevelEntity, 4); !ok || j != 4 {
		t.Errorf("grid 4 → %d %v", j, ok)
	}
	if j, ok := m.mapIn(protocol.ContainerCursor, 0); !ok || j != 46 {
		t.Errorf("cursor → %d %v", j, ok)
	}
	slots := make([]attach.ItemStack, 46)
	slots[45] = attach.ItemStack{ID: 7, Count: 1}
	m.setAll(slots, attach.ItemStack{ID: 9, Count: 2})
	if m.slots[45].ID != 7 || m.slots[46].ID != 9 {
		t.Errorf("setAll: result %+v cursor %+v", m.slots[45], m.slots[46])
	}
	m.at = [3]int32{1, 2, 3}
	if pk := m.crafterProp(3, 1); pk == nil || pk.NBTData["disabled_slots"] != int16(8) || pk.NBTData["crafting_ticks_remaining"] != int32(0) {
		t.Errorf("prop 3 on: %+v", pk)
	}
	if pk := m.crafterProp(0, 1); pk.NBTData["disabled_slots"] != int16(9) {
		t.Errorf("prop 0 on: %+v", pk.NBTData)
	}
	if pk := m.crafterProp(3, 0); pk.NBTData["disabled_slots"] != int16(1) {
		t.Errorf("prop 3 off: %+v", pk.NBTData)
	}
	if pk := m.crafterProp(9, 1); pk.NBTData["crafting_ticks_remaining"] != int32(10000) || pk.Position != (protocol.BlockPos{1, 2, 3}) {
		t.Errorf("triggered: %+v", pk)
	}
	if pk := m.crafterProp(12, 1); pk != nil {
		t.Error("an unknown property sends nothing")
	}
	if s := crafterToggle(&packet.PlayerToggleCrafterSlotRequest{Slot: 4, Disabled: true}); s.Slot != 4 || s.State {
		t.Errorf("toggle → %+v", s)
	}
	if s := crafterToggle(&packet.PlayerToggleCrafterSlotRequest{Slot: 2, Disabled: false}); !s.State {
		t.Errorf("toggle on → %+v", s)
	}
}
