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

// Rowing from PlayerAuthInput: forward rows both paddles, and Bedrock's
// paddle flags are crossed (Geyser: "Yes. These are flipped.").
func TestBoatPaddles(t *testing.T) {
	const size = 128
	for _, tc := range []struct {
		ids  []int32
		want attach.PaddleBoat
	}{
		{nil, attach.PaddleBoat{}},
		{[]int32{packet.InputFlagUp}, attach.PaddleBoat{Left: true, Right: true}},
		{[]int32{packet.InputFlagPaddlingRight}, attach.PaddleBoat{Left: true}},
		{[]int32{packet.InputFlagPaddlingLeft}, attach.PaddleBoat{Right: true}},
	} {
		if got := boatPaddles(protocol.NewInputFlagsFromIDs(size, tc.ids)); got != tc.want {
			t.Errorf("flags %v: %+v, want %+v", tc.ids, got, tc.want)
		}
	}
}
