package gw

import (
	"sync/atomic"

	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
	attach "github.com/tachyne/tachyne-common/attach"
)

// Bedrock inventory rendering.
//
// The engine speaks Java's inventory: one flat window whose slots are numbered
// in Java's order. Bedrock splits the same thing into several containers, each
// with its own numbering, so this file is the translation — the same job the
// Java gateways do in render770, for an edition that lays its slots out
// differently.
//
// Java window 0 (the player's own inventory) is numbered:
//
//	0        crafting result
//	1-4      crafting grid
//	5-8      armour: head, chest, legs, feet
//	9-35     main inventory
//	36-44    hotbar
//	45       offhand
//
// Bedrock instead has ContainerInventory holding 36 slots with the HOTBAR
// FIRST (0-8) and the main inventory after it (9-35), plus separate
// ContainerArmor and ContainerOffhand. So the two halves of Java's storage
// swap places on the way out, which is the only really error-prone part.
const (
	javaArmorFirst  = 5
	javaMainFirst   = 9
	javaHotbarFirst = 36
	javaOffhand     = 45
	javaWindowSize  = 46

	bedrockInvSize   = 36
	bedrockArmorSize = 4
)

// bedrockRuntimeID resolves a Bedrock item identifier to the runtime id the
// client knows it by. Built from the registry we already send at StartGame, so
// the two can never disagree.
var bedrockRuntimeID = func() map[string]int32 {
	m := make(map[string]int32, len(bedrockItemEntries))
	for _, e := range bedrockItemEntries {
		m[e.Name] = int32(e.RuntimeID)
	}
	return m
}()

// stackIDs mints the unique stack network ids Bedrock wants whenever
// ServerAuthoritativeInventory is on (which it is — see the StartGame in
// session.go). With that flag set, sending 1 for everything makes the client
// treat every stack as the same one.
var stackIDs atomic.Int32

// bedrockItemIDs is a canonical item's Bedrock network id and metadata,
// false when it has no Bedrock counterpart.
func bedrockItemIDs(id int32) (rid, data int32, ok bool) {
	if id <= 0 || int(id) >= len(javaItemBedrock) {
		return 0, 0, false
	}
	ref := javaItemBedrock[id]
	if ref.Name == "" {
		return 0, 0, false
	}
	rid, ok = bedrockRuntimeID[ref.Name]
	return rid, int32(ref.Data), ok
}

// bedrockStack renders a domain item stack for the Bedrock client. An unknown
// or unmapped item comes out empty rather than wrong: showing the WRONG item is
// worse than showing a gap, because a player would act on it.
func bedrockStack(st attach.ItemStack) protocol.ItemInstance {
	if st.Count <= 0 {
		return protocol.ItemInstance{}
	}
	rid, data, ok := bedrockItemIDs(st.ID)
	if !ok {
		return protocol.ItemInstance{}
	}
	return protocol.ItemInstance{
		StackNetworkID: stackIDs.Add(1),
		Stack: protocol.ItemStack{
			ItemType: protocol.ItemType{
				NetworkID:     rid,
				MetadataValue: uint32(data),
			},
			Count: uint16(st.Count),
		},
	}
}

// container names the three parts of the player's own inventory.
func fullContainer(id byte) protocol.FullContainerName {
	return protocol.FullContainerName{ContainerID: id}
}

// javaToBedrockSlot maps a slot of Java's player window onto the Bedrock
// container and index that hold it. ok is false for slots Bedrock keeps
// elsewhere — the crafting grid and its result, which are part of a separate
// UI container rather than the inventory.
func javaToBedrockSlot(slot int32) (containerID byte, index uint32, ok bool) {
	switch {
	case slot >= javaHotbarFirst && slot < javaOffhand:
		return protocol.ContainerInventory, uint32(slot - javaHotbarFirst), true
	case slot >= javaMainFirst && slot < javaHotbarFirst:
		return protocol.ContainerInventory, uint32(slot - javaMainFirst + 9), true
	case slot >= javaArmorFirst && slot < javaMainFirst:
		return protocol.ContainerArmor, uint32(slot - javaArmorFirst), true
	case slot == javaOffhand:
		return protocol.ContainerOffhand, 0, true
	case slot == 0: // the 2x2 grid's result: Bedrock's UI window, slot 50
		return protocol.ContainerCraftingOutputPreview, craftOutputSlot, true
	case slot >= 1 && slot <= 4: // the 2x2 grid: UI window slots 28-31
		return protocol.ContainerCraftingInput, playerGridFirst + uint32(slot-1), true
	}
	return 0, 0, false
}

