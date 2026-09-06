package gw

import (
	"testing"

	"github.com/tachyne/tachyne-common/protocol"
)

// The dropped item's stack is read off its metadata entry (index 8, Slot):
// count then id, whatever components trail behind.
func TestItemMetaStack(t *testing.T) {
	meta := []byte{8}
	meta = protocol.AppendVarInt(meta, 7)   // Slot
	meta = protocol.AppendVarInt(meta, 3)   // count
	meta = protocol.AppendVarInt(meta, 855) // id
	meta = protocol.AppendVarInt(meta, 0)   // no components added
	meta = protocol.AppendVarInt(meta, 0)   // none removed
	meta = append(meta, 0xff)
	st, ok := itemMetaStack(meta)
	if !ok || st.ID != 855 || st.Count != 3 {
		t.Fatalf("got %+v ok=%v", st, ok)
	}
	if _, ok := itemMetaStack([]byte{0xff}); ok {
		t.Error("an empty list has no stack")
	}
	other := []byte{16}
	other = protocol.AppendVarInt(other, 1)
	other = protocol.AppendVarInt(other, 5)
	other = append(other, 0xff)
	if _, ok := itemMetaStack(other); ok {
		t.Error("only the item entry counts")
	}
}
