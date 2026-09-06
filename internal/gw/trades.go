package gw

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"

	"github.com/sandertv/gophertunnel/minecraft/nbt"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
	attach "github.com/tachyne/tachyne-common/attach"
	tproto "github.com/tachyne/tachyne-common/protocol"
)

// Villager trading. The world's Trades frame is the canonical
// merchant_offers body (Java clients take it as it is); Bedrock wants the
// same offers as an UpdateTrade packet carrying a network-NBT list in the
// shape the Geyser project established, addressed to the villager the
// player just used. Bedrock's trade screen moves the ingredients into its
// two trade slots by ordinary transfers, then trades with a craft request
// naming the offer (network id = index + 1): the world hears the selection
// first, then the same result-slot clicks as any other menu.

// tradeOffer is one offer as the world describes it.
type tradeOffer struct {
	costA, costB, result attach.ItemStack
	disabled             bool
	uses, maxUses, xp    int32
	special              int32   // reputation/Hero price delta
	mult                 float32 // price multiplier for demand
	demand               int32
}

// tradeList is a merchant screen's offers.
type tradeList struct {
	window            int32
	offers            []tradeOffer
	level, xp         int32
	progress, restock bool
}

// parseTrades reads the canonical merchant_offers body.
func parseTrades(data []byte) (tradeList, bool) {
	var tl tradeList
	r := bytes.NewReader(data)
	vi := func() (int32, bool) {
		v, err := tproto.ReadVarInt(r)
		return v, err == nil
	}
	i32 := func() (int32, bool) {
		var b [4]byte
		if _, err := r.Read(b[:]); err != nil {
			return 0, false
		}
		return int32(binary.BigEndian.Uint32(b[:])), true
	}
	boolean := func() (bool, bool) {
		b, err := r.ReadByte()
		return b != 0, err == nil
	}
	cost := func() (attach.ItemStack, bool) {
		item, ok1 := vi()
		count, ok2 := vi()
		comps, ok3 := vi()
		if !ok1 || !ok2 || !ok3 || comps != 0 {
			return attach.ItemStack{}, false // a predicate we never send
		}
		return attach.ItemStack{ID: item, Count: count}, true
	}
	var ok bool
	if tl.window, ok = vi(); !ok {
		return tl, false
	}
	n, ok := vi()
	if !ok || n < 0 || n > 64 {
		return tl, false
	}
	for i := int32(0); i < n; i++ {
		var o tradeOffer
		if o.costA, ok = cost(); !ok {
			return tl, false
		}
		item, count, ok := tproto.ReadSlot770(r)
		if !ok {
			return tl, false
		}
		o.result = attach.ItemStack{ID: item, Count: count}
		hasB, ok := boolean()
		if !ok {
			return tl, false
		}
		if hasB {
			if o.costB, ok = cost(); !ok {
				return tl, false
			}
		}
		if o.disabled, ok = boolean(); !ok {
			return tl, false
		}
		var ok1, ok2, ok3, ok4, ok6 bool
		o.uses, ok1 = i32()
		o.maxUses, ok2 = i32()
		o.xp, ok3 = i32()
		o.special, ok4 = i32()
		var mb [4]byte
		_, err := r.Read(mb[:])
		o.demand, ok6 = i32()
		if !ok1 || !ok2 || !ok3 || !ok4 || err != nil || !ok6 {
			return tl, false
		}
		o.mult = math.Float32frombits(binary.BigEndian.Uint32(mb[:]))
		tl.offers = append(tl.offers, o)
	}
	if tl.level, ok = vi(); !ok {
		return tl, false
	}
	if tl.xp, ok = vi(); !ok {
		return tl, false
	}
	if tl.progress, ok = boolean(); !ok {
		return tl, false
	}
	if tl.restock, ok = boolean(); !ok {
		return tl, false
	}
	return tl, true
}

// results is what each offer sells, by index — what a trade request takes.
func (tl tradeList) results() []attach.ItemStack {
	out := make([]attach.ItemStack, len(tl.offers))
	for i, o := range tl.offers {
		out[i] = o.result
	}
	return out
}

// tradePacket renders the offers for Bedrock's trade screen between the
// player and the villager (Bedrock unique ids).
func tradePacket(tl tradeList, villager, player int64, title string) (*packet.UpdateTrade, error) {
	tier := tl.level - 1
	if tier < 0 {
		tier = 0
	}
	recipes := make([]map[string]any, 0, len(tl.offers))
	for i, o := range tl.offers {
		maxUses := o.maxUses
		if o.disabled {
			maxUses = 0
		}
		recipes = append(recipes, map[string]any{
			"netId":            int32(i + 1),
			"maxUses":          maxUses,
			"traderExp":        o.xp,
			"priceMultiplierA": o.mult,
			"priceMultiplierB": float32(0),
			"sell":             tradeItem(o.result, o.result.Count),
			"buyCountA":        max32(o.costA.Count, 0),
			"buyCountB":        max32(o.costB.Count, 0),
			"demand":           o.demand,
			"tier":             tier,
			"buyA":             tradeItem(o.costA, adjustedPrice(o)),
			"buyB":             tradeItem(o.costB, o.costB.Count),
			"uses":             o.uses,
			"rewardExp":        byte(1),
		})
	}
	offers := map[string]any{
		"Recipes": recipes,
		"TierExpRequirements": []map[string]any{
			{"0": int32(0)}, {"1": int32(10)}, {"2": int32(70)}, {"3": int32(150)}, {"4": int32(250)},
		},
	}
	data, err := nbt.MarshalEncoding(offers, nbt.NetworkLittleEndian)
	if err != nil {
		return nil, fmt.Errorf("trade offers: %w", err)
	}
	return &packet.UpdateTrade{
		WindowID:          byte(tl.window),
		WindowType:        byte(protocol.ContainerTypeTrade),
		TradeTier:         tier,
		VillagerUniqueID:  villager,
		EntityUniqueID:    player,
		DisplayName:       title,
		NewTradeUI:        true,
		DemandBasedPrices: true,
		SerialisedOffers:  data,
	}, nil
}

// adjustedPrice is vanilla's MerchantOffer.getCostA: the base count plus
// the demand and reputation adjustments, at least one.
func adjustedPrice(o tradeOffer) int32 {
	base := o.costA.Count
	adj := int32(float32(base) * float32(o.demand) * o.mult)
	if adj < 0 {
		adj = 0
	}
	count := base + adj + o.special
	if count < 1 {
		count = 1
	}
	if count > 64 {
		count = 64
	}
	return count
}

// tradeItem is an offer item in Bedrock's trade NBT: empty is the empty
// compound.
func tradeItem(st attach.ItemStack, count int32) map[string]any {
	if st.Count <= 0 || count <= 0 {
		return map[string]any{}
	}
	if st.ID <= 0 || int(st.ID) >= len(javaItemBedrock) || javaItemBedrock[st.ID].Name == "" {
		return map[string]any{}
	}
	ref := javaItemBedrock[st.ID]
	return map[string]any{
		"Count":  byte(count),
		"Damage": int16(ref.Data),
		"Name":   ref.Name,
	}
}

func max32(a, b int32) int32 {
	if a > b {
		return a
	}
	return b
}
