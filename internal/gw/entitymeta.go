package gw

import (
	"bytes"
	"io"

	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
	tproto "github.com/tachyne/tachyne-common/protocol"
)

// Entity metadata. The world's canonical set_entity_data carries what a
// Java client renders; the pieces Bedrock has a twin for are the shared
// entity flags (on fire, sneaking via the crouching pose) and the ageable
// baby flag (a half-scale, baby-flagged actor).

// metaEntry is one parsed canonical entry of a simple type.
type metaEntry struct {
	idx byte
	typ int32
	val int64 // byte/bool/varint/pose payload
}

// parseSimpleMeta walks a canonical metadata list as far as its simple
// value types reach (byte, varint, float, bool, pose); an entry of any
// other type ends the walk, since its size is unknown here.
func parseSimpleMeta(meta []byte) []metaEntry {
	var out []metaEntry
	r := bytes.NewReader(meta)
	for {
		idx, err := r.ReadByte()
		if err != nil || idx == 0xff {
			return out
		}
		typ, err := tproto.ReadVarInt(r)
		if err != nil {
			return out
		}
		var val int64
		switch typ {
		case 0, 8: // byte, boolean
			b, err := r.ReadByte()
			if err != nil {
				return out
			}
			val = int64(b)
		case 1, 21: // varint, pose
			v, err := tproto.ReadVarInt(r)
			if err != nil {
				return out
			}
			val = int64(v)
		case 3: // float
			var f [4]byte
			if _, err := io.ReadFull(r, f[:]); err != nil {
				return out
			}
		default:
			return out
		}
		out = append(out, metaEntry{idx, typ, val})
	}
}

// applyMeta folds the entries Bedrock understands into the entity's state;
// returns whether anything it renders changed.
func (st *entState) applyMeta(entries []metaEntry) bool {
	changed := false
	for _, e := range entries {
		switch {
		case e.idx == 0 && e.typ == 0: // Entity flags byte
			fire, sneak := e.val&0x01 != 0, e.val&0x02 != 0
			if fire != st.onFire || sneak != st.sneaking {
				st.onFire, st.sneaking = fire, sneak
				changed = true
			}
		case e.idx == 6 && e.typ == 21: // pose: crouching is 5
			if crouch := e.val == 5; crouch != st.sneaking {
				st.sneaking = crouch
				changed = true
			}
		case e.idx == 16 && e.typ == 8: // AgeableMob / Zombie baby flag
			if baby := e.val != 0; baby != st.baby {
				st.baby = baby
				changed = true
			}
		}
	}
	return changed
}

// actorData is the SetActorData carrying the entity's current flags.
func actorData(eid int32, st *entState) *packet.SetActorData {
	m := baseMetadata(0, 0)
	if st.onFire {
		m.SetFlag(protocol.EntityDataKeyFlags, protocol.EntityDataFlagOnFire)
	}
	if st.sneaking {
		m.SetFlag(protocol.EntityDataKeyFlags, protocol.EntityDataFlagSneaking)
	}
	if st.baby {
		m.SetFlag(protocol.EntityDataKeyFlags, protocol.EntityDataFlagBaby)
		m[protocol.EntityDataKeyScale] = float32(0.5)
	} else {
		m[protocol.EntityDataKeyScale] = float32(1)
	}
	return &packet.SetActorData{EntityRuntimeID: rt(eid), EntityMetadata: m}
}
