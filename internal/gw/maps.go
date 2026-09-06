package gw

import (
	"bytes"
	"image/color"
	"sync"

	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
	attach "github.com/tachyne/tachyne-common/attach"
	tproto "github.com/tachyne/tachyne-common/protocol"
)

// Maps. The world streams a map as colour patches (vanilla's packed id<<2 |
// brightness bytes over a dirty rectangle) plus its markers; Bedrock wants
// the whole 128x128 texture as RGBA each time and asks for a map it has
// not seen (MapInfoRequest), so each map's texture is kept per session.
// A filled map item carries the map id in its Bedrock NBT so the client
// knows which texture to show.

// mapBaseColours are vanilla's MapColor base colours by id (0 = none).
var mapBaseColours = [...]uint32{
	0x000000, 0x7FB238, 0xF7E9A3, 0xC7C7C7, 0xFF0000, 0xA0A0FF, 0xA7A7A7, 0x007C00,
	0xFFFFFF, 0xA4A8B8, 0x976D4D, 0x707070, 0x4040FF, 0x8F7748, 0xFFFCF5, 0xD87F33,
	0xB24CD8, 0x6699D8, 0xE5E533, 0x7FCC19, 0xF27FA5, 0x4C4C4C, 0x999999, 0x4C7F99,
	0x7F3FB2, 0x334CB2, 0x664C33, 0x667F33, 0x993333, 0x191919, 0xFAEE4D, 0x5CDBD5,
	0x4A80FF, 0x00D93A, 0x815631, 0x700200, 0xD1B1A1, 0x9F5224, 0x95576C, 0x706C8A,
	0xBA8524, 0x677535, 0xA04D4E, 0x392923, 0x876B62, 0x575C5C, 0x7A4958, 0x4C3E5C,
	0x4C3223, 0x4C522A, 0x8E3C2E, 0x251610, 0xBD3031, 0x943F61, 0x5C191D, 0x167E86,
	0x3A8E8C, 0x562C3E, 0x14B485, 0x646464, 0xD8AF93, 0x7FA796,
}

// mapShades are the brightness multipliers by the packed byte's low two
// bits (LOW, NORMAL, HIGH, LOWEST).
var mapShades = [4]uint32{180, 220, 255, 135}

// mapColour is a packed map colour byte as RGBA (base 0 is transparent).
func mapColour(packed byte) color.RGBA {
	base, shade := int(packed>>2), mapShades[packed&3]
	if base == 0 || base >= len(mapBaseColours) {
		return color.RGBA{}
	}
	c := mapBaseColours[base]
	return color.RGBA{R: uint8((c >> 16 & 255) * shade / 255), G: uint8((c >> 8 & 255) * shade / 255), B: uint8((c & 255) * shade / 255), A: 255}
}

// mapIcons renumbers Java's map decoration types (player 0, frame 1, …)
// into Bedrock's icon ids and colours, the pairs Geyser established.
var mapIcons = map[int32]struct {
	id     byte
	colour color.RGBA
}{
	0: {0, color.RGBA{255, 255, 255, 255}}, 1: {7, color.RGBA{255, 255, 255, 255}}, 2: {2, color.RGBA{255, 255, 255, 255}},
	3: {3, color.RGBA{255, 255, 255, 255}}, 4: {4, color.RGBA{0, 0, 0, 255}}, 5: {5, color.RGBA{255, 255, 255, 255}},
	6: {6, color.RGBA{255, 255, 255, 255}}, 7: {13, color.RGBA{255, 255, 255, 255}}, 8: {14, color.RGBA{255, 255, 255, 255}},
	9:  {15, color.RGBA{255, 255, 255, 255}},
	10: {13, color.RGBA{255, 255, 255, 255}}, 11: {13, color.RGBA{249, 128, 29, 255}}, 12: {13, color.RGBA{199, 78, 189, 255}},
	13: {13, color.RGBA{58, 179, 218, 255}}, 14: {13, color.RGBA{254, 216, 61, 255}}, 15: {13, color.RGBA{128, 199, 31, 255}},
	16: {13, color.RGBA{243, 139, 170, 255}}, 17: {13, color.RGBA{71, 79, 82, 255}}, 18: {13, color.RGBA{157, 157, 151, 255}},
	19: {13, color.RGBA{22, 156, 156, 255}}, 20: {13, color.RGBA{137, 50, 184, 255}}, 21: {13, color.RGBA{60, 68, 170, 255}},
	22: {13, color.RGBA{131, 84, 50, 255}}, 23: {13, color.RGBA{94, 124, 22, 255}}, 24: {13, color.RGBA{176, 46, 38, 255}},
	25: {13, color.RGBA{29, 29, 33, 255}}, 26: {4, color.RGBA{255, 255, 255, 255}},
	27: {17, color.RGBA{255, 255, 255, 255}}, 28: {18, color.RGBA{255, 255, 255, 255}}, 29: {19, color.RGBA{255, 255, 255, 255}},
	30: {20, color.RGBA{255, 255, 255, 255}}, 31: {21, color.RGBA{255, 255, 255, 255}}, 32: {22, color.RGBA{255, 255, 255, 255}},
	33: {23, color.RGBA{255, 255, 255, 255}}, 34: {24, color.RGBA{255, 255, 255, 255}},
}

