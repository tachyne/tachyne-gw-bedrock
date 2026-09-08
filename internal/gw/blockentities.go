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

// Block entities beyond signs: banners (their layers, on a base colour
// Bedrock keeps on the block entity rather than the block), campfires
// (the four cooking items) and the ringing bell. Standing ones come out of
// a chunk's block-entity section; changes arrive as the world's frames.

const (
	blockEntityBanner   = 20
	blockEntityCampfire = 33
	blockEntityShelf    = 40 // the 1.21.9 wooden shelves
)

// dyeIDs are Java's dye colour ids by name.
var dyeIDs = map[string]int32{
	"white": 0, "orange": 1, "magenta": 2, "light_blue": 3, "yellow": 4, "lime": 5, "pink": 6, "gray": 7,
	"light_gray": 8, "cyan": 9, "purple": 10, "blue": 11, "brown": 12, "green": 13, "red": 14, "black": 15,
}

// bannerCodes is the Bedrock short id for each Java banner pattern name
// (the loom table, the other way round).
var bannerCodes = func() map[string]string {
	m := map[string]string{}
	for short, java := range bedrockBannerPatterns {
		m[java] = short
	}
	return m
}()

// bannerBaseFor is a banner block state's own dye colour (white when the
// state is not a banner).
func bannerBaseFor(state uint32) int32 {
	for _, r := range bannerBaseRanges {
		if state >= r.Min && state <= r.Max {
			return r.Color
		}
	}
	return 0
}

// isBanner reports whether a block state is a banner.
func isBanner(state uint32) bool {
	for _, r := range bannerBaseRanges {
		if state >= r.Min && state <= r.Max {
			return true
		}
	}
	return false
}

// bannerData renders a banner: Bedrock counts dye colours the other way
// round (15 - Java's id) for both the base and the layers.
func bannerData(x, y, z, base int32, layers []attach.BannerLayer) *packet.BlockActorData {
	patterns := make([]map[string]any, 0, len(layers))
	for _, l := range layers {
		name := strings.TrimPrefix(l.Pattern, "minecraft:")
		code, ok := bannerCodes[name]
		if !ok {
			continue
		}
		patterns = append(patterns, map[string]any{"Pattern": code, "Color": 15 - dyeIDs[l.Color]})
	}
	return &packet.BlockActorData{
		Position: protocol.BlockPos{x, y, z},
		NBTData: map[string]any{
			"id": "Banner", "x": x, "y": y, "z": z, "isMovable": byte(1),
			"Base": 15 - base, "Type": int32(0), "Patterns": patterns,
		},
	}
}

// campfireData renders a campfire's four cooking slots.
func campfireData(x, y, z int32, items [4]string) *packet.BlockActorData {
	nbt := map[string]any{"id": "Campfire", "x": x, "y": y, "z": z, "isMovable": byte(1)}
	for i, it := range items {
		if it == "" {
			continue
		}
		if !strings.Contains(it, ":") {
			it = "minecraft:" + it
		}
		nbt["Item"+string(rune('1'+i))] = map[string]any{"Name": it, "Count": byte(1), "Damage": int16(0)}
	}
	return &packet.BlockActorData{Position: protocol.BlockPos{x, y, z}, NBTData: nbt}
}

// bellData renders a bell mid-ring. The world's param is Java's 3D
// direction (down 0, up 1, north 2, south 3, west 4, east 5); Bedrock
// counts south 0, west 1, north 2, east 3.
func bellData(x, y, z int32, param uint8) *packet.BlockActorData {
	dir := map[uint8]int32{3: 0, 4: 1, 2: 2, 5: 3}[param]
	return &packet.BlockActorData{
		Position: protocol.BlockPos{x, y, z},
		NBTData: map[string]any{
			"id": "Bell", "x": x, "y": y, "z": z, "isMovable": byte(1),
			"Direction": dir, "Ringing": byte(1), "Ticks": int32(0),
		},
	}
}

