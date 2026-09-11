package gw

import (
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/tachyne/tachyne-common/attach"
)

// The last death location. Bedrock's recovery compass reads it from the
// player's own actor data (Geyser's setLastDeathPosition): the block, the
// dimension, and the has-died flag. Sent with the Welcome's value at spawn
// and again with every Dimension frame (respawn, portal travel).

func deathMetadata(d *attach.DeathPos) protocol.EntityMetadata {
	m := protocol.NewEntityMetadata()
	if d == nil {
		m[protocol.EntityDataKeyPlayerHasDied] = byte(0)
		return m
	}
	m[protocol.EntityDataKeyPlayerLastDeathPosition] = protocol.BlockPos{d.X, d.Y, d.Z}
	m[protocol.EntityDataKeyPlayerLastDeathDimension] = d.Dim
	m[protocol.EntityDataKeyPlayerHasDied] = byte(1)
	return m
}
