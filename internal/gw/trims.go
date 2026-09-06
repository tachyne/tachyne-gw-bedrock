package gw

import (
	"strings"

	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
	tproto "github.com/tachyne/tachyne-common/protocol"
)

// Armor trims. Bedrock learns the trim patterns and materials from a
// TrimData packet (each pattern with its template item, each material
// with its item and text colour) and applies them through one smithing
// trim recipe written in item tags, the way BDS and Geyser send it. The
// world computes the trimmed piece; the request takes its preview.

// trimBase is the trim recipe's network id.
const trimBase = 1 << 22

// trimColours are Bedrock's text colour codes for the trim materials.
var trimColours = map[string]string{
	"amethyst": "§u", "copper": "§n", "diamond": "§s", "emerald": "§q", "gold": "§p", "iron": "§i",
	"lapis": "§t", "netherite": "§j", "quartz": "§h", "redstone": "§m", "resin": "§v",
}

// registryNames is a canonical registry's entries without the namespace.
func registryNames(id string) []string {
	for _, r := range tproto.SyncedRegistries {
		if r.ID == id {
			out := make([]string, len(r.Entries))
			for i, e := range r.Entries {
				out[i] = strings.TrimPrefix(e, "minecraft:")
			}
			return out
		}
	}
	return nil
}

// trimData is the patterns and materials the engine knows, with the item
// each rides on (from the shared smithing tables).
func trimData() *packet.TrimData {
	pk := &packet.TrimData{}
	itemFor := func(table map[int32]int32, want int32) string {
		for item, id := range table {
			if id == want && int(item) < len(javaItemBedrock) {
				return javaItemBedrock[item].Name
			}
		}
		return "minecraft:air"
	}
	for i, name := range registryNames("minecraft:trim_pattern") {
		pk.Patterns = append(pk.Patterns, protocol.TrimPattern{ItemName: itemFor(tproto.SmithingTrimTemplate, int32(i)), PatternID: name})
	}
	for i, name := range registryNames("minecraft:trim_material") {
		colour := trimColours[name]
		if colour == "" {
			colour = "§f"
		}
		pk.Materials = append(pk.Materials, protocol.TrimMaterial{MaterialID: name, Colour: colour, ItemName: itemFor(tproto.SmithingTrimMaterial, int32(i))})
	}
	return pk
}

// trimRecipe is the one smithing trim recipe, in item tags.
func trimRecipe() *protocol.SmithingTrimRecipe {
	tag := func(t string) protocol.ItemDescriptorCount {
		return protocol.ItemDescriptorCount{Descriptor: &protocol.ItemTagItemDescriptor{Tag: t}, Count: 1}
	}
	return &protocol.SmithingTrimRecipe{
		RecipeNetworkID: trimBase, RecipeID: "minecraft:smithing_armor_trim",
		Template: tag("minecraft:trim_templates"), Base: tag("minecraft:trimmable_armors"), Addition: tag("minecraft:trim_materials"),
		Block: "smithing_table",
	}
}
