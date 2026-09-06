package gw

import (
	"testing"

	tproto "github.com/tachyne/tachyne-common/protocol"
)

func TestSimpleMetaParse(t *testing.T) {
	meta := []byte{0}
	meta = tproto.AppendVarInt(meta, 0) // byte flags
	meta = append(meta, 0x03)           // on fire + sneaking
	meta = append(meta, 16)
	meta = tproto.AppendVarInt(meta, 8) // bool baby
	meta = append(meta, 1)
	meta = append(meta, 0xff)
	st := &entState{}
	if !st.applyMeta(parseSimpleMeta(meta)) || !st.onFire || !st.sneaking || !st.baby {
		t.Errorf("flags not applied: %+v", st)
	}
	if pd := actorData(5, st); pd.EntityRuntimeID != rt(5) || len(pd.EntityMetadata) == 0 {
		t.Errorf("actor data %+v", pd)
	}
	// An unknown type stops the walk without touching what came before.
	meta2 := []byte{16}
	meta2 = tproto.AppendVarInt(meta2, 8)
	meta2 = append(meta2, 0)
	meta2 = append(meta2, 8)
	meta2 = tproto.AppendVarInt(meta2, 7) // a Slot: not walked
	meta2 = append(meta2, 1, 2, 3)
	st2 := &entState{baby: true}
	if !st2.applyMeta(parseSimpleMeta(meta2)) || st2.baby {
		t.Errorf("baby cleared before the unknown entry: %+v", st2)
	}
}
