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
	mu    sync.Mutex
	slots [javaWindowSize + 1]attach.ItemStack // +1: the cursor
}

func newInvMirror() *invMirror { return &invMirror{} }

// set records a slot the engine has told us about.
func (m *invMirror) set(slot int32, st attach.ItemStack) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if slot < 0 || int(slot) >= len(m.slots) {
		return
	}
	m.slots[slot] = st
}

// setAll replaces the whole window.
func (m *invMirror) setAll(slots []attach.ItemStack, cursor attach.ItemStack) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.slots {
		m.slots[i] = attach.ItemStack{}
	}
	for i, st := range slots {
		if i < javaWindowSize {
			m.slots[i] = st
		}
	}
	m.slots[javaCursorSlot] = cursor
}

// bedrockToJavaSlot is javaToBedrockSlot backwards, plus the cursor, which
// Bedrock keeps in a container of its own.
func bedrockToJavaSlot(container byte, slot byte) (int32, bool) {
	switch container {
	case protocol.ContainerCursor:
		return javaCursorSlot, true
	case protocol.ContainerOffhand:
		return javaOffhand, true
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
func (m *invMirror) applyRequest(req protocol.ItemStackRequest) ([]int32, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	before := m.slots
	touched := map[int32]bool{}

	for _, a := range req.Actions {
		switch act := a.(type) {
		case *protocol.TakeStackRequestAction:
			if !m.applyTransfer(act.Source, act.Destination, int(act.Count), touched) {
				m.slots = before
				return nil, false
			}
		case *protocol.PlaceStackRequestAction:
			if !m.applyTransfer(act.Source, act.Destination, int(act.Count), touched) {
				m.slots = before
				return nil, false
			}
		case *protocol.SwapStackRequestAction:
			src, ok1 := bedrockToJavaSlot(act.Source.Container.ContainerID, act.Source.Slot)
			dst, ok2 := bedrockToJavaSlot(act.Destination.Container.ContainerID, act.Destination.Slot)
			if !ok1 || !ok2 || !m.swap(src, dst) {
				m.slots = before
				return nil, false
			}
			touched[src], touched[dst] = true, true
		default:
			// Drops, destroys and the whole craft family are not bridged yet.
			// Refusing makes the client put it back, which is honest; guessing
			// would desynchronise it from the engine.
			m.slots = before
			return nil, false
		}
	}

	changed := make([]int32, 0, len(touched))
	for slot := range touched {
		changed = append(changed, slot)
	}
	return changed, true
}

func (m *invMirror) applyTransfer(from, to protocol.StackRequestSlotInfo, count int, touched map[int32]bool) bool {
	src, ok1 := bedrockToJavaSlot(from.Container.ContainerID, from.Slot)
	dst, ok2 := bedrockToJavaSlot(to.Container.ContainerID, to.Slot)
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
	e := attach.WindowClick{Cursor: m.slots[javaCursorSlot]}
	for _, slot := range changed {
		if slot == javaCursorSlot {
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
		if slot == javaCursorSlot {
			cid, idx = protocol.ContainerCursor, 0
		} else {
			c, i, mapped := javaToBedrockSlot(slot)
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
