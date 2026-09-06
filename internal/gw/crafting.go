package gw

import (
	"fmt"
	"sync"

	tproto "github.com/tachyne/tachyne-common/protocol"

	"github.com/google/uuid"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
	attach "github.com/tachyne/tachyne-common/attach"
)

// Crafting. A Bedrock client crafts from the recipes it is given: the grid
// is a UI container it fills by ordinary transfers, and a craft is a
// request naming the recipe, consuming the grid and taking the result. The
// world crafts by its own matching, so the bridge is two Java clicks per
// result: the result slot (the world moves the result to the cursor and
// consumes the grid) and the destination. The recipe book's auto-craft
// adds the world's own place-recipe step first, so its grid gets filled
// from the inventory the way the Java book does. The recipes themselves
// come from the world's recipe book, renumbered as Bedrock network ids.

// recipeSet is what the client has been told it can craft.
type recipeSet struct {
	mu        sync.RWMutex // written by the world pump, read by client requests
	shaped    []attach.ShapedRecipe
	shapeless []attach.ShapelessRecipe
	outputs   map[uint32]craftOutput // by Bedrock network id
}

// stonecutBase keeps the stonecutter's network ids clear of the book's.
const stonecutBase = 1 << 20

// stonecutEntry is one stonecutting recipe as the world's menu orders it:
// the button is its row among the recipes for the same input.
type stonecutEntry struct {
	button int32
	result attach.ItemStack
}

// stonecutRecipes is the shared stonecutting table by network id, the
// button being each recipe's row within its input's list (the order the
// world's stonecutter menu shows them).
var stonecutRecipes = func() map[uint32]stonecutEntry {
	m := map[uint32]stonecutEntry{}
	rows := map[int32]int32{}
	for i, r := range tproto.StonecuttingRecipes {
		m[uint32(stonecutBase+i)] = stonecutEntry{button: rows[r.In], result: attach.ItemStack{ID: r.Out, Count: int32(r.Count)}}
		rows[r.In]++
	}
	return m
}()

type craftOutput struct {
	bookID int32 // the world's display id (the auto-craft frame names it)
	result attach.ItemStack
}

func newRecipeSet() *recipeSet { return &recipeSet{outputs: map[uint32]craftOutput{}} }

// networkID is the Bedrock recipe network id for a book display id (0 is
// not a valid network id).
func networkID(bookID int32) uint32 { return uint32(bookID) + 1 }

// add folds a recipe book frame in (Replace = the whole book anew).
func (r *recipeSet) add(b attach.RecipeBook) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if b.Replace {
		r.shaped, r.shapeless = nil, nil
		r.outputs = map[uint32]craftOutput{}
	}
	for _, s := range b.Shaped {
		r.shaped = append(r.shaped, s)
		r.outputs[networkID(s.ID)] = craftOutput{s.ID, attach.ItemStack{ID: s.Result, Count: s.Count}}
	}
	for _, s := range b.Shapeless {
		r.shapeless = append(r.shapeless, s)
		r.outputs[networkID(s.ID)] = craftOutput{s.ID, attach.ItemStack{ID: s.Result, Count: s.Count}}
	}
}

// packet renders the whole set as CraftingData (Bedrock replaces, never
// appends, so every update carries everything).
func (r *recipeSet) packet() *packet.CraftingData {
	r.mu.RLock()
	defer r.mu.RUnlock()
	pk := &packet.CraftingData{ClearRecipes: true}
	for _, s := range r.shaped {
		out, ok := bedrockStackOf(attach.ItemStack{ID: s.Result, Count: s.Count})
		if !ok {
			continue
		}
		in := make([]protocol.ItemDescriptorCount, 0, len(s.Cells))
		usable := true
		for _, c := range s.Cells {
			d, ok := descriptor(c)
			usable = usable && ok
			in = append(in, d)
		}
		if !usable {
			continue
		}
		pk.Recipes = append(pk.Recipes, &protocol.ShapedRecipe{
			RecipeID: fmt.Sprintf("tachyne:%d", s.ID), Width: s.W, Height: s.H,
			Input: in, Output: []protocol.ItemStack{out}, UUID: recipeUUID(s.ID),
			Block: "crafting_table", AssumeSymmetry: true,
			UnlockRequirement: protocol.RecipeUnlockRequirement{Context: protocol.RecipeUnlockContextAlwaysUnlocked},
			RecipeNetworkID:   networkID(s.ID),
		})
	}
	for _, s := range r.shapeless {
		out, ok := bedrockStackOf(attach.ItemStack{ID: s.Result, Count: s.Count})
		if !ok {
			continue
		}
		in := make([]protocol.ItemDescriptorCount, 0, len(s.Ingredients))
		usable := true
		for _, c := range s.Ingredients {
			d, ok := descriptor(c)
			usable = usable && ok && c != 0
			in = append(in, d)
		}
		if !usable {
			continue
		}
		pk.Recipes = append(pk.Recipes, &protocol.ShapelessRecipe{
			RecipeID: fmt.Sprintf("tachyne:%d", s.ID),
			Input:    in, Output: []protocol.ItemStack{out}, UUID: recipeUUID(s.ID),
			Block:             "crafting_table",
			UnlockRequirement: protocol.RecipeUnlockRequirement{Context: protocol.RecipeUnlockContextAlwaysUnlocked},
			RecipeNetworkID:   networkID(s.ID),
		})
	}
	pk.Recipes = append(pk.Recipes, smithingRecipes()...)
	pk.Recipes = append(pk.Recipes, trimRecipe())
	for i, r := range tproto.StonecuttingRecipes { // the stonecutter's, as one-input shapeless recipes
		out, ok := bedrockStackOf(attach.ItemStack{ID: r.Out, Count: int32(r.Count)})
		d, ok2 := descriptor(r.In)
		if !ok || !ok2 {
			continue
		}
		pk.Recipes = append(pk.Recipes, &protocol.ShapelessRecipe{
			RecipeID: fmt.Sprintf("tachyne:stonecutter/%d", i),
			Input:    []protocol.ItemDescriptorCount{d}, Output: []protocol.ItemStack{out}, UUID: recipeUUID(int32(stonecutBase + i)),
			Block:             "stonecutter",
			UnlockRequirement: protocol.RecipeUnlockRequirement{Context: protocol.RecipeUnlockContextAlwaysUnlocked},
			RecipeNetworkID:   uint32(stonecutBase + i),
		})
	}
	return pk
}

