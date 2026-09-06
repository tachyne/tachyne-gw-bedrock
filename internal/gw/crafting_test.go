package gw

import (
	"testing"

	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
	attach "github.com/tachyne/tachyne-common/attach"
	tproto "github.com/tachyne/tachyne-common/protocol"
)

// mappedItem is some canonical item with a Bedrock counterpart.
func mappedItem(t *testing.T, skip int) int32 {
	for id := int32(1); int(id) < len(javaItemBedrock); id++ {
		if _, _, ok := bedrockItemIDs(id); ok {
			if skip == 0 {
				return id
			}
			skip--
		}
	}
	t.Fatal("no mapped item")
	return 0
}

// The recipe book becomes CraftingData with the book ids as network ids;
// a craft request consumes the grid and takes the result through two
// world clicks per result.
func TestCraftingBridge(t *testing.T) {
	plank, stick := mappedItem(t, 0), mappedItem(t, 1)
	rs := newRecipeSet()
	rs.add(attach.RecipeBook{Replace: true,
		Shaped:    []attach.ShapedRecipe{{ID: 0, W: 1, H: 2, Cells: []int32{plank, plank}, Result: stick, Count: 4}},
		Shapeless: []attach.ShapelessRecipe{{ID: 1, Ingredients: []int32{plank}, Result: stick, Count: 1}}})
	pk := rs.packet()
	if !pk.ClearRecipes || len(pk.Recipes) != 2+len(tproto.StonecuttingRecipes) { // the book, then the stonecutter's
		t.Fatalf("crafting data: %d recipes", len(pk.Recipes))
	}
	if sr, ok := pk.Recipes[0].(*protocol.ShapedRecipe); !ok || sr.RecipeNetworkID != 1 || len(sr.Input) != 2 || sr.Output[0].Count != 4 {
		t.Errorf("shaped %+v", pk.Recipes[0])
	}
	if _, ok := rs.output(2); !ok {
		t.Error("shapeless recipe missing")
	}

	// A manual craft in the 2x2 grid: two planks placed at grid slots 28
	// and 29 (Java 1 and 2), the recipe named, the grid consumed, four
	// sticks taken to hotbar slot 3 (Java 39).
	m := newInvMirror()
	m.set(1, attach.ItemStack{ID: plank, Count: 3})
	m.set(2, attach.ItemStack{ID: plank, Count: 1})
	req := protocol.ItemStackRequest{RequestID: 7, Actions: []protocol.StackRequestAction{
		&protocol.CraftRecipeStackRequestAction{RecipeNetworkID: 1, NumberOfCrafts: 1},
		&protocol.CraftResultsDeprecatedStackRequestAction{ResultItems: []protocol.ItemStack{{Count: 4}}, TimesCrafted: 1},
		&protocol.ConsumeStackRequestAction{DestroyStackRequestAction: protocol.DestroyStackRequestAction{Count: 1, Source: slotInfo(protocol.ContainerCraftingInput, 28)}},
		&protocol.ConsumeStackRequestAction{DestroyStackRequestAction: protocol.DestroyStackRequestAction{Count: 1, Source: slotInfo(protocol.ContainerCraftingInput, 29)}},
		placeAction(4, slotInfo(protocol.ContainerCreatedOutput, 50), slotInfo(protocol.ContainerHotBar, 3)),
	}}
	changed, steps, ok := m.applyRequest(req, rs)
	if !ok || len(steps) != 2 || steps[0].click == nil || steps[0].click.Slot != 0 || steps[1].click == nil || steps[1].click.Slot != 39 {
		t.Fatalf("manual craft: ok=%v steps=%+v", ok, steps)
	}
	if m.slots[1].Count != 2 || m.slots[2].Count != 0 || m.slots[39].ID != stick || m.slots[39].Count != 4 || len(changed) != 3 {
		t.Errorf("mirror after craft: grid %+v %+v dest %+v changed %v", m.slots[1], m.slots[2], m.slots[39], changed)
	}
	if steps[1].click.Changed[0].Item.Count != 4 {
		t.Errorf("destination click %+v", steps[1].click)
	}
	var w capture
	respondStackRequest(&w, 7, m, changed, ok)
	if len(w.pkts) != 1 || w.pkts[0].(*packet.ItemStackResponse).Responses[0].Status != stackResponseOK {
		t.Errorf("response %+v", w.pkts)
	}

	// The recipe book's auto-craft places the recipe first, twice for two
	// results, and lands them on the cursor.
	m2 := newInvMirror()
	m2.set(9, attach.ItemStack{ID: plank, Count: 2})
	req2 := protocol.ItemStackRequest{RequestID: 8, Actions: []protocol.StackRequestAction{
		&protocol.AutoCraftRecipeStackRequestAction{RecipeNetworkID: 2, TimesCrafted: 2},
		&protocol.ConsumeStackRequestAction{DestroyStackRequestAction: protocol.DestroyStackRequestAction{Count: 2, Source: slotInfo(protocol.ContainerInventory, 9)}},
		takeAction(2, slotInfo(protocol.ContainerCreatedOutput, 50), slotInfo(protocol.ContainerCursor, 0)),
	}}
	_, steps2, ok := m2.applyRequest(req2, rs)
	if !ok || len(steps2) != 4 || steps2[0].place == nil || steps2[0].place.Recipe != 1 || steps2[1].click == nil || steps2[2].place == nil {
		t.Fatalf("auto craft: ok=%v steps=%+v", ok, steps2)
	}
	if m2.slots[m2.cursor].Count != 2 || m2.slots[9].Count != 0 {
		t.Errorf("auto mirror %+v %+v", m2.slots[m2.cursor], m2.slots[9])
	}

	// Refusals: an unknown recipe, a count that is not whole results.
	bad := protocol.ItemStackRequest{Actions: []protocol.StackRequestAction{
		&protocol.CraftRecipeStackRequestAction{RecipeNetworkID: 99}}}
	if _, _, ok := newInvMirror().applyRequest(bad, rs); ok {
		t.Error("unknown recipe accepted")
	}
	req.Actions[4] = placeAction(3, slotInfo(protocol.ContainerCreatedOutput, 50), slotInfo(protocol.ContainerHotBar, 3))
	m3 := newInvMirror()
	m3.set(1, attach.ItemStack{ID: plank, Count: 1})
	m3.set(2, attach.ItemStack{ID: plank, Count: 1})
	if _, _, ok := m3.applyRequest(req, rs); ok || m3.slots[1].Count != 1 {
		t.Error("partial result accepted or mirror not restored")
	}

	// The crafting table window maps its grid and result into the UI window.
	cm := newWindowMirror(5, craftingLayout)
	if j, ok := cm.mapIn(protocol.ContainerCraftingInput, 32); !ok || j != 1 {
		t.Errorf("table grid → %d %v", j, ok)
	}
	if j, ok := cm.mapIn(protocol.ContainerCreatedOutput, 50); !ok || j != 0 {
		t.Errorf("table result → %d %v", j, ok)
	}
	var w2 capture
	sendWindowSlot(&w2, cm, 3, attach.ItemStack{ID: plank, Count: 1})
	if is, ok := w2.pkts[0].(*packet.InventorySlot); !ok || is.WindowID != protocol.WindowIDUI || is.Slot != 34 {
		t.Errorf("table slot render %+v", w2.pkts[0])
	}
	if mw := menuWindows[12]; mw.ctype != protocol.ContainerTypeWorkbench || len(mw.layout) != 10 {
		t.Error("crafting menu")
	}
}

