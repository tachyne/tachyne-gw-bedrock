package gw

import (
	"encoding/base64"
	"sync"

	"github.com/google/uuid"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/login"
)

// Skins. A Bedrock player's own skin arrives with the login; kept by
// player UUID, it dresses that player in every other Bedrock session's
// player list instead of the grey placeholder. Java players stay grey —
// the world runs without Mojang profiles, so there is nothing to fetch.

// skinStore is the gateway's skins by player UUID.
type skinStore struct{ m sync.Map }

func (s *skinStore) put(id uuid.UUID, sk protocol.Skin) { s.m.Store(id, sk) }
func (s *skinStore) drop(id uuid.UUID)                  { s.m.Delete(id) }

// get is the player's skin, or the placeholder.
func (s *skinStore) get(id uuid.UUID) protocol.Skin {
	if v, ok := s.m.Load(id); ok {
		return v.(protocol.Skin)
	}
	return defaultSkin()
}

// skinFromClientData rebuilds the login's skin as a player-list skin
// (the fields base64-encoded in the login are bytes on the wire).
func skinFromClientData(d login.ClientData) protocol.Skin {
	dec := func(s string) []byte {
		b, _ := base64.StdEncoding.DecodeString(s)
		return b
	}
	sk := protocol.Skin{
		SkinID:                    d.SkinID,
		PlayFabID:                 d.PlayFabID,
		SkinResourcePatch:         dec(d.SkinResourcePatch),
		SkinImageWidth:            uint32(d.SkinImageWidth),
		SkinImageHeight:           uint32(d.SkinImageHeight),
		SkinData:                  dec(d.SkinData),
		CapeImageWidth:            uint32(d.CapeImageWidth),
		CapeImageHeight:           uint32(d.CapeImageHeight),
		CapeData:                  dec(d.CapeData),
		SkinGeometry:              dec(d.SkinGeometry),
		GeometryDataEngineVersion: []byte(d.SkinGeometryVersion),
		AnimationData:             dec(d.SkinAnimationData),
		PremiumSkin:               d.PremiumSkin,
		PersonaSkin:               d.PersonaSkin,
		PersonaCapeOnClassicSkin:  d.CapeOnClassicSkin,
		CapeID:                    d.CapeID,
		FullID:                    d.SkinID,
		SkinColour:                d.SkinColour,
		ArmSize:                   d.ArmSize,
		Trusted:                   true,
	}
	for _, a := range d.AnimatedImageData {
		sk.Animations = append(sk.Animations, protocol.SkinAnimation{
			ImageWidth: uint32(a.ImageWidth), ImageHeight: uint32(a.ImageHeight), ImageData: dec(a.Image),
			AnimationType: uint32(a.Type), FrameCount: float32(a.Frames), ExpressionType: uint32(a.AnimationExpression),
		})
	}
	for _, p := range d.PersonaPieces {
		sk.PersonaPieces = append(sk.PersonaPieces, protocol.PersonaPiece{
			PieceID: p.PieceID, PieceType: p.PieceType, PackID: p.PackID, Default: p.Default, ProductID: p.ProductID,
		})
	}
	for _, t := range d.PieceTintColours {
		sk.PieceTintColours = append(sk.PieceTintColours, protocol.PersonaPieceTintColour{PieceType: t.PieceType, Colours: t.Colours[:]})
	}
	if len(sk.SkinData) == 0 || sk.SkinImageWidth == 0 || sk.SkinImageHeight == 0 ||
		int(sk.SkinImageWidth*sk.SkinImageHeight*4) != len(sk.SkinData) {
		return defaultSkin() // a skin the client did not send whole
	}
	return sk
}
