package gw

import (
	"testing"

	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
	attach "github.com/tachyne/tachyne-common/attach"
	tproto "github.com/tachyne/tachyne-common/protocol"
)

func nbtString(s string) []byte { return append([]byte{0x08, byte(len(s) >> 8), byte(len(s))}, s...) }

// A book's components become Bedrock's book NBT; edits rebuild the page
// list Geyser's way; a lectern shows its book off the block entity.
func TestBooks(t *testing.T) {
	// a signed book: title, author, generation, two pages
	w := tproto.AppendVarInt(nil, 1)
	w = tproto.AppendVarInt(w, 0)
	w = tproto.AppendVarInt(w, componentWrittenBook)
	w = tproto.AppendString(w, "Diary")
	w = append(w, 0)
	w = tproto.AppendString(w, "Steve")
	w = tproto.AppendVarInt(w, 0)
	w = tproto.AppendVarInt(w, 2)
	w = append(w, nbtString("Day one")...)
	w = append(w, 0)
	w = append(w, nbtString("Day two")...)
	w = append(w, 0)
	w = append(w, 1)
	b, ok := parseBook(w)
	if !ok || !b.written || b.title != "Diary" || b.author != "Steve" || len(b.pages) != 2 || b.pages[1] != "Day two" {
		t.Fatalf("written book %+v %v", b, ok)
	}
	nbt := bookNBT(attach.ItemStack{ID: 1, Count: 1, Components: w})
	if nbt["title"] != "Diary" || len(nbt["pages"].([]map[string]any)) != 2 || nbt["pages"].([]map[string]any)[0]["text"] != "Day one" {
		t.Errorf("book nbt %v", nbt)
	}
	// a book and quill: plain pages
	q := tproto.AppendVarInt(nil, 1)
	q = tproto.AppendVarInt(q, 0)
	q = tproto.AppendVarInt(q, componentWritableBook)
	q = tproto.AppendVarInt(q, 1)
	q = tproto.AppendString(q, "draft")
	q = append(q, 0)
	if b, ok := parseBook(q); !ok || b.written || len(b.pages) != 1 || b.pages[0] != "draft" {
		t.Errorf("writable book %+v %v", b, ok)
	}
	if bookNBT(attach.ItemStack{ID: 1, Count: 1}) != nil {
		t.Error("a plain stack has book nbt")
	}

	// Edits.
	pages, ok := editPages([]string{"a"}, packet.BookActionAddPage, 2, 0, "c")
	if !ok || len(pages) != 3 || pages[1] != "" || pages[2] != "c" {
		t.Errorf("add page → %v", pages)
	}
	pages, _ = editPages(pages, packet.BookActionReplacePage, 1, 0, "b")
	pages, _ = editPages(pages, packet.BookActionSwapPages, 0, 2, "")
	if pages[0] != "c" || pages[1] != "b" || pages[2] != "a" {
		t.Errorf("replace+swap → %v", pages)
	}
	pages, _ = editPages(pages, packet.BookActionDeletePage, 2, 0, "")
	if len(pages) != 2 {
		t.Errorf("delete → %v", pages)
	}
	if p, _ := editPages([]string{"x", "", ""}, packet.BookActionSign, 0, 0, ""); len(p) != 1 {
		t.Errorf("trailing blanks kept: %v", p)
	}
	m := newInvMirror()
	m.set(36, attach.ItemStack{ID: 1, Count: 1, Components: q})
	e, ok := m.editBook(36, &packet.BookEdit{InventorySlot: 0, ActionType: packet.BookActionSign, Title: "Notes"})
	if !ok || e.Slot != 0 || !e.HasTitle || e.Title != "Notes" || len(e.Pages) != 1 {
		t.Errorf("sign → %+v %v", e, ok)
	}

	// The lectern.
	ld := lecternData([3]int32{1, 2, 3}, attach.ItemStack{ID: 1, Count: 1, Components: w}, 1)
	if ld.NBTData["hasBook"] != byte(1) || ld.NBTData["page"] != int32(1) || ld.NBTData["totalPages"] != int32(2) || ld.NBTData["book"].(map[string]any)["tag"] == nil {
		t.Errorf("lectern %v", ld.NBTData)
	}
	if empty := lecternData([3]int32{}, attach.ItemStack{}, 0); empty.NBTData["hasBook"] != nil {
		t.Error("empty lectern has a book")
	}
}
