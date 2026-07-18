package gw

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"net"
	"strings"
	"sync/atomic"

	_ "embed"

	attach "github.com/tachyne/tachyne-common/attach"

	dfworld "github.com/df-mc/dragonfly/server/world"
	"github.com/go-gl/mathgl/mgl32"
	"github.com/google/uuid"
	"github.com/sandertv/gophertunnel/minecraft"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
)

// entity_identifiers.dat is the Bedrock AvailableActorIdentifiers payload
// (network-NBT idlist), shipped verbatim; refreshed by scripts/gen_bedrock.py.
//
//go:embed entity_identifiers.dat
var entityIdentifiersDat []byte

// playerEyeOffset: Bedrock network positions for players are at eye height;
// the domain protocol (like Java) uses feet.
const playerEyeOffset = 1.62

const (
	viewRadius = 6  // default chunk radius when the client hasn't asked yet
	viewCap    = 32 // ceiling — matches the world pod's attach maxRadius (raised for earth-mode vistas)
)

// canonicalPlayerType is the canonical entity-type ID of "minecraft:player",
// resolved from the generated table (index of the player identifier).
var canonicalPlayerType = func() int32 {
	for i, id := range bedrockEntityIDs {
		if id == "minecraft:player" {
			return int32(i)
		}
	}
	panic("gw: minecraft:player missing from bedrockEntityIDs")
}()

// bedrockGameMode maps a domain game mode (Java numbering) to Bedrock's.
func bedrockGameMode(mode int32) int32 {
	if mode == 3 {
		return 6 // spectator
	}
	return mode // survival/creative/adventure share numbering
}

func rt(eid int32) uint64 { return uint64(int64(eid)) }

// entState tracks what the client knows about one remote entity so absolute
// domain events can be re-rendered whole (Bedrock movement is absolute).
type entState struct {
	player  bool
	pos     mgl32.Vec3 // feet
	yaw     float32
	pitch   float32
	headYaw float32
}

