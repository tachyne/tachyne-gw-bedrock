package gw

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	_ "image/png" // skin textures are PNGs
	"io"
	"net/http"
	"net/url"
	"sort"
	"sync"
	"time"

	"github.com/go-gl/mathgl/mgl32"
	"github.com/google/uuid"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
	attach "github.com/tachyne/tachyne-common/attach"
	tproto "github.com/tachyne/tachyne-common/protocol"
)

// Player heads. Bedrock has no player head that wears a skin, so — as
// Geyser's SkullCache, SkullPlayerEntity and SkullSkinManager do it — each
// head with an owner in view becomes a fake player: an unlisted, shadowless
// player entity whose skin is the owner's texture on a head-only geometry,
// placed and turned to sit exactly on the block. The skin is fetched from the
// texture URL in the owner's profile (only Mojang's texture server), turned
// from PNG into Bedrock's RGBA skin data, and handed over through a player
// list add and remove (the only way a skin reaches an entity since 1.21.130).
// The nearest heads within the render distance get an entity; the rest wait.

const (
	blockEntitySkull = 16 // the canonical skull block entity type

	// skullEntityBase starts the fake players' entity ids, far above any id
	// the world hands out — and with low 32 bits no world id reaches either,
	// should a client action ever name one and be cut to an int32.
	skullEntityBase = int64(1)<<40 | 0x7ff00000

	// Geyser's defaults: custom-skull-render-distance 32 (vanilla draws no
	// skull past 64), max-visible-custom-skulls 128.
	skullRenderDistance = 32
	maxVisibleSkulls    = 128

	// skullForgetDistance drops heads this far away from the cache (the
	// gateway is not told when a chunk leaves the client).
	skullForgetDistance = 512

	// skullTextureHost is the one host skin textures are fetched from.
	skullTextureHost = "textures.minecraft.net"
)

// The player head states (26.3 numbering): the floor head's 32 states run
// powered true then false, rotation 0-15 within each; the wall head's 8 run
// facing north, south, west, east, powered true then false within each. The
// defaults (powered false, rotation 0 / facing north) are 16 and 1 in.
var (
	playerHeadFirst     = uint32(tproto.CanonicalBlockState("player_head")) - 16
	playerWallHeadFirst = uint32(tproto.CanonicalBlockState("player_wall_head")) - 1
)

// headPose is where a head's skull sits and faces: ok is false for a state
// that is not a player head.
func headPose(state uint32) (wall bool, yaw float32, facing int, ok bool) {
	if state >= playerHeadFirst && state < playerHeadFirst+32 {
		rot := (state - playerHeadFirst) % 16
		yaw := 180 + float32(rot)*22.5 // (180 + rotation × 22.5) mod 360
		if yaw >= 360 {
			yaw -= 360
		}
		return false, yaw, 0, true
	}
	if state >= playerWallHeadFirst && state < playerWallHeadFirst+8 {
		f := int(state-playerWallHeadFirst) / 2 // north, south, west, east
		return true, [4]float32{180, 0, 90, 270}[f], f, true
	}
	return false, 0, 0, false
}

// skullPosition is SkullPlayerEntity.updateSkull's placement: the block's
// centre a hair below its floor, and a wall head up a quarter and pushed
// against its wall.
func skullPosition(p [3]int32, state uint32) (mgl32.Vec3, float32) {
	x, y, z := float32(p[0])+.5, float32(p[1])-.01, float32(p[2])+.5
	wall, yaw, facing, _ := headPose(state)
	if wall {
		y += .25
		switch facing {
		case 0: // north
			z += .24
		case 1: // south
			z -= .24
		case 2: // west
			x += .24
		case 3: // east
			x -= .24
		}
	}
	return mgl32.Vec3{x, y, z}, yaw
}

