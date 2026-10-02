package gw

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"sync"
	"testing"
	"time"

	"github.com/go-gl/mathgl/mgl32"
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
	attach "github.com/tachyne/tachyne-common/attach"
	tproto "github.com/tachyne/tachyne-common/protocol"
)

// The head states, against the 26.3 report (player_head 12802-12833,
// player_wall_head 12834-12841), and where each sits and faces.
func TestHeadPose(t *testing.T) {
	if playerHeadFirst != 12802 || playerWallHeadFirst != 12834 {
		t.Fatalf("head state ranges start at %d and %d", playerHeadFirst, playerWallHeadFirst)
	}
	for _, c := range []struct {
		state uint32
		wall  bool
		yaw   float32
	}{
		{12818, false, 180}, // powered false, rotation 0
		{12802 + 4, false, 270},
		{12802 + 8, false, 0}, // (180 + 180) mod 360
		{12835, true, 180},    // north
		{12836, true, 0},      // south
		{12838, true, 90},     // west
		{12841, true, 270},    // east
	} {
		wall, yaw, _, ok := headPose(c.state)
		if !ok || wall != c.wall || yaw != c.yaw {
			t.Errorf("state %d: wall %v yaw %v ok %v", c.state, wall, yaw, ok)
		}
	}
	if _, _, _, ok := headPose(1); ok {
		t.Error("stone is a head")
	}
	p, _ := skullPosition([3]int32{10, 64, -3}, 12841) // east wall head
	if want := (mgl32.Vec3{10.26, 64.24, -2.5}); p.Sub(want).Len() > 1e-4 {
		t.Errorf("east wall head at %v, want %v", p, want)
	}
}

func texturesProp(url string) attach.Property {
	raw, _ := json.Marshal(map[string]any{"textures": map[string]any{"SKIN": map[string]any{"url": url}}})
	return attach.Property{Name: "textures", Value: base64.StdEncoding.EncodeToString(raw)}
}

func TestSkinURLFromProfile(t *testing.T) {
	const u = "http://textures.minecraft.net/texture/abc"
	if got := skinURLFromProfile([]attach.Property{{Name: "other"}, texturesProp(u)}); got != u {
		t.Fatalf("url %q", got)
	}
	if skinURLFromProfile([]attach.Property{{Name: "textures", Value: "!!"}}) != "" {
		t.Fatal("garbage decoded")
	}
	if _, err := (httpSkins{}).FetchSkin("http://example.com/texture/abc"); err != errSkinHost {
		t.Fatalf("a foreign host was fetched: %v", err)
	}
}

