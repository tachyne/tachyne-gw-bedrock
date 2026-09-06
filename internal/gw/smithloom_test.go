package gw

import (
	"strings"
	"testing"

	"github.com/sandertv/gophertunnel/minecraft/protocol"
	attach "github.com/tachyne/tachyne-common/attach"
	tproto "github.com/tachyne/tachyne-common/protocol"
)

// The upgrade table rides as transform recipes; a smithing request takes
// the transform's result (or the world's preview) through slot 3.
func TestSmithingTable(t *testing.T) {
	recipes := smithingRecipes()
	if len(recipes) != len(tproto.SmithingTransform) || netheriteIngot == 0 {
		t.Fatalf("%d transform recipes (table %d), ingot %d", len(recipes), len(tproto.SmithingTransform), netheriteIngot)
	}
	tr := recipes[0].(*protocol.SmithingTransformRecipe)
	if tr.Block != "smithing_table" || tr.RecipeNetworkID != smithBase || tr.Result.Count != 1 {
		t.Errorf("recipe %+v", tr)
	}
	base := sortedKeys(tproto.SmithingTransform)[0]
	w := &winState{}
	m := w.open(8, smithingLayout, protocol.ContainerTypeSmithingTable, "Smithing Table")
	if !m.smith || m.result != 3 {
		t.Fatalf("smithing window %v %d", m.smith, m.result)
	}
	m.set(0, attach.ItemStack{ID: tproto.SmithingUpgradeTemplate, Count: 1})
	m.set(1, attach.ItemStack{ID: base, Count: 1})
	m.set(2, attach.ItemStack{ID: netheriteIngot, Count: 2})
	req := protocol.ItemStackRequest{RequestID: 9, Actions: []protocol.StackRequestAction{
		&protocol.CraftRecipeStackRequestAction{RecipeNetworkID: smithBase},
		&protocol.ConsumeStackRequestAction{DestroyStackRequestAction: protocol.DestroyStackRequestAction{Count: 1, Source: slotInfo(protocol.ContainerSmithingTableTemplate, 53)}},
		&protocol.ConsumeStackRequestAction{DestroyStackRequestAction: protocol.DestroyStackRequestAction{Count: 1, Source: slotInfo(protocol.ContainerSmithingTableInput, 51)}},
		&protocol.ConsumeStackRequestAction{DestroyStackRequestAction: protocol.DestroyStackRequestAction{Count: 1, Source: slotInfo(protocol.ContainerSmithingTableMaterial, 52)}},
		takeAction(1, slotInfo(protocol.ContainerCreatedOutput, 50), slotInfo(protocol.ContainerCursor, 0)),
	}}
	_, steps, ok := m.applyRequest(req, nil)
	if !ok || len(steps) != 1 || steps[0].click == nil || steps[0].click.Slot != 3 {
		t.Fatalf("smith: ok=%v steps=%+v", ok, steps)
	}
	if m.slots[m.cursor].ID != tproto.SmithingTransform[base] || m.slots[2].Count != 1 || m.slots[0].Count != 0 {
		t.Errorf("mirror cursor %+v addition %+v template %+v", m.slots[m.cursor], m.slots[2], m.slots[0])
	}
}

