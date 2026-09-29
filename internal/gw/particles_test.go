package gw

import (
	"testing"

	"github.com/sandertv/gophertunnel/minecraft/nbt"
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

// Level event 2018 (an enderman's teleport) is Bedrock's teleport trail from
// the old block to the packed new one.
func TestEndermanTeleportTrail(t *testing.T) {
	// Landing 3 east, 1 down, 5 north: each axis biased by 127.
	data := int32((3+127)<<16 | (-1+127)<<8 | (-5 + 127))
	p := teleportTrail(attach.WorldFX{Event: 2018, X: 10, Y: 64, Z: -20, Data: data})
	if p == nil || p.EventID != packet.LevelEventParticlesTeleportTrail {
		t.Fatalf("trail %+v", p)
	}
	// The data is the compound's tags alone: wrap it back up to decode.
	wrapped := append(append([]byte{10, 0}, p.SerialisedEventData...), 0)
	var m map[string]any
	if err := nbt.UnmarshalEncoding(wrapped, &m, nbt.NetworkLittleEndian); err != nil {
		t.Fatal(err)
	}
	f := func(k string) float32 { v, _ := m[k].(float32); return v }
	if f("Startx") != 10.5 || f("Startz") != -19.5 || f("Endx") != 13.5 || f("Endz") != -24.5 || f("Endy") != f("Starty")-1 {
		t.Errorf("ends %v", m)
	}
	if m["Count"] != int32(128) || f("DirScale") != 0.2 || f("Variationx") != 0.6 {
		t.Errorf("spread %v", m)
	}
	if teleportTrail(attach.WorldFX{Event: 2001}) != nil {
		t.Error("only 2018 draws a trail")
	}
}
