package gw

import (
	"encoding/binary"
	"math"
	"testing"

	"github.com/sandertv/gophertunnel/minecraft/nbt"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	attach "github.com/tachyne/tachyne-common/attach"
	tproto "github.com/tachyne/tachyne-common/protocol"
)

// A merchant_offers body parses back into offers, renders as Bedrock's
// trade NBT, and a trade request selects the offer before the clicks.
func TestTrades(t *testing.T) {
	emerald, bread := mappedItem(t, 0), mappedItem(t, 1)
	i32 := func(b []byte, v int32) []byte { return binary.BigEndian.AppendUint32(b, uint32(v)) }
	body := tproto.AppendVarInt(nil, 7) // window
	body = tproto.AppendVarInt(body, 2) // two offers
	// offer 0: 3 emeralds (+1 special) → 6 bread
	body = tproto.AppendVarInt(body, emerald)
	body = tproto.AppendVarInt(body, 3)
	body = tproto.AppendVarInt(body, 0)
	body = tproto.AppendVarInt(body, 6) // result Slot: count, item, 0 comps added/removed
	body = tproto.AppendVarInt(body, bread)
	body = tproto.AppendVarInt(body, 0)
	body = tproto.AppendVarInt(body, 0)
	body = append(body, 0)                          // no cost B
	body = append(body, 0)                          // enabled
	body = i32(body, 2)                             // uses
	body = i32(body, 16)                            // max uses
	body = i32(body, 1)                             // xp
	body = i32(body, 1)                             // special price
	body = i32(body, int32(math.Float32bits(0.05))) // multiplier
	body = i32(body, 0)                             // demand
	// offer 1: 1 bread + 1 emerald → 1 emerald, used up
	body = tproto.AppendVarInt(body, bread)
	body = tproto.AppendVarInt(body, 1)
	body = tproto.AppendVarInt(body, 0)
	body = tproto.AppendVarInt(body, 1)
	body = tproto.AppendVarInt(body, emerald)
	body = tproto.AppendVarInt(body, 0)
	body = tproto.AppendVarInt(body, 0)
	body = append(body, 1) // cost B
	body = tproto.AppendVarInt(body, emerald)
	body = tproto.AppendVarInt(body, 1)
	body = tproto.AppendVarInt(body, 0)
	body = append(body, 1) // disabled
	body = i32(body, 16)
	body = i32(body, 16)
	body = i32(body, 2)
	body = i32(body, 0)
	body = i32(body, int32(math.Float32bits(0.2)))
	body = i32(body, 3)
	body = tproto.AppendVarInt(body, 3) // level
	body = tproto.AppendVarInt(body, 80)
	body = append(body, 1, 1)

	tl, ok := parseTrades(body)
	if !ok || tl.window != 7 || len(tl.offers) != 2 || tl.level != 3 || !tl.restock {
		t.Fatalf("parse: %+v %v", tl, ok)
	}
	if o := tl.offers[0]; o.costA.Count != 3 || o.result.ID != bread || o.result.Count != 6 || o.special != 1 || o.mult != 0.05 {
		t.Errorf("offer 0 %+v", o)
	}
	if o := tl.offers[1]; !o.disabled || o.costB.ID != emerald || o.demand != 3 {
		t.Errorf("offer 1 %+v", o)
	}

	pk, err := tradePacket(tl, 42, 9, "Farmer")
	if err != nil || pk.WindowID != 7 || pk.TradeTier != 2 || pk.VillagerUniqueID != 42 || pk.EntityUniqueID != 9 || !pk.NewTradeUI {
		t.Fatalf("packet %+v %v", pk, err)
	}
	var offers map[string]any
	if err := nbt.UnmarshalEncoding(pk.SerialisedOffers, &offers, nbt.NetworkLittleEndian); err != nil {
		t.Fatalf("offers nbt: %v", err)
	}
	recipes, _ := offers["Recipes"].([]any)
	if len(recipes) != 2 {
		t.Fatalf("recipes %v", offers["Recipes"])
	}
	r0 := recipes[0].(map[string]any)
	if r0["netId"] != int32(1) || r0["buyCountA"] != int32(3) || r0["maxUses"] != int32(16) || r0["tier"] != int32(2) {
		t.Errorf("recipe 0 %v", r0)
	}
	if buyA := r0["buyA"].(map[string]any); buyA["Count"] != byte(4) || buyA["Name"] != javaItemBedrock[emerald].Name { // 3 + special 1
		t.Errorf("buyA %v", buyA)
	}
	if sell := r0["sell"].(map[string]any); sell["Count"] != byte(6) {
		t.Errorf("sell %v", sell)
	}
	r1 := recipes[1].(map[string]any)
	if r1["maxUses"] != int32(0) || r1["buyB"].(map[string]any)["Count"] != byte(1) {
		t.Errorf("recipe 1 %v", r1)
	}
	if tiers, _ := offers["TierExpRequirements"].([]any); len(tiers) != 5 {
		t.Errorf("tiers %v", offers["TierExpRequirements"])
	}

	// The trade: ingredients already in the trade slots, offer 1 (net id 1)
	// chosen, result to the cursor.
	m := newWindowMirror(7, tradeLayout)
	m.setTrades(tl.results())
	m.set(0, attach.ItemStack{ID: emerald, Count: 4})
	req := protocol.ItemStackRequest{RequestID: 5, Actions: []protocol.StackRequestAction{
		&protocol.CraftRecipeStackRequestAction{RecipeNetworkID: 1},
		&protocol.ConsumeStackRequestAction{DestroyStackRequestAction: protocol.DestroyStackRequestAction{Count: 4, Source: slotInfo(protocol.ContainerTradeTwoIngredientOne, 4)}},
		takeAction(6, slotInfo(protocol.ContainerCreatedOutput, 50), slotInfo(protocol.ContainerCursor, 0)),
	}}
	_, steps, ok := m.applyRequest(req, nil)
	if !ok || len(steps) != 2 || steps[0].sel == nil || steps[0].sel.Slot != 0 || steps[1].click == nil || steps[1].click.Slot != 2 {
		t.Fatalf("trade: ok=%v steps=%+v", ok, steps)
	}
	if m.slots[m.cursor].ID != bread || m.slots[m.cursor].Count != 6 || m.slots[0].Count != 0 {
		t.Errorf("trade mirror cursor %+v slot0 %+v", m.slots[m.cursor], m.slots[0])
	}
	if mw := menuWindows[19]; mw.ctype != protocol.ContainerTypeTrade || len(mw.layout) != 3 {
		t.Error("merchant menu")
	}
}
