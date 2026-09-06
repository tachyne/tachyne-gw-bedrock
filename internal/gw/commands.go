package gw

import (
	"bytes"
	"sort"

	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
	tproto "github.com/tachyne/tachyne-common/protocol"
)

// Commands. The world's command tree (canonical brigadier: a root whose
// literal children are the command names, each taking the rest of the
// line as one greedy argument) becomes Bedrock's AvailableCommands, so
// the client offers the command names as it types; the line itself goes
// to the world as it always did.

// commandNames reads the root's literal children out of a canonical
// brigadier tree body (node count; per node: flags, children, optional
// redirect, name and parser for non-root nodes; then the root index).
func commandNames(tree []byte) []string {
	r := bytes.NewReader(tree)
	n, err := tproto.ReadVarInt(r)
	if err != nil || n <= 0 || n > 4096 {
		return nil
	}
	type node struct {
		kind     byte
		children []int32
		name     string
	}
	nodes := make([]node, n)
	for i := range nodes {
		flags, err := r.ReadByte()
		if err != nil {
			return nil
		}
		nodes[i].kind = flags & 3
		c, err := tproto.ReadVarInt(r)
		if err != nil || c < 0 || c > 4096 {
			return nil
		}
		for j := int32(0); j < c; j++ {
			idx, err := tproto.ReadVarInt(r)
			if err != nil {
				return nil
			}
			nodes[i].children = append(nodes[i].children, idx)
		}
		if flags&0x08 != 0 { // redirect
			if _, err := tproto.ReadVarInt(r); err != nil {
				return nil
			}
		}
		if nodes[i].kind == 1 || nodes[i].kind == 2 {
			if nodes[i].name, err = tproto.ReadString(r); err != nil {
				return nil
			}
		}
		if nodes[i].kind == 2 { // an argument: parser id, then properties we only know for the greedy string
			parser, err := tproto.ReadVarInt(r)
			if err != nil {
				return nil
			}
			if parser == 5 { // brigadier:string — one varint of properties
				if _, err := tproto.ReadVarInt(r); err != nil {
					return nil
				}
			} else {
				return nil // a parser with properties we cannot walk past
			}
			if flags&0x10 != 0 { // suggestion type
				if _, err := tproto.ReadString(r); err != nil {
					return nil
				}
			}
		}
	}
	root, err := tproto.ReadVarInt(r)
	if err != nil || root < 0 || int(root) >= len(nodes) {
		return nil
	}
	var names []string
	for _, c := range nodes[root].children {
		if int(c) < len(nodes) && nodes[c].kind == 1 && nodes[c].name != "" {
			names = append(names, nodes[c].name)
		}
	}
	sort.Strings(names)
	return names
}

// availableCommands lists the names for Bedrock's autocomplete, each
// taking the rest of the line as optional raw text.
func availableCommands(names []string) *packet.AvailableCommands {
	pk := &packet.AvailableCommands{}
	for _, n := range names {
		pk.Commands = append(pk.Commands, protocol.Command{
			Name: n, AliasesOffset: 0xFFFFFFFF,
			Overloads: []protocol.CommandOverload{{Parameters: []protocol.CommandParameter{{
				Name: "args", Type: protocol.CommandArgValid | protocol.CommandArgTypeRawText, Optional: true}}}},
		})
	}
	return pk
}
