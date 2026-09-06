package gw

import (
	"hash/fnv"

	"github.com/go-gl/mathgl/mgl32"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
	attach "github.com/tachyne/tachyne-common/attach"
)

// The smaller world frames Bedrock has a twin for: status effects,
// abilities and game events (weather, game mode), arm swings, the
// item-pickup animation, a server-set hotbar slot, difficulty, boss bars
// and a vehicle snap-back.

// bedrockEffects renumbers the canonical mob_effect registry (speed 0 …
// breath_of_the_nautilus 39) into Bedrock's effect ids (speed 1 …); 0 =
// Bedrock has no such effect (glowing, luck, unluck, dolphin's grace).
var bedrockEffects = []int32{
	1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21, 22, 23, // speed … saturation
	0,    // glowing
	24,   // levitation
	0, 0, // luck, unluck
	27, 26, // slow_falling, conduit_power
	0,                                      // dolphins_grace
	28, 29, 30, 31, 36, 32, 33, 34, 35, 37, // bad_omen, hero, darkness, trial_omen, raid_omen, wind_charged, weaving, oozing, infested, breath
}

// effectPacket renders a status effect change, or nil for one Bedrock
// cannot show.
func effectPacket(e attach.Effect) *packet.MobEffect {
	if e.ID < 0 || int(e.ID) >= len(bedrockEffects) || bedrockEffects[e.ID] == 0 {
		return nil
	}
	pk := &packet.MobEffect{EntityRuntimeID: rt(e.EID), Operation: packet.MobEffectAdd, EffectType: bedrockEffects[e.ID],
		Amplifier: e.Amp, Particles: true, Duration: e.Ticks}
	if e.Remove {
		pk.Operation = packet.MobEffectRemove
	}
	return pk
}

// abilityValues is the ability bit set for a player: the fixed survival
// set, plus what the world grants.
func abilityValues(a attach.Abilities) uint32 {
	v := uint32(protocol.AbilityBuild | protocol.AbilityMine | protocol.AbilityDoorsAndSwitches |
		protocol.AbilityOpenContainers | protocol.AbilityAttackPlayers | protocol.AbilityAttackMobs)
	if a.MayFly {
		v |= protocol.AbilityMayFly
	}
	if a.Flying {
		v |= protocol.AbilityFlying
	}
	if a.Creative {
		v |= protocol.AbilityInstantBuild
	}
	if a.Invulnerable {
		v |= protocol.AbilityInvulnerable
	}
	return v
}

// abilitiesPacket is the UpdateAbilities for a player's ability set.
func abilitiesPacket(eid int32, values uint32) *packet.UpdateAbilities {
	return &packet.UpdateAbilities{AbilityData: protocol.AbilityData{
		EntityUniqueID:     int64(eid),
		PlayerPermissions:  packet.PermissionLevelMember,
		CommandPermissions: protocol.CommandPermissionLevelAny,
		Layers: []protocol.AbilityLayer{{
			Type:             protocol.AbilityLayerTypeBase,
			Abilities:        protocol.AbilityCount - 1,
			Values:           values,
			FlySpeed:         protocol.AbilityBaseFlySpeed,
			VerticalFlySpeed: 1,
			WalkSpeed:        protocol.AbilityBaseWalkSpeed,
		}},
	}}
}

// Java game events (the vanilla game_event ids).
const (
	gameEventEndRaining   = 1
	gameEventBeginRaining = 2
	gameEventGameMode     = 3
	gameEventRainLevel    = 7
	gameEventThunderLevel = 8
)

// gameEventPackets renders a game event: weather as level events (the
// strength scaled to Bedrock's 0..65535), a game-mode change as the game
// type plus the abilities that come with it. Others render nothing.
func gameEventPackets(eid int32, e attach.GameEvent) []packet.Packet {
	switch e.Event {
	case gameEventBeginRaining:
		return []packet.Packet{&packet.LevelEvent{EventType: packet.LevelEventStartRaining, EventData: 65535}}
	case gameEventEndRaining:
		return []packet.Packet{&packet.LevelEvent{EventType: packet.LevelEventStopRaining}}
	case gameEventRainLevel:
		if e.Value <= 0 {
			return []packet.Packet{&packet.LevelEvent{EventType: packet.LevelEventStopRaining}}
		}
		return []packet.Packet{&packet.LevelEvent{EventType: packet.LevelEventStartRaining, EventData: int32(e.Value * 65535)}}
	case gameEventThunderLevel:
		if e.Value <= 0 {
			return []packet.Packet{&packet.LevelEvent{EventType: packet.LevelEventStopThunderstorm}}
		}
		return []packet.Packet{&packet.LevelEvent{EventType: packet.LevelEventStartThunderstorm, EventData: int32(e.Value * 65535)}}
	case gameEventGameMode:
		mode := int32(e.Value)
		creative := mode == 1
		return []packet.Packet{
			&packet.SetPlayerGameType{GameType: bedrockGameMode(mode)},
			abilitiesPacket(eid, abilityValues(attach.Abilities{MayFly: creative, Creative: creative})),
		}
	}
	return nil
}

// bossBarID is a stable Bedrock entity id for a boss bar (Bedrock keys
// bars by the boss entity; a bar the world names by UUID gets an
// invisible stand-in of its own).
func bossBarID(id [16]byte) int64 {
	h := fnv.New64a()
	h.Write(id[:])
	return -int64(h.Sum64()>>2) - 1<<40 // negative, clear of real entity ids
}

// bossBarPackets renders a boss bar operation: show spawns the invisible
// stand-in at the player and shows the bar on it; health updates the
// fill; remove hides it and despawns the stand-in.
func bossBarPackets(e attach.BossBar, player int64, at mgl32.Vec3) []packet.Packet {
	id := bossBarID(e.UUID)
	switch e.Op {
	case attach.BossBarAdd:
		m := baseMetadata(0, 0)
		setActorFlag(m, protocol.EntityDataFlagInvisible)
		setActorFlag(m, protocol.EntityDataFlagSilent)
		setActorFlag(m, protocol.EntityDataFlagNoAI)
		m[protocol.EntityDataKeyScale] = float32(0.01)
		return []packet.Packet{
			&packet.AddActor{EntityUniqueID: id, EntityRuntimeID: uint64(id), EntityType: "minecraft:creeper", Position: at, EntityMetadata: m},
			&packet.BossEvent{BossEntityUniqueID: id, PlayerUniqueID: player, EventType: packet.BossEventShow,
				BossBarTitle: e.Title, HealthPercentage: e.Health, Colour: packet.BossEventColourPurple, Overlay: packet.BossEventOverlayProgress},
		}
	case attach.BossBarHealth:
		return []packet.Packet{&packet.BossEvent{BossEntityUniqueID: id, EventType: packet.BossEventHealthPercentage, HealthPercentage: e.Health}}
	case attach.BossBarRemove:
		return []packet.Packet{
			&packet.BossEvent{BossEntityUniqueID: id, EventType: packet.BossEventHide},
			&packet.RemoveActor{EntityUniqueID: id},
		}
	}
	return nil
}
