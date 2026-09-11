package gw

import (
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
	"github.com/tachyne/tachyne-common/attach"
)

// The crafter's menu (Java crafter_3x3, menu 7). Bedrock has a crafter
// screen of its own (ContainerTypeCrafter): the 3×3 grid lives in the
// window's container as LEVEL_ENTITY slots 0-8, the read-only result
// preview is CRAFTER_BLOCK_CONTAINER slot 50 of the same window, and the
// player's inventory is the usual lower half. Java lays the menu out as
// grid 0-8, inventory 9-44, result 45 — the result AFTER the inventory,
// which the mirror models as a tail slot. The disabled-slot overlay and the
// triggered state are Java container properties (0-8 one per slot, 9 the
// trigger) that Bedrock reads from the block entity instead, so each
// property update re-sends the crafter's block-actor tag, the way Geyser's
// CrafterInventoryTranslator does. A slot toggle on Bedrock arrives as
// PlayerToggleCrafterSlotRequest and goes to the world as SlotState.

// crafterLayout is the grid; the result rides the tail (setTail).
var crafterLayout = chestLayout(9)

// crafterTail is the result preview's place: window container, slot 50.
var crafterTail = []winSlot{{protocol.ContainerCrafterLevelEntity, craftOutputSlot}}

// setTail appends container slots that Java numbers AFTER the player's
// inventory (the crafter's result at 45); the cursor moves past them.
func (m *invMirror) setTail(tail []winSlot) {
	m.mu.Lock()
	defer m.mu.Unlock()
	size := len(m.layout)
	base := int32(size + 36)
	m.tail = tail
	m.cursor = base + int32(len(tail))
	slots := make([]attach.ItemStack, m.cursor+1)
	copy(slots, m.slots)
	m.slots = slots
	inner := m.mapOut
	m.mapOut = func(slot int32) (byte, uint32, bool) {
		if slot >= base && slot < base+int32(len(tail)) {
			ws := tail[slot-base]
			return ws.container, ws.idx, true
		}
		return inner(slot)
	}
}

// tailSlot reports the tail entry a Java slot maps to, if any.
func (m *invMirror) tailSlot(slot int32) (winSlot, bool) {
	base := int32(len(m.layout) + 36)
	if slot >= base && slot < base+int32(len(m.tail)) {
		return m.tail[slot-base], true
	}
	return winSlot{}, false
}

// crafterProp records one Java container property (0-8: 1 = that grid
// slot is disabled; 9: triggered) and returns the block-actor tag Bedrock
// draws the overlay from.
func (m *invMirror) crafterProp(prop, value int32) *packet.BlockActorData {
	m.mu.Lock()
	defer m.mu.Unlock()
	switch {
	case prop >= 0 && prop <= 8:
		if value != 0 {
			m.crafterMask |= 1 << uint(prop)
		} else {
			m.crafterMask &^= 1 << uint(prop)
		}
	case prop == 9:
		m.crafterTriggered = value != 0
	default:
		return nil
	}
	return crafterData(m.at, m.crafterMask, m.crafterTriggered)
}

// crafterData is the crafter's block-actor tag: the disabled-slot mask and
// a remaining-ticks count that is non-zero while triggered (the exact
// count is unknown to the gateway; the world resets it when it fires).
func crafterData(at [3]int32, mask int16, triggered bool) *packet.BlockActorData {
	ticks := int32(0)
	if triggered {
		ticks = 10000
	}
	return &packet.BlockActorData{
		Position: protocol.BlockPos{at[0], at[1], at[2]},
		NBTData: map[string]any{
			"id": "Crafter", "x": at[0], "y": at[1], "z": at[2], "isMovable": byte(1),
			"disabled_slots": mask, "crafting_ticks_remaining": ticks,
		},
	}
}

// crafterToggle turns Bedrock's slot toggle into the world's SlotState
// (Bedrock says disabled, Java's newState says enabled).
func crafterToggle(p *packet.PlayerToggleCrafterSlotRequest) attach.SlotState {
	return attach.SlotState{Slot: int32(p.Slot), State: !p.Disabled}
}
