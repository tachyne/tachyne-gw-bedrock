package gw

import (
	"testing"

	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
	"github.com/tachyne/tachyne-common/attach"
)

func TestParticleAndWorldEvents(t *testing.T) {
	if p, ok := particleEvent(attach.Particles{PID: 5, X: 1}, 0).(*packet.LevelEvent); !ok || p.EventType != packet.LevelEventParticlesCritical {
		t.Errorf("crit → %+v", p)
	}
	if p := particleEvent(attach.Particles{PID: 999}, 0); p != nil {
		t.Error("unknown particles stay silent")
	}
	if p := worldEvent(attach.WorldFX{Event: 2001, X: 1, Y: 2, Z: 3, Data: 1}); p == nil || p.EventType != packet.LevelEventParticlesDestroyBlock || p.Position[1] != 2.5 {
		t.Errorf("block break → %+v", p)
	}
	if p := worldEvent(attach.WorldFX{Event: 3003}); p != nil {
		t.Error("an unmapped world event stays silent")
	}
}

// Java level events a Bedrock client hears as its own sounds.
func TestWorldEventSounds(t *testing.T) {
	for ev, want := range map[int32]int32{1029: packet.LevelEventSoundAnvilBroken, 1031: packet.LevelEventSoundAnvilLand, 1045: packet.LevelEventSoundPointedDripstoneLand, 3007: packet.LevelEventParticleSculkShriek} {
		if p := worldEvent(attach.WorldFX{Event: ev}); p == nil || p.EventType != want {
			t.Errorf("event %d: got %+v, want %d", ev, p, want)
		}
	}
	if _, _, ok := bedrockSound("minecraft:block.sculk_shrieker.shriek"); !ok {
		t.Error("the shriek sound has no Bedrock mapping")
	}
}
