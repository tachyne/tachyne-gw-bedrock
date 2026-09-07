package gw

import (
	"testing"

	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
	attach "github.com/tachyne/tachyne-common/attach"
	tproto "github.com/tachyne/tachyne-common/protocol"
)

// slotInfo builds the Bedrock side of a transfer.
func slotInfo(container byte, slot byte) protocol.StackRequestSlotInfo {
	return protocol.StackRequestSlotInfo{
		Container: protocol.FullContainerName{ContainerID: container},
		Slot:      slot,
	}
}

// takeAction / placeAction build the transfer actions. Their fields are
// promoted from an UNEXPORTED embedded struct, so they cannot be set in a
// composite literal from this package — only assigned after the fact.
func takeAction(count byte, src, dst protocol.StackRequestSlotInfo) *protocol.TakeStackRequestAction {
	a := &protocol.TakeStackRequestAction{}
	a.Count, a.Source, a.Destination = count, src, dst
	return a
}

func placeAction(count byte, src, dst protocol.StackRequestSlotInfo) *protocol.PlaceStackRequestAction {
	a := &protocol.PlaceStackRequestAction{}
	a.Count, a.Source, a.Destination = count, src, dst
	return a
}

// mirrorWith puts a stack in a Java slot.
func mirrorWith(t *testing.T, slot int32, count int32) *invMirror {
	t.Helper()
	m := newInvMirror()
	m.set(slot, attach.ItemStack{ID: javaItemID(t, "minecraft:dirt"), Count: count})
	return m
}

// The reverse mapping must undo the forward one for every storage slot, or a
// Bedrock click would land on a different slot than the one it rendered into.
func TestBedrockToJavaRoundTripsEveryStorageSlot(t *testing.T) {
	for slot := int32(0); slot < javaWindowSize; slot++ {
		cid, idx, ok := javaToBedrockSlot(slot)
		if !ok {
			continue
		}
		back, ok := bedrockToJavaSlot(cid, byte(idx))
		if !ok {
			t.Errorf("java %d -> %d/%d did not map back", slot, cid, idx)
			continue
		}
		if back != slot {
			t.Errorf("java %d -> %d/%d -> java %d; the mapping is not reversible",
				slot, cid, idx, back)
		}
	}
}

// Taking part of a stack moves exactly that many and leaves the rest.
func TestTakeMovesPartOfAStack(t *testing.T) {
	m := mirrorWith(t, 36, 10) // first hotbar slot
	req := protocol.ItemStackRequest{RequestID: 7, Actions: []protocol.StackRequestAction{
		takeAction(4, slotInfo(protocol.ContainerInventory, 0), slotInfo(protocol.ContainerCursor, 0)),
	}}
	changed, _, ok := m.applyRequest(req, nil)
	if !ok {
		t.Fatal("a plain take was refused")
	}
	if len(changed) != 2 {
		t.Errorf("%d slots changed, want 2", len(changed))
	}
	if got := m.slots[36].Count; got != 6 {
		t.Errorf("source left %d, want 6", got)
	}
	if got := m.slots[javaCursorSlot].Count; got != 4 {
		t.Errorf("cursor holds %d, want 4", got)
	}
}

// Bedrock transactions are all-or-nothing: if the second action is impossible
// the FIRST must be rolled back, or the client and the engine end up holding
// different inventories.
func TestAFailedActionRollsTheWholeRequestBack(t *testing.T) {
	m := mirrorWith(t, 36, 10)
	req := protocol.ItemStackRequest{RequestID: 9, Actions: []protocol.StackRequestAction{
		takeAction(4, slotInfo(protocol.ContainerInventory, 0), slotInfo(protocol.ContainerCursor, 0)),
		&protocol.DropStackRequestAction{}, // not bridged yet
	}}
	changed, _, ok := m.applyRequest(req, nil)
	if ok {
		t.Fatal("a request containing an unsupported action was accepted")
	}
	if changed != nil {
		t.Errorf("changed = %v, want nothing on a refused request", changed)
	}
	if got := m.slots[36].Count; got != 10 {
		t.Errorf("source holds %d after a refused request, want the original 10", got)
	}
	if m.slots[javaCursorSlot].Count != 0 {
		t.Error("the cursor kept items from a refused request")
	}
}

