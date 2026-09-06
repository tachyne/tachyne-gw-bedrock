package gw

import (
	"fmt"
	"strings"

	"github.com/sandertv/gophertunnel/minecraft/protocol"
	attach "github.com/tachyne/tachyne-common/attach"
	tproto "github.com/tachyne/tachyne-common/protocol"
)

// The smithing table and the loom.
//
// Smithing: Bedrock crafts from smithing recipes it is given, so the
// shared upgrade table (diamond gear + the netherite template + an ingot)
// rides in the crafting data as transform recipes; the table's four slots
// live in the UI window. A craft request naming a recipe takes whatever
// the world previewed (it carries the base's wear, enchantments and
// name), through the slot-3 click. Armor trims wait on Bedrock's trim
// data packet.
//
// Loom: Bedrock's loom lists its own patterns and names the chosen one by
// its short id in a CraftLoom request; that becomes the world's button
// click on the pattern's row in the list the world offers (the base list,
// or a pattern item's), then the banner is taken through the slot-3
// click.

// smithBase keeps the smithing recipes' network ids clear of the rest.
const smithBase = 1 << 21

// smithRecipes is the shared upgrade table by network id: the result of
// each transform.
var smithRecipes = func() map[uint32]attach.ItemStack {
	m := map[uint32]attach.ItemStack{}
	i := 0
	for _, base := range sortedKeys(tproto.SmithingTransform) {
		m[uint32(smithBase+i)] = attach.ItemStack{ID: tproto.SmithingTransform[base], Count: 1}
		i++
	}
	return m
}()

// netheriteIngot is the canonical id of the upgrade's addition.
var netheriteIngot = func() int32 {
	for id, ref := range javaItemBedrock {
		if ref.Name == "minecraft:netherite_ingot" {
			return int32(id)
		}
	}
	return 0
}()

func sortedKeys(m map[int32]int32) []int32 {
	keys := make([]int32, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	for i := 1; i < len(keys); i++ { // small table: insertion sort keeps this dependency-free
		for j := i; j > 0 && keys[j-1] > keys[j]; j-- {
			keys[j-1], keys[j] = keys[j], keys[j-1]
		}
	}
	return keys
}

// smithingRecipes renders the upgrade table as Bedrock transform recipes.
func smithingRecipes() []protocol.Recipe {
	var out []protocol.Recipe
	tmpl, ok1 := descriptor(tproto.SmithingUpgradeTemplate)
	add, ok2 := descriptor(netheriteIngot)
	if !ok1 || !ok2 || netheriteIngot == 0 {
		return nil
	}
	for i, base := range sortedKeys(tproto.SmithingTransform) {
		b, ok := descriptor(base)
		res, ok3 := bedrockStackOf(attach.ItemStack{ID: tproto.SmithingTransform[base], Count: 1})
		if !ok || !ok3 {
			continue
		}
		out = append(out, &protocol.SmithingTransformRecipe{
			RecipeNetworkID: uint32(smithBase + i), RecipeID: fmt.Sprintf("tachyne:smithing/%d", i),
			Template: tmpl, Base: b, Addition: add, Result: res, Block: "smithing_table",
		})
	}
	return out
}

// bedrockBannerPatterns is Bedrock's short pattern id → the Java pattern
// name (the pairs Geyser's table establishes).
var bedrockBannerPatterns = map[string]string{
	"b": "base", "bl": "square_bottom_left", "br": "square_bottom_right", "tl": "square_top_left", "tr": "square_top_right",
	"bs": "stripe_bottom", "ts": "stripe_top", "ls": "stripe_left", "rs": "stripe_right", "cs": "stripe_center",
	"ms": "stripe_middle", "drs": "stripe_downright", "dls": "stripe_downleft", "ss": "small_stripes", "cr": "cross",
	"sc": "straight_cross", "bt": "triangle_bottom", "tt": "triangle_top", "bts": "triangles_bottom", "tts": "triangles_top",
	"ld": "diagonal_left", "rd": "diagonal_up_right", "lud": "diagonal_up_left", "rud": "diagonal_right", "mc": "circle",
	"mr": "rhombus", "vh": "half_vertical", "hh": "half_horizontal", "vhr": "half_vertical_right", "hhb": "half_horizontal_bottom",
	"bo": "border", "cbo": "curly_border", "gra": "gradient", "gru": "gradient_up", "bri": "bricks", "glb": "globe",
	"cre": "creeper", "sku": "skull", "flo": "flower", "moj": "mojang", "pig": "piglin", "flw": "flow", "gus": "guster",
}

// bannerPatternID is a Java pattern name's canonical registry id.
func bannerPatternID(name string) (int32, bool) {
	for _, reg := range tproto.SyncedRegistries {
		if reg.ID == "minecraft:banner_pattern" {
			for i, e := range reg.Entries {
				if e == "minecraft:"+name {
					return int32(i), true
				}
			}
		}
	}
	return 0, false
}

// loomButton is the row of a Bedrock pattern in the list the world offers
// for the loom's current pattern item (0 = none).
func loomButton(bedrockPattern string, patternItem int32) (int32, bool) {
	name, ok := bedrockBannerPatterns[bedrockPattern]
	if !ok {
		return 0, false
	}
	id, ok := bannerPatternID(name)
	if !ok {
		return 0, false
	}
	list, byTag := tproto.LoomPatterns()
	if patternItem > 0 && int(patternItem) < len(javaItemBedrock) {
		item := strings.TrimPrefix(javaItemBedrock[patternItem].Name, "minecraft:")
		if strings.HasSuffix(item, "_banner_pattern") {
			list = byTag[strings.TrimSuffix(item, "_banner_pattern")]
		}
	}
	for i, p := range list {
		if p == id {
			return int32(i), true
		}
	}
	return 0, false
}
