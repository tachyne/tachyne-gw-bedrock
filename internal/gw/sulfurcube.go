package gw

import (
	"strings"

	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
	tproto "github.com/tachyne/tachyne-common/protocol"
)

// The sulfur cube, as Bedrock draws it (Geyser's SulfurCubeEntity is the
// reference). Java shows the swallowed block in the cube's BODY slot and
// picks the cube's look from the block's #sulfur_cube_archetype/* tag;
// Bedrock carries the block in the mob's HAND (a MobEquipment) and the look
// as an entity property, minecraft:sulfur_cube_archetype, whose enum the
// server declares after StartGame ("none" first, then the archetypes — the
// client matches the names, so the order is ours). The cube's size is not
// a scale on Bedrock: the baby flag alone shrinks it, and a baby keeps
// scale 1. A lit cube's MAX_FUSE is its fuse time, as for primed TNT.

const bedrockSulfurCube = "minecraft:sulfur_cube"

// sulfurArchetypes are the twelve SulfurCubeArchetypes by name.
var sulfurArchetypes = []string{"regular", "bouncy", "slow_bouncy", "slow_flat", "fast_flat", "light",
	"fast_sliding", "slow_sliding", "sticky", "high_resistance", "explosive", "hot"}

// sulfurArchetypeEnum is the property's enum in index order.
var sulfurArchetypeEnum = append([]string{"none"}, sulfurArchetypes...)

// sulfurArchetypeByItem maps a canonical item id to its enum index, from
// the 26.3 item tags (tachyne-common).
var sulfurArchetypeByItem = func() map[int32]int32 {
	m := map[int32]int32{}
	for i, name := range sulfurArchetypes {
		for _, it := range tproto.ItemTag263("minecraft:sulfur_cube_archetype/" + name) {
			m[tproto.CanonicalItem(strings.TrimPrefix(it, "minecraft:"))] = int32(i + 1)
		}
	}
	return m
}()

// sulfurArchetypeIndex is the property value for a swallowed item: "none"
// for an empty cube, "regular" for a block no archetype names (Geyser's
// fallback — the property needs a value).
func sulfurArchetypeIndex(item int32) int32 {
	if item == 0 {
		return 0
	}
	if i, ok := sulfurArchetypeByItem[item]; ok {
		return i
	}
	return 1
}

// sulfurCubeProperty is the property definition packet.
func sulfurCubeProperty() *packet.SyncActorProperty {
	return &packet.SyncActorProperty{PropertyData: map[string]any{
		"type": bedrockSulfurCube,
		"properties": []map[string]any{{
			"name": "minecraft:sulfur_cube_archetype",
			"type": int32(3),
			"enum": sulfurArchetypeEnum,
		}},
	}}
}

// sulfurBodyPackets are what a sulfur cube's equipment becomes: the block
// in its hand, and its archetype look.
func sulfurBodyPackets(eid int32, st *entState, body protocol.ItemInstance, bodyItem int32) []packet.Packet {
	st.look.hasArchetype = true
	st.look.archetype = sulfurArchetypeIndex(bodyItem)
	return []packet.Packet{
		&packet.MobEquipment{EntityRuntimeID: rt(eid), NewItem: body},
		actorData(eid, st),
	}
}
