package gw

import (
	"bytes"
	"encoding/binary"
	"strings"

	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
	attach "github.com/tachyne/tachyne-common/attach"
	tproto "github.com/tachyne/tachyne-common/protocol"
)

// Signs. Java carries a sign's two sides as block-entity NBT (four
// messages, a dye colour, a glow flag, waxed) — in a chunk's block-entity
// section for signs already standing, and as a SignText frame when one is
// edited. Bedrock wants the same as its own block entity ("FrontText" /
// "BackText" with the lines joined by newlines, the colour as ARGB, glow as
// IgnoreLighting). Editing: the world opens the editor with a SignEditor
// frame (Bedrock's OpenSign), and the client hands back the whole block
// entity, whose edited side is split into four lines for the world.

// Block-entity types (the canonical block_entity_type registry).
const (
	blockEntitySign        = 7
	blockEntityHangingSign = 8
)

// signColours are Bedrock's ARGB sign text colours by dye name (the table
// Geyser established; black is the default 0).
var signColours = map[string]int32{
	"white": 16383998, "orange": 16351261, "magenta": 13061821, "light_blue": 3847130, "yellow": 16701501,
	"lime": 8439583, "pink": 15961002, "gray": 4673362, "light_gray": 10329495, "cyan": 1481884,
	"purple": 8991416, "blue": 3949738, "brown": 8606770, "green": 6192150, "red": 11546150,
}

// signSideNBT renders one side.
func signSideNBT(s attach.SignSide) map[string]any {
	text := strings.TrimRight(strings.Join(s.Lines[:], "\n"), "\n")
	return map[string]any{
		"Text":           text,
		"SignTextColor":  signColours[s.Color] | int32(-1<<24), // alpha 255
		"IgnoreLighting": s.Glow,
	}
}

// signData renders a sign as Bedrock's block entity.
func signData(x, y, z int32, front, back attach.SignSide, waxed, hanging bool) *packet.BlockActorData {
	id := "Sign"
	if hanging {
		id = "HangingSign"
	}
	return &packet.BlockActorData{
		Position: protocol.BlockPos{x, y, z},
		NBTData: map[string]any{
			"id": id, "x": x, "y": y, "z": z, "isMovable": byte(1),
			"FrontText": signSideNBT(front), "BackText": signSideNBT(back), "IsWaxed": waxed,
		},
	}
}

// signSideFromNBT reads a Java sign side (messages, color, has_glowing_text).
func signSideFromNBT(v any) attach.SignSide {
	var s attach.SignSide
	m, ok := v.(map[string]any)
	if !ok {
		return s
	}
	if msgs, ok := m["messages"].([]any); ok {
		for i := 0; i < 4 && i < len(msgs); i++ {
			s.Lines[i], _ = msgs[i].(string)
		}
	}
	s.Color, _ = m["color"].(string)
	if g, ok := m["has_glowing_text"].(byte); ok {
		s.Glow = g != 0
	}
	return s
}

// chunkSigns pulls the signs out of a chunk's block-entity section
// (count, then per entry: packed xz, y, type, NBT) as Bedrock block
// entities. Entries whose NBT is not a sign's are walked past.
func chunkSigns(h attach.ChunkHeader) []packet.Packet {
	if len(h.BEs) == 0 {
		return nil
	}
	r := bytes.NewReader(h.BEs)
	n, err := tproto.ReadVarInt(r)
	if err != nil || n <= 0 || n > 4096 {
		return nil
	}
	var out []packet.Packet
	for i := int32(0); i < n; i++ {
		xz, err := r.ReadByte()
		if err != nil {
			return out
		}
		var yb [2]byte
		if _, err := r.Read(yb[:]); err != nil {
			return out
		}
		y := int32(int16(binary.BigEndian.Uint16(yb[:])))
		typ, err := tproto.ReadVarInt(r)
		if err != nil {
			return out
		}
		tag, ok := readJavaNBT(r)
		if !ok {
			return out
		}
		if typ != blockEntitySign && typ != blockEntityHangingSign {
			continue
		}
		x, z := h.CX*16+int32(xz>>4), h.CZ*16+int32(xz&15)
		waxed := false
		if w, ok := tag["is_waxed"].(byte); ok {
			waxed = w != 0
		}
		out = append(out, signData(x, y, z, signSideFromNBT(tag["front_text"]), signSideFromNBT(tag["back_text"]),
			waxed, typ == blockEntityHangingSign))
	}
	return out
}

// signLines splits an edited Bedrock sign text into Java's four lines.
func signLines(text string) [4]string {
	var lines [4]string
	for i, l := range strings.SplitN(text, "\n", 4) {
		lines[i] = l
	}
	return lines
}
