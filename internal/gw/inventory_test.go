package gw

import (
	"testing"

	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
	attach "github.com/tachyne/tachyne-common/attach"
)

// capture collects what would go to a client, so the slot mapping can be
// checked without a Bedrock connection.
type capture struct{ pkts []packet.Packet }

func (c *capture) WritePacket(p packet.Packet) error {
	c.pkts = append(c.pkts, p)
	return nil
}

func (c *capture) content(t *testing.T, window uint32) *packet.InventoryContent {
	t.Helper()
	for _, p := range c.pkts {
		if ic, ok := p.(*packet.InventoryContent); ok && ic.WindowID == window {
			return ic
		}
	}
	t.Fatalf("no InventoryContent for window %d", window)
	return nil
}

// The mapping that is easy to get wrong: Java puts its main inventory BEFORE
// the hotbar, Bedrock puts the hotbar first. Getting this backwards silently
// swaps a player's toolbar with their backpack.
func TestJavaSlotsMapOntoBedrockContainers(t *testing.T) {
	cases := []struct {
		name      string
		java      int32
		container byte
		index     uint32
		ok        bool
	}{
		{"first hotbar slot", 36, protocol.ContainerInventory, 0, true},
		{"last hotbar slot", 44, protocol.ContainerInventory, 8, true},
		{"first main slot", 9, protocol.ContainerInventory, 9, true},
		{"last main slot", 35, protocol.ContainerInventory, 35, true},
		{"helmet", 5, protocol.ContainerArmor, 0, true},
		{"boots", 8, protocol.ContainerArmor, 3, true},
		{"offhand", 45, protocol.ContainerOffhand, 0, true},
		{"crafting result", 0, protocol.ContainerCraftingOutputPreview, craftOutputSlot, true},
		{"crafting grid", 3, protocol.ContainerCraftingInput, playerGridFirst + 2, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			id, idx, ok := javaToBedrockSlot(c.java)
			if ok != c.ok {
				t.Fatalf("ok = %v, want %v", ok, c.ok)
			}
			if !ok {
				return
			}
			if id != c.container || idx != c.index {
				t.Errorf("java %d -> container %d index %d, want %d/%d",
					c.java, id, idx, c.container, c.index)
			}
		})
	}
}

// Every storage slot must land somewhere, exactly once — a collision would
// have two Java slots overwriting each other in one Bedrock cell.
func TestEveryStorageSlotMapsSomewhereUnique(t *testing.T) {
	seen := map[[2]uint32]int32{}
	for slot := int32(0); slot < javaWindowSize; slot++ {
		id, idx, ok := javaToBedrockSlot(slot)
		if !ok {
			continue
		}
		key := [2]uint32{uint32(id), idx}
		if prev, dup := seen[key]; dup {
			t.Errorf("java slots %d and %d both map to container %d index %d",
				prev, slot, id, idx)
		}
		seen[key] = slot
	}
	// 36 storage + 4 armour + 1 offhand + the 2x2 grid and its result.
	if len(seen) != 46 {
		t.Errorf("%d slots mapped, want 46", len(seen))
	}
}

// A whole window renders into the three containers, with the hotbar at the
// front of the inventory one.
func TestSendPlayerInventorySplitsTheWindow(t *testing.T) {
	slots := make([]attach.ItemStack, javaWindowSize)
	dirt := javaItemID(t, "minecraft:dirt")
	slots[36] = attach.ItemStack{ID: dirt, Count: 5} // first hotbar slot
	slots[9] = attach.ItemStack{ID: dirt, Count: 11} // first main slot
	slots[5] = attach.ItemStack{ID: dirt, Count: 1}  // helmet slot
	slots[45] = attach.ItemStack{ID: dirt, Count: 2} // offhand

	c := &capture{}
	sendPlayerInventory(c, slots)

	inv := c.content(t, uint32(protocol.WindowIDInventory))
	if len(inv.Content) != bedrockInvSize {
		t.Fatalf("inventory has %d slots, want %d", len(inv.Content), bedrockInvSize)
	}
	if got := inv.Content[0].Stack.Count; got != 5 {
		t.Errorf("bedrock slot 0 count %d, want the hotbar's 5", got)
	}
	if got := inv.Content[9].Stack.Count; got != 11 {
		t.Errorf("bedrock slot 9 count %d, want the main inventory's 11", got)
	}
	armor := c.content(t, uint32(protocol.WindowIDArmour))
	if len(armor.Content) != bedrockArmorSize || armor.Content[0].Stack.Count != 1 {
		t.Errorf("armour content %+v, want the helmet in slot 0", armor.Content)
	}
	off := c.content(t, uint32(protocol.WindowIDOffHand))
	if len(off.Content) != 1 || off.Content[0].Stack.Count != 2 {
		t.Errorf("offhand content %+v, want the 2", off.Content)
	}
}

