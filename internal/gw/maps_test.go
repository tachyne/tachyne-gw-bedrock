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
	scale, _ := pk.Scale.Value()
	width, _ := pk.Width.Value()
	pixels, _ := pk.Pixels.Value()
	decor, _ := pk.Decorations.Value()
	if pk.MapID != 5 || scale != 1 || pk.Dimension != 1 || width != 128 || len(pixels) != 128*128 {
		t.Fatalf("map packet %+v", pk.MapID)
	}
	if pixels[20*128+10].G != 0xB2 || pixels[21*128+11].G != 0xB2 || pixels[0].A != 0 {
		t.Errorf("patch not applied")
	}
	if len(decor) != 2 || decor[0].Type != 0 || decor[0].X != 4 || int8(decor[0].Y) != -6 ||
		decor[1].Type != 13 || decor[1].Colour != (color.RGBA{176, 46, 38, 255}) {
		t.Errorf("decorations %+v", decor)
	}
	again := s.get(5, 1)
	if again == nil {
		t.Fatal("map not kept for the client's request")
	}
	againPixels, _ := again.Pixels.Value()
	againDecor, _ := again.Decorations.Value()
	if againPixels[20*128+10].G != 0xB2 || len(againDecor) != 2 {
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
