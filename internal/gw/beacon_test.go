package gw

import (
	"testing"

	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
	attach "github.com/tachyne/tachyne-common/attach"
)

// The beacon's effects reach the client as its block entity, and a
// payment request becomes the world's SetBeacon with nothing else moved.
func TestBeacon(t *testing.T) {
	w := &winState{}
	w.used(10, 64, -3)
	m := w.open(12, beaconLayout, protocol.ContainerTypeBeacon, "Beacon")
	if !m.beacon || m.at != [3]int32{10, 64, -3} {
		t.Fatalf("beacon window %v %v", m.beacon, m.at)
	}
	var c capture
	windowData(&c, m, protocol.ContainerTypeBeacon, 0, 4)  // levels: nothing to render
	windowData(&c, m, protocol.ContainerTypeBeacon, 1, 2)  // primary: the world's effect number (registry id + 1)
	windowData(&c, m, protocol.ContainerTypeBeacon, 2, -1) // secondary: none
	if len(c.pkts) != 2 {
		t.Fatalf("%d block entity packets", len(c.pkts))
	}
	bd := c.pkts[1].(*packet.BlockActorData)
	if bd.Position != (protocol.BlockPos{10, 64, -3}) || bd.NBTData["primary"] != int32(2) || bd.NBTData["secondary"] != int32(0) || bd.NBTData["id"] != "Beacon" {
		t.Errorf("block entity %+v", bd)
	}
	m.set(0, attach.ItemStack{ID: 1, Count: 1})
	req := protocol.ItemStackRequest{RequestID: 3, Actions: []protocol.StackRequestAction{
		&protocol.BeaconPaymentStackRequestAction{PrimaryEffect: 2, SecondaryEffect: 0},
		&protocol.ConsumeStackRequestAction{DestroyStackRequestAction: protocol.DestroyStackRequestAction{Count: 1, Source: slotInfo(protocol.ContainerBeaconPayment, 27)}},
	}}
	changed, steps, ok := m.applyRequest(req, nil)
	if !ok || len(changed) != 0 || len(steps) != 1 || steps[0].beacon == nil || steps[0].beacon.Primary != 2 || steps[0].beacon.Secondary != 0 || m.slots[0].Count != 1 {
		t.Fatalf("payment: ok=%v changed=%v steps=%+v", ok, changed, steps)
	}
	if j, ok := m.mapIn(protocol.ContainerBeaconPayment, 27); !ok || j != 0 {
		t.Errorf("payment slot → %d %v", j, ok)
	}
	if _, _, ok := newInvMirror().applyRequest(req, nil); ok {
		t.Error("a beacon payment outside a beacon was accepted")
	}
}
