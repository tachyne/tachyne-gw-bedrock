package gw

import (
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
	attach "github.com/tachyne/tachyne-common/attach"
	tproto "github.com/tachyne/tachyne-common/protocol"
)

// The creative inventory. Bedrock's creative screen shows what the server
// lists in CreativeContent (and crashes if opened before it arrives), so
// every canonical item with a Bedrock counterpart is listed — blocks under
// the construction tab, the rest under items — numbered by position. A
// creative request names an entry and moves copies of it into the player's
// slots or cursor, drops it, or destroys a slot; on the player's own window
// those become the world's creative slot sets (the world checks the mode),
// inside a container they are declared clicks, which the world accepts in
// creative.

// creativeItems is the listing: canonical item ids, network id = index + 1.
var creativeItems = func() []int32 {
	var out []int32
	for id := int32(1); int(id) < len(javaItemBedrock); id++ {
		if _, _, ok := bedrockItemIDs(id); ok {
			out = append(out, id)
		}
	}
	return out
}()

// creativeContent lists the items in two anonymous groups: construction
// (items that place a block) and items.
func creativeContent() *packet.CreativeContent {
	pk := &packet.CreativeContent{Groups: []protocol.CreativeGroup{
		{Category: protocol.CreativeCategoryConstruction},
		{Category: protocol.CreativeCategoryItems},
	}}
	for i, id := range creativeItems {
		st, ok := bedrockStackOf(attach.ItemStack{ID: id, Count: 1})
		if !ok {
			continue
		}
		group := uint32(1)
		if _, isBlock := tproto.BlockForItem(id); isBlock {
			group = 0
		}
		pk.Items = append(pk.Items, protocol.CreativeItem{CreativeItemNetworkID: uint32(i + 1), Item: st, GroupIndex: group})
	}
	return pk
}

// creativeItem is the canonical item behind a creative network id.
func creativeItem(netID uint32) (int32, bool) {
	if netID == 0 || int(netID) > len(creativeItems) {
		return 0, false
	}
	return creativeItems[netID-1], true
}

// applyCreative resolves a creative request against the mirror (held
// locked): copies of the named item into slots or the cursor, dropped, or
// a slot destroyed.
func (m *invMirror) applyCreative(req protocol.ItemStackRequest) ([]int32, []craftStep, bool) {
	before := append([]attach.ItemStack(nil), m.slots...)
	touched := map[int32]bool{}
	var steps []craftStep
	var item int32
	fail := func() ([]int32, []craftStep, bool) {
		m.slots = before
		return nil, nil, false
	}
	// declare is the world's word for a slot's new content: a creative slot
	// set on the player's window, a declared click inside a container.
	declare := func(slot int32) {
		touched[slot] = true
		if m.window == 0 {
			steps = append(steps, craftStep{creative: &attach.CreativeSlot{Slot: slot, Item: m.slots[slot]}})
			return
		}
		steps = append(steps, craftStep{click: &attach.WindowClick{ID: m.window, Slot: slot, Mode: 0,
			Cursor: m.slots[m.cursor], Changed: []attach.ClickChange{{Slot: slot, Item: m.slots[slot]}}}})
	}
	for _, a := range req.Actions {
		switch act := a.(type) {
		case *protocol.CraftCreativeStackRequestAction:
			id, ok := creativeItem(act.CreativeItemNetworkID)
			if item != 0 || !ok {
				return fail()
			}
			item = id
		case *protocol.CraftResultsDeprecatedStackRequestAction:
		case *protocol.DestroyStackRequestAction:
			slot, ok := m.mapIn(act.Source.Container.ContainerID, act.Source.Slot)
			if !ok {
				return fail()
			}
			m.slots[slot] = attach.ItemStack{}
			if slot == m.cursor {
				touched[slot] = true
				continue
			}
			declare(slot)
		case *protocol.TakeStackRequestAction:
			if !m.takeCreative(item, act.Source, act.Destination, int32(act.Count), touched, declare) {
				return fail()
			}
		case *protocol.PlaceStackRequestAction:
			if !m.takeCreative(item, act.Source, act.Destination, int32(act.Count), touched, declare) {
				return fail()
			}
		case *protocol.DropStackRequestAction:
			if item == 0 || act.Source.Container.ContainerID != protocol.ContainerCreatedOutput || m.window != 0 {
				return fail()
			}
			steps = append(steps, craftStep{creative: &attach.CreativeSlot{Slot: -1, Item: attach.ItemStack{ID: item, Count: int32(act.Count)}}})
		default:
			return fail()
		}
	}
	changed := make([]int32, 0, len(touched))
	for slot := range touched {
		changed = append(changed, slot)
	}
	return changed, steps, true
}

// takeCreative moves count copies of the creative item from the created
// output to dest (a slot or the cursor).
func (m *invMirror) takeCreative(item int32, src, dst protocol.StackRequestSlotInfo, count int32,
	touched map[int32]bool, declare func(int32)) bool {
	if item == 0 || count <= 0 || src.Container.ContainerID != protocol.ContainerCreatedOutput {
		return false
	}
	dest, ok := m.mapIn(dst.Container.ContainerID, dst.Slot)
	if !ok || dest == m.result {
		return false
	}
	d := m.slots[dest]
	if d.Count > 0 && d.ID != item {
		return false
	}
	d.ID, d.Count = item, d.Count+count
	m.slots[dest] = d
	if dest == m.cursor {
		touched[dest] = true // the cursor is the client's own until it puts the stack down
		return true
	}
	declare(dest)
	return true
}