// skinURLFromProfile reads the skin URL out of a profile's textures property
// (base64 JSON: textures.SKIN.url), "" when it has none.
func skinURLFromProfile(props []attach.Property) string {
	for _, p := range props {
		if p.Name != "textures" {
			continue
		}
		raw, err := base64.StdEncoding.DecodeString(p.Value)
		if err != nil {
			if raw, err = base64.RawStdEncoding.DecodeString(p.Value); err != nil {
				return ""
			}
		}
		var t struct {
			Textures struct {
				Skin struct {
					URL string `json:"url"`
				} `json:"SKIN"`
			} `json:"textures"`
		}
		if json.Unmarshal(raw, &t) != nil {
			return ""
		}
		return t.Textures.Skin.URL
	}
	return ""
}

// SkinFetcher fetches a skin texture as Bedrock skin data: 64×64 RGBA.
type SkinFetcher interface {
	FetchSkin(textureURL string) ([]byte, error)
}

var errSkinHost = errors.New("skin texture not on " + skullTextureHost)

// httpSkins fetches textures from Mojang's texture server.
type httpSkins struct {
	client *http.Client
}

// FetchSkin downloads the PNG (only from the texture server, over HTTPS,
// at most 1 MB) and converts it.
func (h httpSkins) FetchSkin(textureURL string) ([]byte, error) {
	u, err := url.Parse(textureURL)
	if err != nil || u.Hostname() != skullTextureHost || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, errSkinHost
	}
	u.Scheme = "https"
	client := h.client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	resp, err := client.Get(u.String())
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, errors.New("skin texture: " + resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	return skinRGBA(data)
}

// skinRGBA decodes a skin PNG into 64×64 non-premultiplied RGBA; a legacy
// 64×32 skin fills the top half (the head is all a skull shows).
func skinRGBA(png []byte) ([]byte, error) {
	img, _, err := image.Decode(bytes.NewReader(png))
	if err != nil {
		return nil, err
	}
	b := img.Bounds()
	if b.Dx() != 64 || (b.Dy() != 64 && b.Dy() != 32) {
		return nil, errors.New("skin texture: not 64x64 or 64x32")
	}
	out := make([]byte, 64*64*4)
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < 64; x++ {
			c := color.NRGBAModel.Convert(img.At(b.Min.X+x, b.Min.Y+y)).(color.NRGBA)
			i := (y*64 + x) * 4
			out[i], out[i+1], out[i+2], out[i+3] = c.R, c.G, c.B, c.A
		}
	}
	return out, nil
}

// skullGeometry is Geyser's geometry.humanoid.customskull: the head and its
// hat layer, nothing else.
const skullGeometry = `{"format_version":"1.10.0","geometry.humanoid.customskull":{"texturewidth":64,"textureheight":64,` +
	`"visible_bounds_width":2,"visible_bounds_height":1,"visible_bounds_offset":[0,0,0],"bones":[` +
	`{"name":"head","pivot":[0,24,0],"cubes":[{"origin":[-4,0,-4],"size":[8,8,8],"uv":[0,0]}]},` +
	`{"name":"hat","parent":"head","pivot":[0,24,0],"cubes":[{"origin":[-4,0,-4],"size":[8,8,8],"uv":[32,0],"inflate":0.5}]}]}}`

// skullSkin is SkullSkinManager.buildSkullEntryManually.
func skullSkin(textureURL string, rgba []byte) protocol.Skin {
	id := textureURL + "_skull"
	return protocol.Skin{
		SkinID:                    id,
		FullID:                    id,
		SkinResourcePatch:         []byte(`{"geometry" :{"default" :"geometry.humanoid.customskull"}}`),
		SkinImageWidth:            64,
		SkinImageHeight:           64,
		SkinData:                  rgba,
		SkinGeometry:              []byte(skullGeometry),
		GeometryDataEngineVersion: []byte(protocol.CurrentVersion),
		PremiumSkin:               true,
		ArmSize:                   protocol.ArmSizeWide,
		Trusted:                   true,
	}
}