// The loom: Bedrock's short pattern id becomes the row in the world's
// list, and the banner is taken through slot 3.
func TestLoom(t *testing.T) {
	base, byTag := tproto.LoomPatterns()
	if len(base) < 2 {
		t.Skip("no loom patterns")
	}
	want := base[1]
	name := ""
	for short, java := range bedrockBannerPatterns {
		if id, ok := bannerPatternID(java); ok && id == want {
			name = short
		}
	}
	if name == "" {
		t.Fatalf("no Bedrock id for pattern %d (%s)", want, tproto.BannerPatternName(want))
	}
	if b, ok := loomButton(name, 0); !ok || b != 1 {
		t.Errorf("button for %s = %d %v, want 1", name, b, ok)
	}
	if _, ok := loomButton("zzz", 0); ok {
		t.Error("an unknown pattern id was accepted")
	}
	if creeper, ok := bannerPatternID("creeper"); ok {
		var item int32
		for id, ref := range javaItemBedrock {
			if ref.Name == "minecraft:creeper_banner_pattern" {
				item = int32(id)
			}
		}
		if list := byTag["creeper"]; item != 0 && len(list) == 1 && list[0] == creeper {
			if b, ok := loomButton("cre", item); !ok || b != 0 {
				t.Errorf("creeper with its item → %d %v", b, ok)
			}
			if _, ok := loomButton("cre", 0); ok {
				t.Error("creeper selectable without its item")
			}
		}
	}
	w := &winState{}
	m := w.open(3, loomLayout, protocol.ContainerTypeLoom, "Loom")
	banner := mappedItem(t, 0)
	m.set(0, attach.ItemStack{ID: banner, Count: 3})
	m.set(1, attach.ItemStack{ID: mappedItem(t, 1), Count: 2})
	req := protocol.ItemStackRequest{RequestID: 10, Actions: []protocol.StackRequestAction{
		&protocol.CraftLoomRecipeStackRequestAction{Pattern: name, TimesCrafted: 1},
		&protocol.CraftResultsDeprecatedStackRequestAction{ResultItems: []protocol.ItemStack{{Count: 1}}, TimesCrafted: 1},
		&protocol.ConsumeStackRequestAction{DestroyStackRequestAction: protocol.DestroyStackRequestAction{Count: 1, Source: slotInfo(protocol.ContainerLoomInput, 9)}},
		&protocol.ConsumeStackRequestAction{DestroyStackRequestAction: protocol.DestroyStackRequestAction{Count: 1, Source: slotInfo(protocol.ContainerLoomDye, 10)}},
		takeAction(1, slotInfo(protocol.ContainerCreatedOutput, 50), slotInfo(protocol.ContainerCursor, 0)),
	}}
	_, steps, ok := m.applyRequest(req, nil)
	if !ok || len(steps) != 2 || steps[0].ench == nil || steps[0].ench.Button != 1 || steps[1].click == nil || steps[1].click.Slot != 3 {
		t.Fatalf("loom: ok=%v steps=%+v", ok, steps)
	}
	if m.slots[m.cursor].ID != banner || m.slots[0].Count != 2 || m.slots[1].Count != 1 {
		t.Errorf("loom mirror %+v %+v cursor %+v", m.slots[0], m.slots[1], m.slots[m.cursor])
	}
}

// Trim data names every pattern's template and every material's item and
// colour; the trim recipe is written in tags and takes the world's preview.
func TestArmorTrims(t *testing.T) {
	td := trimData()
	if len(td.Patterns) != 18 || len(td.Materials) != 11 {
		t.Fatalf("%d patterns, %d materials", len(td.Patterns), len(td.Materials))
	}
	for _, p := range td.Patterns {
		if p.ItemName == "minecraft:air" || !strings.HasSuffix(p.ItemName, p.PatternID+"_armor_trim_smithing_template") {
			t.Errorf("pattern %+v", p)
		}
	}
	for _, m := range td.Materials {
		if m.ItemName == "minecraft:air" || !strings.HasPrefix(m.Colour, "§") {
			t.Errorf("material %+v", m)
		}
	}
	r := trimRecipe()
	if _, ok := r.Base.Descriptor.(*protocol.ItemTagItemDescriptor); !ok || r.RecipeNetworkID != trimBase {
		t.Errorf("trim recipe %+v", r)
	}
	base := tproto.SmithingTrimmable[0]
	w := &winState{}
	m := w.open(8, smithingLayout, protocol.ContainerTypeSmithingTable, "Smithing Table")
	m.set(1, attach.ItemStack{ID: base, Count: 1})
	m.set(3, attach.ItemStack{ID: base, Count: 1}) // the world's trimmed preview
	req := protocol.ItemStackRequest{RequestID: 11, Actions: []protocol.StackRequestAction{
		&protocol.CraftRecipeStackRequestAction{RecipeNetworkID: trimBase},
		&protocol.ConsumeStackRequestAction{DestroyStackRequestAction: protocol.DestroyStackRequestAction{Count: 1, Source: slotInfo(protocol.ContainerSmithingTableInput, 51)}},
		takeAction(1, slotInfo(protocol.ContainerCreatedOutput, 50), slotInfo(protocol.ContainerCursor, 0)),
	}}
	if _, steps, ok := m.applyRequest(req, nil); !ok || len(steps) != 1 || steps[0].click.Slot != 3 {
		t.Fatalf("trim: ok=%v steps=%+v", ok, steps)
	}
}
