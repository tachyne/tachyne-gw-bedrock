package gw

import (
	"encoding/json"
	"testing"

	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
	attach "github.com/tachyne/tachyne-common/attach"
)

// A world with a fourth dimension: each engine dimension shows as the
// Bedrock one its type resembles (by name in the minecraft namespace, by
// skybox otherwise), its chunks render in that layout, and a death there
// points the compass at that Bedrock dimension. The frame goes through its
// JSON form, as the Welcome arrives.
func TestFourthDimensionOnBedrock(t *testing.T) {
	raw, _ := json.Marshal(attach.Welcome{Dim: 3, Config: &attach.ConfigData{Dimensions: []attach.DimensionInfo{
		{ID: 0, Key: "minecraft:overworld"},
		{ID: 1, Key: "minecraft:the_nether"},
		{ID: 2, Key: "minecraft:the_end"},
		{ID: 3, Key: "tachyne:caverns", Type: "tachyne:caverns", TypeData: json.RawMessage(`{"skybox":"none"}`)},
		{ID: 4, Key: "tachyne:moon", Type: "tachyne:moon", TypeData: json.RawMessage(`{"skybox":"overworld"}`)},
		{ID: 5, Key: "tachyne:void", Type: "minecraft:the_end"},
	}}})
	var w attach.Welcome
	if err := json.Unmarshal(raw, &w); err != nil {
		t.Fatal(err)
	}
	dims := newDimTable(w.Config)
	for dim, want := range map[int32]int32{0: packet.DimensionOverworld, 1: packet.DimensionNether, 2: packet.DimensionEnd,
		3: packet.DimensionNether, 4: packet.DimensionOverworld, 5: packet.DimensionEnd, 9: packet.DimensionOverworld} {
		if got := dims.bedrock(dim); got != want {
			t.Errorf("engine dim %d shows as %d, want %d", dim, got, want)
		}
	}
	if def := newDimTable(nil); def.bedrock(1) != packet.DimensionNether || def.bedrock(2) != packet.DimensionEnd {
		t.Error("default table")
	}

	body := &attach.ChunkBody{BlockStates: make([]uint32, 24*4096)}
	pk := renderChunkIn(attach.ChunkHeader{CX: 1, CZ: 2, Dim: 3}, body, dims.bedrock(3))
	if pk.Dimension != packet.DimensionNether || pk.SubChunkCount != 8 {
		t.Errorf("caverns chunk: dim %d, %d subchunks", pk.Dimension, pk.SubChunkCount)
	}
	m := deathMetadataIn(&attach.DeathPos{Dim: 5, X: 1, Y: 2, Z: 3}, dims)
	if m[protocol.EntityDataKeyPlayerLastDeathDimension] != int32(packet.DimensionEnd) {
		t.Errorf("death dimension %v", m[protocol.EntityDataKeyPlayerLastDeathDimension])
	}
	if temporaryDim(packet.DimensionOverworld) != packet.DimensionNether || temporaryDim(packet.DimensionEnd) != packet.DimensionOverworld {
		t.Error("temporary dimension")
	}
}
