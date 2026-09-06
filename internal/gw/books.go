package gw

import (
	"bytes"
	"strings"

	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
	attach "github.com/tachyne/tachyne-common/attach"
	tproto "github.com/tachyne/tachyne-common/protocol"
)

// Books. Java carries a book's pages as item components; Bedrock keeps
// them as item NBT ("pages" of text, plus title/author/generation for a
// signed book), so a book stack gets its NBT from its components. A book
// and quill is edited page by page on Bedrock (BookEdit); each edit is
// applied to the pages the stack carries and sent to the world as the
// whole book, the way Java's edit_book does. A lectern shows its book off
// its block entity, opened as Bedrock's lectern container; page turns
// come back as LecternUpdate and become the world's jump-to-page button.

const (
	componentWritableBook = 45 // minecraft:writable_book_content, canonical
	componentWrittenBook  = 46 // minecraft:written_book_content, canonical
	lecternJumpButton     = 100
)

// bookContent is what a book stack's components say.
type bookContent struct {
	written       bool
	title, author string
	generation    int32
	pages         []string
}

// parseBook reads the book component out of a stack's component bytes
// (add count, remove count, entries). Only the book component is
// understood; a stack whose first component is something else reads as
// no book.
func parseBook(comps []byte) (bookContent, bool) {
	var b bookContent
	r := bytes.NewReader(comps)
	addC, err := tproto.ReadVarInt(r)
	if err != nil || addC <= 0 {
		return b, false
	}
	if _, err := tproto.ReadVarInt(r); err != nil { // removed count
		return b, false
	}
	id, err := tproto.ReadVarInt(r)
	if err != nil {
		return b, false
	}
	switch id {
	case componentWrittenBook:
		b.written = true
		if b.title, err = tproto.ReadString(r); err != nil {
			return b, false
		}
		if !skipFiltered(r) {
			return b, false
		}
		if b.author, err = tproto.ReadString(r); err != nil {
			return b, false
		}
		if b.generation, err = tproto.ReadVarInt(r); err != nil {
			return b, false
		}
		n, err := tproto.ReadVarInt(r)
		if err != nil || n < 0 || n > 100 {
			return b, false
		}
		for i := int32(0); i < n; i++ {
			p, ok := readNBTString(r)
			if !ok || !skipFilteredNBT(r) {
				return b, false
			}
			b.pages = append(b.pages, p)
		}
	case componentWritableBook:
		n, err := tproto.ReadVarInt(r)
		if err != nil || n < 0 || n > 100 {
			return b, false
		}
		for i := int32(0); i < n; i++ {
			p, err := tproto.ReadString(r)
			if err != nil || !skipFiltered(r) {
				return b, false
			}
			b.pages = append(b.pages, p)
		}
	default:
		return b, false
	}
	return b, true
}

// skipFiltered passes an optional filtered string (bool + string).
func skipFiltered(r *bytes.Reader) bool {
	has, err := r.ReadByte()
	if err != nil {
		return false
	}
	if has != 0 {
		if _, err := tproto.ReadString(r); err != nil {
			return false
		}
	}
	return true
}

// readNBTString reads a text component in the one form the world emits:
// a bare network-NBT TAG_String.
func readNBTString(r *bytes.Reader) (string, bool) {
	var hdr [3]byte
	if n, err := r.Read(hdr[:]); err != nil || n != 3 || hdr[0] != 0x08 {
		return "", false
	}
	buf := make([]byte, int(hdr[1])<<8|int(hdr[2]))
	if n, err := r.Read(buf); err != nil || n != len(buf) {
		return "", false
	}
	return string(buf), true
}

// skipFilteredNBT passes an optional filtered text component.
func skipFilteredNBT(r *bytes.Reader) bool {
	has, err := r.ReadByte()
	if err != nil {
		return false
	}
	if has != 0 {
		if _, ok := readNBTString(r); !ok {
			return false
		}
	}
	return true
}

// bookNBT is a book's Bedrock item NBT, or nil for a stack that is not a
// book with contents.
func bookNBT(st attach.ItemStack) map[string]any {
	if len(st.Components) == 0 {
		return nil
	}
	b, ok := parseBook(st.Components)
	if !ok {
		return nil
	}
	pages := make([]map[string]any, 0, len(b.pages))
	for _, p := range b.pages {
		pages = append(pages, map[string]any{"photoname": "", "text": p})
	}
	nbt := map[string]any{"pages": pages}
	if b.written {
		nbt["title"] = b.title
		nbt["author"] = b.author
		nbt["generation"] = b.generation
	}
	return nbt
}

// editPages applies one Bedrock book edit to a page list, Geyser's way:
// pages are padded up to the edited one, and trailing empty pages fall
// off. Returns false for an action that changes nothing.
func editPages(pages []string, action uint32, page, page2 int, text string) ([]string, bool) {
	if page < 0 || page >= 100 {
		return pages, false
	}
	out := append([]string(nil), pages...)
	switch action {
	case packet.BookActionAddPage, packet.BookActionReplacePage:
		for len(out) < page {
			out = append(out, "")
		}
		if action == packet.BookActionReplacePage && page < len(out) {
			out[page] = text
		} else {
			out = append(out[:page], append([]string{text}, out[page:]...)...)
		}
	case packet.BookActionDeletePage:
		if page >= len(out) {
			return pages, false
		}
		out = append(out[:page], out[page+1:]...)
	case packet.BookActionSwapPages:
		if page >= len(out) || page2 < 0 || page2 >= len(out) {
			return pages, false
		}
		out[page], out[page2] = out[page2], out[page]
	case packet.BookActionSign:
	default:
		return pages, false
	}
	for len(out) > 0 && strings.TrimSpace(out[len(out)-1]) == "" {
		out = out[:len(out)-1]
	}
	return out, true
}

// lecternLayout: the one book slot, never shown as a container (the block
// entity carries the book).
var lecternLayout = []winSlot{{protocol.ContainerLevelEntity, 0}}

// lecternData renders a lectern's block entity: its book, the open page
// and the page count.
func lecternData(at [3]int32, book attach.ItemStack, page int32) *packet.BlockActorData {
	nbt := map[string]any{"id": "Lectern", "x": at[0], "y": at[1], "z": at[2], "isMovable": byte(1)}
	if book.Count > 0 {
		item := map[string]any{"Count": byte(1), "Damage": int16(0), "Name": "minecraft:written_book"}
		if int(book.ID) < len(javaItemBedrock) && javaItemBedrock[book.ID].Name != "" {
			item["Name"] = javaItemBedrock[book.ID].Name
		}
		total := int32(1)
		if tag := bookNBT(book); tag != nil {
			item["tag"] = tag
			if n := len(tag["pages"].([]map[string]any)); n > 0 {
				total = int32(n)
			}
		}
		nbt["book"] = item
		nbt["hasBook"] = byte(1)
		nbt["page"] = page
		nbt["totalPages"] = total
	}
	return &packet.BlockActorData{Position: protocol.BlockPos{at[0], at[1], at[2]}, NBTData: nbt}
}
