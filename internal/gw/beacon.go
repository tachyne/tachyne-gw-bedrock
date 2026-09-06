package gw

import (
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
	attach "github.com/tachyne/tachyne-common/attach"
)

// The beacon. Bedrock's beacon screen reads the chosen effects off the
// beacon's block entity (Java sends them as window properties) and pays
// with one request naming both effects — Bedrock's effect numbers are the
// canonical registry id plus one, which is exactly how the world's
// SetBeacon frame counts them, with 0 for none.

// beaconLayout: the payment slot, in the UI window.
var beaconLayout = []winSlot{{protocol.ContainerBeaconPayment, 27}}

// beaconProp folds a window property (1 primary, 2 secondary; -1 none)
// into the mirror and renders the beacon's block entity for the client.
func (m *invMirror) beaconProp(prop, value int32) *packet.BlockActorData {
	m.mu.Lock()
	defer m.mu.Unlock()
	if value < 0 {
		value = 0
	}
	switch prop {
	case 1:
		m.beaconPrimary = value
	case 2:
		m.beaconSecondary = value
	default:
		return nil
	}
	return &packet.BlockActorData{
		Position: protocol.BlockPos{m.at[0], m.at[1], m.at[2]},
		NBTData: map[string]any{
			"id": "Beacon", "x": m.at[0], "y": m.at[1], "z": m.at[2], "isMovable": byte(1),
			"primary": m.beaconPrimary, "secondary": m.beaconSecondary,
		},
	}
}

// beaconPayment is the world's SetBeacon for a Bedrock payment request.
func beaconPayment(a *protocol.BeaconPaymentStackRequestAction) *attach.SetBeacon {
	return &attach.SetBeacon{Primary: max32(a.PrimaryEffect, 0), Secondary: max32(a.SecondaryEffect, 0)}
}
