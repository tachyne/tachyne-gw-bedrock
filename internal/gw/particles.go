package gw

import (
	"github.com/go-gl/mathgl/mgl32"
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

// particleEvent renders a particle burst, or nil.
func particleEvent(e attach.Particles) *packet.LevelEvent {
	ev, ok := particleEvents[e.PID]
	if !ok {
		return nil
	}
	return &packet.LevelEvent{EventType: ev, Position: mgl32.Vec3{float32(e.X), float32(e.Y), float32(e.Z)}}
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
	}
	return nil
}
