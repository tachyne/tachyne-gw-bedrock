package gw

import (
	"strings"

	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
	tproto "github.com/tachyne/tachyne-common/protocol"
)

// The enchanting table. Java describes the three rows as window
// properties (0-2 the level cost, 4-6 the hinted enchantment, 7-9 its
// level, 3 the glyph seed); Bedrock wants them as PlayerEnchantOptions,
// each row a recipe network id the client crafts with. Choosing a row is
// the Java button click (Enchant), which the world answers by enchanting
// the item in place and re-rolling.

// enchantRow is one of the table's three rows as the world describes it.
type enchantRow struct {
	cost    int32
	bedrock int32 // Bedrock enchantment id, -1 none / unknown
	level   int32 // -1 = no hint yet
	netID   uint32
}

// bedrockEnchantments is Bedrock's enchantment numbering, by Java name
// (the order Geyser's table establishes).
var bedrockEnchantments = func() map[string]int32 {
	names := []string{"protection", "fire_protection", "feather_falling", "blast_protection", "projectile_protection",
		"thorns", "respiration", "depth_strider", "aqua_affinity", "sharpness", "smite", "bane_of_arthropods", "knockback",
		"fire_aspect", "looting", "efficiency", "silk_touch", "unbreaking", "fortune", "power", "punch", "flame", "infinity",
		"luck_of_the_sea", "lure", "frost_walker", "mending", "binding_curse", "vanishing_curse", "impaling", "riptide",
		"loyalty", "channeling", "multishot", "piercing", "quick_charge", "soul_speed", "swift_sneak", "wind_burst",
		"density", "breach", "lunge"}
	m := map[string]int32{}
	for i, n := range names {
		m[n] = int32(i)
	}
	return m
}()

// bedrockEnchantInvalid marks an enchantment Bedrock has no number for
// (one past the table, as Geyser sends it).
var bedrockEnchantInvalid = int32(len(bedrockEnchantments) + 1)

// canonicalEnchantments is the engine's enchantment registry order.
var canonicalEnchantments = func() []string {
	for _, r := range tproto.SyncedRegistries {
		if r.ID == "minecraft:enchantment" {
			return r.Entries
		}
	}
	return nil
}()

// bedrockEnchantment renumbers a canonical enchantment id: -1 for none,
// the invalid marker for one Bedrock lacks.
func bedrockEnchantment(canonical int32) int32 {
	if canonical < 0 || int(canonical) >= len(canonicalEnchantments) {
		return -1
	}
	if id, ok := bedrockEnchantments[strings.TrimPrefix(canonicalEnchantments[canonical], "minecraft:")]; ok {
		return id
	}
	return bedrockEnchantInvalid
}

// enchantProp folds one window property into the rows; true when the
// row's level arrived, which is when the world has said everything about
// it and the options are worth resending.
func (m *invMirror) enchantProp(prop, value int32) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	switch {
	case prop >= 0 && prop <= 2:
		m.ench[prop].cost = value
	case prop >= 4 && prop <= 6:
		m.ench[prop-4].bedrock = bedrockEnchantment(value)
	case prop >= 7 && prop <= 9:
		m.ench[prop-7].level = value
		return true
	}
	return false
}

// enchantOptions renders the rows as Bedrock's options: the row's slot is
// 16 + its index, the hint sits in the "self" list, and a fresh network
// id names the row for the craft request that picks it.
func (m *invMirror) enchantOptions() *packet.PlayerEnchantOptions {
	m.mu.Lock()
	defer m.mu.Unlock()
	pk := &packet.PlayerEnchantOptions{}
	for i := range m.ench {
		r := &m.ench[i]
		opt := protocol.EnchantmentOption{Cost: uint8(max32(r.cost, 0)), Name: "nothing"}
		opt.Enchantments.Slot = int32(16 + i)
		if r.level >= 0 && r.cost > 0 {
			id := r.bedrock
			if id < 0 {
				id = bedrockEnchantInvalid
			}
			opt.Enchantments.Enchantments[1] = []protocol.EnchantmentInstance{{Type: byte(id), Level: byte(max32(r.level, 1))}}
			opt.Name = enchantName(id)
			r.netID = uint32(stackIDs.Add(1))
			opt.RecipeNetworkID = r.netID
		} else {
			r.netID = 0
		}
		pk.Options = append(pk.Options, opt)
	}
	return pk
}

// enchantName is the row's hover text (the client renders it in the
// glyph alphabet): the enchantment's own name.
func enchantName(bedrock int32) string {
	for n, id := range bedrockEnchantments {
		if id == bedrock {
			return strings.ReplaceAll(n, "_", " ")
		}
	}
	return "mystery"
}

// enchantButton is the Java button (row) a Bedrock craft request names.
func (m *invMirror) enchantButton(netID uint32) (int32, bool) {
	if netID == 0 {
		return 0, false
	}
	for i := range m.ench {
		if m.ench[i].netID == netID {
			return int32(i), true
		}
	}
	return 0, false
}
