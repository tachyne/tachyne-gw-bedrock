package gw

import "github.com/sandertv/gophertunnel/minecraft/protocol/packet"

// Climate variants. Java's pigs, cows and chickens carry a temperate/warm/
// cold variant holder in their metadata; Bedrock renders the same three
// looks from an entity property, minecraft:climate_variant, whose
// definition the server declares per entity type once after StartGame
// (SyncActorProperty: {type, properties: [{name, type: 3 (enum), enum}]}).
// The enum order below is the property's index order — the value an actor
// carries in its integer properties (index 0, this being the type's only
// property) is an index into it.

// climateVariants is the property's enum in index order.
var climateVariants = []string{"temperate", "warm", "cold"}

// climateProperty is the definition packet for one entity type.
func climateProperty(entityType string) *packet.SyncActorProperty {
	return &packet.SyncActorProperty{PropertyData: map[string]any{
		"type": entityType,
		"properties": []map[string]any{{
			"name": "minecraft:climate_variant",
			"type": int32(3),
			"enum": climateVariants,
		}},
	}}
}
