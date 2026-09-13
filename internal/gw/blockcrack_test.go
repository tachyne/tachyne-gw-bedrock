package gw

import (
	"testing"

	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
	"github.com/tachyne/tachyne-common/attach"
)

func TestBlockCrackEstimate(t *testing.T) {
	var tr blockCrackTracker
	pk := tr.packet(attach.BlockBreakProgress{EID: 7, X: 1, Y: 64, Z: 2, Progress: 0}, 100)
	if pk.EventType != packet.LevelEventStartBlockCracking || pk.EventData != 65535/6000 {
		t.Fatalf("first stage starts a slow crack: %+v", pk)
	}
	// Twenty-four ticks per stage (a zombie on a door): remaining 9 stages.
	pk = tr.packet(attach.BlockBreakProgress{EID: 7, X: 1, Y: 64, Z: 2, Progress: 1}, 124)
	if pk.EventType != packet.LevelEventUpdateBlockCracking || pk.EventData != 65535/9*24 {
		t.Fatalf("second stage re-times: %+v", pk)
	}
	pk = tr.packet(attach.BlockBreakProgress{EID: 7, X: 1, Y: 64, Z: 2, Progress: -1}, 200)
	if pk.EventType != packet.LevelEventStopBlockCracking {
		t.Fatalf("-1 clears: %+v", pk)
	}
	if _, ok := tr.last[crackKey{1, 64, 2}]; ok {
		t.Fatal("cleared blocks are forgotten")
	}
}
