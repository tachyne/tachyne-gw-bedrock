package gw

import (
	"github.com/sandertv/gophertunnel/minecraft"

	"github.com/tachyne/tachyne-common/attach"
)

// The Bedrock server list's player count has the same problem the Java one
// had: gophertunnel's default provider reports this listener's own count, so
// it shows only the Bedrock clients on this gateway and misses everyone who
// came in through a Java one. The world holds the whole roster, so ask it.

// statusMaxPlayers is the slot count shown in the list. Nothing enforces it.
const statusMaxPlayers = 100

// worldStatus is a gophertunnel ServerStatusProvider backed by the world's
// roster, cached so a list full of entries does not ask once per ping.
type worldStatus struct {
	motd  string
	world *attach.StatusCache
}

func (w worldStatus) ServerStatus(playerCount, maxPlayers int) minecraft.ServerStatus {
	st := minecraft.ServerStatus{
		ServerName:    w.motd,
		ServerSubName: "tachyne",
		PlayerCount:   playerCount, // this listener's own, until the world answers
		MaxPlayers:    statusMaxPlayers,
	}
	if ros, live := w.world.Get(); live || ros.Online > 0 {
		st.PlayerCount = ros.Online
		if ros.Max > 0 {
			st.MaxPlayers = ros.Max
		}
	}
	return st
}
