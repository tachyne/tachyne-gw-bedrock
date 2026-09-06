package gw

import (
	"sync"

	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
	attach "github.com/tachyne/tachyne-common/attach"
)

// The serverbound half: Bedrock's item-stack transactions, turned into the
// window clicks the engine speaks.
//
// The two editions disagree about who decides. Java — and so this engine — is
// DECLARATIVE: the client says what the window looks like afterwards and the
// server reconciles it, spawning drops for anything that went missing. Bedrock
// is TRANSACTIONAL: the client sends an ItemStackRequest of structured actions
// (take 5 from here to there, swap these two) and waits for the server to
// accept or reject each one.
//
// The gateway is where that gap closes, not the engine — the engine must never
// learn that Bedrock exists. Because this gateway already renders the window,
// it can hold its own copy, apply the requested actions to it, and report the
// result twice: to the engine as an ordinary Java click carrying the changed
// slots, and to the client as the ItemStackResponse it is waiting for.
//
// Rejecting is safe and visible: a non-zero response status makes the client
// put everything back. That is the right answer for an action we cannot honour
// yet, and much better than accepting one we would apply wrongly.

const (
	// The window mirror is Java-shaped, so the cursor needs somewhere to live
	// past the real slots rather than a magic number scattered about.
	javaCursorSlot = javaWindowSize

	stackResponseOK    = 0
	stackResponseError = 1
)

// invMirror is what this client's window is believed to hold. It exists only
// so a Bedrock transaction can be resolved into a Java click; the engine
// remains the authority and its next window frame overwrites this wholesale.
type invMirror struct {
	mu     sync.Mutex
	slots  []attach.ItemStack                       // the window's Java slots, then the cursor last
	cursor int32                                    // index of the cursor slot
	window int32                                    // the Java window id the clicks name (0 = the player's own)
	mapIn  func(container, slot byte) (int32, bool) // Bedrock slot → Java slot
	mapOut func(slot int32) (byte, uint32, bool)    // Java slot → Bedrock container + index
	layout []winSlot                                // container windows: where Bedrock keeps each container slot
	result int32                                    // the Java slot a craft's result comes from (-1 = none)
	trades []attach.ItemStack                       // a trade screen: what each offer sells, by index
	ench   [3]enchantRow                            // an enchanting table's rows
	table  bool                                     // …which this window is
	cutter bool                                     // a stonecutter
	smith  bool                                     // a smithing table
	loom   bool                                     // a loom
}

// setTrades records a trade screen's offers (what each sells).
func (m *invMirror) setTrades(results []attach.ItemStack) {
	m.mu.Lock()
	m.trades = results
	m.mu.Unlock()
}

// newInvMirror is the player's own inventory window.
func newInvMirror() *invMirror {
	return &invMirror{slots: make([]attach.ItemStack, javaWindowSize+1), cursor: javaCursorSlot,
		mapIn: bedrockToJavaSlot, mapOut: javaToBedrockSlot, result: 0}
}

// winSlot is where Bedrock keeps one of a window's container slots: the
// named container and the index within the window's content.
type winSlot struct {
	container byte
	idx       uint32
}

// chestLayout is the chest-shaped layout: n slots of the block entity, in
// order.
func chestLayout(n int) []winSlot {
	l := make([]winSlot, n)
	for i := range l {
		l[i] = winSlot{protocol.ContainerLevelEntity, uint32(i)}
	}
	return l
}

// newWindowMirror is a container window whose container slots Bedrock keeps
// as layout says (indexed by Java slot): Java lays a window out as the
// container first, then the player's main inventory (27) and hotbar (9).
func newWindowMirror(id int32, layout []winSlot) *invMirror {
	size := len(layout)
	m := &invMirror{slots: make([]attach.ItemStack, size+36+1), cursor: int32(size + 36), window: id, layout: layout, result: -1}
	for j, ws := range layout {
		if resultContainer(ws.container) {
			m.result = int32(j)
		}
	}
	m.mapIn = func(container, slot byte) (int32, bool) {
		switch container {
		case protocol.ContainerCursor:
			return m.cursor, true
		case protocol.ContainerInventory, protocol.ContainerHotBar, protocol.ContainerCombinedHotBarAndInventory:
			if slot < 9 {
				return int32(size+27) + int32(slot), true // hotbar comes last in Java
			}
			if slot < bedrockInvSize {
				return int32(size) + int32(slot) - 9, true
			}
		default:
			if container == protocol.ContainerCreatedOutput { // a crafted result leaves through this name
				if m.result >= 0 {
					return m.result, true
				}
				return 0, false
			}
			for j, ws := range layout {
				if ws.container == container && ws.idx == uint32(slot) {
					return int32(j), true
				}
			}
		}
		return 0, false
	}
	m.mapOut = func(slot int32) (byte, uint32, bool) {
		switch {
		case slot >= 0 && int(slot) < size:
			return layout[slot].container, layout[slot].idx, true
		case int(slot) >= size && int(slot) < size+27:
			return protocol.ContainerInventory, uint32(int(slot) - size + 9), true
		case int(slot) >= size+27 && int(slot) < size+36:
			return protocol.ContainerInventory, uint32(int(slot) - size - 27), true
		case slot == m.cursor:
			return protocol.ContainerCursor, 0, true
		}
		return 0, 0, false
	}
	return m
}

