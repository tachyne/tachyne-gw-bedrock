// Package gw is the tachyne gateway for Minecraft Bedrock edition: it
// terminates Bedrock client connections (RakNet + encryption + XBL login via
// gophertunnel), authorizes them via tachyne-access, and RENDERS the world —
// consumed over the domain attach protocol from a tachyne-world pod — into
// Bedrock wire format. Worlds are versionless; all Minecraft protocol lives
// in the gateways.
package gw

import (
	"context"
	"errors"
	"log"
	"log/slog"
	"net"

	"github.com/tachyne/tachyne-common/access"

	"github.com/sandertv/gophertunnel/minecraft"
	gtprotocol "github.com/sandertv/gophertunnel/minecraft/protocol"
)

// Version pinning for this gateway build: gophertunnel supports exactly one
// current Bedrock protocol per release; Bedrock clients auto-update, so
// tracking latest is the norm (unlike Java's frozen-version fleet).
const (
	Protocol    = gtprotocol.CurrentProtocol
	VersionName = gtprotocol.CurrentVersion
)

// Server is one gateway instance.
type Server struct {
	Listen       string // UDP listen address, e.g. ":19132"
	Backend      string // world pod attach address (login/west shard)
	WorldPattern string // dial pattern for a neighbour shard on handover (%d = sid)
	AttachToken  string
	MOTD         string
	SID          int
	AuthDisabled bool           // XBL authentication off (dev / offline probes)
	Access       *access.Client // nil = open (dev only)
}

func (s *Server) Run(ctx context.Context) error {
	cfg := minecraft.ListenConfig{
		ErrorLog:               slog.Default(),
		AuthenticationDisabled: s.AuthDisabled,
		StatusProvider:         minecraft.NewStatusProvider(s.MOTD, "tachyne"),
	}
	ln, err := cfg.Listen("raknet", s.Listen)
	if err != nil {
		return err
	}
	go func() { <-ctx.Done(); ln.Close() }()
	for {
		c, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			log.Printf("accept: %v", err)
			continue
		}
		go s.handle(ln, c.(*minecraft.Conn))
	}
}

// handle authorizes one accepted (already logged-in) Bedrock connection and
// bridges it to the world. Disconnect goes through the listener so the client
// gets a proper reason screen.
func (s *Server) handle(ln *minecraft.Listener, c *minecraft.Conn) {
	id := c.IdentityData()
	name, uuid, xuid := id.DisplayName, id.Identity, id.XUID
	remote := c.RemoteAddr().String()

	roles := []string{}
	if s.Access != nil {
		ip := remote
		if host, _, err := net.SplitHostPort(remote); err == nil {
			ip = host
		}
		v := s.Access.Check(context.Background(), access.Request{
			Name: name, UUID: uuid, IP: ip, Edition: "bedrock",
		})
		if !v.Allow {
			log.Printf("%s: login %q (xuid %s) DENIED: %s", remote, name, xuid, v.Reason)
			ln.Disconnect(c, v.Reason)
			return
		}
		roles = v.Roles
	}
	log.Printf("%s: login %q (xuid %s) allowed (roles %v) — attaching to %s", remote, name, xuid, roles, s.Backend)
	if err := s.session(ln, c, name, uuid, roles); err != nil {
		log.Printf("%s: session %q ended: %v", remote, name, err)
	}
}