// mapState is one map's texture and markers as this session knows them.
type mapState struct {
	pixels [128 * 128]color.RGBA
	scale  byte
	locked bool
	decor  []attach.MapDecoration
}

// mapStore keeps the session's maps by id.
type mapStore struct {
	mu   sync.Mutex
	maps map[int32]*mapState
}

func newMapStore() *mapStore { return &mapStore{maps: map[int32]*mapState{}} }

// apply folds a map frame in and renders the whole map for the client.
func (s *mapStore) apply(e attach.MapData, dim int32) *packet.ClientBoundMapItemData {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := s.maps[e.MapID]
	if m == nil {
		m = &mapState{}
		s.maps[e.MapID] = m
	}
	m.scale, m.locked = byte(e.Scale), e.Locked
	if e.Width > 0 && e.Height > 0 && len(e.Colors) >= int(e.Width)*int(e.Height) {
		for row := 0; row < int(e.Height); row++ {
			for col := 0; col < int(e.Width); col++ {
				x, y := int(e.X)+col, int(e.Y)+row
				if x < 128 && y < 128 {
					m.pixels[y*128+x] = mapColour(e.Colors[row*int(e.Width)+col])
				}
			}
		}
	}
	if e.HasDecor || e.Decor != nil {
		m.decor = e.Decor
	}
	return s.render(e.MapID, m, dim)
}

// get renders a map the client asked for, or nil for one never seen.
func (s *mapStore) get(id int32, dim int32) *packet.ClientBoundMapItemData {
	s.mu.Lock()
	defer s.mu.Unlock()
	if m := s.maps[id]; m != nil {
		return s.render(id, m, dim)
	}
	return nil
}

func (s *mapStore) render(id int32, m *mapState, dim int32) *packet.ClientBoundMapItemData {
	pk := &packet.ClientBoundMapItemData{
		MapID: int64(id), UpdateFlags: packet.MapUpdateFlagTexture | packet.MapUpdateFlagDecoration | packet.MapUpdateFlagInitialisation,
		Dimension: byte(dim), LockedMap: m.locked, Scale: m.scale, MapsIncludedIn: []int64{int64(id)},
		Height: 128, Width: 128, Pixels: append([]color.RGBA(nil), m.pixels[:]...),
	}
	for i, d := range m.decor {
		icon, ok := mapIcons[d.Type]
		if !ok {
			continue
		}
		pk.TrackedObjects = append(pk.TrackedObjects, protocol.MapTrackedObject{Type: protocol.MapObjectTypeEntity, EntityUniqueID: int64(i)})
		pk.Decorations = append(pk.Decorations, protocol.MapDecoration{Type: icon.id, Rotation: d.Rot, X: byte(d.X), Y: byte(d.Z), Label: d.Name, Colour: icon.colour})
	}
	return pk
}

// mapItemNBT is a filled map's Bedrock NBT: the map id the client asks
// the texture for. The map id is the stack's map_id component (the one
// component a plain filled map carries).
func mapItemNBT(st attach.ItemStack) map[string]any {
	if len(st.Components) == 0 {
		return nil
	}
	r := bytes.NewReader(st.Components)
	addC, err := tproto.ReadVarInt(r)
	if err != nil || addC <= 0 {
		return nil
	}
	if _, err := tproto.ReadVarInt(r); err != nil {
		return nil
	}
	if id, err := tproto.ReadVarInt(r); err != nil || id != componentMapID {
		return nil
	}
	mapID, err := tproto.ReadVarInt(r)
	if err != nil {
		return nil
	}
	return map[string]any{"map_uuid": int64(mapID), "map_name_index": mapID, "map_display_players": byte(1)}
}

const componentMapID = 37 // minecraft:map_id, canonical