// set records a slot the engine has told us about.
func (m *invMirror) set(slot int32, st attach.ItemStack) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if slot < 0 || int(slot) >= len(m.slots) {
		return
	}
	m.slots[slot] = st
}

// playerView is the 46-slot Java player window as a container window's
// player part shows it (for the shared player-inventory sender).
func (m *invMirror) playerView() []attach.ItemStack {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.window == 0 {
		return append([]attach.ItemStack(nil), m.slots[:javaWindowSize]...)
	}
	size := len(m.slots) - 36 - 1
	view := make([]attach.ItemStack, javaWindowSize)
	copy(view[javaMainFirst:javaHotbarFirst], m.slots[size:size+27])
	copy(view[javaHotbarFirst:javaHotbarFirst+9], m.slots[size+27:size+36])
	return view
}

// setAll replaces the whole window.
func (m *invMirror) setAll(slots []attach.ItemStack, cursor attach.ItemStack) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.slots {
		m.slots[i] = attach.ItemStack{}
	}
	for i, st := range slots {
		if int32(i) < m.cursor {
			m.slots[i] = st
		}
	}
	m.slots[m.cursor] = cursor
}

// bedrockToJavaSlot is javaToBedrockSlot backwards, plus the cursor, which
// Bedrock keeps in a container of its own.
func bedrockToJavaSlot(container byte, slot byte) (int32, bool) {
	switch container {
	case protocol.ContainerCursor:
		return javaCursorSlot, true
	case protocol.ContainerOffhand:
		return javaOffhand, true
	case protocol.ContainerCraftingInput: // the 2x2 grid
		if slot >= playerGridFirst && slot < playerGridFirst+4 {
			return int32(slot-playerGridFirst) + 1, true
		}
	case protocol.ContainerCraftingOutputPreview, protocol.ContainerCreatedOutput:
		return 0, true
	case protocol.ContainerArmor:
		if slot < bedrockArmorSize {
			return javaArmorFirst + int32(slot), true
		}
	case protocol.ContainerInventory, protocol.ContainerHotBar,
		protocol.ContainerCombinedHotBarAndInventory:
		// Bedrock counts the hotbar first; Java counts it last.
		if slot < 9 {
			return javaHotbarFirst + int32(slot), true
		}
		if slot < bedrockInvSize {
			return javaMainFirst + int32(slot) - 9, true
		}
	}
	return 0, false
}

// transfer moves count items from one slot of the mirror to another, merging
// onto a matching stack and refusing anything it cannot represent.
func (m *invMirror) transfer(from, to int32, count int) bool {
	if from == to || from < 0 || to < 0 ||
		int(from) >= len(m.slots) || int(to) >= len(m.slots) {
		return false
	}
	src, dst := m.slots[from], m.slots[to]
	if src.Count <= 0 || count <= 0 || int32(count) > src.Count {
		return false
	}
	if dst.Count > 0 && dst.ID != src.ID {
		return false // a real client does not ask for this; a confused one might
	}
	src.Count -= int32(count)
	moved := src
	moved.Count = int32(count)
	if dst.Count > 0 {
		dst.Count += int32(count)
	} else {
		dst = moved
	}
	if src.Count <= 0 {
		src = attach.ItemStack{}
	}
	m.slots[from], m.slots[to] = src, dst
	return true
}

// swap exchanges two slots outright.
func (m *invMirror) swap(a, b int32) bool {
	if a < 0 || b < 0 || int(a) >= len(m.slots) || int(b) >= len(m.slots) {
		return false
	}
	m.slots[a], m.slots[b] = m.slots[b], m.slots[a]
	return true
}

