package gw

import (
	"encoding/base64"
	"testing"

	"github.com/google/uuid"
	"github.com/sandertv/gophertunnel/minecraft/protocol/login"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
	"github.com/tachyne/tachyne-common/attach"
)

// A login skin becomes a player-list skin; a torn one falls back to the
// placeholder; the store dresses only the players it knows.
func TestSkins(t *testing.T) {
	pix := make([]byte, 64*64*4)
	pix[0] = 0xff
	d := login.ClientData{SkinID: "abc", SkinImageWidth: 64, SkinImageHeight: 64,
		SkinData: base64.StdEncoding.EncodeToString(pix), ArmSize: "slim",
		SkinResourcePatch: base64.StdEncoding.EncodeToString([]byte(`{"geometry":{"default":"geometry.humanoid.customSlim"}}`))}
	sk := skinFromClientData(d)
	if sk.SkinID != "abc" || len(sk.SkinData) != len(pix) || sk.SkinData[0] != 0xff || sk.ArmSize != "slim" || !sk.Trusted || string(sk.SkinResourcePatch) == "" {
		t.Errorf("skin %+v", sk.SkinID)
	}
	torn := d
	torn.SkinImageHeight = 32
	if s := skinFromClientData(torn); s.SkinID != "tachyne_default" {
		t.Errorf("torn skin accepted: %s", s.SkinID)
	}
	var store skinStore
	id := uuid.New()
	if store.get(id).SkinID != "tachyne_default" {
		t.Error("unknown player not grey")
	}
	store.put(id, sk)
	if store.get(id).SkinID != "abc" {
		t.Error("stored skin not returned")
	}
	store.drop(id)
	if store.get(id).SkinID != "tachyne_default" {
		t.Error("dropped skin still returned")
	}
}

// A particle Bedrock has no level event for is spawned by Geyser's name.
func TestNamedParticles(t *testing.T) {
	named := int32(-1)
	for i, n := range bedrockParticleNames {
		if n != "" {
			if _, ok := particleEvents[int32(i)]; !ok {
				named = int32(i)
				break
			}
		}
	}
	if named < 0 {
		t.Skip("no named-only particle")
	}
	pk := particleEvent(attach.Particles{PID: named, X: 1, Y: 2, Z: 3}, 1)
	sp, ok := pk.(*packet.SpawnParticleEffect)
	if !ok || sp.Dimension != 1 || sp.ParticleName != bedrockParticleNames[named] || sp.EntityUniqueID != -1 {
		t.Errorf("named particle %+v", pk)
	}
	if _, ok := particleEvent(attach.Particles{PID: 5}, 0).(*packet.LevelEvent); !ok {
		t.Error("crit is a level event")
	}
	if particleEvent(attach.Particles{PID: 9999}, 0) != nil {
		t.Error("an unknown particle rendered")
	}
}
