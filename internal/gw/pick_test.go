package gw

import (
	"testing"

	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
	"github.com/tachyne/tachyne-common/attach"
)

func TestPickFrame(t *testing.T) {
	got, ok := pickFrame(&packet.BlockPickRequest{Position: protocol.BlockPos{-3, 64, 700}, AddBlockNBT: true, HotBarSlot: 2})
	if !ok || got != (attach.PickItem{X: -3, Y: 64, Z: 700, IncludeData: true}) {
		t.Fatalf("block pick = %+v, %v", got, ok)
	}
	got, ok = pickFrame(&packet.ActorPickRequest{EntityUniqueID: 41})
	if !ok || got != (attach.PickItem{Entity: true, EID: 41}) {
		t.Fatalf("actor pick = %+v, %v", got, ok)
	}
	if _, ok := pickFrame(&packet.Animate{}); ok {
		t.Fatal("an unrelated packet became a pick")
	}
}
