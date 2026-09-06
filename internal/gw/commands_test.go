package gw

import (
	"testing"

	tproto "github.com/tachyne/tachyne-common/protocol"
)

// The world's tree (root → literal commands → one greedy argument) lists
// its command names for Bedrock's autocomplete.
func TestCommandNames(t *testing.T) {
	names := []string{"tp", "help", "gamemode"}
	b := tproto.AppendVarInt(nil, int32(2+len(names)))
	b = tproto.AppendU8(b, 0x00) // root
	b = tproto.AppendVarInt(b, int32(len(names)))
	for i := range names {
		b = tproto.AppendVarInt(b, int32(2+i))
	}
	b = tproto.AppendU8(b, 0x06) // the greedy args argument
	b = tproto.AppendVarInt(b, 0)
	b = tproto.AppendString(b, "args")
	b = tproto.AppendVarInt(b, 5) // brigadier:string
	b = tproto.AppendVarInt(b, 2) // greedy
	for _, n := range names {
		b = tproto.AppendU8(b, 0x05)
		b = tproto.AppendVarInt(b, 1)
		b = tproto.AppendVarInt(b, 1)
		b = tproto.AppendString(b, n)
	}
	b = tproto.AppendVarInt(b, 0)
	got := commandNames(b)
	if len(got) != 3 || got[0] != "gamemode" || got[1] != "help" || got[2] != "tp" {
		t.Fatalf("names %v", got)
	}
	pk := availableCommands(got)
	if len(pk.Commands) != 3 || pk.Commands[2].Name != "tp" || len(pk.Commands[2].Overloads[0].Parameters) != 1 || !pk.Commands[2].Overloads[0].Parameters[0].Optional {
		t.Errorf("commands %+v", pk.Commands)
	}
	if commandNames([]byte{7}) != nil {
		t.Error("a torn tree listed names")
	}
}