// Taking more than a slot holds is refused rather than creating items.
func TestTakingMoreThanThereIsIsRefused(t *testing.T) {
	m := mirrorWith(t, 36, 3)
	req := protocol.ItemStackRequest{Actions: []protocol.StackRequestAction{
		takeAction(64, slotInfo(protocol.ContainerInventory, 0), slotInfo(protocol.ContainerCursor, 0)),
	}}
	if _, _, ok := m.applyRequest(req, nil); ok {
		t.Fatal("taking 64 from a stack of 3 was accepted")
	}
	if got := m.slots[36].Count; got != 3 {
		t.Errorf("source holds %d, want the untouched 3", got)
	}
}

// Merging onto a different item is refused — it would silently destroy one.
func TestMergingOntoADifferentItemIsRefused(t *testing.T) {
	m := newInvMirror()
	m.set(36, attach.ItemStack{ID: javaItemID(t, "minecraft:dirt"), Count: 5})
	m.set(37, attach.ItemStack{ID: javaItemID(t, "minecraft:stone"), Count: 5})
	req := protocol.ItemStackRequest{Actions: []protocol.StackRequestAction{
		placeAction(5,
			slotInfo(protocol.ContainerInventory, 0),  // java 36
			slotInfo(protocol.ContainerInventory, 1)), // java 37
	}}
	if _, _, ok := m.applyRequest(req, nil); ok {
		t.Error("dirt was merged onto stone")
	}
}

// Swapping exchanges two slots outright.
func TestSwapExchangesTwoSlots(t *testing.T) {
	m := newInvMirror()
	dirt, stone := javaItemID(t, "minecraft:dirt"), javaItemID(t, "minecraft:stone")
	m.set(36, attach.ItemStack{ID: dirt, Count: 5})
	m.set(37, attach.ItemStack{ID: stone, Count: 7})
	req := protocol.ItemStackRequest{Actions: []protocol.StackRequestAction{
		&protocol.SwapStackRequestAction{
			Source:      slotInfo(protocol.ContainerInventory, 0),
			Destination: slotInfo(protocol.ContainerInventory, 1),
		},
	}}
	if _, _, ok := m.applyRequest(req, nil); !ok {
		t.Fatal("a swap was refused")
	}
	if m.slots[36].ID != stone || m.slots[37].ID != dirt {
		t.Errorf("after the swap: %d and %d, want %d and %d",
			m.slots[36].ID, m.slots[37].ID, stone, dirt)
	}
}

// The click handed to the engine carries the changed slots and the cursor —
// which is what the engine's declarative reconciliation reads.
func TestTheSynthesizedClickCarriesTheResult(t *testing.T) {
	m := mirrorWith(t, 36, 10)
	req := protocol.ItemStackRequest{Actions: []protocol.StackRequestAction{
		takeAction(4, slotInfo(protocol.ContainerInventory, 0), slotInfo(protocol.ContainerCursor, 0)),
	}}
	changed, _, _ := m.applyRequest(req, nil)
	click := m.clickFor(changed)

	if click.Cursor.Count != 4 {
		t.Errorf("click cursor %d, want the 4 now held", click.Cursor.Count)
	}
	if len(click.Changed) != 1 || click.Changed[0].Slot != 36 || click.Changed[0].Item.Count != 6 {
		t.Errorf("click changed %+v, want slot 36 holding 6", click.Changed)
	}
}

// A refused request must still be answered, or the client waits forever with
// the items stuck to its mouse.
func TestARefusedRequestStillGetsAResponse(t *testing.T) {
	m := newInvMirror()
	c := &capture{}
	respondStackRequest(c, 42, m, nil, false)
	if len(c.pkts) != 1 {
		t.Fatalf("%d packets, want 1", len(c.pkts))
	}
	r := c.pkts[0].(*packet.ItemStackResponse)
	if len(r.Responses) != 1 || r.Responses[0].Status != stackResponseError {
		t.Errorf("response %+v, want an error status", r.Responses)
	}
	if r.Responses[0].RequestID != 42 {
		t.Errorf("request id %d, want 42 — the client matches on it", r.Responses[0].RequestID)
	}
}

