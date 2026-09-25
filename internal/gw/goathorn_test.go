package gw

import (
	"testing"

	"github.com/tachyne/tachyne-common/attach"
)

// A seek horn (registry index 5, holder 6) shows as Bedrock's goat horn
// with aux 2, Seek.
func TestGoatHornCallIsTheAux(t *testing.T) {
	st := attach.ItemStack{ID: itemGoatHorn, Count: 1, Components: []byte{1, 0, componentInstrument, 1, 6}}
	if call, ok := goatHornCall(st); !ok || call != 2 {
		t.Fatalf("goatHornCall = %d, %v; want 2 (seek)", call, ok)
	}
	if got := bedrockStack(st); got.Stack.ItemType.MetadataValue != 2 {
		t.Fatalf("the Bedrock horn carries aux %d, want 2", got.Stack.ItemType.MetadataValue)
	}
	if _, ok := goatHornCall(attach.ItemStack{ID: itemGoatHorn, Count: 1}); ok {
		t.Fatal("a horn with no components read a call")
	}
}
