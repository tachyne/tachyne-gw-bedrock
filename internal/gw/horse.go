package gw

import (
	"github.com/sandertv/gophertunnel/minecraft/nbt"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// The mount screen. Java opens a horse's inventory as its own menu
// (saddle, armour — or a llama's carpet — then a chested animal's chest
// slots); Bedrock opens it as a horse container on the entity, told which
// items each equipment slot takes (UpdateEquip), with the equipment in
// the horse-equip container and the chest in the level-entity one, the
// way Geyser lays it out.

// horseUnmapped marks an equipment slot Bedrock's screen has no place for
// (a donkey's armour, a llama's saddle).
const horseUnmapped = 99

// horseLayout is the window layout for a mount: columns is the chest's
// width (0 = no chest); a llama's equipment slot is its carpet (Java
// slot 1), every other animal's is its saddle (Java 0).
func horseLayout(columns int32, llama bool) []winSlot {
	var l []winSlot
	if columns == 0 {
		l = []winSlot{{protocol.ContainerHorseEquip, 0}, {protocol.ContainerHorseEquip, 1}}
	} else if llama {
		l = []winSlot{{protocol.ContainerHorseEquip, horseUnmapped}, {protocol.ContainerHorseEquip, 0}}
	} else {
		l = []winSlot{{protocol.ContainerHorseEquip, 0}, {protocol.ContainerHorseEquip, horseUnmapped}}
	}
	for i := uint32(0); i < uint32(columns)*3; i++ {
		l = append(l, winSlot{protocol.ContainerLevelEntity, 1 + i})
	}
	return l
}

// horseEquipPacket tells the client what the mount's equipment slots take.
func horseEquipPacket(window int32, entity int64, columns int32, llama bool) *packet.UpdateEquip {
	item := func(n string) map[string]any { return map[string]any{"Name": n, "Count": byte(1), "Damage": int16(0)} }
	var slots []map[string]any
	switch {
	case llama:
		var carpets []map[string]any
		for _, d := range []string{"white", "orange", "magenta", "light_blue", "yellow", "lime", "pink", "gray",
			"light_gray", "cyan", "purple", "blue", "brown", "green", "red", "black"} {
			carpets = append(carpets, item("minecraft:"+d+"_carpet"))
		}
		slots = []map[string]any{{"slotNumber": int32(0), "acceptedItems": carpets}}
	default:
		slots = []map[string]any{{"slotNumber": int32(0), "acceptedItems": []map[string]any{item("minecraft:saddle")}}}
		if columns == 0 {
			slots = append(slots, map[string]any{"slotNumber": int32(1), "acceptedItems": []map[string]any{
				item("minecraft:leather_horse_armor"), item("minecraft:iron_horse_armor"),
				item("minecraft:golden_horse_armor"), item("minecraft:diamond_horse_armor")}})
		}
	}
	data, err := nbt.MarshalEncoding(map[string]any{"slots": slots}, nbt.NetworkLittleEndian)
	if err != nil {
		data = nil
	}
	return &packet.UpdateEquip{WindowID: byte(window), WindowType: byte(protocol.ContainerTypeHorse), EntityUniqueID: entity, SerialisedInventoryData: data}
}
