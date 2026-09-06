package gw

import (
	"strings"

	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
	attach "github.com/tachyne/tachyne-common/attach"
)

// Advancements. Bedrock has no advancement tree to render, but it has the
// toast: when the player's progress completes an advancement whose display
// wants one, a ToastRequest shows "Advancement Made!" over its title, as
// Geyser does. The book keeps the tree and the player's criteria so that a
// completion is noticed exactly once, from the streamed increments and
// never from the join-time snapshot.
type advBook struct {
	nodes    map[string]attach.AdvNode
	done     map[string]map[string]bool
	complete map[string]bool
}

func newAdvBook() *advBook {
	return &advBook{nodes: map[string]attach.AdvNode{}, done: map[string]map[string]bool{}, complete: map[string]bool{}}
}

func (b *advBook) tree(t attach.AdvTree) {
	for _, n := range t.Nodes {
		b.nodes[n.ID] = n
	}
}

// progress folds one progress frame in and returns the advancements it
// completed that want a toast. A Reset (the join-time snapshot) replaces
// the book silently.
func (b *advBook) progress(p attach.AdvProgress) []attach.AdvNode {
	if p.Reset {
		b.done = map[string]map[string]bool{}
		b.complete = map[string]bool{}
	}
	var toasts []attach.AdvNode
	for _, e := range p.Entries {
		d := b.done[e.ID]
		if d == nil {
			d = map[string]bool{}
			b.done[e.ID] = d
		}
		for c := range e.Done {
			d[c] = true
		}
		if b.complete[e.ID] || !b.isDone(e.ID) {
			continue
		}
		b.complete[e.ID] = true
		if n, ok := b.nodes[e.ID]; ok && !p.Reset && n.HasDisplay && n.ShowToast {
			toasts = append(toasts, n)
		}
	}
	return toasts
}

// isDone is vanilla's AdvancementProgress.isDone: every requirement group
// has at least one obtained criterion.
func (b *advBook) isDone(id string) bool {
	n, ok := b.nodes[id]
	if !ok || len(n.Reqs) == 0 {
		return false
	}
	d := b.done[id]
	for _, group := range n.Reqs {
		hit := false
		for _, c := range group {
			if d[c] {
				hit = true
				break
			}
		}
		if !hit {
			return false
		}
	}
	return true
}

// advToast is the toast for a completed advancement: the frame's line
// over the title, in English (Bedrock cannot translate Java keys).
func advToast(n attach.AdvNode) *packet.ToastRequest {
	kind := "task"
	switch n.Frame {
	case 1:
		kind = "challenge"
	case 2:
		kind = "goal"
	}
	return &packet.ToastRequest{Title: advText("advancements.toast." + kind), Message: advText(n.Title)}
}

// advText resolves a translate key to English, or passes a literal through.
func advText(key string) string {
	if s, ok := advLangEN[key]; ok {
		return s
	}
	return key
}

// boatWoods is Bedrock's boat variant order.
var boatWoods = []string{"oak", "spruce", "birch", "jungle", "acacia", "dark_oak", "mangrove", "bamboo", "cherry", "pale_oak"}

// boatVariant is the Bedrock boat variant for a Java boat type (each wood
// is its own entity type on Java; one boat with a variant on Bedrock).
func boatVariant(javaName string) (int32, bool) {
	wood := javaName
	for _, suffix := range []string{"_chest_boat", "_boat", "_chest_raft", "_raft"} {
		if strings.HasSuffix(wood, suffix) {
			wood = strings.TrimSuffix(wood, suffix)
			for i, w := range boatWoods {
				if w == wood {
					return int32(i), true
				}
			}
			return 0, false
		}
	}
	return 0, false
}
