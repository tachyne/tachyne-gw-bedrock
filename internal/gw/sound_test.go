package gw

import (
	"testing"

	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
	"github.com/tachyne/tachyne-common/attach"
)

func TestBedrockSoundMapping(t *testing.T) {
	st, et, ok := bedrockSound("minecraft:entity.zombie.hurt")
	if !ok || st != packet.SoundEventHurt || et != "minecraft:zombie" {
		t.Errorf("zombie hurt → %q %q %v", st, et, ok)
	}
	st, et, ok = bedrockSound("minecraft:entity.generic.explode")
	if !ok || st != packet.SoundEventExplode || et != "" {
		t.Errorf("explode → %q %q %v", st, et, ok)
	}
	if _, _, ok := bedrockSound("minecraft:music_disc.13"); ok {
		t.Error("an unmapped sound stays silent rather than guessing")
	}
	if p := levelSound(attach.Sound{Name: "minecraft:block.chest.open", X: 1, Y: 2, Z: 3}); p == nil || p.SoundType != packet.SoundEventChestOpen || p.Position[1] != 2 {
		t.Errorf("chest open packet %+v", p)
	}
}