// The anvil and grindstone craft whatever the world previewed in slot 2:
// a rename rides ahead of the clicks, the inputs are consumed, the
// result taken through the created-output name.
func TestAnvilAndGrindstone(t *testing.T) {
	sword, iron := mappedItem(t, 0), mappedItem(t, 1)
	one := func(id int32) attach.ItemStack { return attach.ItemStack{ID: id, Count: 1} }
	m := newWindowMirror(9, anvilLayout)
	if m.result != 2 {
		t.Fatalf("anvil result slot %d", m.result)
	}
	m.set(0, one(sword))
	m.set(1, attach.ItemStack{ID: iron, Count: 3})
	m.set(2, one(sword))
	req := protocol.ItemStackRequest{RequestID: 3, FilterStrings: []string{"Excalibur"}, Actions: []protocol.StackRequestAction{
		&protocol.CraftRecipeOptionalStackRequestAction{FilterStringIndex: 0},
		&protocol.CraftResultsDeprecatedStackRequestAction{ResultItems: []protocol.ItemStack{{Count: 1}}, TimesCrafted: 1},
		&protocol.ConsumeStackRequestAction{DestroyStackRequestAction: protocol.DestroyStackRequestAction{Count: 1, Source: slotInfo(protocol.ContainerAnvilInput, 1)}},
		&protocol.ConsumeStackRequestAction{DestroyStackRequestAction: protocol.DestroyStackRequestAction{Count: 1, Source: slotInfo(protocol.ContainerAnvilMaterial, 2)}},
		takeAction(1, slotInfo(protocol.ContainerCreatedOutput, 50), slotInfo(protocol.ContainerCursor, 0)),
	}}
	changed, steps, ok := m.applyRequest(req, nil)
	if !ok || len(steps) != 2 || steps[0].name == nil || *steps[0].name != "Excalibur" || steps[1].click == nil || steps[1].click.Slot != 2 {
		t.Fatalf("anvil: ok=%v steps=%+v", ok, steps)
	}
	if m.slots[0].Count != 0 || m.slots[1].Count != 2 || m.slots[m.cursor].ID != sword || len(changed) != 3 {
		t.Errorf("anvil mirror %+v %+v cursor %+v", m.slots[0], m.slots[1], m.slots[m.cursor])
	}
	if c, idx, ok := m.mapOut(1); !ok || c != protocol.ContainerAnvilMaterial || idx != 2 {
		t.Errorf("anvil material → (%d,%d) %v", c, idx, ok)
	}

	g := newWindowMirror(10, grindstoneLayout)
	g.set(0, one(sword))
	g.set(2, one(sword))
	greq := protocol.ItemStackRequest{RequestID: 4, Actions: []protocol.StackRequestAction{
		&protocol.CraftGrindstoneRecipeStackRequestAction{},
		&protocol.ConsumeStackRequestAction{DestroyStackRequestAction: protocol.DestroyStackRequestAction{Count: 1, Source: slotInfo(protocol.ContainerGrindstoneInput, 16)}},
		placeAction(1, slotInfo(protocol.ContainerCreatedOutput, 50), slotInfo(protocol.ContainerHotBar, 0)),
	}}
	if _, steps, ok := g.applyRequest(greq, nil); !ok || len(steps) != 2 || steps[0].click.Slot != 2 || steps[1].click.Slot != 3+27 {
		t.Fatalf("grindstone: ok=%v steps=%+v", ok, steps)
	}
	// No preview yet: nothing to take.
	e := newWindowMirror(11, grindstoneLayout)
	e.set(0, one(sword))
	if _, _, ok := e.applyRequest(greq, nil); ok {
		t.Error("took a result the world never previewed")
	}
	if mw := menuWindows[8]; mw.ctype != protocol.ContainerTypeAnvil || len(mw.layout) != 3 {
		t.Error("anvil menu")
	}
}

