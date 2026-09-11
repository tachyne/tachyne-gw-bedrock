package gw

import (
	"bytes"

	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	tproto "github.com/tachyne/tachyne-common/protocol"
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

// canonicalItemType is the canonical entity-type ID of "minecraft:item".
var canonicalItemType = func() int32 {
	for i, id := range bedrockEntityIDs {
		if id == "minecraft:item" {
			return int32(i)
		}
	}
	return -1
}()

// itemMetaStack pulls the dropped item's stack (index 8, a Slot: count,
// then id) out of a canonical set_entity_data list. Components that may
// follow are not needed for the pick-up icon and are left unread.
func itemMetaStack(meta []byte) (attach.ItemStack, bool) {
	r := bytes.NewReader(meta)
	for {
		idx, err := r.ReadByte()
		if err != nil || idx == 0xff {
			return attach.ItemStack{}, false
		}
		typ, err := tproto.ReadVarInt(r)
		if err != nil {
			return attach.ItemStack{}, false
		}
		if idx != 8 || typ != 7 { // 7 = the Slot serializer (1.21.5)
			return attach.ItemStack{}, false // only the item entry is understood
		}
		count, err := tproto.ReadVarInt(r)
		if err != nil || count <= 0 {
			return attach.ItemStack{}, false
		}
		id, err := tproto.ReadVarInt(r)
		if err != nil {
			return attach.ItemStack{}, false
		}
		return attach.ItemStack{ID: id, Count: count}, true
	}
}

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
	player   bool
	ident    string     // the Bedrock identifier (which mob this is, for metadata)
	pos      mgl32.Vec3 // feet
	velocity mgl32.Vec3 // a dropped item's launch (AddItemActor carries it)
	yaw      float32
	pitch    float32
	headYaw  float32
	look     mobLook // what Bedrock renders of the entity's metadata (entitymeta.go)
}

