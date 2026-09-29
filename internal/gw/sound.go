package gw

import (
	"strings"

	"github.com/go-gl/mathgl/mgl32"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
	"github.com/tachyne/tachyne-common/attach"
)

// Sounds. The world names sounds the Java way ("minecraft:entity.zombie.hurt");
// Bedrock plays most of them from a LevelSoundEvent — a generic event type
// plus, for mobs, the entity type — so a mob sound maps by pattern and the
// common world sounds by a short table. Anything unmapped stays silent.

// mobSoundKinds maps the Java suffix of an entity sound to Bedrock's event.
var mobSoundKinds = map[string]string{
	"ambient": packet.SoundEventAmbient,
	"hurt":    packet.SoundEventHurt,
	"death":   packet.SoundEventDeath,
	"step":    packet.SoundEventStep,
	"attack":  packet.SoundEventAttack,
	"shoot":   packet.SoundEventBow,
}

// worldSounds are the non-mob sounds with a Bedrock event of their own.
var worldSounds = map[string]string{
	"block.sculk_shrieker.shriek":    packet.SoundEventSculkShriekerShriek, // level event 3007's sound
	"entity.generic.explode":         packet.SoundEventExplode,
	"entity.generic.eat":             packet.SoundEventEat,
	"entity.generic.drink":           packet.SoundEventDrink,
	"entity.generic.splash":          packet.SoundEventSplash,
	"entity.generic.big_fall":        packet.SoundEventFall,
	"entity.generic.small_fall":      packet.SoundEventFall,
	"entity.player.burp":             packet.SoundEventBurp,
	"entity.player.attack.strong":    packet.SoundEventAttackStrong,
	"entity.player.attack.crit":      packet.SoundEventAttackCritical,
	"entity.player.attack.nodamage":  packet.SoundEventAttackNoDamage,
	"entity.player.attack.sweep":     packet.SoundEventAttack,
	"entity.player.attack.weak":      packet.SoundEventAttack,
	"entity.player.attack.knockback": packet.SoundEventAttackStrong,
	"entity.arrow.shoot":             packet.SoundEventBow,
	"entity.arrow.hit":               packet.SoundEventBowHit,
	"entity.arrow.hit_player":        packet.SoundEventBowHit,
	"entity.snowball.throw":          packet.SoundEventThrow,
	"entity.egg.throw":               packet.SoundEventThrow,
	"entity.ender_pearl.throw":       packet.SoundEventThrow,
	"entity.splash_potion.throw":     packet.SoundEventThrow,
	"entity.tnt.primed":              packet.SoundEventFuse,
	"entity.lightning_bolt.thunder":  packet.SoundEventThunder,
	"entity.lightning_bolt.impact":   packet.SoundEventExplode,
	"entity.sheep.shear":             packet.SoundEventShear,
	"entity.cow.milk":                packet.SoundEventMilk,
	"entity.item.pickup":             packet.SoundEventPop,
	"block.chest.open":               packet.SoundEventChestOpen,
	"block.chest.close":              packet.SoundEventChestClosed,
	"block.ender_chest.open":         packet.SoundEventChestOpen,
	"block.ender_chest.close":        packet.SoundEventChestClosed,
	"block.barrel.open":              packet.SoundEventChestOpen,
	"block.barrel.close":             packet.SoundEventChestClosed,
	"block.shulker_box.open":         packet.SoundEventChestOpen,
	"block.shulker_box.close":        packet.SoundEventChestClosed,
	"block.wooden_door.open":         packet.SoundEventDoorOpen,
	"block.wooden_door.close":        packet.SoundEventDoorClose,
	"block.iron_door.open":           packet.SoundEventDoorOpen,
	"block.iron_door.close":          packet.SoundEventDoorClose,
	"block.wooden_trapdoor.open":     packet.SoundEventDoorOpen,
	"block.wooden_trapdoor.close":    packet.SoundEventDoorClose,
	"block.fence_gate.open":          packet.SoundEventDoorOpen,
	"block.fence_gate.close":         packet.SoundEventDoorClose,
	"block.anvil.use":                packet.SoundEventAnvilUse,
	"block.bell.use":                 packet.SoundEventBell,
	"block.fire.extinguish":          packet.SoundEventFizz,
	"block.lava.extinguish":          packet.SoundEventFizz,
	"block.portal.travel":            packet.SoundEventPortal,
	"block.portal.trigger":           packet.SoundEventPortal,
	"item.flintandsteel.use":         packet.SoundEventIgnite,
}

