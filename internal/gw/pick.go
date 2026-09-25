package gw

import (
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
	"github.com/tachyne/tachyne-common/attach"
)

// pickFrame turns a Bedrock middle click into the world's PickItem: the
// world finds the item, selects or swaps it in, and answers with the held
// slot (PlayerHotBar) and the slots it changed. An actor's unique id is its
// runtime id, which is the world's eid.
func pickFrame(pk packet.Packet) (attach.PickItem, bool) {
	switch p := pk.(type) {
	case *packet.BlockPickRequest:
		return attach.PickItem{X: p.Position.X(), Y: p.Position.Y(), Z: p.Position.Z(), IncludeData: p.AddBlockNBT}, true
	case *packet.ActorPickRequest:
		return attach.PickItem{Entity: true, EID: int32(p.EntityUniqueID), IncludeData: p.WithData}, true
	}
	return attach.PickItem{}, false
}

// boatPaddles is the rower's paddle input from PlayerAuthInput, the way
// Geyser reads it: forward rows both, and Bedrock's paddle flags are
// crossed — PADDLE_RIGHT is the left paddle, PADDLE_LEFT the right.
func boatPaddles(in protocol.InputFlags) attach.PaddleBoat {
	up := in.Load(packet.InputFlagUp)
	return attach.PaddleBoat{
		Left:  up || in.Load(packet.InputFlagPaddlingRight),
		Right: up || in.Load(packet.InputFlagPaddlingLeft),
	}
}
