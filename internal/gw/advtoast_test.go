package gw

import (
	"testing"

	attach "github.com/tachyne/tachyne-common/attach"
)

// A toast fires once, when the streamed increments complete an advancement
// (every requirement group satisfied); the join snapshot never toasts.
func TestAdvancementToast(t *testing.T) {
	b := newAdvBook()
	b.tree(attach.AdvTree{Nodes: []attach.AdvNode{
		{ID: "story/mine_stone", Reqs: [][]string{{"a", "b"}, {"c"}}, HasDisplay: true, ShowToast: true,
			Title: "advancements.story.mine_stone.title", Frame: 0},
		{ID: "hidden", Reqs: [][]string{{"x"}}},
	}})
	if got := b.progress(attach.AdvProgress{Reset: true, Entries: []attach.AdvProgressEntry{
		{ID: "hidden", Done: map[string]int64{"x": 1}}}}); len(got) != 0 || !b.complete["hidden"] {
		t.Errorf("snapshot toasted: %v", got)
	}
	if got := b.progress(attach.AdvProgress{Entries: []attach.AdvProgressEntry{
		{ID: "story/mine_stone", Done: map[string]int64{"a": 1}}}}); len(got) != 0 {
		t.Errorf("half done toasted: %v", got)
	}
	got := b.progress(attach.AdvProgress{Entries: []attach.AdvProgressEntry{
		{ID: "story/mine_stone", Done: map[string]int64{"c": 1}}}})
	if len(got) != 1 {
		t.Fatalf("completion did not toast: %v", got)
	}
	if pk := advToast(got[0]); pk.Title != "Advancement Made!" || pk.Message != "Stone Age" {
		t.Errorf("toast %+v", pk)
	}
	if got := b.progress(attach.AdvProgress{Entries: []attach.AdvProgressEntry{
		{ID: "story/mine_stone", Done: map[string]int64{"b": 1}}}}); len(got) != 0 {
		t.Errorf("toasted twice: %v", got)
	}
	if pk := advToast(attach.AdvNode{Title: "My Custom", Frame: 1}); pk.Title != "Challenge Complete!" || pk.Message != "My Custom" {
		t.Errorf("literal title %+v", pk)
	}
}

func TestBoatVariant(t *testing.T) {
	for name, want := range map[string]int32{"oak_boat": 0, "dark_oak_chest_boat": 5, "bamboo_raft": 7, "bamboo_chest_raft": 7, "pale_oak_boat": 9} {
		if v, ok := boatVariant(name); !ok || v != want {
			t.Errorf("%s → %d %v, want %d", name, v, ok, want)
		}
	}
	if _, ok := boatVariant("cow"); ok {
		t.Error("a cow is not a boat")
	}
}
