package gw

import (
	"testing"

	attach "github.com/tachyne/tachyne-common/attach"
)

// The nether and end render only the sections inside Bedrock's shorter
// ranges, at the same absolute y; the overworld keeps the whole stack.
func TestDimensionChunkLayout(t *testing.T) {
	body := &attach.ChunkBody{BlockStates: make([]uint32, 24*4096)}
	// a stone block at y=64 (engine section 8) and one at y=-60 (section 0)
	set := func(y int) { sec, ly := (y+64)/16, (y+64)%16; body.BlockStates[sec*4096+(ly*16+0)*16+0] = 1 }
	set(64)
	set(-60)
	for _, c := range []struct {
		dim  int32
		subs uint32
	}{{0, 24}, {1, 8}, {2, 16}} {
		pk := renderChunk(attach.ChunkHeader{CX: 1, CZ: 2, Dim: c.dim}, body)
		if pk.Dimension != c.dim || pk.SubChunkCount != c.subs {
			t.Errorf("dim %d: %d subchunks in dim %d", c.dim, pk.SubChunkCount, pk.Dimension)
		}
	}
	if pk := emptyChunk(1, 0, 0); pk.SubChunkCount != 8 || len(pk.RawPayload) == 0 {
		t.Errorf("empty nether column: %+v", pk.SubChunkCount)
	}
	if r, base := dimLayout(1); r.Min() != 0 || r.Max() != 127 || base != 4 {
		t.Errorf("nether layout %v %d", r, base)
	}
}
