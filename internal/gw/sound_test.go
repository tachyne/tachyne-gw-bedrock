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

// Button clicks play by name at Geyser's pitch_adjust times the Java pitch.
func TestButtonClicksPlayByName(t *testing.T) {
	for name, want := range map[string]struct {
		sound string
		pitch float32
	}{
		"minecraft:block.stone_button.click_on":         {"random.click", 0.6},
		"minecraft:block.stone_button.click_off":        {"random.click", 0.5},
		"minecraft:block.wooden_button.click_on":        {"random.wood_click", 0.6},
		"minecraft:block.cherry_wood_button.click_off":  {"click_off.cherry_wood_button", 0.5},
		"minecraft:block.metal_pressure_plate.click_on": {"click_on.metal_pressure_plate", 1},
	} {
		p, ok := soundPacket(attach.Sound{Name: name, X: 1, Y: 2, Z: 3, Volume: 1, Pitch: 1}).(*packet.PlaySound)
		if !ok || p.SoundName != want.sound || p.Pitch != want.pitch || p.Volume != 1 || p.Position[1] != 2 {
			t.Errorf("%s → %+v, want %s at pitch %v", name, p, want.sound, want.pitch)
		}
	}
	if p, ok := soundPacket(attach.Sound{Name: "minecraft:block.chest.open"}).(*packet.LevelSoundEvent); !ok || p.SoundType != packet.SoundEventChestOpen {
		t.Errorf("a level-event sound still goes as one: %+v", p)
	}
	if p := soundPacket(attach.Sound{Name: "minecraft:music_disc.13"}); p != nil {
		t.Errorf("an unmapped sound stays silent, got %+v", p)
	}
}
