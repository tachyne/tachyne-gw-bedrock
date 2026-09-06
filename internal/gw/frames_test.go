package gw

import (
	"testing"

	"github.com/go-gl/mathgl/mgl32"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
	attach "github.com/tachyne/tachyne-common/attach"
)

// Effects renumber into Bedrock's ids (and the ones Bedrock lacks render
// nothing); weather and game-mode events, abilities and boss bars render
// their Bedrock twins.
func TestSmallFrames(t *testing.T) {
	if len(bedrockEffects) != 40 {
		t.Fatalf("%d effects", len(bedrockEffects))
	}
	if pk := effectPacket(attach.Effect{EID: 4, ID: 0, Amp: 1, Ticks: 200}); pk == nil || pk.EffectType != packet.EffectSpeed || pk.Operation != packet.MobEffectAdd || pk.Amplifier != 1 {
		t.Errorf("speed %+v", pk)
	}
	if pk := effectPacket(attach.Effect{ID: 27, Remove: true}); pk == nil || pk.EffectType != 27 || pk.Operation != packet.MobEffectRemove {
		t.Errorf("slow falling %+v", pk)
	}
	if effectPacket(attach.Effect{ID: 23}) != nil || effectPacket(attach.Effect{ID: 99}) != nil {
		t.Error("glowing or an unknown effect rendered")
	}
	if effectPacket(attach.Effect{ID: 39}).EffectType != 37 || effectPacket(attach.Effect{ID: 34}).EffectType != 36 {
		t.Error("newer effects misnumbered")
	}
	v := abilityValues(attach.Abilities{MayFly: true, Flying: true, Creative: true})
	if v&protocol.AbilityMayFly == 0 || v&protocol.AbilityFlying == 0 || v&protocol.AbilityInstantBuild == 0 || v&protocol.AbilityMine == 0 {
		t.Errorf("abilities %b", v)
	}
	if pks := gameEventPackets(1, attach.GameEvent{Event: gameEventGameMode, Value: 1}); len(pks) != 2 || pks[0].(*packet.SetPlayerGameType).GameType != bedrockGameMode(1) ||
		pks[1].(*packet.UpdateAbilities).AbilityData.Layers[0].Values&protocol.AbilityMayFly == 0 {
		t.Errorf("game mode %+v", pks)
	}
	if pks := gameEventPackets(1, attach.GameEvent{Event: gameEventRainLevel, Value: 0.5}); len(pks) != 1 || pks[0].(*packet.LevelEvent).EventData != 32767 {
		t.Errorf("rain %+v", pks)
	}
	if pks := gameEventPackets(1, attach.GameEvent{Event: 13}); pks != nil {
		t.Error("wait-for-chunks rendered")
	}
	var id [16]byte
	id[0] = 7
	show := bossBarPackets(attach.BossBar{UUID: id, Op: attach.BossBarAdd, Title: "Dragon", Health: 0.5}, 9, mgl32.Vec3{1, 2, 3})
	if len(show) != 2 || show[0].(*packet.AddActor).EntityUniqueID >= 0 || show[1].(*packet.BossEvent).BossBarTitle != "Dragon" || show[1].(*packet.BossEvent).HealthPercentage != 0.5 {
		t.Errorf("boss show %+v", show)
	}
	if hide := bossBarPackets(attach.BossBar{UUID: id, Op: attach.BossBarRemove}, 9, mgl32.Vec3{}); len(hide) != 2 || hide[1].(*packet.RemoveActor).EntityUniqueID != show[0].(*packet.AddActor).EntityUniqueID {
		t.Errorf("boss hide %+v", hide)
	}
}