// session bridges one authorized Bedrock client to the world over the attach
// protocol. The conn is already logged in (gophertunnel handled RakNet,
// encryption and login); we run StartGame and then two pumps.
func (s *Server) session(ln *minecraft.Listener, c *minecraft.Conn, name, uuidStr string, roles []string) error {
	// Shared attach client machinery (tachyne-common/attach) — the same dial
	// the Java gateways use; only the client transport differs.
	w, welcome, err := attach.DialSession(s.Backend, attach.Hello{
		Token: s.AttachToken, Gateway: fmt.Sprintf("gw-bedrock/%d", s.SID),
		Name: name, UUID: uuidStr, Roles: roles, Edition: "bedrock",
	})
	if err != nil {
		if errors.Is(err, attach.ErrRefused) {
			ln.Disconnect(c, "The world refused the session.")
		} else {
			ln.Disconnect(c, "The world is unreachable right now — please try again shortly.")
		}
		return err
	}
	defer w.Close()

	spawn := welcome.Spawn
	if err := c.StartGame(minecraft.GameData{
		WorldName:         s.MOTD,
		EntityUniqueID:    int64(welcome.EID),
		EntityRuntimeID:   rt(welcome.EID),
		PlayerGameMode:    bedrockGameMode(welcome.Gamemode),
		WorldGameMode:     bedrockGameMode(welcome.Gamemode),
		BaseGameVersion:   protocol.CurrentVersion,
		PlayerPosition:    mgl32.Vec3{float32(spawn.X), float32(spawn.Y) + playerEyeOffset, float32(spawn.Z)},
		Yaw:               spawn.Yaw,
		Pitch:             spawn.Pitch,
		Dimension:         packet.DimensionOverworld,
		WorldSpawn:        protocol.BlockPos{int32(spawn.X), int32(spawn.Y), int32(spawn.Z)},
		Difficulty:        2,
		Time:              welcome.Time,
		PlayerPermissions: 1, // member
		ChunkRadius:       viewCap,
		Items:             bedrockItemEntries,
		PlayerMovementSettings: protocol.PlayerMovementSettings{
			ServerAuthoritativeBlockBreaking: true,
		},
		ServerAuthoritativeInventory: true,
		UseBlockNetworkIDHashes:      true,
	}); err != nil {
		return fmt.Errorf("start game: %w", err)
	}
	// Real actor identifiers (gophertunnel only sends empty defaults): needed
	// before AddActor renders anything.
	c.WritePacket(&packet.AvailableActorIdentifiers{SerialisedEntityIdentifiers: entityIdentifiersDat})
	// Real biome definitions: gophertunnel's empty default leaves the client
	// with zero resolvable biomes, which renders the whole world black.
	biomeDefs, biomeStrs := dfworld.BiomeDefinitions()
	c.WritePacket(&packet.BiomeDefinitionList{BiomeDefinitions: biomeDefs, StringList: biomeStrs})
	// Abilities + attributes: vanilla/dragonfly/PMMP all send these right
	// after spawn; without them the client's self-physics defaults are not
	// dependable.
	abilities := uint32(protocol.AbilityBuild | protocol.AbilityMine |
		protocol.AbilityDoorsAndSwitches | protocol.AbilityOpenContainers |
		protocol.AbilityAttackPlayers | protocol.AbilityAttackMobs)
	if welcome.Gamemode == 1 { // creative
		abilities |= protocol.AbilityMayFly | protocol.AbilityInstantBuild
	}
	c.WritePacket(&packet.UpdateAbilities{AbilityData: protocol.AbilityData{
		EntityUniqueID:     int64(welcome.EID),
		PlayerPermissions:  packet.PermissionLevelMember,
		CommandPermissions: protocol.CommandPermissionLevelAny,
		Layers: []protocol.AbilityLayer{{
			Type:             protocol.AbilityLayerTypeBase,
			Abilities:        protocol.AbilityCount - 1,
			Values:           abilities,
			FlySpeed:         protocol.AbilityBaseFlySpeed,
			VerticalFlySpeed: 1,
			WalkSpeed:        protocol.AbilityBaseWalkSpeed,
		}},
	}})
	c.WritePacket(&packet.UpdateAttributes{
		EntityRuntimeID: rt(welcome.EID),
		Attributes: []protocol.Attribute{
			{AttributeValue: protocol.AttributeValue{Name: "minecraft:health", Value: 20, Max: 20}, DefaultMax: 20, Default: 20},
			{AttributeValue: protocol.AttributeValue{Name: "minecraft:movement", Value: 0.1, Max: 3.4e38}, DefaultMax: 3.4e38, Default: 0.1},
		},
	})
	// Self entity metadata: the client's OWN physics honors these flags —
	// without has_gravity it glides at a fixed height and cannot jump.
	c.WritePacket(&packet.SetActorData{EntityRuntimeID: rt(welcome.EID), EntityMetadata: baseMetadata(0.6, 1.8)})

	log.Printf("%s: %q spawned (%.1f,%.1f,%.1f)", c.RemoteAddr(), name, spawn.X, spawn.Y, spawn.Z)
	return s.play(c, w, name, uuidStr, roles, welcome)
}

// play runs the bridge: world frames → Bedrock packets, client packets →
// Move/Want frames. Mirrors the Java gateways' play() but renders Bedrock —
// the world side rides the SHARED attach.Backend (swappable world-pod conn)
// and attach.DialSession (login + handover resume); only the client transport
// differs (gophertunnel).

// dialResume opens the destination pod on a handover and resumes the player
// there (Hello{Purpose:"resume", token}). Returns the new conn + its Welcome.
func (s *Server) dialResume(destSID int32, token, name, uuidStr string, roles []string) (net.Conn, attach.Welcome, error) {
	return attach.DialSession(fmt.Sprintf(s.WorldPattern, destSID), attach.Hello{
		Token: s.AttachToken, Gateway: fmt.Sprintf("gw-bedrock/%d", s.SID),
		Name: name, UUID: uuidStr, Roles: roles, Edition: "bedrock",
		Purpose: "resume", ResumeToken: token,
	})
}