// chunkBlockEntities pulls the block entities Bedrock renders out of a
// chunk's block-entity section (count, then per entry: packed xz, y,
// type, NBT): signs, banners (their base colour read off the chunk's
// block state) and campfires. Other entries are walked past. Banner
// positions are remembered so a later pattern frame knows its base.
func chunkBlockEntities(h attach.ChunkHeader, body *attach.ChunkBody, banners map[[3]int32]int32) []packet.Packet {
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
		lx, lz := int32(xz>>4), int32(xz&15)
		x, z := h.CX*16+lx, h.CZ*16+lz
		switch typ {
		case blockEntitySign, blockEntityHangingSign:
			waxed := false
			if w, ok := tag["is_waxed"].(byte); ok {
				waxed = w != 0
			}
			out = append(out, signData(x, y, z, signSideFromNBT(tag["front_text"]), signSideFromNBT(tag["back_text"]),
				waxed, typ == blockEntityHangingSign))
		case blockEntityBanner:
			base := int32(0)
			if body != nil {
				if idx := blockIndex(lx, y, lz); idx >= 0 && idx < len(body.BlockStates) {
					base = bannerBaseFor(body.BlockStates[idx])
				}
			}
			if banners != nil {
				banners[[3]int32{x, y, z}] = base
			}
			var layers []attach.BannerLayer
			if pats, ok := tag["patterns"].([]any); ok {
				for _, p := range pats {
					if pm, ok := p.(map[string]any); ok {
						pat, _ := pm["pattern"].(string)
						col, _ := pm["color"].(string)
						layers = append(layers, attach.BannerLayer{Pattern: pat, Color: col})
					}
				}
			}
			out = append(out, bannerData(x, y, z, base, layers))
		case blockEntityCampfire:
			var items [4]string
			if list, ok := tag["Items"].([]any); ok {
				for _, it := range list {
					if im, ok := it.(map[string]any); ok {
						slot, _ := im["Slot"].(byte)
						id, _ := im["id"].(string)
						if slot < 4 {
							items[slot] = id
						}
					}
				}
			}
			out = append(out, campfireData(x, y, z, items))
		case blockEntityShelf:
			var items [3]attach.ShelfItem
			if list, ok := tag["Items"].([]any); ok {
				for _, it := range list {
					if im, ok := it.(map[string]any); ok {
						slot, _ := im["Slot"].(byte)
						id, _ := im["id"].(string)
						count, _ := im["count"].(int32)
						if slot < 3 {
							items[slot] = attach.ShelfItem{Name: id, Count: count}
						}
					}
				}
			}
			out = append(out, shelfData(x, y, z, items))
		}
	}
	return out
}

// shelfData renders a wooden shelf's three display slots. Bedrock places
// the items by list index, so the list is always three long with empty
// items (Geyser's EMPTY_ITEM: no name, count 0) for the bare slots.
func shelfData(x, y, z int32, items [3]attach.ShelfItem) *packet.BlockActorData {
	list := make([]any, 3)
	for i, it := range items {
		if it.Name == "" || it.Count <= 0 {
			list[i] = map[string]any{"Name": "", "Count": byte(0), "Damage": int16(0)}
			continue
		}
		name := it.Name
		if !strings.Contains(name, ":") {
			name = "minecraft:" + name
		}
		list[i] = map[string]any{"Name": name, "Count": byte(it.Count), "Damage": int16(0)}
	}
	return &packet.BlockActorData{
		Position: protocol.BlockPos{x, y, z},
		NBTData:  map[string]any{"id": "Shelf", "x": x, "y": y, "z": z, "isMovable": byte(1), "Items": list},
	}
}

// blockIndex is a block's index in the chunk body (sections bottom-up,
// (y*16+z)*16+x within a section), -1 below the floor.
func blockIndex(lx, y, lz int32) int {
	if y < minY {
		return -1
	}
	sec := int(y-minY) / 16
	ly := int(y-minY) % 16
	return sec*4096 + (ly*16+int(lz))*16 + int(lx)
}
