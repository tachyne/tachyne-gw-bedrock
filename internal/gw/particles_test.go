package gw

import (
	"testing"

	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
	"github.com/tachyne/tachyne-common/attach"
)

func TestParticleAndWorldEvents(t *testing.T) {
	if p := particleEvent(attach.Particles{PID: 5, X: 1}); p == nil || p.EventType != packet.LevelEventParticlesCritical {
		t.Errorf("crit → %+v", p)
	}
	if p := particleEvent(attach.Particles{PID: 999}); p != nil {
		t.Error("unknown particles stay silent")
	}
	if p := worldEvent(attach.WorldFX{Event: 2001, X: 1, Y: 2, Z: 3, Data: 1}); p == nil || p.EventType != packet.LevelEventParticlesDestroyBlock || p.Position[1] != 2.5 {
		t.Errorf("block break → %+v", p)
	}
	if p := worldEvent(attach.WorldFX{Event: 3003}); p != nil {
		t.Error("an unmapped world event stays silent")
	}
}