func (s *Server) play(c *minecraft.Conn, w net.Conn, name, uuidStr string, roles []string, welcome attach.Welcome) error {
	b := attach.NewBackend(w)
	defer func() { b.Get().Close() }()
	pos := welcome.Spawn
	var curDim atomic.Int32
	ccx, ccz := int32(math.Floor(pos.X))>>4, int32(math.Floor(pos.Z))>>4
	var viewDist atomic.Int32
	viewDist.Store(viewRadius)

	// publish declares the render area: the Bedrock client only MESHES chunks
	// inside the last NetworkChunkPublisherUpdate area — chunks outside it are
	// stored (collision works) but never drawn. Must accompany every window
	// move, like vanilla/dragonfly do.
	publish := func(x, y, z float64, radius int32) {
		c.WritePacket(&packet.NetworkChunkPublisherUpdate{
			Position: protocol.BlockPos{int32(x), int32(y), int32(z)},
			Radius:   uint32(radius) << 4,
		})
	}
	publish(pos.X, pos.Y, pos.Z, viewDist.Load())
	if err := b.Write(attach.MsgWant, attach.Want{CX: ccx, CZ: ccz, Radius: viewDist.Load(), Dim: 0}); err != nil {
		return err
	}

	errs := make(chan error, 2)

	// World → client.
	go func() {
		ents := map[int32]*entState{}  // remote entities the client renders
		skipped := map[int32]bool{}    // entities with no Bedrock form
		names := map[[16]byte]string{} // uuid → username (PlayerInfo)
		for {
			typ, payload, err := attach.ReadFrame(b.Get())
			if err != nil {
				errs <- fmt.Errorf("world: %w", err)
				return
			}
			switch typ {
			case attach.MsgChunk:
				h, body, err := attach.DecodeChunk(payload)
				if err != nil {
					errs <- err
					return
				}
				if h.Dim != curDim.Load() {
					continue // stale chunk from before a dimension switch
				}
				if err := c.WritePacket(renderChunk(h, body)); err != nil {
					errs <- err
					return
				}
			case attach.MsgTime:
				var t attach.Time
				if json.Unmarshal(payload, &t) == nil {
					c.WritePacket(&packet.SetTime{Time: int32(t.Time % 24000)})
				}
			case attach.MsgChat:
				var e attach.Chat
				if json.Unmarshal(payload, &e) == nil {
					tt := byte(packet.TextTypeRaw)
					msg := e.Text
					switch {
					case e.ActionBar:
						tt = packet.TextTypeTip
					case e.Sender != "":
						// Player chat: the Java path renders "<sender> msg" via
						// profileless_chat; Bedrock has no equivalent, so compose the
						// same line here (Text is just the message when Sender is set).
						msg = "<" + e.Sender + "> " + e.Text
					}
					c.WritePacket(&packet.Text{TextType: tt, Message: msg})
				}
			case attach.MsgBlockSet:
				var e attach.BlockSet
				if json.Unmarshal(payload, &e) == nil {
					c.WritePacket(&packet.UpdateBlock{
						Position:          protocol.BlockPos{int32(e.X), int32(e.Y), int32(e.Z)},
						NewBlockRuntimeID: bedrockBlockRID(e.State),
						Flags:             packet.BlockUpdateNetwork,
					})
				}
			case attach.MsgPlayerInfo:
				var e attach.PlayerInfo
				if json.Unmarshal(payload, &e) == nil {
					names[e.UUID] = e.Name
					c.WritePacket(&packet.PlayerList{
						ActionType: packet.PlayerListActionAdd,
						Entries: []protocol.PlayerListEntry{{
							UUID:     uuid.UUID(e.UUID),
							Username: e.Name,
							Skin:     defaultSkin(),
						}},
					})
				}
			case attach.MsgPlayerGone:
				var e attach.PlayerGone
				if json.Unmarshal(payload, &e) == nil {
					delete(names, e.UUID)
					c.WritePacket(&packet.PlayerList{
						ActionType: packet.PlayerListActionRemove,
						Entries:    []protocol.PlayerListEntry{{UUID: uuid.UUID(e.UUID)}},
					})
				}
			case attach.MsgEntityAdd:
				var e attach.EntityAdd
				if json.Unmarshal(payload, &e) == nil {
					st := &entState{
						pos: mgl32.Vec3{float32(e.X), float32(e.Y), float32(e.Z)},
						yaw: e.Yaw, pitch: e.Pitch, headYaw: e.Yaw,
					}
					switch {
					case e.Type == canonicalPlayerType:
						st.player = true
						ents[e.EID] = st
						c.WritePacket(&packet.AddPlayer{
							UUID:            uuid.UUID(e.UUID),
							Username:        names[e.UUID],
							EntityRuntimeID: rt(e.EID),
							Position:        st.pos.Add(mgl32.Vec3{0, playerEyeOffset, 0}),
							Pitch:           e.Pitch, Yaw: e.Yaw, HeadYaw: e.Yaw,
							EntityMetadata: baseMetadata(0.6, 1.8),
							AbilityData:    protocol.AbilityData{EntityUniqueID: int64(e.EID)},
						})
					default:
						ident := ""
						if int(e.Type) < len(bedrockEntityIDs) {
							ident = bedrockEntityIDs[e.Type]
						}
						if ident == "" {
							skipped[e.EID] = true // no Bedrock form (item frames, displays, …)
							continue
						}
						ents[e.EID] = st
						c.WritePacket(&packet.AddActor{
							EntityUniqueID:  int64(e.EID),
							EntityRuntimeID: rt(e.EID),
							EntityType:      ident,
							Position:        st.pos,
							Velocity:        mgl32.Vec3{float32(e.VX), float32(e.VY), float32(e.VZ)},
							Pitch:           e.Pitch, Yaw: e.Yaw, HeadYaw: e.Yaw, BodyYaw: e.Yaw,
							EntityMetadata: baseMetadata(0, 0),
						})
					}
				}
			case attach.MsgEntityMove:
				var e attach.EntityMove
				if json.Unmarshal(payload, &e) == nil {
					st := ents[e.EID]
					if st == nil {
						continue
					}
					st.pos = mgl32.Vec3{float32(e.X), float32(e.Y), float32(e.Z)}
					st.yaw, st.pitch = e.Yaw, e.Pitch
					moveEntity(c, e.EID, st, e.OnGround)
				}
			case attach.MsgEntityHead:
				var e attach.EntityHead
				if json.Unmarshal(payload, &e) == nil {
					if st := ents[e.EID]; st != nil {
						st.headYaw = e.Yaw
						moveEntity(c, e.EID, st, true)
					}
				}
			case attach.MsgEntityRemove:
				var e attach.EntityRemove
				if json.Unmarshal(payload, &e) == nil {
					for _, eid := range e.EIDs {
						if skipped[eid] {
							delete(skipped, eid)
							continue
						}
						delete(ents, eid)
						c.WritePacket(&packet.RemoveActor{EntityUniqueID: int64(eid)})
					}
				}
			case attach.MsgVelocity:
				var e attach.Velocity
				if json.Unmarshal(payload, &e) == nil {
					if _, ok := ents[e.EID]; ok {
						c.WritePacket(&packet.SetActorMotion{
							EntityRuntimeID: rt(e.EID),
							Velocity:        mgl32.Vec3{float32(e.VX), float32(e.VY), float32(e.VZ)},
						})
					}
				}
			case attach.MsgTeleport:
				var e attach.Teleport
				if json.Unmarshal(payload, &e) == nil {
					pos = e.Pos
					ccx, ccz = int32(math.Floor(pos.X))>>4, int32(math.Floor(pos.Z))>>4
					c.WritePacket(&packet.MovePlayer{
						EntityRuntimeID: rt(welcome.EID),
						Position:        mgl32.Vec3{float32(pos.X), float32(pos.Y) + playerEyeOffset, float32(pos.Z)},
						Pitch:           pos.Pitch, Yaw: pos.Yaw, HeadYaw: pos.Yaw,
						Mode: packet.MoveModeTeleport,
					})
					publish(pos.X, pos.Y, pos.Z, viewDist.Load())
					b.Write(attach.MsgWant, attach.Want{CX: ccx, CZ: ccz, Radius: viewDist.Load(), Dim: curDim.Load()})
				}
			case attach.MsgDimension:
				// Cross-dimension play is not rendered to Bedrock yet (needs
				// ChangeDimension + per-dimension chunk ranges). Track the dim
				// so stale chunks are dropped; the client stays put.
				var e attach.Dimension
				if json.Unmarshal(payload, &e) == nil {
					curDim.Store(e.Dim)
					log.Printf("session %q: dimension switch to %d not yet rendered on bedrock", name, e.Dim)
				}
			case attach.MsgRehome:
				// Player migrated to a neighbour shard: SILENT swap of the world
				// backend, mirroring the Java gateways' no-Respawn crossing (see
				// TODO.md dedup task). Bedrock never had a reload transition, so
				// the client's chunks and position are already untouched — the one
				// job here is entity reconciliation: destroy exactly what this
				// session has rendered (the resume join re-adds the destination's
				// roster under the same session-stable eids, so crossers and
				// shadows stay continuous). Movement is client-authoritative on
				// Bedrock (PlayerAuthInput), so no position pin is needed — and
				// none is sent, which also preserves momentum through the seam.
				var rh attach.Rehome
				if json.Unmarshal(payload, &rh) != nil {
					continue
				}
				nw, wel, err := s.dialResume(rh.DestSID, rh.Token, name, uuidStr, roles)
				if err != nil {
					errs <- fmt.Errorf("rehome: %w", err)
					return
				}
				b.Swap(nw)
				pos = wel.Spawn
				ccx, ccz = int32(math.Floor(pos.X))>>4, int32(math.Floor(pos.Z))>>4
				curDim.Store(0)
				for eid := range ents {
					c.WritePacket(&packet.RemoveActor{EntityUniqueID: int64(eid)})
					delete(ents, eid)
				}
				clear(skipped) // stale no-Bedrock-form latches must not swallow the new shard's removes
				publish(pos.X, pos.Y, pos.Z, viewDist.Load())
				b.Write(attach.MsgWant, attach.Want{CX: ccx, CZ: ccz, Radius: viewDist.Load(), Dim: 0})
			case attach.MsgResync:
				b.Write(attach.MsgWant, attach.Want{CX: ccx, CZ: ccz, Radius: viewDist.Load(), Dim: curDim.Load(), Force: true})
			case attach.MsgPing:
				var buf [8]byte
				copy(buf[:], payload)
				fr := make([]byte, 0, 16)
				fr = binary.BigEndian.AppendUint32(fr, 9)
				fr = append(fr, attach.MsgPong)
				fr = append(fr, buf[:]...)
				b.Get().Write(fr) // through the CURRENT backend — after a swap, pongs must reach the NEW pod
			case attach.MsgBye:
				var bye attach.Bye
				json.Unmarshal(payload, &bye)
				errs <- fmt.Errorf("world closed session: %s", bye.Reason)
				return
			}
		}
	}()

	// Client → world.
	go func() {
		lastX, lastY, lastZ := pos.X, pos.Y, pos.Z
		lastYaw, lastPitch := pos.Yaw, pos.Pitch
		lastOnGround := true
		for {
			pk, err := c.ReadPacket()
			if err != nil {
				if errors.Is(err, net.ErrClosed) {
					errs <- nil // clean disconnect
				} else {
					errs <- fmt.Errorf("client: %w", err)
				}
				return
			}
			switch p := pk.(type) {
			case *packet.PlayerAuthInput:
				// Block actions ride the input packet (server-auth breaking):
				// start/abort/finish map onto the domain Dig statuses.
				for _, a := range p.BlockActions {
					status := int32(-1)
					switch a.Action {
					case protocol.PlayerActionStartBreak:
						status = 0
					case protocol.PlayerActionAbortBreak:
						status = 1
					case protocol.PlayerActionPredictDestroyBlock:
						status = 2
					}
					if status >= 0 {
						b.Write(attach.MsgDig, attach.Dig{
							Status: status,
							X:      int(a.BlockPos.X()), Y: int(a.BlockPos.Y()), Z: int(a.BlockPos.Z()),
							Face: a.Face,
						})
					}
				}
				if p.InputData.Load(packet.InputFlagStartSneaking) {
					b.Write(attach.MsgInput, attach.Input{Sneak: true})
				}
				if p.InputData.Load(packet.InputFlagStopSneaking) {
					b.Write(attach.MsgInput, attach.Input{Sneak: false})
				}
				// Bedrock streams input every tick; forward only real movement.
				x := float64(p.Position.X())
				y := float64(p.Position.Y()) - playerEyeOffset
				z := float64(p.Position.Z())
				// PlayerAuthInput carries no on-ground flag; a stable Y is the
				// usable proxy (jumping/falling/swimming all move Y every tick).
				onGround := math.Abs(y-lastY) < 1e-6
				if math.Abs(x-lastX) < 1e-4 && math.Abs(y-lastY) < 1e-4 && math.Abs(z-lastZ) < 1e-4 &&
					p.Yaw == lastYaw && p.Pitch == lastPitch && onGround == lastOnGround {
					continue
				}
				lastX, lastY, lastZ, lastYaw, lastPitch, lastOnGround = x, y, z, p.Yaw, p.Pitch, onGround
				b.Write(attach.MsgMove, attach.Move{
					Pos:      attach.Pos{X: x, Y: y, Z: z, Yaw: p.Yaw, Pitch: p.Pitch},
					OnGround: onGround,
				})
				if ncx, ncz := int32(math.Floor(x))>>4, int32(math.Floor(z))>>4; ncx != ccx || ncz != ccz {
					ccx, ccz = ncx, ncz
					publish(x, y, z, viewDist.Load())
					b.Write(attach.MsgWant, attach.Want{CX: ccx, CZ: ccz, Radius: viewDist.Load(), Dim: curDim.Load()})
				}
			case *packet.InventoryTransaction:
				switch td := p.TransactionData.(type) {
				case *protocol.UseItemOnEntityTransactionData:
					b.Write(attach.MsgUseEntity, attach.UseEntity{
						Target: int32(td.TargetEntityRuntimeID),
						Attack: td.ActionType == protocol.UseItemOnEntityActionAttack,
					})
				case *protocol.UseItemTransactionData:
					switch td.ActionType {
					case protocol.UseItemActionClickBlock:
						b.Write(attach.MsgPlace, attach.Place{
							X: int(td.BlockPosition.X()), Y: int(td.BlockPosition.Y()), Z: int(td.BlockPosition.Z()),
							Face: td.BlockFace,
							CX:   td.ClickedPosition.X(), CY: td.ClickedPosition.Y(), CZ: td.ClickedPosition.Z(),
						})
					case protocol.UseItemActionClickAir:
						b.Write(attach.MsgUseItem, attach.UseItem{})
					}
				case *protocol.ReleaseItemTransactionData:
					// Bow release / stop eating: Java carries this as dig status 5.
					b.Write(attach.MsgDig, attach.Dig{Status: 5})
				}
			case *packet.MobEquipment:
				b.Write(attach.MsgHeldSlot, attach.HeldSlot{Slot: int16(p.HotBarSlot)})
			case *packet.Text:
				if p.TextType == packet.TextTypeChat && p.Message != "" {
					b.Write(attach.MsgChat, attach.Chat{Text: p.Message})
				}
			case *packet.CommandRequest:
				if cmd := strings.TrimPrefix(p.CommandLine, "/"); cmd != "" {
					b.Write(attach.MsgCommand, attach.Command{Cmd: cmd})
				}
			case *packet.RequestChunkRadius:
				r := p.ChunkRadius
				if r < 2 {
					r = 2
				}
				if r > viewCap {
					r = viewCap
				}
				viewDist.Store(r)
				c.WritePacket(&packet.ChunkRadiusUpdated{ChunkRadius: r})
				publish(lastX, lastY, lastZ, r)
				b.Write(attach.MsgWant, attach.Want{CX: ccx, CZ: ccz, Radius: r, Dim: curDim.Load()})
			}
		}
	}()

	err := <-errs
	b.Write(attach.MsgBye, attach.Bye{Reason: "client gone"})
	log.Printf("session %q done: %v", name, err)
	return err
}

