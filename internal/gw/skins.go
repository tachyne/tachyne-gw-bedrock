package gw

import (
	"encoding/base64"
	"image/color"
	"strconv"
	"strings"
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
		SkinColour:                skinColour(d.SkinColour),
		ArmSize:                   armSize(d.ArmSize),
		Trusted:                   true,
		ProfileHash:               d.ProfileHash,
	}
	for _, a := range d.AnimatedImageData {
		sk.Animations = append(sk.Animations, protocol.SkinAnimation{
			ImageWidth: uint32(a.ImageWidth), ImageHeight: uint32(a.ImageHeight), ImageData: dec(a.Image),
			AnimationType: uint32(a.Type), FrameCount: float32(a.Frames), ExpressionType: uint32(a.AnimationExpression),
		})
	}
	for _, p := range d.PersonaPieces {
		sk.PersonaPieces = append(sk.PersonaPieces, protocol.PersonaPiece{
			PieceID: p.PieceID, PieceType: pieceType(p.PieceType), PackID: uuid.MustParse(orNil(p.PackID)), Default: p.Default, ProductID: p.ProductID,
		})
	}
	for _, t := range d.PieceTintColours {
		tint := protocol.PersonaPieceTintColour{PieceType: t.PieceType}
		for i, c := range t.Colours {
			tint.Colours[i] = skinColour(c)
		}
		sk.PieceTintColours = append(sk.PieceTintColours, tint)
	}
	if len(sk.SkinData) == 0 || sk.SkinImageWidth == 0 || sk.SkinImageHeight == 0 ||
		int(sk.SkinImageWidth*sk.SkinImageHeight*4) != len(sk.SkinData) {
		return defaultSkin() // a skin the client did not send whole
	}
	return sk
}

// Since 1.26.40 the skin's arm size, colours and persona piece types travel
// typed, while the login still carries the strings: these read them the way
// the protocol's own tint-colour mapping does.

// armSize reads the login's "wide"/"slim".
func armSize(s string) uint8 {
	if s == "slim" {
		return protocol.ArmSizeSlim
	}
	return protocol.ArmSizeWide
}

// skinColour reads a login colour: "#rrggbb", or "#aarrggbb" as the tints
// send them. Anything unreadable is opaque black rather than a failed skin.
func skinColour(s string) color.RGBA {
	h := strings.TrimPrefix(s, "#")
	v, err := strconv.ParseUint(h, 16, 32)
	if err != nil {
		return color.RGBA{A: 0xff}
	}
	c := color.RGBA{R: uint8(v >> 16), G: uint8(v >> 8), B: uint8(v), A: 0xff}
	if len(h) == 8 {
		c.A = uint8(v >> 24)
	}
	return c
}

// personaPieceTypes is the wire enum's order (protocol.PieceType*), by the
// name the login uses without its "persona_" prefix.
var personaPieceTypes = func() map[string]uint32 {
	names := []string{"skeleton", "body", "skin", "bottom", "feet", "dress", "top", "high_pants", "hands",
		"outerwear", "facial_hair", "mouth", "eyes", "hair", "hood", "back", "face_accessory", "head", "legs",
		"left_leg", "right_leg", "arms", "left_arm", "right_arm", "capes", "classic_skin", "emote"}
	m := make(map[string]uint32, len(names))
	for i, n := range names {
		m[n] = uint32(protocol.PieceTypeSkeleton + i)
	}
	return m
}()

// pieceType reads a login piece type ("persona_hair"; the hands are
// "persona_hand", as the protocol's tint mapping has it).
func pieceType(s string) uint32 {
	n := strings.TrimPrefix(s, "persona_")
	if n == "hand" {
		n = "hands"
	}
	if t, ok := personaPieceTypes[n]; ok {
		return t
	}
	return protocol.PieceTypeUnknown
}

// orNil is a pack id that parses: the login's, or the nil UUID.
func orNil(s string) string {
	if _, err := uuid.Parse(s); err != nil {
		return uuid.Nil.String()
	}
	return s
}