// applyRequest walks one Bedrock request over the mirror, returning the Java
// slots it changed. A false return means the whole request is refused: Bedrock
// transactions are all-or-nothing, and half-applying one would leave the
// client and the engine holding different inventories.
func (m *invMirror) applyRequest(req protocol.ItemStackRequest, recipes *recipeSet) ([]int32, []craftStep, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(req.Actions) > 0 {
		switch req.Actions[0].(type) {
		case *protocol.CraftRecipeStackRequestAction, *protocol.AutoCraftRecipeStackRequestAction,
			*protocol.CraftRecipeOptionalStackRequestAction, *protocol.CraftGrindstoneRecipeStackRequestAction,
			*protocol.CraftLoomRecipeStackRequestAction:
			return m.applyCraft(req, recipes)
		}
	}
	before := append([]attach.ItemStack(nil), m.slots...)
	touched := map[int32]bool{}

	for _, a := range req.Actions {
		switch act := a.(type) {
		case *protocol.TakeStackRequestAction:
			if !m.applyTransfer(act.Source, act.Destination, int(act.Count), touched) {
				m.slots = before
				return nil, nil, false
			}
		case *protocol.PlaceStackRequestAction:
			if !m.applyTransfer(act.Source, act.Destination, int(act.Count), touched) {
				m.slots = before
				return nil, nil, false
			}
		case *protocol.SwapStackRequestAction:
			src, ok1 := m.mapIn(act.Source.Container.ContainerID, act.Source.Slot)
			dst, ok2 := m.mapIn(act.Destination.Container.ContainerID, act.Destination.Slot)
			if !ok1 || !ok2 || !m.swap(src, dst) {
				m.slots = before
				return nil, nil, false
			}
			touched[src], touched[dst] = true, true
		default:
			// Drops, destroys and the whole craft family are not bridged yet.
			// Refusing makes the client put it back, which is honest; guessing
			// would desynchronise it from the engine.
			m.slots = before
			return nil, nil, false
		}
	}

	changed := make([]int32, 0, len(touched))
	for slot := range touched {
		changed = append(changed, slot)
	}
	return changed, nil, true
}

func (m *invMirror) applyTransfer(from, to protocol.StackRequestSlotInfo, count int, touched map[int32]bool) bool {
	src, ok1 := m.mapIn(from.Container.ContainerID, from.Slot)
	dst, ok2 := m.mapIn(to.Container.ContainerID, to.Slot)
	if !ok1 || !ok2 || !m.transfer(src, dst, count) {
		return false
	}
	touched[src], touched[dst] = true, true
	return true
}

// clickFor turns the mirror's new state into the Java click the engine
// expects: the changed slots, and the cursor as it now stands.
func (m *invMirror) clickFor(changed []int32) attach.WindowClick {
	m.mu.Lock()
	defer m.mu.Unlock()
	e := attach.WindowClick{ID: m.window, Cursor: m.slots[m.cursor]}
	for _, slot := range changed {
		if slot == m.cursor {
			continue // carried in Cursor, not as a slot
		}
		e.Changed = append(e.Changed, attach.ClickChange{
			Slot: slot,
			Item: m.slots[slot],
		})
	}
	// A slot the engine treats as ordinary storage, so its click handler takes
	// the reconciliation path rather than a crafting-result one.
	if len(e.Changed) > 0 {
		e.Slot = e.Changed[0].Slot
	}
	return e
}

// respondStackRequest tells the client what the slots it touched now hold.
func respondStackRequest(w packetWriter, requestID int32, m *invMirror, changed []int32, ok bool) {
	if !ok {
		w.WritePacket(&packet.ItemStackResponse{Responses: []protocol.ItemStackResponse{{
			Status:    stackResponseError,
			RequestID: requestID,
		}}})
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	byContainer := map[byte][]protocol.StackResponseSlotInfo{}
	for _, slot := range changed {
		var cid byte
		var idx uint32
		if slot == m.cursor {
			cid, idx = protocol.ContainerCursor, 0
		} else {
			c, i, mapped := m.mapOut(slot)
			if !mapped {
				continue
			}
			cid, idx = c, i
		}
		st := m.slots[slot]
		info := protocol.StackResponseSlotInfo{
			Slot:       byte(idx),
			HotbarSlot: byte(idx),
			Count:      byte(st.Count),
		}
		if st.Count > 0 {
			info.StackNetworkID = stackIDs.Add(1)
		}
		byContainer[cid] = append(byContainer[cid], info)
	}
	resp := protocol.ItemStackResponse{Status: stackResponseOK, RequestID: requestID}
	for cid, slots := range byContainer {
		resp.ContainerInfo = append(resp.ContainerInfo, protocol.StackResponseContainerInfo{
			Container: fullContainer(cid),
			SlotInfo:  slots,
		})
	}
	w.WritePacket(&packet.ItemStackResponse{Responses: []protocol.ItemStackResponse{resp}})
}