// baseMetadata is the default actor data every Bedrock entity carries
// (dragonfly parseEntityMetadata): gravity + collision + breathing flags,
// and a hitbox when the caller knows it. The client's own physics reads
// these off its OWN entity — they are not cosmetic.
func baseMetadata(width, height float32) protocol.EntityMetadata {
	m := protocol.NewEntityMetadata()
	if width > 0 {
		m[protocol.EntityDataKeyWidth] = width
		m[protocol.EntityDataKeyHeight] = height
	}
	m.SetFlag(protocol.EntityDataKeyFlags, protocol.EntityDataFlagHasGravity)
	m.SetFlag(protocol.EntityDataKeyFlags, protocol.EntityDataFlagHasCollision)
	m.SetFlag(protocol.EntityDataKeyFlags, protocol.EntityDataFlagBreathing)
	m.SetFlag(protocol.EntityDataKeyFlags, protocol.EntityDataFlagClimb)
	return m
}

// moveEntity renders one absolute movement state to the client. Players use
// MovePlayer (eye-height offset); everything else MoveActorAbsolute.
func moveEntity(c *minecraft.Conn, eid int32, st *entState, onGround bool) {
	if st.player {
		c.WritePacket(&packet.MovePlayer{
			EntityRuntimeID: rt(eid),
			Position:        st.pos.Add(mgl32.Vec3{0, playerEyeOffset, 0}),
			Pitch:           st.pitch, Yaw: st.yaw, HeadYaw: st.headYaw,
			Mode:     packet.MoveModeNormal,
			OnGround: onGround,
		})
		return
	}
	var flags byte
	if onGround {
		flags |= packet.MoveFlagOnGround
	}
	c.WritePacket(&packet.MoveActorAbsolute{
		EntityRuntimeID: rt(eid),
		Flags:           flags,
		Position:        st.pos,
		Rotation:        mgl32.Vec3{st.pitch, st.headYaw, st.yaw},
	})
}