// An accepted request reports the new contents of the slots it touched.
func TestAnAcceptedRequestReportsTheNewSlots(t *testing.T) {
	m := mirrorWith(t, 36, 10)
	changed, _, ok := m.applyRequest(protocol.ItemStackRequest{Actions: []protocol.StackRequestAction{
		takeAction(4, slotInfo(protocol.ContainerInventory, 0), slotInfo(protocol.ContainerCursor, 0)),
	}}, nil)
	if !ok {
		t.Fatal("take refused")
	}
	c := &capture{}
	respondStackRequest(c, 5, m, changed, true)
	r := c.pkts[0].(*packet.ItemStackResponse).Responses[0]
	if r.Status != stackResponseOK {
		t.Fatalf("status %d, want OK", r.Status)
	}
	var sawInv, sawCursor bool
	for _, ci := range r.ContainerInfo {
		switch ci.Container.ContainerID {
		case protocol.ContainerInventory:
			sawInv = true
			if ci.SlotInfo[0].Count != 6 {
				t.Errorf("inventory slot reported %d, want 6", ci.SlotInfo[0].Count)
			}
		case protocol.ContainerCursor:
			sawCursor = true
			if ci.SlotInfo[0].Count != 4 {
				t.Errorf("cursor reported %d, want 4", ci.SlotInfo[0].Count)
			}
		}
	}
	if !sawInv || !sawCursor {
		t.Errorf("containers reported: inv=%v cursor=%v, want both", sawInv, sawCursor)
	}
}

// Dropping: a window drop sheds the slot and declares it (the world drops
// the difference); the in-world drop of a held stack does the same
// through the transaction path.
func TestDrops(t *testing.T) {
	m := mirrorWith(t, 39, 10) // hotbar slot 3, ten dirt
	drop := &protocol.DropStackRequestAction{Count: 4, Source: slotInfo(protocol.ContainerHotBar, 3)}
	changed, _, ok := m.applyRequest(protocol.ItemStackRequest{Actions: []protocol.StackRequestAction{drop}}, nil)
	if !ok || len(changed) != 1 || changed[0] != 39 || m.slots[39].Count != 6 {
		t.Fatalf("drop: ok=%v changed=%v slot %+v", ok, changed, m.slots[39])
	}
	if e := m.clickFor(changed); e.Slot != 39 || e.Changed[0].Item.Count != 6 {
		t.Errorf("declared %+v", e)
	}
	if _, _, ok := m.applyRequest(protocol.ItemStackRequest{Actions: []protocol.StackRequestAction{
		&protocol.DropStackRequestAction{Count: 7, Source: slotInfo(protocol.ContainerHotBar, 3)}}}, nil); ok || m.slots[39].Count != 6 {
		t.Error("dropped more than held")
	}
	click, ok := m.dropHeld(protocol.ContainerInventory, 3, 6)
	if !ok || click.Slot != 39 || click.Changed[0].Item.Count != 0 || m.slots[39].Count != 0 {
		t.Errorf("held drop: ok=%v click=%+v slot %+v", ok, click, m.slots[39])
	}
}

// The world's cursor stack lands on the mirror's cursor.
func TestCursorItem(t *testing.T) {
	m := newInvMirror()
	m.setCursor(attach.ItemStack{ID: 3, Count: 7})
	if m.slots[m.cursor].Count != 7 {
		t.Errorf("cursor %+v", m.slots[m.cursor])
	}
	w := newWindowMirror(2, chestLayout(9))
	w.setCursor(attach.ItemStack{ID: 3, Count: 1})
	if j, ok := w.mapIn(protocol.ContainerCursor, 0); !ok || w.slots[j].Count != 1 {
		t.Errorf("window cursor %+v", w.slots[w.cursor])
	}
}

// A dyed leather chestplate carries Bedrock's customColor tag.
func TestDyedLeatherCustomColor(t *testing.T) {
	comps := tproto.AppendVarInt(nil, 1)
	comps = tproto.AppendVarInt(comps, 0)
	comps = tproto.AppendVarInt(comps, componentDyedColor)
	comps = tproto.AppendVarInt(comps, 0xA06540)
	nbt := stackNBT(attach.ItemStack{ID: 1, Count: 1, Components: comps})
	if nbt == nil || nbt["customColor"] != int32(0xA06540) {
		t.Fatalf("customColor NBT = %v", nbt)
	}
	if stackNBT(attach.ItemStack{ID: 1, Count: 1}) != nil {
		t.Fatal("a plain stack has no NBT")
	}
}