// A legacy 64×32 skin fills the top half; the rest is transparent.
func TestSkinRGBA(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 64, 32))
	img.SetNRGBA(8, 8, color.NRGBA{R: 10, G: 20, B: 30, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	rgba, err := skinRGBA(buf.Bytes())
	if err != nil || len(rgba) != 64*64*4 {
		t.Fatalf("rgba %d bytes, %v", len(rgba), err)
	}
	if i := (8*64 + 8) * 4; !bytes.Equal(rgba[i:i+4], []byte{10, 20, 30, 255}) {
		t.Fatalf("pixel %v", rgba[i:i+4])
	}
	if rgba[len(rgba)-1] != 0 {
		t.Fatal("the bottom half is not transparent")
	}
}

// recorder collects what the cache sends, from any goroutine.
type recorder struct {
	mu  sync.Mutex
	pks []packet.Packet
}

func (r *recorder) send(p packet.Packet) { r.mu.Lock(); r.pks = append(r.pks, p); r.mu.Unlock() }
func (r *recorder) take() []packet.Packet {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := r.pks
	r.pks = nil
	return out
}

// fakeSkins reports each fetch and answers once released.
type fakeSkins struct {
	calls   chan string
	release chan struct{}
}

func (f fakeSkins) FetchSkin(u string) ([]byte, error) {
	f.calls <- u
	<-f.release
	return make([]byte, 64*64*4), nil
}

// A head in a chunk becomes an invisible fake player on its block, dressed
// once its skin arrives (player list add and remove, then visible); a far
// head waits until the player comes near; breaking the head removes it.
func TestSkullCacheLifecycle(t *testing.T) {
	const u = "http://textures.minecraft.net/texture/abc"
	// The chunk's block-entity section, as the world sends it: one skull at
	// local (1, 64, 1) whose profile carries the textures property.
	be := tproto.AppendVarInt(nil, 1)
	be = append(be, 1<<4|1, 0, 64)
	be = tproto.AppendVarInt(be, blockEntitySkull)
	nbt := tproto.NBTCompound(tproto.NBTRoot(), "profile")
	nbt = tproto.NBTString(nbt, "name", "Steve")
	nbt = tproto.NBTCompoundList(nbt, "properties", 1)
	prop := texturesProp(u)
	nbt = tproto.NBTEnd(tproto.NBTString(tproto.NBTString(nbt, "name", prop.Name), "value", prop.Value))
	nbt = tproto.NBTEnd(tproto.NBTEnd(nbt))
	be = append(be, nbt...)
	body := &attach.ChunkBody{BlockStates: make([]uint32, 24*4096)}
	body.BlockStates[blockIndex(1, 64, 1)] = 12818
	found := chunkSkulls(attach.ChunkHeader{CX: 0, CZ: 0, BEs: be}, body)
	if len(found) != 1 || found[0].pos != [3]int32{1, 64, 1} || found[0].url != u {
		t.Fatalf("chunk skulls %+v", found)
	}

	rec := &recorder{}
	calls, release := make(chan string, 4), make(chan struct{})
	c := newSkullCache(rec.send, fakeSkins{calls, release})
	c.moved(0, 64, 0)
	c.put(found[0].pos, found[0].state, found[0].url)
	pks := rec.take()
	if len(pks) != 1 {
		t.Fatalf("spawn sent %d packets", len(pks))
	}
	add := pks[0].(*packet.AddPlayer)
	if want := (mgl32.Vec3{1.5, 63.99 + playerEyeOffset, 1.5}); add.Position.Sub(want).Len() > 1e-3 || add.Yaw != 180 {
		t.Fatalf("fake player at %v yaw %v", add.Position, add.Yaw)
	}
	if add.EntityMetadata[protocol.EntityDataKeyFlags].(int64)&(1<<protocol.EntityDataFlagInvisible) == 0 {
		t.Fatal("visible before its skin")
	}
	select {
	case got := <-calls:
		if got != u {
			t.Fatalf("fetched %q", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no fetch")
	}
	close(release)
	deadline := time.Now().Add(5 * time.Second)
	for len(pks) < 4 && time.Now().Before(deadline) { // the add, then the three dressing packets
		time.Sleep(10 * time.Millisecond)
		pks = append(pks, rec.take()...)
	}
	pks = pks[1:]
	if len(pks) != 3 {
		t.Fatalf("dressing sent %d packets", len(pks))
	}
	list := pks[0].(*packet.PlayerList)
	if e := list.Entries[0]; e.ActionType != protocol.PlayerListActionAdd || e.UUID != add.UUID ||
		string(e.Skin.SkinResourcePatch) != `{"geometry" :{"default" :"geometry.humanoid.customskull"}}` || len(e.Skin.SkinData) != 64*64*4 {
		t.Fatalf("skin entry %+v", e.ActionType)
	}
	if pks[1].(*packet.PlayerList).Entries[0].ActionType != protocol.PlayerListActionRemove {
		t.Fatal("the fake player stayed listed")
	}
	if md := pks[2].(*packet.SetActorData).EntityMetadata; md[protocol.EntityDataKeyFlags].(int64)&(1<<protocol.EntityDataFlagInvisible) != 0 {
		t.Fatal("still invisible after its skin")
	}

	// Far away: no entity until the player comes near.
	c.put([3]int32{200, 64, 0}, 12818, u)
	if pks := rec.take(); len(pks) != 0 {
		t.Fatalf("a far head spawned: %d packets", len(pks))
	}
	c.moved(190, 64, 0)
	pks = rec.take()
	var spawned, freed bool
	for _, p := range pks {
		switch p.(type) {
		case *packet.AddPlayer:
			spawned = true
		case *packet.RemoveActor:
			freed = true // the first head is now out of range
		}
	}
	if !spawned || !freed {
		t.Fatalf("after moving: spawned %v freed %v", spawned, freed)
	}

	// Broken: the block becomes air.
	c.blockSet([3]int32{200, 64, 0}, 0)
	if pks := rec.take(); len(pks) != 1 {
		t.Fatalf("breaking sent %d packets", len(pks))
	} else if _, ok := pks[0].(*packet.RemoveActor); !ok {
		t.Fatalf("breaking sent %T", pks[0])
	}
}
