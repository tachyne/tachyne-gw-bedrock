package gw

import (
	"strings"

	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
	attach "github.com/tachyne/tachyne-common/attach"
	tproto "github.com/tachyne/tachyne-common/protocol"
)

// What a vault, a trial spawner and a half-brushed suspicious block show
// (the world's MsgBlockDisplay), as Bedrock block actor data. The tags are
// Geyser's VaultBlockEntityTranslator, TrialSpawnerBlockEntityTranslator and
// BrushableBlockEntityTranslator.

// bedrockEntityByName maps a Java entity type name (no namespace) to its
// Bedrock actor identifier.
var bedrockEntityByName = func() map[string]string {
	m := make(map[string]string, len(javaEntityNames))
	for i, n := range javaEntityNames {
		if i < len(bedrockEntityIDs) && bedrockEntityIDs[i] != "" {
			m[n] = bedrockEntityIDs[i]
		}
	}
	return m
}()

// canonicalItemByName is the canonical id of a Java item name, false for a
// name the registry lacks (tproto.CanonicalItem panics on one).
func canonicalItemByName(name string) (id int32, ok bool) {
	defer func() {
		if recover() != nil {
			id, ok = 0, false
		}
	}()
	return tproto.CanonicalItem(strings.TrimPrefix(name, "minecraft:")), true
}

// displayItemNBT is a Bedrock item tag (Name, Count, Damage) for a Java item
// name; an empty or unmapped one is the empty item (no name, count 0).
func displayItemNBT(name string, count int32) map[string]any {
	empty := map[string]any{"Name": "", "Count": byte(0), "Damage": int16(0)}
	if name == "" || count <= 0 {
		return empty
	}
	id, ok := canonicalItemByName(name)
	if !ok || int(id) >= len(javaItemBedrock) || javaItemBedrock[id].Name == "" {
		return empty
	}
	ref := javaItemBedrock[id]
	return map[string]any{"Name": ref.Name, "Count": byte(min(count, 127)), "Damage": ref.Data}
}

// blockDisplayData renders one display frame, nil for an unknown kind.
func blockDisplayData(e attach.BlockDisplay) *packet.BlockActorData {
	x, y, z := e.Pos[0], e.Pos[1], e.Pos[2]
	pos := protocol.BlockPos{x, y, z}
	switch e.Kind {
	case attach.DisplayVault:
		// A vault's tag carries neither id nor position; Bedrock always
		// includes the particle range Java leaves at its default.
		return &packet.BlockActorData{Position: pos, NBTData: map[string]any{
			"display_item":             displayItemNBT(e.Name, max(e.Count, 1)),
			"connected_players":        []any{},
			"connected_particle_range": float32(4.5),
		}}
	case attach.DisplayTrialSpawner:
		nbt := map[string]any{"id": "TrialSpawner", "x": x, "y": y, "z": z}
		if id, ok := bedrockEntityByName[strings.TrimPrefix(e.Name, "minecraft:")]; ok {
			nbt["spawn_data"] = map[string]any{"TypeId": id, "Weight": int32(1)}
		}
		return &packet.BlockActorData{Position: pos, NBTData: nbt}
	case attach.DisplayBrushable:
		nbt := map[string]any{"id": "BrushableBlock", "x": x, "y": y, "z": z}
		// No face (the item sinking back) or nothing buried: the bare tag.
		if item := displayItemNBT(e.Name, max(e.Count, 1)); e.HitDir > 0 && item["Name"] != "" {
			delete(item, "Damage")
			nbt["item"] = item
			nbt["brush_direction"] = byte(e.HitDir - 1)
			nbt["brush_count"] = e.Dusted
			block := e.Block
			if block == "" {
				block = "minecraft:suspicious_sand"
			}
			nbt["type"] = block
		}
		return &packet.BlockActorData{Position: pos, NBTData: nbt}
	}
	return nil
}
