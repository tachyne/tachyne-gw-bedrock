package gw

import (
	"testing"

	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
	attach "github.com/tachyne/tachyne-common/attach"
	tproto "github.com/tachyne/tachyne-common/protocol"
)

// A chunk's sign entries and the world's sign frames both become Bedrock
// sign block entities; an edited sign's text splits back into four lines.
func TestSigns(t *testing.T) {
	front := tproto.SignSideNBT{Lines: [4]string{"Welcome", "to", "tachyne", ""}, Color: "red", Glow: true}
	back := tproto.SignSideNBT{}
	// a block-entity section: one sign at (3, 70, 9) of chunk (2, -1), then a chest with no data
	bes := tproto.AppendVarInt(nil, 2)
	bes = append(bes, byte(3<<4|9))
	bes = tproto.AppendI16(bes, 70)
	bes = tproto.AppendVarInt(bes, blockEntitySign)
	bes = tproto.AppendSignNBT(bes, front, back, true)
	bes = append(bes, byte(0))
	bes = tproto.AppendI16(bes, 12)
	bes = tproto.AppendVarInt(bes, 1)
	bes = append(bes, 0) // TAG_End: no data
	pks := chunkBlockEntities(attach.ChunkHeader{CX: 2, CZ: -1, BEs: bes}, nil, nil)
	if len(pks) != 1 {
		t.Fatalf("%d sign packets", len(pks))
	}
	bd := pks[0].(*packet.BlockActorData)
	if bd.Position.X() != 35 || bd.Position.Y() != 70 || bd.Position.Z() != -7 || bd.NBTData["id"] != "Sign" || bd.NBTData["IsWaxed"] != true {
		t.Errorf("sign block entity %+v", bd)
	}
	ft := bd.NBTData["FrontText"].(map[string]any)
	if ft["Text"] != "Welcome\nto\ntachyne" || ft["IgnoreLighting"] != true || ft["SignTextColor"] != int32(11546150|int32(-1<<24)) {
		t.Errorf("front %v", ft)
	}
	if bt := bd.NBTData["BackText"].(map[string]any); bt["Text"] != "" {
		t.Errorf("back %v", bt)
	}
	// A hanging sign from the world's frame.
	h := signData(1, 2, 3, attach.SignSide{Lines: [4]string{"a", "b", "c", "d"}}, attach.SignSide{}, false, true)
	if h.NBTData["id"] != "HangingSign" || h.NBTData["FrontText"].(map[string]any)["Text"] != "a\nb\nc\nd" {
		t.Errorf("hanging %v", h.NBTData)
	}
	if l := signLines("one\ntwo"); l != [4]string{"one", "two", "", ""} {
		t.Errorf("lines %v", l)
	}
	if l := signLines("1\n2\n3\n4\n5"); l[3] != "4\n5" {
		t.Errorf("overflow lines %v", l)
	}
}

// Banners carry their base colour off the block state and their layers
// Bedrock's way round; campfires their four items; a bell its ring.
func TestBannersCampfiresBells(t *testing.T) {
	red := bannerBaseRanges[14] // red_banner (the standing ones come first, by dye)
	if red.Color != 14 || !isBanner(red.Min) || isBanner(1) {
		t.Fatalf("banner ranges %+v", red)
	}
	body := &attach.ChunkBody{BlockStates: make([]uint32, 24*4096)}
	body.BlockStates[blockIndex(5, 64, 6)] = red.Min
	bes := tproto.AppendVarInt(nil, 2)
	bes = append(bes, byte(5<<4|6))
	bes = tproto.AppendI16(bes, 64)
	bes = tproto.AppendVarInt(bes, blockEntityBanner)
	bes = tproto.AppendBannerNBT(bes, []tproto.BannerLayerNBT{{Pattern: "minecraft:creeper", Color: "lime"}})
	bes = append(bes, byte(0<<4|1))
	bes = tproto.AppendI16(bes, 65)
	bes = tproto.AppendVarInt(bes, blockEntityCampfire)
	bes = tproto.AppendCampfireNBT(bes, [4]string{"minecraft:beef", "", "minecraft:cod", ""})
	banners := map[[3]int32]int32{}
	pks := chunkBlockEntities(attach.ChunkHeader{CX: 0, CZ: 0, BEs: bes}, body, banners)
	if len(pks) != 2 {
		t.Fatalf("%d block entities", len(pks))
	}
	bd := pks[0].(*packet.BlockActorData)
	if bd.NBTData["id"] != "Banner" || bd.NBTData["Base"] != int32(1) || banners[[3]int32{5, 64, 6}] != 14 {
		t.Errorf("banner %v (bases %v)", bd.NBTData, banners)
	}
	pats := bd.NBTData["Patterns"].([]map[string]any)
	if len(pats) != 1 || pats[0]["Pattern"] != "cre" || pats[0]["Color"] != int32(10) {
		t.Errorf("patterns %v", pats)
	}
	cd := pks[1].(*packet.BlockActorData)
	if cd.NBTData["id"] != "Campfire" || cd.NBTData["Item1"].(map[string]any)["Name"] != "minecraft:beef" || cd.NBTData["Item2"] != nil || cd.NBTData["Item3"].(map[string]any)["Name"] != "minecraft:cod" {
		t.Errorf("campfire %v", cd.NBTData)
	}
	if b := bellData(1, 2, 3, 5); b.NBTData["Direction"] != int32(3) || b.NBTData["Ringing"] != byte(1) {
		t.Errorf("bell %v", b.NBTData)
	}
	if fr := bannerData(0, 0, 0, 15, []attach.BannerLayer{{Pattern: "minecraft:nope", Color: "red"}}); fr.NBTData["Base"] != int32(0) || len(fr.NBTData["Patterns"].([]map[string]any)) != 0 {
		t.Errorf("black banner with an unknown pattern %v", fr.NBTData)
	}
}
