// Command bedrockprobe is a headless Bedrock client (gophertunnel Dialer,
// offline mode) that smoke-tests a tachyne Bedrock gateway: it joins, waits
// for spawn, verifies chunks decode (via dragonfly's NetworkDecode — the
// same decoder a real client implements), and reports a packet histogram.
//
//	go run ./cmd/bedrockprobe -addr 127.0.0.1:19132 -name probe1 -t 10s
//
// The gateway must run with TACHYNE_XBL_AUTH=off (offline probes cannot
// present a real XBL chain).
package main

import (
	"flag"
	"fmt"
	"log"
	"sort"
	"time"

	"github.com/df-mc/dragonfly/server/block/cube"
	"github.com/df-mc/dragonfly/server/world/chunk"
	"github.com/google/uuid"
	"github.com/sandertv/gophertunnel/minecraft"
	"github.com/sandertv/gophertunnel/minecraft/protocol/login"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// probeRegistry satisfies chunk.BlockRegistry for decoding (hashed air ID).
type probeRegistry struct{}

func (probeRegistry) BlockCount() int                                        { return 1 }
func (probeRegistry) AirRuntimeID() uint32                                   { return 0xdbf44120 }
func (probeRegistry) RuntimeIDToState(uint32) (string, map[string]any, bool) { return "", nil, false }
func (probeRegistry) StateToRuntimeID(string, map[string]any) (uint32, bool) { return 0, false }
func (probeRegistry) FilteringBlock(uint32) uint8                            { return 15 }
func (probeRegistry) LightBlock(uint32) uint8                                { return 0 }
func (probeRegistry) RandomTickBlock(uint32) bool                            { return false }
func (probeRegistry) NBTBlock(uint32) bool                                   { return false }
func (probeRegistry) LiquidDisplacingBlock(uint32) bool                      { return false }
func (probeRegistry) LiquidBlock(uint32) bool                                { return false }
func (probeRegistry) HashToRuntimeID(uint32) (uint32, bool)                  { return 0, false }

func main() {
	addr := flag.String("addr", "127.0.0.1:19132", "gateway address")
	name := flag.String("name", "probe1", "player name")
	dur := flag.Duration("t", 10*time.Second, "listen duration after spawn")
	chatMsg := flag.String("chat", "hello from bedrockprobe", "chat message to send ('' = none)")
	flag.Parse()

	dialer := minecraft.Dialer{
		IdentityData: login.IdentityData{
			DisplayName: *name,
			Identity:    uuid.NewString(),
		},
	}
	start := time.Now()
	conn, err := dialer.Dial("raknet", *addr)
	if err != nil {
		log.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	log.Printf("connected in %s", time.Since(start).Round(time.Millisecond))

	if err := conn.DoSpawnTimeout(30 * time.Second); err != nil {
		log.Fatalf("spawn: %v", err)
	}
	gd := conn.GameData()
	log.Printf("SPAWNED in %s: eid=%d pos=%.1f,%.1f,%.1f gamemode=%d items=%d hashedIDs=%v",
		time.Since(start).Round(time.Millisecond), gd.EntityRuntimeID,
		gd.PlayerPosition.X(), gd.PlayerPosition.Y(), gd.PlayerPosition.Z(),
		gd.PlayerGameMode, len(gd.Items), gd.UseBlockNetworkIDHashes)

	if *chatMsg != "" {
		conn.WritePacket(&packet.Text{
			TextType:   packet.TextTypeChat,
			SourceName: *name,
			Message:    *chatMsg,
			XUID:       conn.IdentityData().XUID,
		})
	}

	hist := map[string]int{}
	chunks, decodeErrs := 0, 0
	var nonAir int
	deadline := time.Now().Add(*dur)
	conn.SetReadDeadline(deadline)
	for time.Now().Before(deadline) {
		pk, err := conn.ReadPacket()
		if err != nil {
			break
		}
		hist[fmt.Sprintf("%T", pk)]++
		switch p := pk.(type) {
		case *packet.LevelChunk:
			chunks++
			ch, err := chunk.NetworkDecode(probeRegistry{}, p.RawPayload, int(p.SubChunkCount),
				cube.Range{-64, 319})
			if err != nil {
				decodeErrs++
				log.Printf("chunk %v DECODE ERROR: %v", p.Position, err)
				continue
			}
			// count non-air in the surface region as a sanity signal
			for y := int16(60); y < 80; y++ {
				for x := uint8(0); x < 16; x += 4 {
					for z := uint8(0); z < 16; z += 4 {
						if ch.Block(x, y, z, 0) != 0xdbf44120 {
							nonAir++
						}
					}
				}
			}
		case *packet.Text:
			log.Printf("text: %q", p.Message)
		case *packet.Disconnect:
			log.Fatalf("DISCONNECTED by server: %s", p.Message)
		}
	}

	keys := make([]string, 0, len(hist))
	for k := range hist {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		log.Printf("  %-45s %d", k, hist[k])
	}
	log.Printf("chunks=%d decodeErrs=%d surfaceNonAirSamples=%d", chunks, decodeErrs, nonAir)
	if chunks == 0 || decodeErrs > 0 || nonAir == 0 {
		log.Fatal("PROBE FAILED: expected >0 chunks, 0 decode errors, terrain present")
	}
	log.Print("PROBE OK")
}