// skullMetadata is SkullPlayerEntity.initializeMetadata: a slightly larger
// head on a near-zero box (it can be mined through and casts no shadow),
// invisible until its skin has arrived.
func skullMetadata(visible bool) protocol.EntityMetadata {
	m := protocol.NewEntityMetadata()
	m[protocol.EntityDataKeyScale] = float32(1.08)
	m[protocol.EntityDataKeyWidth] = float32(0.001)
	m[protocol.EntityDataKeyHeight] = float32(0.001)
	if !visible {
		m.SetFlag(protocol.EntityDataKeyFlags, protocol.EntityDataFlagInvisible)
	}
	return m
}

// skull is one owned head the client can see.
type skull struct {
	pos   [3]int32
	state uint32
	url   string
	id    int64     // the fake player's entity id; 0 = none
	uid   uuid.UUID // the fake player's UUID
	dist2 float64
}

// skullCache is the session's heads. Its methods lock: the world reader
// feeds it heads and the client reader the player's position, and a fetched
// skin lands from its own goroutine.
type skullCache struct {
	mu      sync.Mutex
	send    func(packet.Packet)
	fetch   SkinFetcher
	skulls  map[[3]int32]*skull
	heads   map[[3]int32]uint32 // player head block states seen, for a profile that arrives alone
	skins   map[string][]byte   // fetched skins by texture URL
	pending map[string]bool     // fetches under way
	nextID  int64
	at      mgl32.Vec3 // the player, at the last visibility pass
	placed  bool       // at is known
}

func newSkullCache(send func(packet.Packet), fetch SkinFetcher) *skullCache {
	return &skullCache{send: send, fetch: fetch, skulls: map[[3]int32]*skull{}, heads: map[[3]int32]uint32{},
		skins: map[string][]byte{}, pending: map[string]bool{}, nextID: skullEntityBase}
}

// put is SkullCache.putSkull: a head and its owner's skin URL ("" = no skin:
// the head is a plain one, and any fake player it had goes).
func (c *skullCache) put(pos [3]int32, state uint32, textureURL string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.heads[pos] = state
	if textureURL == "" {
		c.removeLocked(pos)
		return
	}
	s := c.skulls[pos]
	if s == nil {
		s = &skull{pos: pos}
		c.skulls[pos] = s
	}
	changed := s.url != textureURL
	s.state, s.url = state, textureURL
	if s.id != 0 {
		if changed { // a new skin: hide, then dress again (updateSkull)
			c.send(&packet.SetActorData{EntityRuntimeID: uint64(s.id), EntityMetadata: skullMetadata(false)})
			c.dressLocked(s)
		}
		c.placeLocked(s)
		return
	}
	c.cullLocked()
}

// display is a skull's update tag (attach MsgBlockDisplay): its owner, on the
// head state the block last had.
func (c *skullCache) display(e attach.BlockDisplay) {
	if e.Kind != attach.DisplaySkull {
		return
	}
	c.mu.Lock()
	state, ok := c.heads[e.Pos]
	c.mu.Unlock()
	if !ok {
		state = playerHeadFirst + 16 // the floor head's default until the block says otherwise
	}
	textureURL := ""
	if e.Profile != nil {
		textureURL = skinURLFromProfile(e.Profile.Properties)
	}
	c.put(e.Pos, state, textureURL)
}

// blockSet is a block change: a head turned or swapped keeps its skull (and
// moves it), anything else breaks it (SkullCache.updateSkull / removeSkull).
func (c *skullCache) blockSet(pos [3]int32, state uint32) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, _, _, ok := headPose(state); !ok {
		delete(c.heads, pos)
		c.removeLocked(pos)
		return
	}
	c.heads[pos] = state
	if s := c.skulls[pos]; s != nil && s.state != state {
		s.state = state
		if s.id != 0 {
			c.placeLocked(s)
		}
	}
}