// output is the recipe behind a Bedrock network id.
func (r *recipeSet) output(id uint32) (craftOutput, bool) {
	if r == nil {
		return craftOutput{}, false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	o, ok := r.outputs[id]
	return o, ok
}

// descriptor is one recipe cell: the item, or the empty descriptor for 0.
func descriptor(item int32) (protocol.ItemDescriptorCount, bool) {
	if item == 0 {
		return protocol.ItemDescriptorCount{Descriptor: &protocol.InvalidItemDescriptor{}}, true
	}
	rid, data, ok := bedrockItemIDs(item)
	if !ok {
		return protocol.ItemDescriptorCount{Descriptor: &protocol.InvalidItemDescriptor{}}, false
	}
	return protocol.ItemDescriptorCount{
		Descriptor: &protocol.DefaultItemDescriptor{NetworkID: int16(rid), MetadataValue: int16(data)},
		Count:      1,
	}, true
}

// bedrockStackOf is bedrockStack without a stack network id, as recipe
// outputs are described.
func bedrockStackOf(st attach.ItemStack) (protocol.ItemStack, bool) {
	inst := bedrockStack(st)
	if inst.Stack.Count == 0 {
		return protocol.ItemStack{}, false
	}
	return inst.Stack, true
}

func recipeUUID(id int32) uuid.UUID {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(fmt.Sprintf("tachyne-recipe-%d", id)))
}

// craftStep is one thing the world must do for a craft request, in order:
// place a book recipe into the grid, or a window click.
type craftStep struct {
	place    *attach.Craft
	click    *attach.WindowClick
	name     *string              // the anvil's rename box
	sel      *attach.SelTrade     // the trade screen's chosen offer
	ench     *attach.Enchant      // the enchanting table's chosen row, or the stonecutter's
	beacon   *attach.SetBeacon    // the beacon's chosen effects
	creative *attach.CreativeSlot // a creative-mode slot set (or drop, slot -1)
}

