package gw

import (
	"bytes"

	"github.com/go-gl/mathgl/mgl32"
	"github.com/sandertv/gophertunnel/minecraft/nbt"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
	"github.com/tachyne/tachyne-common/attach"
)

// Particles and world events. The world names particles by canonical
// (1.21.5) id and world events by Java's level-event numbers; Bedrock plays
// the ones it has a twin for as level events.

// particleEvents maps canonical particle ids to Bedrock level events.
var particleEvents = map[int32]int32{
	5:  packet.LevelEventParticlesCritical,  // crit
	21: packet.LevelEventParticlesExplosion, // explosion_emitter
	56: packet.LevelEventParticleDeathSmoke, // poof
	3:  packet.LevelEventParticlesBubble,    // bubble
}

// particleEvent renders a particle burst: the level event Bedrock has for
// it, else the named particle Geyser's table maps it to (spawned in the
// player's dimension), else nil.
func particleEvent(e attach.Particles, dim int32) packet.Packet {
	pos := mgl32.Vec3{float32(e.X), float32(e.Y), float32(e.Z)}
	if ev, ok := particleEvents[e.PID]; ok {
		return &packet.LevelEvent{EventType: ev, Position: pos}
	}
	if int(e.PID) < len(bedrockParticleNames) && bedrockParticleNames[e.PID] != "" {
		return &packet.SpawnParticleEffect{Dimension: byte(dim), EntityUniqueID: -1, Position: pos, ParticleName: bedrockParticleNames[e.PID]}
	}
	return nil
}

// worldEvent renders a Java level event: block-break particles carry the
// Bedrock block runtime id, bone meal its crop-growth sparkle.
func worldEvent(e attach.WorldFX) *packet.LevelEvent {
	pos := mgl32.Vec3{float32(e.X) + 0.5, float32(e.Y) + 0.5, float32(e.Z) + 0.5}
	switch e.Event {
	case 2001: // block break: particles + the block's break sound
		return &packet.LevelEvent{EventType: packet.LevelEventParticlesDestroyBlock, Position: pos, EventData: int32(bedrockBlockRID(uint32(e.Data)))}
	case 2005: // bone meal
		return &packet.LevelEvent{EventType: packet.LevelEventParticleCropGrowth, Position: pos, EventData: e.Data}
	case 1029: // anvil destroyed
		return &packet.LevelEvent{EventType: packet.LevelEventSoundAnvilBroken, Position: pos}
	case 1030: // anvil used
		return &packet.LevelEvent{EventType: packet.LevelEventSoundAnvilUsed, Position: pos}
	case 1031: // anvil landed
		return &packet.LevelEvent{EventType: packet.LevelEventSoundAnvilLand, Position: pos}
	case 1045: // pointed dripstone landed
		return &packet.LevelEvent{EventType: packet.LevelEventSoundPointedDripstoneLand, Position: pos}
	case 3007: // a sculk shrieker shrieks: its particles (the sound rides separately)
		return &packet.LevelEvent{EventType: packet.LevelEventParticleSculkShriek, Position: pos}
	}
	return nil
}

// Enderman teleport trail. Java's level event 2018 (Enderman.teleport) sits
// at the old block and packs the offset to the new one into its data
// (BlockUtil.clampedPackDifferenceInPosition: 8 bits per axis, x high, each
// biased by the radius 127). The client draws 128 portal particles along
// the line, each spread by the enderman's width and height, drifting at
// (random - 0.5) * 0.2.
const (
	endermanTrailRadius = 127
	endermanTrailCount  = 128
	endermanWidth       = 0.6
	endermanHalfHeight  = 1.45
)

// teleportTrail renders level event 2018 as Bedrock's ParticlesTeleportTrail
// (2023), a generic level event whose loose NBT tags name the two ends, the
// spread, the drift and the count (Startx…, Endx…, Variationx/y, DirScale,
// Count; ViaBedrock's reading of the event is the layout reference). The
// client centres the trail a block below the given ends and spreads it ± the
// variation, so the ends sit a block above the body's middle. Nil for any
// other event.
func teleportTrail(e attach.WorldFX) *packet.LevelEventGeneric {
	if e.Event != 2018 {
		return nil
	}
	dx := (e.Data>>16)&0xff - endermanTrailRadius
	dy := (e.Data>>8)&0xff - endermanTrailRadius
	dz := e.Data&0xff - endermanTrailRadius
	sx, sy, sz := float32(e.X)+0.5, float32(e.Y)+endermanHalfHeight+1, float32(e.Z)+0.5
	var buf bytes.Buffer
	if err := nbt.NewEncoderWithEncoding(&buf, nbt.NetworkLittleEndian).Encode(map[string]any{
		"Startx": sx, "Starty": sy, "Startz": sz,
		"Endx": sx + float32(dx), "Endy": sy + float32(dy), "Endz": sz + float32(dz),
		"Variationx": float32(endermanWidth), "Variationy": float32(endermanHalfHeight),
		"DirScale": float32(0.2), "Count": int32(endermanTrailCount),
	}); err != nil {
		return nil
	}
	// The event data is the compound's tags alone: no compound header
	// (type byte + empty name) and no closing end tag.
	b := buf.Bytes()
	if len(b) < 3 {
		return nil
	}
	return &packet.LevelEventGeneric{EventID: packet.LevelEventParticlesTeleportTrail, SerialisedEventData: b[2 : len(b)-1]}
}
