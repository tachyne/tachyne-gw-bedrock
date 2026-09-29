package gw

import (
	"testing"

	"github.com/tachyne/tachyne-common/attach"
)

// Vault, trial spawner and brushed block displays become Geyser's tags.
func TestBlockDisplayTags(t *testing.T) {
	v := blockDisplayData(attach.BlockDisplay{Pos: [3]int32{1, 2, 3}, Kind: attach.DisplayVault, Name: "minecraft:diamond", Count: 2})
	if v == nil || v.Position[1] != 2 {
		t.Fatalf("vault %+v", v)
	}
	if _, ok := v.NBTData["id"]; ok {
		t.Error("a vault's tag carries no id")
	}
	item, _ := v.NBTData["display_item"].(map[string]any)
	if item["Name"] != "minecraft:diamond" || item["Count"] != byte(2) || v.NBTData["connected_particle_range"] != float32(4.5) {
		t.Errorf("vault tag %v", v.NBTData)
	}
	if e := blockDisplayData(attach.BlockDisplay{Kind: attach.DisplayVault}); e.NBTData["display_item"].(map[string]any)["Name"] != "" {
		t.Errorf("an idle vault shows the empty item: %v", e.NBTData)
	}

	ts := blockDisplayData(attach.BlockDisplay{Pos: [3]int32{4, 5, 6}, Kind: attach.DisplayTrialSpawner, Name: "minecraft:zombie"})
	sd, _ := ts.NBTData["spawn_data"].(map[string]any)
	if ts.NBTData["id"] != "TrialSpawner" || ts.NBTData["x"] != int32(4) || sd["TypeId"] != "minecraft:zombie" || sd["Weight"] != int32(1) {
		t.Errorf("trial spawner tag %v", ts.NBTData)
	}

	b := blockDisplayData(attach.BlockDisplay{Kind: attach.DisplayBrushable, Name: "minecraft:emerald", Count: 1, HitDir: 2, Dusted: 2, Block: "minecraft:suspicious_gravel"})
	bi, _ := b.NBTData["item"].(map[string]any)
	if b.NBTData["id"] != "BrushableBlock" || bi["Name"] != "minecraft:emerald" || bi["Count"] != byte(1) ||
		b.NBTData["brush_direction"] != byte(1) || b.NBTData["brush_count"] != int32(2) || b.NBTData["type"] != "minecraft:suspicious_gravel" {
		t.Errorf("brushable tag %v", b.NBTData)
	}
	if bare := blockDisplayData(attach.BlockDisplay{Kind: attach.DisplayBrushable, Name: "minecraft:emerald", Count: 1}); bare.NBTData["item"] != nil {
		t.Errorf("no face: the item sinks back, %v", bare.NBTData)
	}
	if blockDisplayData(attach.BlockDisplay{Kind: 99}) != nil {
		t.Error("an unknown kind renders nothing")
	}
}
