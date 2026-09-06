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
	pks := chunkSigns(attach.ChunkHeader{CX: 2, CZ: -1, BEs: bes})
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
