package gw

import (
	"sync"

	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
	attach "github.com/tachyne/tachyne-common/attach"
)

// Container windows. The world opens a Java menu by canonical id; the
// chest-shaped ones (the generic rows, the 3×3 bin, the hopper, the shulker
// box) have a Bedrock container of the same shape, opened at the block the
// player last used, with the container's slots as ContainerLevelEntity and
// the player's own below. Menus with no Bedrock twin are closed straight
// back so the world does not wait on them.

// menuWindow describes a canonical menu Bedrock can show.
type menuWindow struct {
	size  int
	ctype byte
}

var menuWindows = map[int32]menuWindow{
	0:  {9, protocol.ContainerTypeContainer},  // generic_9x1
	1:  {18, protocol.ContainerTypeContainer}, // generic_9x2
	2:  {27, protocol.ContainerTypeContainer}, // generic_9x3
	3:  {36, protocol.ContainerTypeContainer}, // generic_9x4
	4:  {45, protocol.ContainerTypeContainer}, // generic_9x5
	5:  {54, protocol.ContainerTypeContainer}, // generic_9x6
	6:  {9, protocol.ContainerTypeDispenser},  // generic_3x3 (dispenser/dropper)
	16: {5, protocol.ContainerTypeHopper},     // hopper
	20: {27, protocol.ContainerTypeContainer}, // shulker_box
}

// winState is the container window open for this client, shared between
// the world-side and client-side goroutines.
type winState struct {
	mu     sync.Mutex
	mirror *invMirror // nil = no container open
	ctype  byte
	// lastUse is the block the client last used — where the window sits.
	lastUse [3]int32
}

func (w *winState) open(id int32, size int, ctype byte) *invMirror {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.mirror = newWindowMirror(id, size)
	w.ctype = ctype
	return w.mirror
}

// current returns the open window's mirror (nil when none).
func (w *winState) current() *invMirror {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.mirror
}

func (w *winState) close() (id int32, ctype byte, was bool) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.mirror == nil {
		return 0, 0, false
	}
	id, ctype = w.mirror.window, w.ctype
	w.mirror = nil
	return id, ctype, true
}

func (w *winState) used(x, y, z int32) {
	w.mu.Lock()
	w.lastUse = [3]int32{x, y, z}
	w.mu.Unlock()
}

func (w *winState) usedAt() [3]int32 {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.lastUse
}

// sendWindowItems renders a container window: its own slots, then the
// player's inventory as the window's lower half shows it.
func sendWindowItems(w packetWriter, m *invMirror, slots []attach.ItemStack) {
	size := len(m.slots) - 36 - 1
	content := make([]protocol.ItemInstance, size)
	for i := 0; i < size && i < len(slots); i++ {
		content[i] = bedrockStack(slots[i])
	}
	w.WritePacket(&packet.InventoryContent{
		WindowID:  uint32(m.window),
		Content:   content,
		Container: fullContainer(protocol.ContainerLevelEntity),
	})
	sendPlayerInventory(w, m.playerView())
}

// sendWindowSlot renders one changed slot of a container window.
func sendWindowSlot(w packetWriter, m *invMirror, slot int32, st attach.ItemStack) {
	size := len(m.slots) - 36 - 1
	if int(slot) < size {
		w.WritePacket(&packet.InventorySlot{
			WindowID:  uint32(m.window),
			Slot:      uint32(slot),
			NewItem:   bedrockStack(st),
			Container: protocol.Option(fullContainer(protocol.ContainerLevelEntity)),
		})
		return
	}
	if _, idx, ok := m.mapOut(slot); ok {
		// Bedrock's inventory index: hotbar 0-8 (Java 36-44), main 9-35 (Java 9-35).
		if idx < 9 {
			sendInventorySlot(w, javaHotbarFirst+int32(idx), st)
		} else {
			sendInventorySlot(w, int32(idx), st)
		}
	}
}
