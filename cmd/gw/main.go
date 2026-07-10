// Command gw runs the tachyne Bedrock gateway: it renders a tachyne-world
// (domain attach protocol) into Minecraft Bedrock edition wire format via
// gophertunnel (RakNet, encryption and XBL login handled by the library).
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/tachyne/tachyne-common/access"

	"github.com/tachyne/tachyne-gw-bedrock/internal/gw"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lmicroseconds)
	s := &gw.Server{
		Listen:       envOr("TACHYNE_LISTEN", ":19132"),
		Backend:      envOr("TACHYNE_BACKEND", "127.0.0.1:25500"),
		WorldPattern: envOr("TACHYNE_WORLD_PATTERN", "tachyne-world-%d.tachyne-world-hl.tachyne.svc.cluster.local:25500"),
		AttachToken:  os.Getenv("TACHYNE_ATTACH_TOKEN"),
		MOTD:         envOr("TACHYNE_MOTD", "tachyne"),
		SID:          ordinal(os.Getenv("POD_NAME")),
		// XBL authentication is ON unless explicitly disabled for local dev.
		// Offline Bedrock clients (gophertunnel Dialer probes) need it off.
		AuthDisabled: os.Getenv("TACHYNE_XBL_AUTH") == "off",
	}
	if url := os.Getenv("TACHYNE_ACCESS_URL"); url != "" {
		s.Access = access.New(url, os.Getenv("TACHYNE_ACCESS_TOKEN"), 30*time.Second)
		log.Printf("access control via %s (fail closed)", url)
	} else {
		log.Print("WARNING: TACHYNE_ACCESS_URL unset — running OPEN (no access control)")
	}
	if s.AuthDisabled {
		log.Print("WARNING: TACHYNE_XBL_AUTH=off — accepting unauthenticated Bedrock clients (dev only)")
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	log.Printf("tachyne-gw-bedrock sid=%d (%s, proto %d) listening on udp %s, world %s",
		s.SID, gw.VersionName, gw.Protocol, s.Listen, s.Backend)
	if err := s.Run(ctx); err != nil {
		log.Fatal(err)
	}
}

func envOr(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func ordinal(pod string) int {
	i := strings.LastIndexByte(pod, '-')
	if i < 0 {
		return 0
	}
	n, err := strconv.Atoi(pod[i+1:])
	if err != nil || n < 0 {
		return 0
	}
	return n
}