// applyCraft resolves a craft request against the mirror (held locked):
// the recipe named, the grid (or, auto-crafting, the inventory) consumed,
// the results taken. Returns the Java slots it changed and the world's
// steps; false refuses the whole request.
func (m *invMirror) applyCraft(req protocol.ItemStackRequest, recipes *recipeSet) ([]int32, []craftStep, bool) {
	before := append([]attach.ItemStack(nil), m.slots...)
	touched := map[int32]bool{}
	var steps []craftStep
	var out craftOutput
	auto, named := false, false
	fail := func() ([]int32, []craftStep, bool) {
		m.slots = before
		return nil, nil, false
	}
	// A menu with its own result slot (anvil, grindstone) crafts whatever
	// the world previewed there; the recipe named means nothing to it.
	preview := func() bool {
		if named || m.result <= 0 || m.slots[m.result].Count <= 0 {
			return false
		}
		out, named = craftOutput{result: m.slots[m.result]}, true
		return true
	}
	for _, a := range req.Actions {
		switch act := a.(type) {
		case *protocol.CraftRecipeOptionalStackRequestAction: // the anvil: maybe a rename
			if m.result != 2 || !preview() {
				return fail()
			}
			if i := int(act.FilterStringIndex); i >= 0 && i < len(req.FilterStrings) {
				name := req.FilterStrings[i]
				steps = append(steps, craftStep{name: &name})
			}
		case *protocol.CraftGrindstoneRecipeStackRequestAction:
			if m.result != 2 || !preview() {
				return fail()
			}
		case *protocol.CraftLoomRecipeStackRequestAction: // the loom: the pattern's row, then the banner
			if !m.loom || named || m.slots[0].Count <= 0 {
				return fail()
			}
			button, ok := loomButton(act.Pattern, m.slots[2].ID)
			if !ok {
				return fail()
			}
			out, named = craftOutput{result: attach.ItemStack{ID: m.slots[0].ID, Count: 1}}, true
			steps = append(steps, craftStep{ench: &attach.Enchant{Button: button}})
		case *protocol.CraftRecipeStackRequestAction:
			if m.table { // the enchanting table: the row is a button, the world does the rest
				button, ok := m.enchantButton(act.RecipeNetworkID)
				if !ok || named {
					return fail()
				}
				m.slots = before
				return nil, []craftStep{{ench: &attach.Enchant{Button: button}}}, true
			}
			if m.smith { // the smithing table: the transform's result, or the world's preview
				if named {
					return fail()
				}
				if res, ok := smithRecipes[act.RecipeNetworkID]; ok {
					out, named = craftOutput{result: res}, true
				} else if !preview() {
					return fail()
				}
				continue
			}
			if m.cutter { // the stonecutter: the recipe picks the row, the row's result is taken
				e, ok := stonecutRecipes[act.RecipeNetworkID]
				if named || !ok {
					return fail()
				}
				out, named = craftOutput{result: e.result}, true
				steps = append(steps, craftStep{ench: &attach.Enchant{Button: e.button}})
				continue
			}
			if m.trades != nil { // a trade screen: the offer by index
				idx := int(act.RecipeNetworkID) - 1
				if named || idx < 0 || idx >= len(m.trades) || m.trades[idx].Count <= 0 {
					return fail()
				}
				out, named = craftOutput{result: m.trades[idx]}, true
				steps = append(steps, craftStep{sel: &attach.SelTrade{Slot: int32(idx)}})
				continue
			}
			if m.result > 0 { // any other result-slot menu (cartography): the world's preview
				if !preview() {
					return fail()
				}
				continue
			}
			o, ok := recipes.output(act.RecipeNetworkID)
			if named || !ok {
				return fail()
			}
			out, named = o, true
		case *protocol.AutoCraftRecipeStackRequestAction:
			o, ok := recipes.output(act.RecipeNetworkID)
			if named || !ok || m.result != 0 {
				return fail()
			}
			out, named, auto = o, true, true
		case *protocol.CraftResultsDeprecatedStackRequestAction:
			// The client's own idea of the result; the recipe already says.
		case *protocol.ConsumeStackRequestAction:
			slot, ok := m.mapIn(act.Source.Container.ContainerID, act.Source.Slot)
			if !named || !ok || slot == m.cursor || slot == m.result {
				return fail()
			}
			st := m.slots[slot]
			if st.Count < int32(act.Count) {
				return fail()
			}
			st.Count -= int32(act.Count)
			if st.Count == 0 {
				st = attach.ItemStack{}
			}
			m.slots[slot] = st
			touched[slot] = true
		case *protocol.TakeStackRequestAction:
			if !m.takeResult(act.Source, act.Destination, int32(act.Count), out, auto, named, touched, &steps) {
				return fail()
			}
		case *protocol.PlaceStackRequestAction:
			if !m.takeResult(act.Source, act.Destination, int32(act.Count), out, auto, named, touched, &steps) {
				return fail()
			}
		default:
			return fail()
		}
	}
	if len(steps) == 0 {
		return fail()
	}
	changed := make([]int32, 0, len(touched))
	for slot := range touched {
		changed = append(changed, slot)
	}
	return changed, steps, true
}

// takeResult moves count crafted items from the created output to dest:
// per result, the world takes the result slot to its cursor and puts the
// cursor down on dest (or keeps it, when dest is the cursor).
func (m *invMirror) takeResult(src, dst protocol.StackRequestSlotInfo, count int32, out craftOutput,
	auto, named bool, touched map[int32]bool, steps *[]craftStep) bool {
	if !named || src.Container.ContainerID != protocol.ContainerCreatedOutput || out.result.Count <= 0 {
		return false
	}
	dest, ok := m.mapIn(dst.Container.ContainerID, dst.Slot)
	if !ok || dest == m.result || count <= 0 || count%out.result.Count != 0 {
		return false
	}
	times := count / out.result.Count
	for i := int32(0); i < times; i++ {
		if auto {
			*steps = append(*steps, craftStep{place: &attach.Craft{Window: m.window, Recipe: out.bookID}})
		}
		// The result slot: the world moves the result onto the cursor.
		*steps = append(*steps, craftStep{click: &attach.WindowClick{ID: m.window, Slot: m.result, Mode: 0, Cursor: out.result}})
		if dest == m.cursor {
			c := m.slots[m.cursor]
			if c.Count > 0 && c.ID != out.result.ID {
				return false
			}
			c.ID, c.Count = out.result.ID, c.Count+out.result.Count
			m.slots[m.cursor] = c
			continue
		}
		d := m.slots[dest]
		if d.Count > 0 && d.ID != out.result.ID {
			return false
		}
		d.ID, d.Count = out.result.ID, d.Count+out.result.Count
		m.slots[dest] = d
		*steps = append(*steps, craftStep{click: &attach.WindowClick{ID: m.window, Slot: dest, Mode: 0,
			Changed: []attach.ClickChange{{Slot: dest, Item: d}}}})
	}
	touched[dest] = true
	return true
}