// Bedrock keeps crafting in its UI window (id 124): the player's 2x2 grid
// at 28-31, a crafting table's 3x3 at 32-40, the result at 50.
const (
	playerGridFirst = 28
	tableGridFirst  = 32
	craftOutputSlot = 50
)

// uiContainer reports whether a container lives in the UI window rather
// than the inventory or a block's own window.
func uiContainer(id byte) bool {
	switch id {
	case protocol.ContainerCraftingInput, protocol.ContainerCraftingOutputPreview, protocol.ContainerCreatedOutput,
		protocol.ContainerAnvilInput, protocol.ContainerAnvilMaterial, protocol.ContainerAnvilResultPreview,
		protocol.ContainerGrindstoneInput, protocol.ContainerGrindstoneAdditional, protocol.ContainerGrindstoneResultPreview,
		protocol.ContainerTradeTwoIngredientOne, protocol.ContainerTradeTwoIngredientTwo, protocol.ContainerTradeTwoResultPreview,
		protocol.ContainerEnchantingInput, protocol.ContainerEnchantingMaterial,
		protocol.ContainerStonecutterInput, protocol.ContainerStonecutterResultPreview:
		return true
	}
	return false
}

// resultContainer reports whether a container is a menu's result preview
// (what a craft takes through the created-output name).
func resultContainer(id byte) bool {
	switch id {
	case protocol.ContainerCraftingOutputPreview, protocol.ContainerAnvilResultPreview, protocol.ContainerGrindstoneResultPreview,
		protocol.ContainerTradeTwoResultPreview, protocol.ContainerStonecutterResultPreview:
		return true
	}
	return false
}

// sendPlayerInventory renders a whole Java player window as the three Bedrock
// containers it corresponds to.
func sendPlayerInventory(w packetWriter, slots []attach.ItemStack) {
	inv := make([]protocol.ItemInstance, bedrockInvSize)
	armor := make([]protocol.ItemInstance, bedrockArmorSize)
	var offhand protocol.ItemInstance

	for i, st := range slots {
		id, idx, ok := javaToBedrockSlot(int32(i))
		if !ok {
			continue
		}
		item := bedrockStack(st)
		switch id {
		case protocol.ContainerInventory:
			if int(idx) < len(inv) {
				inv[idx] = item
			}
		case protocol.ContainerArmor:
			if int(idx) < len(armor) {
				armor[idx] = item
			}
		case protocol.ContainerOffhand:
			offhand = item
		}
	}

	w.WritePacket(&packet.InventoryContent{
		WindowID:  protocol.WindowIDInventory,
		Content:   inv,
		Container: fullContainer(protocol.ContainerInventory),
	})
	w.WritePacket(&packet.InventoryContent{
		WindowID:  protocol.WindowIDArmour,
		Content:   armor,
		Container: fullContainer(protocol.ContainerArmor),
	})
	w.WritePacket(&packet.InventoryContent{
		WindowID:  protocol.WindowIDOffHand,
		Content:   []protocol.ItemInstance{offhand},
		Container: fullContainer(protocol.ContainerOffhand),
	})
	for slot := 0; slot <= 4 && slot < len(slots); slot++ { // the 2x2 grid and its result: UI slots, one by one
		sendInventorySlot(w, int32(slot), slots[slot])
	}
}

// sendInventorySlot renders one changed slot of the player's window.
func sendInventorySlot(w packetWriter, slot int32, st attach.ItemStack) {
	id, idx, ok := javaToBedrockSlot(slot)
	if !ok {
		return
	}
	win := protocol.WindowIDInventory
	container := protocol.Optional[protocol.FullContainerName]{}
	switch id {
	case protocol.ContainerArmor:
		win = protocol.WindowIDArmour
	case protocol.ContainerOffhand:
		win = protocol.WindowIDOffHand
	case protocol.ContainerCraftingInput, protocol.ContainerCraftingOutputPreview:
		win = protocol.WindowIDUI
		container = protocol.Option(fullContainer(id))
	}
	w.WritePacket(&packet.InventorySlot{
		WindowID:  uint32(win),
		Slot:      idx,
		Container: container,
		NewItem:   bedrockStack(st),
	})
}

// packetWriter is the little the inventory code needs of a client connection,
// so the mapping can be tested without one.
type packetWriter interface {
	WritePacket(packet.Packet) error
}