// session bridges one authorized Bedrock client to the world over the attach
// protocol. The conn is already logged in (gophertunnel handled RakNet,
// encryption and login); we run StartGame and then two pumps.
func (s *Server) session(ln *minecraft.Listener, c *minecraft.Conn, name, uuidStr string, roles []string) error {
	if id, err := uuid.Parse(uuidStr); err == nil {
		s.skins.put(id, skinFromClientData(c.ClientData()))
		defer s.skins.drop(id)
	}
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
	// Spawn-time packets, in order. Each used to be fire-and-forget; a client
	// that dropped during them was only noticed once its read side failed.
	c.WritePacket(creativeContent()) // the creative screen crashes without its listing, whatever the mode now
	abilities := abilityValues(attach.Abilities{MayFly: welcome.Gamemode == 1, Creative: welcome.Gamemode == 1})
	biomeDefs, biomeStrs := dfworld.BiomeDefinitions()
	if err := sendAll(c,
		// Real actor identifiers (gophertunnel only sends empty defaults):
		// needed before AddActor renders anything.
		&packet.AvailableActorIdentifiers{SerialisedEntityIdentifiers: entityIdentifiersDat},
		// Real biome definitions: gophertunnel's empty default leaves the
		// client with zero resolvable biomes, which renders the world black.
		&packet.BiomeDefinitionList{BiomeDefinitions: biomeDefs, StringList: biomeStrs},
		// Abilities + attributes: vanilla/dragonfly/PMMP all send these right
		// after spawn; without them the client's self-physics defaults are
		// not dependable.
		abilitiesPacket(welcome.EID, abilities),
		&packet.UpdateAttributes{
			EntityRuntimeID: rt(welcome.EID),
			Attributes: []protocol.Attribute{
				{AttributeValue: protocol.AttributeValue{Name: "minecraft:health", Value: 20, Max: 20}, DefaultMax: 20, Default: 20},
				{AttributeValue: protocol.AttributeValue{Name: "minecraft:movement", Value: 0.1, Max: 3.4e38}, DefaultMax: 3.4e38, Default: 0.1},
			},
		},
		// Self entity metadata: the client's OWN physics honors these flags —
		// without has_gravity it glides at a fixed height and cannot jump.
		&packet.SetActorData{EntityRuntimeID: rt(welcome.EID), EntityMetadata: baseMetadata(0.6, 1.8)},
		// Entity property definitions (the way Geyser syncs them right after
		// StartGame): the farm animals' climate variant.
		climateProperty("minecraft:pig"),
		climateProperty("minecraft:cow"),
		climateProperty("minecraft:chicken"),
	); err != nil {
		return fmt.Errorf("spawn packets: %w", err)
	}
	if welcome.Death != nil { // where the player last died, for the recovery compass
		c.WritePacket(&packet.SetActorData{EntityRuntimeID: rt(welcome.EID), EntityMetadata: deathMetadata(welcome.Death)})
	}

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
	dead := false // between the world's death screen and the respawn teleport
	var curDim atomic.Int32
	ccx, ccz := int32(math.Floor(pos.X))>>4, int32(math.Floor(pos.Z))>>4
	var viewDist atomic.Int32
	viewDist.Store(viewRadius)
	var riding atomic.Int32 // the vehicle this player rides (0 = none): its moves carry the chunk window, the player's own are camera

	// publish declares the render area: the Bedrock client only MESHES chunks
	// inside the last NetworkChunkPublisherUpdate area — chunks outside it are
	// stored (collision works) but never drawn. Must accompany every window
	// move, like vanilla/dragonfly do.
	errs := make(chan error, 3)
	// send is the one write path for this session. A write error means the
	// client is gone; it ends the session once, instead of every later write
	// failing silently until the read side happens to notice.
	var sendFailed atomic.Bool
	send := func(pk packet.Packet) {
		if err := c.WritePacket(pk); err != nil && sendFailed.CompareAndSwap(false, true) {
			select {
			case errs <- fmt.Errorf("client write: %w", err):
			default:
			}
		}
	}
	publish := func(x, y, z float64, radius int32) {
		send(&packet.NetworkChunkPublisherUpdate{
			Position: protocol.BlockPos{int32(x), int32(y), int32(z)},
			Radius:   uint32(radius) << 4,
		})
	}
	publish(pos.X, pos.Y, pos.Z, viewDist.Load())
	if err := b.Write(attach.MsgWant, attach.Want{CX: ccx, CZ: ccz, Radius: viewDist.Load(), Dim: 0}); err != nil {
		return err
	}

	// The window mirror is touched by BOTH goroutines below — the world reader
	// writes what the engine sends, the client reader resolves stack requests
	// against it — so it carries its own lock rather than living in either.
	mirror := newInvMirror()
	win := &winState{}                             // the open container window, if any
	var signEdit atomic.Pointer[attach.SignEditor] // the sign side the world opened for editing
	maps := newMapStore()                          // map textures, for the frames and the client's requests
	board := newScoreboard()                       // the Java scoreboard, redrawn into Bedrock's slots
	recipes := newRecipeSet()                      // what the client may craft (CraftingData); written by the world pump, read by requests

	// World → client.
	go func() {
		ents := map[int32]*entState{}         // remote entities the client renders
		skipped := map[int32]bool{}           // entities with no Bedrock form
		pendingItems := map[int32]*entState{} // dropped items waiting for their stack (metadata) before AddItemActor
		names := map[[16]byte]string{}        // uuid → username (PlayerInfo)
		links := map[int32]int32{}            // rider → vehicle (actor links)
		banners := map[[3]int32]int32{}       // banner base colours by position (Bedrock keeps them on the block entity)
		adv := newAdvBook()                   // advancement progress, for the toast
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
				for _, pk := range chunkBlockEntities(h, body, banners) { // signs, banners, campfires
					send(pk)
				}
			case attach.MsgTime:
				var t attach.Time
				if json.Unmarshal(payload, &t) == nil {
					send(&packet.SetTime{Time: int32(t.Time % 24000)})
				}
			case attach.MsgHealth:
				// Bedrock reads health, hunger and saturation off the player's
				// attributes; until this the client sat at a static 20.
				var e attach.Health
				if json.Unmarshal(payload, &e) == nil {
					send(&packet.UpdateAttributes{
						EntityRuntimeID: rt(welcome.EID),
						Attributes: []protocol.Attribute{
							{AttributeValue: protocol.AttributeValue{Name: "minecraft:health", Value: e.Health, Max: 20}, DefaultMax: 20, Default: 20},
							{AttributeValue: protocol.AttributeValue{Name: "minecraft:player.hunger", Value: float32(e.Food), Max: 20}, DefaultMax: 20, Default: 20},
							{AttributeValue: protocol.AttributeValue{Name: "minecraft:player.saturation", Value: e.Saturation, Max: 20}, DefaultMax: 20, Default: 20},
						},
					})
				}
			case attach.MsgDeath:
				// The death screen: Bedrock shows it off the health attribute
				// (already zero from the Health frame) and the Respawn packet's
				// searching state; the respawn button answers with its own
				// Respawn (client-ready), relayed to the world as MsgRespawnReq.
				var e attach.Death
				if json.Unmarshal(payload, &e) == nil {
					if e.Message != "" {
						send(&packet.Text{TextType: packet.TextTypeRaw, Message: e.Message})
					}
					dead = true
					send(&packet.Respawn{Position: mgl32.Vec3{float32(pos.X), float32(pos.Y), float32(pos.Z)},
						State: packet.RespawnStateSearchingForSpawn, EntityRuntimeID: rt(welcome.EID)})
				}
			case attach.MsgXP:
				var e attach.XP
				if json.Unmarshal(payload, &e) == nil {
					send(&packet.UpdateAttributes{
						EntityRuntimeID: rt(welcome.EID),
						Attributes: []protocol.Attribute{
							{AttributeValue: protocol.AttributeValue{Name: "minecraft:player.experience", Value: e.Progress, Max: 1}, DefaultMax: 1},
							{AttributeValue: protocol.AttributeValue{Name: "minecraft:player.level", Value: float32(e.Level), Max: 24791}, DefaultMax: 24791},
						},
					})
				}
			case attach.MsgTrades:
				var tr attach.Trades
				if json.Unmarshal(payload, &tr) == nil {
					if tl, ok := parseTrades(tr.Data); ok {
						if wm := win.current(); wm != nil && wm.window == tl.window {
							wm.setTrades(tl.results())
							if pk, err := tradePacket(tl, win.usedEntityID(), int64(welcome.EID), win.currentTitle()); err == nil {
								send(pk)
							}
						}
					}
				}
			case attach.MsgCursorItem:
				// The stack on the cursor, as the world holds it: into the
				// open window's mirror (or the player's) and Bedrock's cursor.
				var e attach.CursorItem
				if json.Unmarshal(payload, &e) == nil {
					m := mirror
					if wm := win.current(); wm != nil {
						m = wm
					}
					m.setCursor(e.Item)
					send(&packet.InventorySlot{WindowID: protocol.WindowIDUI, Slot: 0,
						Container: protocol.Option(fullContainer(protocol.ContainerCursor)), NewItem: bedrockStack(e.Item)})
				}
			case attach.MsgCommandTree:
				var e attach.CommandTree
				if json.Unmarshal(payload, &e) == nil {
					if names := commandNames(e.Data); len(names) > 0 {
						send(availableCommands(names))
					}
				}
			case attach.MsgHorseScreen:
				var e attach.HorseScreen
				if json.Unmarshal(payload, &e) == nil {
					llama := false
					if st := ents[e.EID]; st != nil {
						llama = st.ident == "minecraft:llama" || st.ident == "minecraft:trader_llama"
					}
					win.open(e.ID, horseLayout(e.Columns, llama), protocol.ContainerTypeHorse, "")
					send(horseEquipPacket(e.ID, int64(e.EID), e.Columns, llama))
					send(&packet.ContainerOpen{WindowID: byte(e.ID), ContainerType: protocol.ContainerTypeHorse,
						ContainerPosition: protocol.BlockPos{int32(pos.X), int32(pos.Y), int32(pos.Z)}, ContainerEntityUniqueID: int64(e.EID)})
				}
			case attach.MsgObjective:
				var e attach.Objective
				if json.Unmarshal(payload, &e) == nil {
					for _, pk := range board.objective(e) {
						send(pk)
					}
				}
			case attach.MsgDisplaySlot:
				var e attach.DisplaySlot
				if json.Unmarshal(payload, &e) == nil {
					for _, pk := range board.display(e) {
						send(pk)
					}
				}
			case attach.MsgScore:
				var e attach.Score
				if json.Unmarshal(payload, &e) == nil {
					for _, pk := range board.score(e) {
						send(pk)
					}
				}
			case attach.MsgTeam:
				var e attach.Team
				if json.Unmarshal(payload, &e) == nil {
					for _, pk := range board.team(e) {
						send(pk)
					}
				}
			case attach.MsgMapData:
				var e attach.MapData
				if json.Unmarshal(payload, &e) == nil {
					send(maps.apply(e, curDim.Load()))
				}
			case attach.MsgEffect:
				var e attach.Effect
				if json.Unmarshal(payload, &e) == nil {
					if pk := effectPacket(e); pk != nil {
						send(pk)
					}
				}
			case attach.MsgAbilities:
				var e attach.Abilities
				if json.Unmarshal(payload, &e) == nil {
					send(abilitiesPacket(welcome.EID, abilityValues(e)))
				}
			case attach.MsgGameEvent:
				var e attach.GameEvent
				if json.Unmarshal(payload, &e) == nil {
					for _, pk := range gameEventPackets(welcome.EID, e) {
						send(pk)
					}
				}
			case attach.MsgSwing:
				var e attach.Swing
				if json.Unmarshal(payload, &e) == nil && e.EID != welcome.EID {
					send(&packet.Animate{ActionType: packet.AnimateActionSwingArm, EntityRuntimeID: rt(e.EID)})
				}
			case attach.MsgCollect:
				var e attach.Collect
				if json.Unmarshal(payload, &e) == nil {
					send(&packet.TakeItemActor{ItemEntityRuntimeID: rt(e.Collected), TakerEntityRuntimeID: rt(e.Collector)})
				}
			case attach.MsgHeldSync:
				var e attach.HeldSync
				if json.Unmarshal(payload, &e) == nil && e.Slot >= 0 && e.Slot < 9 {
					send(&packet.PlayerHotBar{SelectedHotBarSlot: uint32(e.Slot), WindowID: protocol.WindowIDInventory, SelectHotBarSlot: true})
				}
			case attach.MsgDifficulty:
				var e attach.Difficulty
				if json.Unmarshal(payload, &e) == nil && e.Level >= 0 && e.Level <= 3 {
					send(&packet.SetDifficulty{Difficulty: uint32(e.Level)})
				}
			case attach.MsgBossBar:
				var e attach.BossBar
				if json.Unmarshal(payload, &e) == nil {
					for _, pk := range bossBarPackets(e, int64(welcome.EID), mgl32.Vec3{float32(pos.X), float32(pos.Y), float32(pos.Z)}) {
						send(pk)
					}
				}
			case attach.MsgVehicleMove:
				var e attach.VehicleMove
				if json.Unmarshal(payload, &e) == nil {
					if v := riding.Load(); v != 0 {
						if st := ents[v]; st != nil { // the ride snapped back: move it, and us with it
							st.pos = mgl32.Vec3{float32(e.X), float32(e.Y), float32(e.Z)}
							st.yaw = e.Yaw
							moveEntityVia(send, c, v, st, true)
							pos.X, pos.Y, pos.Z = e.X, e.Y, e.Z
						}
					}
				}
			case attach.MsgBannerPatterns:
				var e attach.BannerPatterns
				if json.Unmarshal(payload, &e) == nil {
					send(bannerData(e.X, e.Y, e.Z, banners[[3]int32{e.X, e.Y, e.Z}], e.Layers))
				}
			case attach.MsgCampfireItems:
				var e attach.CampfireItems
				if json.Unmarshal(payload, &e) == nil {
					send(campfireData(e.X, e.Y, e.Z, e.Items))
				}
			case attach.MsgShelfItems:
				var e attach.ShelfItems
				if json.Unmarshal(payload, &e) == nil {
					send(shelfData(e.X, e.Y, e.Z, e.Items))
				}
			case attach.MsgMovingPiston:
				// Java animates the carried block through a moving_piston
				// block entity; Bedrock's moving-block actor is a different
				// machine, so the carried block is laid down at once — the
				// world's final block set two ticks later matches it.
				var e attach.MovingPiston
				if json.Unmarshal(payload, &e) == nil {
					send(&packet.UpdateBlock{
						Position:          protocol.BlockPos{e.X, e.Y, e.Z},
						NewBlockRuntimeID: bedrockBlockRID(e.State),
						Flags:             packet.BlockUpdateNetwork,
					})
				}
			case attach.MsgBlockEvent:
				var e attach.BlockEvent
				if json.Unmarshal(payload, &e) == nil && e.Action == 1 { // the bell's ring (the one block event the world sends)
					send(bellData(e.X, e.Y, e.Z, e.Param))
				}
			case attach.MsgSignText:
				var e attach.SignText
				if json.Unmarshal(payload, &e) == nil {
					send(signData(e.X, e.Y, e.Z, e.Front, e.Back, e.Waxed, e.Hanging))
				}
			case attach.MsgSignEditor:
				var e attach.SignEditor
				if json.Unmarshal(payload, &e) == nil {
					signEdit.Store(&e)
					send(&packet.OpenSign{Position: protocol.BlockPos{e.X, e.Y, e.Z}, FrontSide: e.Front})
				}
			case attach.MsgRecipeBook:
				var rb attach.RecipeBook
				if json.Unmarshal(payload, &rb) == nil {
					if rb.Replace {
						send(trimData()) // the trim recipe below refers to these
					}
					recipes.add(rb)
					send(recipes.packet())
				}
			case attach.MsgAdvTree:
				var t attach.AdvTree
				if json.Unmarshal(payload, &t) == nil {
					adv.tree(t)
				}
			case attach.MsgAdvProgress:
				var p attach.AdvProgress
				if json.Unmarshal(payload, &p) == nil {
					for _, n := range adv.progress(p) {
						send(advToast(n))
					}
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
					send(&packet.Text{TextType: tt, Message: msg})
				}
			case attach.MsgBlockSet:
				var e attach.BlockSet
				if json.Unmarshal(payload, &e) == nil {
					send(&packet.UpdateBlock{
						Position:          protocol.BlockPos{int32(e.X), int32(e.Y), int32(e.Z)},
						NewBlockRuntimeID: bedrockBlockRID(e.State),
						Flags:             packet.BlockUpdateNetwork,
					})
					pos := [3]int32{int32(e.X), int32(e.Y), int32(e.Z)}
					if isBanner(e.State) { // its layers arrive in their own frame; remember the base for it
						banners[pos] = bannerBaseFor(e.State)
					} else {
						delete(banners, pos)
					}
				}
			case attach.MsgPlayerInfo:
				var e attach.PlayerInfo
				if json.Unmarshal(payload, &e) == nil {
					names[e.UUID] = e.Name
					send(&packet.PlayerList{
						ActionType: packet.PlayerListActionAdd,
						Entries: []protocol.PlayerListEntry{{
							UUID:     uuid.UUID(e.UUID),
							Username: e.Name,
							Skin:     s.skins.get(uuid.UUID(e.UUID)),
						}},
					})
				}
			case attach.MsgPlayerGone:
				var e attach.PlayerGone
				if json.Unmarshal(payload, &e) == nil {
					delete(names, e.UUID)
					send(&packet.PlayerList{
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
						st.ident = "minecraft:player"
						ents[e.EID] = st
						send(&packet.AddPlayer{
							UUID:            uuid.UUID(e.UUID),
							Username:        names[e.UUID],
							EntityRuntimeID: rt(e.EID),
							Position:        st.pos.Add(mgl32.Vec3{0, playerEyeOffset, 0}),
							Pitch:           e.Pitch, Yaw: e.Yaw, HeadYaw: e.Yaw,
							EntityMetadata: baseMetadata(0.6, 1.8),
							AbilityData:    protocol.AbilityData{EntityUniqueID: int64(e.EID)},
						})
					case e.Type == canonicalItemType:
						// A dropped item is an AddItemActor with its stack, which
						// arrives in the metadata frame right behind the add.
						st.velocity = mgl32.Vec3{float32(e.VX), float32(e.VY), float32(e.VZ)}
						pendingItems[e.EID] = st
					default:
						ident := ""
						if int(e.Type) < len(bedrockEntityIDs) {
							ident = bedrockEntityIDs[e.Type]
						}
						if ident == "" {
							skipped[e.EID] = true // no Bedrock form (item frames, displays, …)
							continue
						}
						st.ident = ident
						if int(e.Type) < len(javaEntityNames) {
							if v, ok := boatVariant(javaEntityNames[e.Type]); ok {
								st.look.hasVariant, st.look.variant = true, v // the boat's wood
							}
						}
						ents[e.EID] = st
						ad := actorData(e.EID, st)
						send(&packet.AddActor{
							EntityUniqueID:  int64(e.EID),
							EntityRuntimeID: rt(e.EID),
							EntityType:      ident,
							Position:        st.pos,
							Velocity:        mgl32.Vec3{float32(e.VX), float32(e.VY), float32(e.VZ)},
							Pitch:           e.Pitch, Yaw: e.Yaw, HeadYaw: e.Yaw, BodyYaw: e.Yaw,
							EntityMetadata:   ad.EntityMetadata,
							EntityProperties: ad.EntityProperties,
						})
					}
				}
			case attach.MsgWindowOpen:
				var e attach.WindowOpen
				if json.Unmarshal(payload, &e) == nil {
					if mw, ok := menuWindows[e.Menu]; ok {
						win.open(e.ID, mw.layout, mw.ctype, e.Title)
						p := win.usedAt()
						if mw.ctype != protocol.ContainerTypeTrade && mw.ctype != protocol.ContainerTypeLectern { // these open on their own data
							send(&packet.ContainerOpen{WindowID: byte(e.ID), ContainerType: mw.ctype,
								ContainerPosition: protocol.BlockPos{p[0], p[1], p[2]}, ContainerEntityUniqueID: -1})
						}
						if mw.ctype == protocol.ContainerTypeBrewingStand { // vanilla's fuel bar spans 20 uses
							send(&packet.ContainerSetData{WindowID: byte(e.ID), Key: packet.ContainerDataBrewingStandFuelTotal, Value: 20})
						}
					} else {
						b.Write(attach.MsgWindowClose, attach.WindowClose{}) // no Bedrock twin: do not leave the world waiting
					}
				}
			case attach.MsgWindowItems:
				var e attach.WindowItems
				if json.Unmarshal(payload, &e) == nil {
					if e.ID == 0 {
						mirror.setAll(e.Slots, e.Cursor)
						sendPlayerInventory(c, e.Slots)
					} else if wm := win.current(); wm != nil && wm.window == e.ID {
						wm.setAll(e.Slots, e.Cursor)
						if wm.lecternBook { // the book rides the block entity; then the lectern screen opens on it
							if len(e.Slots) > 0 {
								send(lecternData(wm.at, e.Slots[0], wm.lecternPage))
							}
							p := win.usedAt()
							send(&packet.ContainerOpen{WindowID: byte(e.ID), ContainerType: protocol.ContainerTypeLectern,
								ContainerPosition: protocol.BlockPos{p[0], p[1], p[2]}, ContainerEntityUniqueID: -1})
						} else {
							sendWindowItems(c, wm, e.Slots)
						}
					}
				}
			case attach.MsgWindowData:
				var e attach.WindowData
				if json.Unmarshal(payload, &e) == nil {
					if wm, ctype := win.currentType(); wm != nil && wm.window == e.ID {
						windowData(c, wm, ctype, e.Prop, e.Value)
					}
				}
			case attach.MsgWindowSlot:
				var e attach.WindowSlot
				if json.Unmarshal(payload, &e) == nil {
					if e.ID == 0 {
						mirror.set(e.Slot, e.Item)
						sendInventorySlot(c, e.Slot, e.Item)
					} else if wm := win.current(); wm != nil && wm.window == e.ID {
						wm.set(e.Slot, e.Item)
						sendWindowSlot(c, wm, e.Slot, e.Item)
					}
				}
			case attach.MsgEntityLink:
				// Leashes are METADATA on Bedrock, not an actor link: the
				// EntityLink packet only models riding (Remove/Rider/Passenger),
				// while a lead is the leashed entity's LeashHolder key plus the
				// Leashed flag.
				//
				// The flags word is rebuilt from baseMetadata rather than set
				// bare, because SetActorData merges by KEY: a flags value
				// carrying only the leash bit would clear this mob's gravity,
				// collision and breathing along with it.
				var e attach.EntityLink
				if json.Unmarshal(payload, &e) != nil {
					break
				}
				m := baseMetadata(0, 0)
				if e.Holder != 0 {
					m[protocol.EntityDataKeyLeashHolder] = int64(e.Holder)
					m.SetFlag(protocol.EntityDataKeyFlags, protocol.EntityDataFlagLeashed)
				} else {
					m[protocol.EntityDataKeyLeashHolder] = int64(-1) // no holder
				}
				send(&packet.SetActorData{
					EntityRuntimeID: rt(e.Leashed),
					EntityMetadata:  m,
				})
			case attach.MsgEntityMove:
				var e attach.EntityMove
				if json.Unmarshal(payload, &e) == nil {
					if riding.Load() == e.EID { // our ride: the window follows it
						pos.X, pos.Y, pos.Z = e.X, e.Y, e.Z
						if ncx, ncz := int32(math.Floor(e.X))>>4, int32(math.Floor(e.Z))>>4; ncx != ccx || ncz != ccz {
							ccx, ccz = ncx, ncz
							publish(e.X, e.Y, e.Z, viewDist.Load())
							b.Write(attach.MsgWant, attach.Want{CX: ccx, CZ: ccz, Radius: viewDist.Load(), Dim: curDim.Load()})
						}
					}
					st := ents[e.EID]
					if st == nil {
						continue
					}
					st.pos = mgl32.Vec3{float32(e.X), float32(e.Y), float32(e.Z)}
					st.yaw, st.pitch = e.Yaw, e.Pitch
					moveEntityVia(send, c, e.EID, st, e.OnGround)
				}
			case attach.MsgPassengers:
				// Who rides what: Bedrock seats riders with actor links (the
				// first rider drives), and unlinks those who got off.
				var e attach.Passengers
				if json.Unmarshal(payload, &e) == nil {
					still := map[int32]bool{}
					for i, r := range e.Riders {
						still[r] = true
						kind := byte(protocol.EntityLinkPassenger)
						if i == 0 {
							kind = protocol.EntityLinkRider
						}
						send(&packet.SetActorLink{EntityLink: protocol.EntityLink{
							RiddenEntityUniqueID: int64(e.Vehicle), RiderEntityUniqueID: int64(r), Type: kind, Immediate: true}})
						links[r] = e.Vehicle
						if r == welcome.EID {
							riding.Store(e.Vehicle)
						}
					}
					for r, v := range links {
						if v == e.Vehicle && !still[r] {
							send(&packet.SetActorLink{EntityLink: protocol.EntityLink{
								RiddenEntityUniqueID: int64(v), RiderEntityUniqueID: int64(r), Type: protocol.EntityLinkRemove, Immediate: true}})
							delete(links, r)
							if r == welcome.EID {
								riding.Store(0)
							}
						}
					}
				}
			case attach.MsgEntityMeta:
				var e attach.EntityMeta
				if json.Unmarshal(payload, &e) == nil {
					if st := ents[e.EID]; st != nil && e.EID != welcome.EID {
						if st.applyMeta(parseSimpleMeta(e.Meta)) {
							send(actorData(e.EID, st))
						}
					}
					if st := pendingItems[e.EID]; st != nil {
						if stack, ok := itemMetaStack(e.Meta); ok {
							delete(pendingItems, e.EID)
							ents[e.EID] = st
							send(&packet.AddItemActor{
								EntityUniqueID:  int64(e.EID),
								EntityRuntimeID: rt(e.EID),
								Item:            bedrockStack(stack),
								Position:        st.pos,
								Velocity:        st.velocity,
								EntityMetadata:  baseMetadata(0.25, 0.25),
							})
						}
					}
				}
			case attach.MsgEquipment:
				// What an entity holds and wears (players' own hotbar is the
				// inventory's business, so a player gets only its armour).
				var e attach.Equipment
				if json.Unmarshal(payload, &e) == nil {
					if e.EID != welcome.EID {
						send(&packet.MobEquipment{EntityRuntimeID: rt(e.EID), NewItem: bedrockStack(e.Slots[attach.EquipMainHand])})
						// The off hand (a piglin's gold, a player's shield): Geyser's
						// second MobEquipment with the off-hand container id.
						send(&packet.MobEquipment{EntityRuntimeID: rt(e.EID), NewItem: bedrockStack(e.Slots[attach.EquipOffhand]),
							InventorySlot: 0, HotBarSlot: 0xff, WindowID: protocol.ContainerOffhand})
					}
					send(&packet.MobArmourEquipment{
						EntityRuntimeID: rt(e.EID),
						Helmet:          bedrockStack(e.Slots[attach.EquipHead]),
						Chestplate:      bedrockStack(e.Slots[attach.EquipChest]),
						Leggings:        bedrockStack(e.Slots[attach.EquipLegs]),
						Boots:           bedrockStack(e.Slots[attach.EquipFeet]),
						Body:            bedrockStack(e.Slots[attach.EquipBody]),
					})
				}
			case attach.MsgParticles:
				var e attach.Particles
				if json.Unmarshal(payload, &e) == nil {
					if p := particleEvent(e, curDim.Load()); p != nil {
						send(p)
					}
				}
			case attach.MsgWorldFX:
				var e attach.WorldFX
				if json.Unmarshal(payload, &e) == nil {
					if p := worldEvent(e); p != nil {
						send(p)
					}
				}
			case attach.MsgSound:
				var e attach.Sound
				if json.Unmarshal(payload, &e) == nil {
					if p := levelSound(e); p != nil {
						send(p)
					}
				}
			case attach.MsgHurt:
				// The hurt flash + tilt (Java's damage event) is an actor event here.
				var e attach.Hurt
				if json.Unmarshal(payload, &e) == nil {
					send(&packet.ActorEvent{EntityRuntimeID: rt(e.EID), EventType: packet.ActorEventHurt})
				}
			case attach.MsgEntityStatus:
				// Only the death animation has a Bedrock twin worth sending;
				// the other statuses (love, tame, cure) have no direct match.
				var e attach.EntityStatus
				if json.Unmarshal(payload, &e) == nil && e.Status == 3 {
					send(&packet.ActorEvent{EntityRuntimeID: rt(e.EID), EventType: packet.ActorEventDeath})
				}
			case attach.MsgEntityHead:
				var e attach.EntityHead
				if json.Unmarshal(payload, &e) == nil {
					if st := ents[e.EID]; st != nil {
						st.headYaw = e.Yaw
						moveEntityVia(send, c, e.EID, st, true)
					}
				}
			case attach.MsgEntityRemove:
				var e attach.EntityRemove
				if json.Unmarshal(payload, &e) == nil {
					for _, eid := range e.EIDs {
						delete(pendingItems, eid)
						if skipped[eid] {
							delete(skipped, eid)
							continue
						}
						delete(ents, eid)
						send(&packet.RemoveActor{EntityUniqueID: int64(eid)})
					}
				}
			case attach.MsgVelocity:
				var e attach.Velocity
				if json.Unmarshal(payload, &e) == nil {
					if _, ok := ents[e.EID]; ok {
						send(&packet.SetActorMotion{
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
					if dead { // the respawn teleport: tell the client its spawn is ready
						dead = false
						send(&packet.Respawn{Position: mgl32.Vec3{float32(pos.X), float32(pos.Y), float32(pos.Z)},
							State: packet.RespawnStateReadyToSpawn, EntityRuntimeID: rt(welcome.EID)})
					}
					send(&packet.MovePlayer{
						EntityRuntimeID: rt(welcome.EID),
						Position:        mgl32.Vec3{float32(pos.X), float32(pos.Y) + playerEyeOffset, float32(pos.Z)},
						Pitch:           pos.Pitch, Yaw: pos.Yaw, HeadYaw: pos.Yaw,
						Mode: packet.MoveModeTeleport,
					})
					publish(pos.X, pos.Y, pos.Z, viewDist.Load())
					b.Write(attach.MsgWant, attach.Want{CX: ccx, CZ: ccz, Radius: viewDist.Load(), Dim: curDim.Load()})
				}
			case attach.MsgDimension:
				// Portal travel. The client is sent through Bedrock's dimension
				// change screen the way Geyser does it: ChangeDimension parked
				// at (0, 32767, 0), all sound stopped, the change acknowledged
				// on its behalf, and empty columns around that parking spot so
				// the screen can finish; the world's Teleport that follows
				// places the player, and its chunks stream in behind. Java
				// discards the entity world on a respawn, so the rendered
				// entities go too — the world re-adds the new dimension's.
				var e attach.Dimension
				if json.Unmarshal(payload, &e) == nil && e.Death != nil {
					send(&packet.SetActorData{EntityRuntimeID: rt(welcome.EID), EntityMetadata: deathMetadata(e.Death)})
				}
				if json.Unmarshal(payload, &e) == nil && e.Dim != curDim.Load() {
					curDim.Store(e.Dim)
					for eid := range ents {
						send(&packet.RemoveActor{EntityUniqueID: int64(eid)})
						delete(ents, eid)
					}
					clear(skipped)
					clear(pendingItems)
					send(&packet.ChangeDimension{Dimension: e.Dim, Position: mgl32.Vec3{0, 32767, 0}, Respawn: true})
					send(&packet.StopSound{StopAll: true})
					send(&packet.PlayerAction{EntityRuntimeID: rt(welcome.EID), ActionType: protocol.PlayerActionDimensionChangeDone})
					for dx := int32(-3); dx <= 3; dx++ {
						for dz := int32(-3); dz <= 3; dz++ {
							send(emptyChunk(e.Dim, dx, dz))
							send(&packet.UpdateBlock{Position: protocol.BlockPos{dx << 4, 80, dz << 4},
								NewBlockRuntimeID: bedrockBlockRID(1), Flags: packet.BlockUpdateNetwork})
						}
					}
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
					send(&packet.RemoveActor{EntityUniqueID: int64(eid)})
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
		sneaking := false
		var lastInput attach.Input
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
			case *packet.Respawn:
				if p.State == packet.RespawnStateClientReadyToSpawn {
					b.Write(attach.MsgRespawnReq, attach.RespawnReq{}) // the death screen's respawn button
				}
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
				// The key state, whenever it changes: the world steers a ridden
				// vehicle from it (and sneak dismounts).
				if p.InputData.Load(packet.InputFlagStartSneaking) {
					sneaking = true
				}
				if p.InputData.Load(packet.InputFlagStopSneaking) {
					sneaking = false
				}
				in := attach.Input{
					Forward: p.InputData.Load(packet.InputFlagUp), Backward: p.InputData.Load(packet.InputFlagDown),
					Left: p.InputData.Load(packet.InputFlagLeft), Right: p.InputData.Load(packet.InputFlagRight),
					Jump: p.InputData.Load(packet.InputFlagJumping), Sneak: sneaking,
					Sprint: p.InputData.Load(packet.InputFlagSprinting),
				}
				if in != lastInput {
					lastInput = in
					b.Write(attach.MsgInput, in)
				}
				if riding.Load() != 0 {
					continue // a rider's own moves are camera only; the vehicle carries the player
				}
				// Bedrock streams input every tick; forward only real movement.
				x := float64(p.Position.X())
				y := float64(p.Position.Y()) - playerEyeOffset
				z := float64(p.Position.Z())
				// On the ground = the client reports a vertical collision this
				// tick (Bedrock's own ground test, as Geyser reads it), which is
				// what the world's fall damage keys off.
				onGround := p.InputData.Load(packet.InputFlagVerticalCollision)
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
			case *packet.ItemStackRequest:
				// Bedrock's transactional inventory, resolved against our copy
				// of the window and reported to the engine as a Java click.
				m := mirror
				if wm := win.current(); wm != nil {
					m = wm // an open container: its window, its slot map
				}
				for _, req := range p.Requests {
					changed, steps, ok := m.applyRequest(req, recipes)
					switch {
					case ok && steps != nil:
						for _, s := range steps {
							if s.name != nil {
								b.Write(attach.MsgNameItem, attach.NameItem{Name: *s.name})
							}
							if s.sel != nil {
								b.Write(attach.MsgSelTrade, *s.sel)
							}
							if s.ench != nil {
								b.Write(attach.MsgEnchant, *s.ench)
							}
							if s.beacon != nil {
								b.Write(attach.MsgSetBeacon, *s.beacon)
							}
							if s.creative != nil {
								b.Write(attach.MsgCreativeSlot, *s.creative)
							}
							if s.place != nil {
								b.Write(attach.MsgCraft, *s.place)
							}
							if s.click != nil {
								b.Write(attach.MsgWindowClick, *s.click)
							}
						}
					case ok:
						b.Write(attach.MsgWindowClick, m.clickFor(changed))
					}
					respondStackRequest(c, req.RequestID, m, changed, ok)
				}
			case *packet.PlayerToggleCrafterSlotRequest:
				if _, ctype := win.currentType(); ctype == protocol.ContainerTypeCrafter {
					b.Write(attach.MsgSlotState, crafterToggle(p))
				}
			case *packet.PlayerAction:
				if p.ActionType == protocol.PlayerActionDimensionChangeDone {
					// The screen is gone: pin the client where the world put it
					// (the Teleport may have landed while the screen was up).
					send(&packet.MovePlayer{
						EntityRuntimeID: rt(welcome.EID),
						Position:        mgl32.Vec3{float32(pos.X), float32(pos.Y) + playerEyeOffset, float32(pos.Z)},
						Pitch:           pos.Pitch, Yaw: pos.Yaw, HeadYaw: pos.Yaw,
						Mode: packet.MoveModeTeleport,
					})
					publish(pos.X, pos.Y, pos.Z, viewDist.Load())
				}
			case *packet.BlockActorData:
				// The edited sign comes back whole; its edited side is the
				// world's four lines.
				if id, _ := p.NBTData["id"].(string); strings.HasSuffix(id, "Sign") {
					ed := signEdit.Load()
					if ed == nil {
						break
					}
					side := "BackText"
					if ed.Front {
						side = "FrontText"
					}
					text := ""
					if m, ok := p.NBTData[side].(map[string]any); ok {
						text, _ = m["Text"].(string)
					}
					b.Write(attach.MsgSignUpdate, attach.SignUpdate{X: p.Position.X(), Y: p.Position.Y(), Z: p.Position.Z(),
						Front: ed.Front, Lines: signLines(text)})
				}
			case *packet.MapInfoRequest:
				if pk := maps.get(int32(p.MapID), curDim.Load()); pk != nil {
					send(pk)
				}
			case *packet.LecternUpdate:
				if wm := win.current(); wm != nil && wm.lecternBook {
					b.Write(attach.MsgEnchant, attach.Enchant{Button: lecternJumpButton + int32(p.Page)})
				}
			case *packet.BookEdit:
				// A book-and-quill edit: apply it to the pages the held stack
				// carries and send the world the whole book (hotbar only).
				if p.InventorySlot >= 0 && p.InventorySlot < 9 {
					if book, ok := mirror.editBook(javaHotbarFirst+p.InventorySlot, p); ok {
						b.Write(attach.MsgEditBook, book)
					}
				}
			case *packet.ContainerClose:
				if id, ctype, was := win.close(); was {
					b.Write(attach.MsgWindowClose, attach.WindowClose{})
					send(&packet.ContainerClose{WindowID: byte(id), ContainerType: ctype})
				}
			case *packet.InventoryTransaction:
				switch td := p.TransactionData.(type) {
				case *protocol.NormalTransactionData:
					// The drop key on a held stack: a world-sourced action
					// alongside the inventory slot that shrank.
					toWorld := false
					for _, a := range p.Actions {
						if a.SourceType == protocol.InventoryActionSourceWorld {
							toWorld = true
						}
					}
					if !toWorld {
						break
					}
					for _, a := range p.Actions {
						if a.SourceType != protocol.InventoryActionSourceContainer || a.WindowID != protocol.WindowIDInventory {
							continue
						}
						if shed := int32(a.OldItem.Stack.Count) - int32(a.NewItem.Stack.Count); shed > 0 {
							if click, ok := mirror.dropHeld(protocol.ContainerInventory, byte(a.InventorySlot), shed); ok {
								b.Write(attach.MsgWindowClick, click)
							}
						}
					}
				case *protocol.UseItemOnEntityTransactionData:
					win.usedEntity(int64(td.TargetEntityRuntimeID))
					b.Write(attach.MsgUseEntity, attach.UseEntity{
						Target: int32(td.TargetEntityRuntimeID),
						Attack: td.ActionType == protocol.UseItemOnEntityActionAttack,
					})
				case *protocol.UseItemTransactionData:
					switch td.ActionType {
					case protocol.UseItemActionClickBlock:
						win.used(td.BlockPosition.X(), td.BlockPosition.Y(), td.BlockPosition.Z())
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
				send(&packet.ChunkRadiusUpdated{ChunkRadius: r})
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
func moveEntity(c *minecraft.Conn, eid int32, st *entState, onGround bool) error {
	if st.player {
		return c.WritePacket(&packet.MovePlayer{
			EntityRuntimeID: rt(eid),
			Position:        st.pos.Add(mgl32.Vec3{0, playerEyeOffset, 0}),
			Pitch:           st.pitch, Yaw: st.yaw, HeadYaw: st.headYaw,
			Mode:     packet.MoveModeNormal,
			OnGround: onGround,
		})
	}
	var flags byte
	if onGround {
		flags |= packet.MoveFlagOnGround
	}
	return c.WritePacket(&packet.MoveActorAbsolute{
		EntityRuntimeID: rt(eid),
		Flags:           flags,
		Position:        st.pos,
		Rotation:        mgl32.Vec3{st.pitch, st.headYaw, st.yaw},
	})
}

// moveEntityVia is moveEntity for the session loop: a failed write is fed to
// the session's send path so it ends the session like any other dead write.
func moveEntityVia(send func(packet.Packet), c *minecraft.Conn, eid int32, st *entState, onGround bool) {
	if err := moveEntity(c, eid, st, onGround); err != nil {
		send(&packet.MoveActorAbsolute{}) // a write on a dead conn fails fast and trips the session's error
	}
}

// sendAll writes packets in order and returns the first error.
func sendAll(c *minecraft.Conn, pks ...packet.Packet) error {
	for _, pk := range pks {
		if err := c.WritePacket(pk); err != nil {
			return err
		}
	}
	return nil
}