// The stonecutter: its recipes ride in the crafting data under the
// stonecutter block, and cutting picks the world's row before the take.
func TestStonecutter(t *testing.T) {
	if len(tproto.StonecuttingRecipes) == 0 {
		t.Skip("no stonecutting table")
	}
	rs := newRecipeSet()
	pk := rs.packet()
	var cut *protocol.ShapelessRecipe
	for _, r := range pk.Recipes {
		if sr, ok := r.(*protocol.ShapelessRecipe); ok && sr.Block == "stonecutter" {
			cut = sr
			break
		}
	}
	if cut == nil || cut.RecipeNetworkID < stonecutBase || len(cut.Input) != 1 {
		t.Fatalf("stonecutter recipes missing: %+v", cut)
	}
	e, ok := stonecutRecipes[cut.RecipeNetworkID]
	if !ok {
		t.Fatal("recipe not indexed")
	}
	// The second recipe for the same input is button 1.
	first := tproto.StonecuttingRecipes[0]
	second := -1
	for i := 1; i < len(tproto.StonecuttingRecipes); i++ {
		if tproto.StonecuttingRecipes[i].In == first.In {
			second = i
			break
		}
	}
	if second > 0 && stonecutRecipes[uint32(stonecutBase+second)].button != 1 {
		t.Errorf("row order: %+v", stonecutRecipes[uint32(stonecutBase+second)])
	}

	w := &winState{}
	m := w.open(6, stonecutterLayout, protocol.ContainerTypeStonecutter, "Stonecutter")
	if !m.cutter || m.result != 1 {
		t.Fatalf("stonecutter window %v %d", m.cutter, m.result)
	}
	m.set(0, attach.ItemStack{ID: first.In, Count: 2})
	req := protocol.ItemStackRequest{RequestID: 2, Actions: []protocol.StackRequestAction{
		&protocol.CraftRecipeStackRequestAction{RecipeNetworkID: cut.RecipeNetworkID, NumberOfCrafts: 1},
		&protocol.ConsumeStackRequestAction{DestroyStackRequestAction: protocol.DestroyStackRequestAction{Count: 1, Source: slotInfo(protocol.ContainerStonecutterInput, 3)}},
		takeAction(byte(e.result.Count), slotInfo(protocol.ContainerCreatedOutput, 50), slotInfo(protocol.ContainerCursor, 0)),
	}}
	_, steps, ok := m.applyRequest(req, rs)
	if !ok || len(steps) != 2 || steps[0].ench == nil || steps[0].ench.Button != e.button || steps[1].click == nil || steps[1].click.Slot != 1 {
		t.Fatalf("cut: ok=%v steps=%+v", ok, steps)
	}
	if m.slots[0].Count != 1 || m.slots[m.cursor].ID != e.result.ID {
		t.Errorf("mirror %+v cursor %+v", m.slots[0], m.slots[m.cursor])
	}
}