// javaItemID finds a canonical Java item id whose Bedrock counterpart is the
// given identifier, so tests do not hard-code an id that a regeneration moves.
func javaItemID(t *testing.T, bedrockName string) int32 {
	t.Helper()
	for i, ref := range javaItemBedrock {
		if ref.Name == bedrockName {
			return int32(i)
		}
	}
	t.Fatalf("no java item maps to %s", bedrockName)
	return 0
}

// An item with no Bedrock counterpart renders as an empty slot rather than as
// the wrong item — a player would act on a wrong item, but a gap is honest.
func TestUnmappableItemsRenderEmpty(t *testing.T) {
	for _, st := range []attach.ItemStack{
		{ID: 0, Count: 1},                               // air
		{ID: 999999, Count: 1},                          // beyond the table
		{ID: javaItemID(t, "minecraft:dirt"), Count: 0}, // empty stack
	} {
		if got := bedrockStack(st); got.Stack.Count != 0 || got.Stack.NetworkID != 0 {
			t.Errorf("%+v rendered as %+v, want an empty slot", st, got.Stack)
		}
	}
}

// A rendered item carries the Bedrock runtime id from the registry we send at
// StartGame, not the Java id.
func TestRenderedItemUsesTheBedrockRuntimeID(t *testing.T) {
	java := javaItemID(t, "minecraft:dirt")
	got := bedrockStack(attach.ItemStack{ID: java, Count: 3})
	want := bedrockRuntimeID["minecraft:dirt"]
	if got.Stack.NetworkID != want {
		t.Errorf("network id %d, want the Bedrock runtime id %d (java id was %d)",
			got.Stack.NetworkID, want, java)
	}
	if got.Stack.Count != 3 {
		t.Errorf("count %d, want 3", got.Stack.Count)
	}
}

// ServerAuthoritativeInventory is on, so every stack needs its OWN network id;
// reusing one makes the client treat two stacks as the same.
func TestStackNetworkIDsAreUnique(t *testing.T) {
	java := javaItemID(t, "minecraft:dirt")
	a := bedrockStack(attach.ItemStack{ID: java, Count: 1})
	b := bedrockStack(attach.ItemStack{ID: java, Count: 1})
	if a.StackNetworkID == b.StackNetworkID {
		t.Errorf("two stacks share network id %d", a.StackNetworkID)
	}
	if a.StackNetworkID == 0 || b.StackNetworkID == 0 {
		t.Error("a non-empty stack got network id 0")
	}
}

// A single changed slot goes to the container that holds it, not always the
// main inventory.
func TestSendInventorySlotPicksTheRightWindow(t *testing.T) {
	java := javaItemID(t, "minecraft:dirt")
	cases := []struct {
		slot int32
		win  uint32
		idx  uint32
	}{
		{36, uint32(protocol.WindowIDInventory), 0},
		{5, uint32(protocol.WindowIDArmour), 0},
		{45, uint32(protocol.WindowIDOffHand), 0},
	}
	for _, c := range cases {
		cap := &capture{}
		sendInventorySlot(cap, c.slot, attach.ItemStack{ID: java, Count: 1})
		if len(cap.pkts) != 1 {
			t.Fatalf("slot %d produced %d packets, want 1", c.slot, len(cap.pkts))
		}
		is, ok := cap.pkts[0].(*packet.InventorySlot)
		if !ok {
			t.Fatalf("slot %d produced %T, want InventorySlot", c.slot, cap.pkts[0])
		}
		if is.WindowID != c.win || is.Slot != c.idx {
			t.Errorf("slot %d -> window %d index %d, want %d/%d",
				c.slot, is.WindowID, is.Slot, c.win, c.idx)
		}
	}
}

// The crafting grid lives in Bedrock's UI window (28-31, result 50), never
// in the main inventory's slot 0.
func TestCraftingSlotsGoToTheUIWindow(t *testing.T) {
	java := javaItemID(t, "minecraft:dirt")
	for slot, idx := range map[int32]uint32{0: craftOutputSlot, 1: playerGridFirst, 4: playerGridFirst + 3} {
		cap := &capture{}
		sendInventorySlot(cap, slot, attach.ItemStack{ID: java, Count: 1})
		if len(cap.pkts) != 1 {
			t.Fatalf("java slot %d sent %d packets", slot, len(cap.pkts))
		}
		if is, ok := cap.pkts[0].(*packet.InventorySlot); !ok || is.WindowID != protocol.WindowIDUI || is.Slot != idx {
			t.Errorf("java slot %d -> %+v, want UI window slot %d", slot, cap.pkts[0], idx)
		}
	}
}
