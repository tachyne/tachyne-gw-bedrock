package gw

import (
	"image/color"
	"testing"

	attach "github.com/tachyne/tachyne-common/attach"
	tproto "github.com/tachyne/tachyne-common/protocol"
)

// Map colours shade vanilla's base table; patches accumulate into the
// texture; markers renumber into Bedrock icons; a filled map's id rides
// as NBT.
func TestMaps(t *testing.T) {
	if len(mapBaseColours) != 62 {
		t.Fatalf("%d base colours", len(mapBaseColours))
	}
	if c := mapColour(1<<2 | 2); c != (color.RGBA{0x7F, 0xB2, 0x38, 255}) { // grass, HIGH
		t.Errorf("grass %+v", c)
	}
	if c := mapColour(1<<2 | 0); c.R != 0x7F*180/255 || c.A != 255 {
		t.Errorf("grass LOW %+v", c)
	}
	if c := mapColour(0); c.A != 0 {
		t.Errorf("none %+v", c)
	}
	s := newMapStore()
	patch := make([]byte, 4)
	for i := range patch {
		patch[i] = 1<<2 | 2
	}
	pk := s.apply(attach.MapData{MapID: 5, Scale: 1, X: 10, Y: 20, Width: 2, Height: 2, Colors: patch,
		HasDecor: true, Decor: []attach.MapDecoration{{Type: 0, X: 4, Z: -6, Rot: 3}, {Type: 24, X: 0, Z: 0}}}, 1)
	if pk.MapID != 5 || pk.Scale != 1 || pk.Dimension != 1 || pk.Width != 128 || len(pk.Pixels) != 128*128 {
		t.Fatalf("map packet %+v", pk.MapID)
	}
	if pk.Pixels[20*128+10].G != 0xB2 || pk.Pixels[21*128+11].G != 0xB2 || pk.Pixels[0].A != 0 {
		t.Errorf("patch not applied")
	}
	if len(pk.Decorations) != 2 || pk.Decorations[0].Type != 0 || pk.Decorations[0].X != 4 || int8(pk.Decorations[0].Y) != -6 ||
		pk.Decorations[1].Type != 13 || pk.Decorations[1].Colour != (color.RGBA{176, 46, 38, 255}) {
		t.Errorf("decorations %+v", pk.Decorations)
	}
	if again := s.get(5, 1); again == nil || again.Pixels[20*128+10].G != 0xB2 || len(again.Decorations) != 2 {
		t.Error("map not kept for the client's request")
	}
	if s.get(6, 1) != nil {
		t.Error("an unseen map rendered")
	}
	comps := tproto.AppendVarInt(nil, 1)
	comps = tproto.AppendVarInt(comps, 0)
	comps = tproto.AppendVarInt(comps, componentMapID)
	comps = tproto.AppendVarInt(comps, 5)
	nbt := stackNBT(attach.ItemStack{ID: 1, Count: 1, Components: comps})
	if nbt == nil || nbt["map_uuid"] != int64(5) {
		t.Errorf("map item nbt %v", nbt)
	}
}