// moved is SkullCache.updateVisibleSkulls: the nearest heads in range get
// fake players, the rest give theirs up; a pass needs the player to have
// moved at least two blocks.
func (c *skullCache) moved(x, y, z float64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	p := mgl32.Vec3{float32(x), float32(y), float32(z)}
	if c.placed && p.Sub(c.at).LenSqr() < 4 {
		return
	}
	c.at, c.placed = p, true
	c.cullLocked()
}

// clear drops every head (a dimension change: the client's world is gone).
func (c *skullCache) clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, s := range c.skulls {
		c.freeLocked(s)
	}
	c.skulls = map[[3]int32]*skull{}
	c.heads = map[[3]int32]uint32{}
}

func (c *skullCache) removeLocked(pos [3]int32) {
	if s := c.skulls[pos]; s != nil {
		c.freeLocked(s)
		delete(c.skulls, pos)
		c.cullLocked() // a farther head may take its place
	}
}

// cullLocked gives the nearest maxVisibleSkulls heads within the render
// distance an entity and takes the others' away.
func (c *skullCache) cullLocked() {
	if !c.placed {
		return
	}
	var in []*skull
	for pos, s := range c.skulls {
		d := mgl32.Vec3{float32(pos[0]) + .5, float32(pos[1]) + .5, float32(pos[2]) + .5}.Sub(c.at)
		s.dist2 = float64(d.LenSqr())
		switch {
		case s.dist2 > skullForgetDistance*skullForgetDistance:
			c.freeLocked(s)
			delete(c.skulls, pos)
			delete(c.heads, pos)
		case s.dist2 > skullRenderDistance*skullRenderDistance:
			c.freeLocked(s)
		default:
			in = append(in, s)
		}
	}
	sort.Slice(in, func(i, j int) bool { return in[i].dist2 < in[j].dist2 })
	for i, s := range in {
		if i < maxVisibleSkulls {
			c.spawnLocked(s)
		} else {
			c.freeLocked(s)
		}
	}
}

// spawnLocked is assignSkullEntity: an invisible fake player on the block,
// dressed once its skin is in.
func (c *skullCache) spawnLocked(s *skull) {
	if s.id != 0 {
		return
	}
	c.nextID++
	s.id, s.uid = c.nextID, uuid.New()
	pos, yaw := skullPosition(s.pos, s.state)
	c.send(&packet.AddPlayer{
		UUID:            s.uid,
		EntityRuntimeID: uint64(s.id),
		Position:        pos.Add(mgl32.Vec3{0, playerEyeOffset, 0}),
		Yaw:             yaw, HeadYaw: yaw,
		EntityMetadata: skullMetadata(false),
		AbilityData:    protocol.AbilityData{EntityUniqueID: s.id},
	})
	c.dressLocked(s)
}

// freeLocked is freeSkullEntity.
func (c *skullCache) freeLocked(s *skull) {
	if s.id == 0 {
		return
	}
	c.send(&packet.RemoveActor{EntityUniqueID: s.id})
	s.id = 0
}

// placeLocked moves a head's fake player onto its block (a turned head).
func (c *skullCache) placeLocked(s *skull) {
	pos, yaw := skullPosition(s.pos, s.state)
	c.send(&packet.MovePlayer{
		EntityRuntimeID: uint64(s.id),
		Position:        pos.Add(mgl32.Vec3{0, playerEyeOffset, 0}),
		Yaw:             yaw, HeadYaw: yaw,
		Mode: packet.MoveModeTeleport,
	})
}

// dressLocked hands the fake player its skin, fetching it first if need be.
func (c *skullCache) dressLocked(s *skull) {
	if rgba, ok := c.skins[s.url]; ok {
		c.applySkinLocked(s, rgba)
		return
	}
	if c.fetch == nil || c.pending[s.url] {
		return
	}
	c.pending[s.url] = true
	go c.fetchSkin(s.url)
}