// namedSound is a sound Bedrock plays by its resource-pack name (PlaySound)
// and the factor its pitch takes: Geyser's sounds.json playsound_mapping and
// pitch_adjust, which SoundUtils.playSound prefers wherever it has one.
type namedSound struct {
	name  string
	pitch float32
}

// namedSounds are the sounds with no level sound event of their own: the
// button and pressure-plate clicks of every block set type, and the
// enderman's teleport.
var namedSounds = map[string]namedSound{
	"block.stone_button.click_on":                {"random.click", 0.6},
	"block.stone_button.click_off":               {"random.click", 0.5},
	"block.wooden_button.click_on":               {"random.wood_click", 0.6},
	"block.wooden_button.click_off":              {"random.wood_click", 0.5},
	"block.cherry_wood_button.click_on":          {"click_on.cherry_wood_button", 0.6},
	"block.cherry_wood_button.click_off":         {"click_off.cherry_wood_button", 0.5},
	"block.bamboo_wood_button.click_on":          {"click_on.bamboo_wood_button", 0.6},
	"block.bamboo_wood_button.click_off":         {"click_off.bamboo_wood_button", 0.5},
	"block.nether_wood_button.click_on":          {"click_on.nether_wood_button", 0.6},
	"block.nether_wood_button.click_off":         {"click_off.nether_wood_button", 0.5},
	"block.stone_pressure_plate.click_on":        {"click_on.stone_pressure_plate", 1},
	"block.stone_pressure_plate.click_off":       {"click_off.stone_pressure_plate", 1},
	"block.wooden_pressure_plate.click_on":       {"click_on.wooden_pressure_plate", 1},
	"block.wooden_pressure_plate.click_off":      {"click_off.wooden_pressure_plate", 1},
	"block.metal_pressure_plate.click_on":        {"click_on.metal_pressure_plate", 1},
	"block.metal_pressure_plate.click_off":       {"click_off.metal_pressure_plate", 1},
	"block.cherry_wood_pressure_plate.click_on":  {"click_on.cherry_wood_pressure_plate", 1},
	"block.cherry_wood_pressure_plate.click_off": {"click_off.cherry_wood_pressure_plate", 1},
	"block.bamboo_wood_pressure_plate.click_on":  {"click_on.bamboo_wood_pressure_plate", 1},
	"block.bamboo_wood_pressure_plate.click_off": {"click_off.bamboo_wood_pressure_plate", 1},
	"block.nether_wood_pressure_plate.click_on":  {"click_on.nether_wood_pressure_plate", 1},
	"block.nether_wood_pressure_plate.click_off": {"click_off.nether_wood_pressure_plate", 1},
	"entity.enderman.teleport":                   {"mob.endermen.portal", 1},
}

// soundPacket renders a world sound frame: a named PlaySound for the sounds
// above, else the level sound event, else nil.
func soundPacket(e attach.Sound) packet.Packet {
	if ns, ok := namedSounds[strings.TrimPrefix(e.Name, "minecraft:")]; ok {
		vol, pitch := e.Volume, e.Pitch
		if vol == 0 {
			vol = 1
		}
		if pitch == 0 {
			pitch = 1
		}
		return &packet.PlaySound{
			SoundName: ns.name,
			Position:  mgl32.Vec3{float32(e.X), float32(e.Y), float32(e.Z)},
			Volume:    vol,
			Pitch:     pitch * ns.pitch,
		}
	}
	if p := levelSound(e); p != nil {
		return p
	}
	return nil
}

// bedrockSound turns a Java sound name into a Bedrock level sound event
// (ok false = no equivalent).
func bedrockSound(name string) (soundType string, entityType string, ok bool) {
	n := strings.TrimPrefix(name, "minecraft:")
	if st, found := worldSounds[n]; found {
		return st, "", true
	}
	if strings.HasPrefix(n, "entity.") {
		parts := strings.Split(n, ".")
		if len(parts) == 3 {
			if st, found := mobSoundKinds[parts[2]]; found {
				return st, "minecraft:" + parts[1], true
			}
		}
	}
	return "", "", false
}

// levelSound builds the packet for a world sound frame, or nil.
func levelSound(e attach.Sound) *packet.LevelSoundEvent {
	st, et, ok := bedrockSound(e.Name)
	if !ok {
		return nil
	}
	return &packet.LevelSoundEvent{
		SoundType:  st,
		Position:   mgl32.Vec3{float32(e.X), float32(e.Y), float32(e.Z)},
		ExtraData:  -1,
		EntityType: et,
	}
}
