package gw

import (
	"testing"

	"github.com/sandertv/gophertunnel/minecraft/protocol"
	attach "github.com/tachyne/tachyne-common/attach"
)

// A container window's slot map: the container's slots first, then the
// player's main inventory and hotbar, the cursor last — both ways.
func TestWindowMirrorSlotMap(t *testing.T) {
	m := newWindowMirror(3, 27)
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
	if mw, ok := menuWindows[2]; !ok || mw.size != 27 || mw.ctype != protocol.ContainerTypeContainer {
		t.Error("generic_9x3 is a 27-slot container")
	}
}
