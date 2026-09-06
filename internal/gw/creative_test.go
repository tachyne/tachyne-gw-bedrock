package gw

import (
	"testing"

	"github.com/sandertv/gophertunnel/minecraft/protocol"
	tproto "github.com/tachyne/tachyne-common/protocol"
)

// The creative listing covers every mapped item, blocks under construction;
// a creative request becomes the world's creative slot sets.
func TestCreativeInventory(t *testing.T) {
	pk := creativeContent()
	if len(pk.Groups) != 2 || len(pk.Items) != len(creativeItems) || len(pk.Items) < 1000 {
		t.Fatalf("%d groups, %d items", len(pk.Groups), len(pk.Items))
	}
	blocks := 0
	for _, it := range pk.Items {
		if it.GroupIndex == 0 {
			blocks++
		}
	}
	if blocks < 500 {
		t.Errorf("only %d block items", blocks)
	}
	var stone uint32
	for i, id := range creativeItems {
		if _, ok := tproto.BlockForItem(id); ok && stone == 0 {
			stone = uint32(i + 1)
		}
	}
	id, ok := creativeItem(stone)
	if !ok || id != creativeItems[stone-1] {
		t.Fatalf("creative item %d → %d %v", stone, id, ok)
	}

	m := newInvMirror()
	req := protocol.ItemStackRequest{RequestID: 1, Actions: []protocol.StackRequestAction{
		&protocol.CraftCreativeStackRequestAction{CreativeItemNetworkID: stone},
		&protocol.CraftResultsDeprecatedStackRequestAction{ResultItems: []protocol.ItemStack{{Count: 64}}, TimesCrafted: 1},
		placeAction(64, slotInfo(protocol.ContainerCreatedOutput, 50), slotInfo(protocol.ContainerHotBar, 2)),
	}}
	changed, steps, ok := m.applyRequest(req, nil)
	if !ok || len(changed) != 1 || len(steps) != 1 || steps[0].creative == nil || steps[0].creative.Slot != 38 || steps[0].creative.Item.Count != 64 || steps[0].creative.Item.ID != id {
		t.Fatalf("creative place: ok=%v changed=%v steps=%+v", ok, changed, steps)
	}
	if m.slots[38].Count != 64 {
		t.Errorf("mirror %+v", m.slots[38])
	}
	// To the cursor: the client's own business until it puts it down.
	cur := protocol.ItemStackRequest{RequestID: 2, Actions: []protocol.StackRequestAction{
		&protocol.CraftCreativeStackRequestAction{CreativeItemNetworkID: stone},
		takeAction(1, slotInfo(protocol.ContainerCreatedOutput, 50), slotInfo(protocol.ContainerCursor, 0)),
	}}
	if _, steps, ok := m.applyRequest(cur, nil); !ok || len(steps) != 0 || m.slots[m.cursor].Count != 1 {
		t.Errorf("creative cursor: ok=%v steps=%+v cursor %+v", ok, steps, m.slots[m.cursor])
	}
	// Destroying a slot and dropping from the listing.
	del := protocol.ItemStackRequest{RequestID: 3, Actions: []protocol.StackRequestAction{
		&protocol.CraftCreativeStackRequestAction{CreativeItemNetworkID: stone},
		&protocol.CraftResultsDeprecatedStackRequestAction{},
		&protocol.DestroyStackRequestAction{Count: 64, Source: slotInfo(protocol.ContainerHotBar, 2)},
		&protocol.DropStackRequestAction{Count: 3, Source: slotInfo(protocol.ContainerCreatedOutput, 50)},
	}}
	_, steps, ok = m.applyRequest(del, nil)
	if !ok || len(steps) != 2 || steps[0].creative.Slot != 38 || steps[0].creative.Item.Count != 0 || steps[1].creative.Slot != -1 || steps[1].creative.Item.Count != 3 {
		t.Fatalf("destroy/drop: ok=%v steps=%+v", ok, steps)
	}
	// Inside a container the placement is a declared click.
	w := newWindowMirror(4, chestLayout(27))
	in := protocol.ItemStackRequest{RequestID: 4, Actions: []protocol.StackRequestAction{
		&protocol.CraftCreativeStackRequestAction{CreativeItemNetworkID: stone},
		placeAction(8, slotInfo(protocol.ContainerCreatedOutput, 50), slotInfo(protocol.ContainerLevelEntity, 5)),
	}}
	if _, steps, ok := w.applyRequest(in, nil); !ok || len(steps) != 1 || steps[0].click == nil || steps[0].click.Slot != 5 || steps[0].click.Changed[0].Item.ID != id || steps[0].click.Changed[0].Item.Count != 8 {
		t.Errorf("creative into a chest: ok=%v steps=%+v", ok, steps)
	}
	if _, _, ok := newInvMirror().applyRequest(protocol.ItemStackRequest{Actions: []protocol.StackRequestAction{
		&protocol.CraftCreativeStackRequestAction{CreativeItemNetworkID: 1 << 30}}}, nil); ok {
		t.Error("an unknown creative id was accepted")
	}
}
