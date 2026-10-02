package gw

import (
	"encoding/json"
	"strings"

	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
	attach "github.com/tachyne/tachyne-common/attach"
)

// Dimensions. The engine runs a table of dimensions (attach Welcome.Config;
// none = the overworld 0, the Nether 1 and the End 2), but a Bedrock client
// knows only its three. Each engine dimension shows as the Bedrock one its
// type resembles, the way Geyser's JavaDimension does it: a type in the
// minecraft namespace by its name (the_nether, the_end, else overworld), a
// custom type by its skybox ("none" the Nether, "end" the End, else the
// overworld). Two engine dimensions that share a Bedrock one need a detour
// through another on a switch (Geyser's getTemporaryDimension).

// dimTable maps engine dimension ids to Bedrock dimension ids.
type dimTable map[int32]int32

// newDimTable reads the world's dimension table; nil or empty is the
// default three, which map to themselves.
func newDimTable(cfg *attach.ConfigData) dimTable {
	t := dimTable{0: packet.DimensionOverworld, 1: packet.DimensionNether, 2: packet.DimensionEnd}
	if cfg == nil || len(cfg.Dimensions) == 0 {
		return t
	}
	t = dimTable{}
	for _, d := range cfg.Dimensions {
		t[d.ID] = bedrockDimFor(d)
	}
	return t
}

// bedrockDimFor is JavaDimension's bedrockId.
func bedrockDimFor(d attach.DimensionInfo) int32 {
	typ := d.Type
	if typ == "" {
		typ = d.Key
	}
	if strings.HasPrefix(typ, "minecraft:") {
		return javaToBedrockDim(typ)
	}
	var data struct {
		Skybox string `json:"skybox"`
	}
	if len(d.TypeData) > 0 {
		json.Unmarshal(d.TypeData, &data)
	}
	switch data.Skybox {
	case "none":
		return packet.DimensionNether
	case "end":
		return packet.DimensionEnd
	}
	return packet.DimensionOverworld
}

// javaToBedrockDim is DimensionUtils.javaToBedrock.
func javaToBedrockDim(name string) int32 {
	switch name {
	case "minecraft:the_nether":
		return packet.DimensionNether
	case "minecraft:the_end":
		return packet.DimensionEnd
	}
	return packet.DimensionOverworld
}

// bedrock is the Bedrock dimension an engine dimension shows as (an unknown
// id shows as the overworld).
func (t dimTable) bedrock(dim int32) int32 {
	if b, ok := t[dim]; ok {
		return b
	}
	return packet.DimensionOverworld
}

// temporaryDim is getTemporaryDimension: where to send a client first when
// it moves between two engine dimensions shown as the same Bedrock one.
func temporaryDim(current int32) int32 {
	if current == packet.DimensionOverworld {
		return packet.DimensionNether
	}
	return packet.DimensionOverworld
}
