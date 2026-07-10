package gw

// render.go turns domain attach data into Bedrock wire format. Chunks are
// re-encoded from the attach ChunkBody (canonical Java block-state IDs,
// bottom→top sections, (y*16+z)*16+x within a section) into Bedrock
// sub-chunk v9 payloads via dragonfly's proven chunk encoder. Palette values
// are hashed block network IDs (StartGame UseBlockNetworkIDHashes=true), so
// there is no dependency on the vanilla Bedrock palette order — the
// generated bedrockBlockRIDs table maps canonical state → fnv1a-32 hash of
// the Bedrock {name, states} NBT (see scripts/gen_bedrock_blocks.py).

import (
	attach "github.com/tachyne/tachyne-common/attach"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

const (
	sections = attach.Sections
	minY     = attach.MinY
)

// bedrockBlockRID maps a canonical Java block-state ID to the Bedrock hashed
// block network ID. Out-of-range (states newer than the generated table)
// degrades to the info-update block rather than misrendering as air.
func bedrockBlockRID(state uint32) uint32 {
	if state < uint32(len(bedrockBlockRIDs)) {
		return bedrockBlockRIDs[state]
	}
	return bedrockFallbackRID
}

// registry is the minimal chunk.BlockRegistry dragonfly's encoder needs: for
// NETWORK encoding only AirRuntimeID is ever consulted (palette values pass
// through verbatim); the rest of the interface exists for disk encoding and
// lighting, which we never use.
type registry struct{}

func (registry) BlockCount() int                                        { return len(bedrockBlockRIDs) }
func (registry) AirRuntimeID() uint32                                   { return bedrockAirRID }
func (registry) RuntimeIDToState(uint32) (string, map[string]any, bool) { return "", nil, false }
func (registry) StateToRuntimeID(string, map[string]any) (uint32, bool) { return 0, false }
func (registry) FilteringBlock(uint32) uint8                            { return 15 }
func (registry) LightBlock(uint32) uint8                                { return 0 }
func (registry) RandomTickBlock(uint32) bool                            { return false }
func (registry) NBTBlock(uint32) bool                                   { return false }
func (registry) LiquidDisplacingBlock(uint32) bool                      { return false }
func (registry) LiquidBlock(uint32) bool                                { return false }
func (registry) HashToRuntimeID(uint32) (uint32, bool)                  { return 0, false }

// overworldRange matches the engine's world exactly: 24 sections, -64..319 —
// the same vertical range Bedrock's overworld uses.
var overworldRange = cube.Range{minY, minY + sections*16 - 1}

// renderChunk re-encodes one domain chunk into a Bedrock LevelChunk packet
// (classic full-payload path: cache off, literal sub-chunk count).
func renderChunk(h attach.ChunkHeader, body *attach.ChunkBody) *packet.LevelChunk {
	c := chunk.New(registry{}, overworldRange)

	for sec := 0; sec < sections; sec++ {
		blocks := body.BlockStates[sec*4096 : (sec+1)*4096]
		baseY := int16(minY + sec*16)

		biome := uint32(1) // plains
		if sec < len(h.Biomes) && h.Biomes[sec] != "" {
			if id, ok := bedrockBiomeIDs[h.Biomes[sec]]; ok {
				biome = id
			}
		}

		for y := 0; y < 16; y++ {
			for z := 0; z < 16; z++ {
				row := blocks[(y*16+z)*16:]
				for x := 0; x < 16; x++ {
					if s := row[x]; s != 0 { // canonical air = 0; chunk.New prefills air
						c.SetBlock(uint8(x), baseY+int16(y), uint8(z), 0, bedrockBlockRID(s))
					}
					c.SetBiome(uint8(x), baseY+int16(y), uint8(z), biome)
				}
			}
		}
	}

	d := chunk.Encode(c, chunk.NetworkEncoding)
	payload := make([]byte, 0, 64*1024)
	for _, sub := range d.SubChunks {
		payload = append(payload, sub...)
	}
	payload = append(payload, d.Biomes...)
	payload = append(payload, 0) // border blocks

	return &packet.LevelChunk{
		Position:      [2]int32{h.CX, h.CZ},
		Dimension:     h.Dim,
		SubChunkCount: uint32(len(d.SubChunks)),
		RawPayload:    payload,
	}
}

// defaultSkin is a plain opaque 64×64 skin for PlayerList entries: the domain
// protocol carries Java skin textures (Mojang property blobs), which Bedrock
// cannot consume, so remote players render with a neutral placeholder.
func defaultSkin() protocol.Skin {
	data := make([]byte, 64*64*4)
	for i := 0; i < len(data); i += 4 {
		data[i], data[i+1], data[i+2], data[i+3] = 0x8f, 0x8f, 0x8f, 0xff
	}
	return protocol.Skin{
		SkinID:            "tachyne_default",
		SkinResourcePatch: []byte(`{"geometry":{"default":"geometry.humanoid.standard"}}`),
		SkinImageWidth:    64,
		SkinImageHeight:   64,
		SkinData:          data,
		ArmSize:           "wide",
		Trusted:           true,
	}
}
