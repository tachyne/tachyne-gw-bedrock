package gw

import (
	"testing"

	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
	attach "github.com/tachyne/tachyne-common/attach"
)

func canonicalEnchant(t *testing.T, name string) int32 {
	for i, n := range canonicalEnchantments {
		if n == "minecraft:"+name {
			return int32(i)
		}
	}
	t.Fatalf("no enchantment %s", name)
	return 0
}

// The table's window properties become Bedrock's enchant options, and
// crafting with an option's network id presses the Java button.
func TestEnchantingTable(t *testing.T) {
	if bedrockEnchantment(canonicalEnchant(t, "sharpness")) != 9 || bedrockEnchantment(canonicalEnchant(t, "sweeping_edge")) != bedrockEnchantInvalid || bedrockEnchantment(-1) != -1 {
		t.Fatal("enchantment renumbering")
	}
	w := &winState{}
	m := w.open(4, enchantLayout, protocol.ContainerTypeEnchantment, "Enchant")
	if !m.table || m.ench[0].level != -1 {
		t.Fatalf("table window %+v", m.ench)
	}
	var c capture
	for prop, val := range map[int32]int32{0: 1, 1: 7, 2: 30, 4: canonicalEnchant(t, "sharpness"), 5: -1, 6: canonicalEnchant(t, "sweeping_edge")} {
		windowData(&c, m, protocol.ContainerTypeEnchantment, prop, val)
	}
	if len(c.pkts) != 0 {
		t.Fatalf("options sent before the levels: %d", len(c.pkts))
	}
	windowData(&c, m, protocol.ContainerTypeEnchantment, 7, 1)
	windowData(&c, m, protocol.ContainerTypeEnchantment, 8, -1)
	windowData(&c, m, protocol.ContainerTypeEnchantment, 9, 3)
	if len(c.pkts) != 3 {
		t.Fatalf("%d option packets", len(c.pkts))
	}
	pk := c.pkts[2].(*packet.PlayerEnchantOptions)
	if len(pk.Options) != 3 || pk.Options[0].Cost != 1 || pk.Options[0].Enchantments.Slot != 16 || pk.Options[0].RecipeNetworkID == 0 {
		t.Fatalf("options %+v", pk.Options)
	}
	if e := pk.Options[0].Enchantments.Enchantments[1]; len(e) != 1 || e[0].Type != 9 || e[0].Level != 1 || pk.Options[0].Name != "sharpness" {
		t.Errorf("row 0 %+v %q", e, pk.Options[0].Name)
	}
	if pk.Options[1].RecipeNetworkID != 0 || len(pk.Options[1].Enchantments.Enchantments[1]) != 0 {
		t.Errorf("row 1 (no hint) %+v", pk.Options[1])
	}
	if e := pk.Options[2].Enchantments.Enchantments[1]; len(e) != 1 || e[0].Type != byte(bedrockEnchantInvalid) || e[0].Level != 3 {
		t.Errorf("row 2 (Java-only enchantment) %+v", e)
	}
	// Choosing row 2 by its network id presses button 2 and touches no slot.
	req := protocol.ItemStackRequest{RequestID: 1, Actions: []protocol.StackRequestAction{
		&protocol.CraftRecipeStackRequestAction{RecipeNetworkID: pk.Options[2].RecipeNetworkID},
		&protocol.ConsumeStackRequestAction{DestroyStackRequestAction: protocol.DestroyStackRequestAction{Count: 3, Source: slotInfo(protocol.ContainerEnchantingMaterial, 15)}},
	}}
	m.set(1, attach.ItemStack{ID: 1, Count: 3})
	changed, steps, ok := m.applyRequest(req, nil)
	if !ok || len(changed) != 0 || len(steps) != 1 || steps[0].ench == nil || steps[0].ench.Button != 2 || m.slots[1].Count != 3 {
		t.Fatalf("enchant request: ok=%v changed=%v steps=%+v", ok, changed, steps)
	}
	if _, _, ok := m.applyRequest(protocol.ItemStackRequest{Actions: []protocol.StackRequestAction{
		&protocol.CraftRecipeStackRequestAction{RecipeNetworkID: 999}}}, nil); ok {
		t.Error("an unknown option enchanted")
	}
	if j, ok := m.mapIn(protocol.ContainerEnchantingInput, 14); !ok || j != 0 {
		t.Errorf("input slot → %d %v", j, ok)
	}
}