// fetchSkin runs off the session's readers; the result dresses every head
// wearing that skin. A failed fetch leaves those heads invisible (vanilla
// shows the default skin; nothing better is known here).
func (c *skullCache) fetchSkin(textureURL string) {
	rgba, err := c.fetch.FetchSkin(textureURL)
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.pending, textureURL)
	if err != nil {
		return
	}
	if len(c.skins) >= 256 { // a bounded cache: start over rather than grow
		c.skins = map[string][]byte{}
	}
	c.skins[textureURL] = rgba
	for _, s := range c.skulls {
		if s.url == textureURL && s.id != 0 {
			c.applySkinLocked(s, rgba)
		}
	}
}

// applySkinLocked is sendSkinUsingPlayerList (not persistent) followed by
// making the skull visible.
func (c *skullCache) applySkinLocked(s *skull, rgba []byte) {
	c.send(&packet.PlayerList{Entries: []protocol.PlayerListEntry{{
		ActionType: protocol.PlayerListActionAdd, UUID: s.uid, EntityUniqueID: s.id,
		Skin: skullSkin(s.url, rgba), PlayerColour: color.RGBA{R: 0xc0, G: 0xc0, B: 0xc0, A: 0xff},
	}}})
	c.send(&packet.PlayerList{Entries: []protocol.PlayerListEntry{{ActionType: protocol.PlayerListActionRemove, UUID: s.uid}}})
	c.send(&packet.SetActorData{EntityRuntimeID: uint64(s.id), EntityMetadata: skullMetadata(true)})
}

// chunkSkulls reads the owned player heads out of a chunk's block-entity
// section (the same walk as chunkBlockEntities): position, block state and
// the owner's skin URL.
func chunkSkulls(h attach.ChunkHeader, body *attach.ChunkBody) []skullAt {
	if len(h.BEs) == 0 {
		return nil
	}
	r := bytes.NewReader(h.BEs)
	n, err := tproto.ReadVarInt(r)
	if err != nil || n <= 0 || n > 4096 {
		return nil
	}
	var out []skullAt
	for i := int32(0); i < n; i++ {
		xz, err := r.ReadByte()
		if err != nil {
			return out
		}
		var yb [2]byte
		if _, err := io.ReadFull(r, yb[:]); err != nil {
			return out
		}
		y := int32(int16(binary.BigEndian.Uint16(yb[:])))
		typ, err := tproto.ReadVarInt(r)
		if err != nil {
			return out
		}
		tag, ok := readJavaNBT(r)
		if !ok {
			return out
		}
		if typ != blockEntitySkull {
			continue
		}
		lx, lz := int32(xz>>4), int32(xz&15)
		var state uint32
		if body != nil {
			if idx := blockIndex(lx, y, lz); idx >= 0 && idx < len(body.BlockStates) {
				state = body.BlockStates[idx]
			}
		}
		if _, _, _, ok := headPose(state); !ok {
			continue // a mob skull, or a head the chunk does not show
		}
		out = append(out, skullAt{pos: [3]int32{h.CX*16 + lx, y, h.CZ*16 + lz}, state: state, url: skinURLFromProfile(profileProps(tag["profile"]))})
	}
	return out
}

// skullAt is one head found in a chunk.
type skullAt struct {
	pos   [3]int32
	state uint32
	url   string
}

// profileProps reads ResolvableProfile's properties list out of a skull's
// profile compound.
func profileProps(v any) []attach.Property {
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	list, _ := m["properties"].([]any)
	var out []attach.Property
	for _, e := range list {
		pm, ok := e.(map[string]any)
		if !ok {
			continue
		}
		name, _ := pm["name"].(string)
		value, _ := pm["value"].(string)
		sig, _ := pm["signature"].(string)
		out = append(out, attach.Property{Name: name, Value: value, Signature: sig})
	}
	return out
}
