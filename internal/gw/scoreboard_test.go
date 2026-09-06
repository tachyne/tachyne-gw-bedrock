package gw

import (
	"testing"

	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
	attach "github.com/tachyne/tachyne-common/attach"
)

// An objective shown in the sidebar draws its lines; scores update them;
// a team decorates the names; removal clears the slot.
func TestScoreboard(t *testing.T) {
	b := newScoreboard()
	if pks := b.objective(attach.Objective{Name: "kills", Method: attach.ObjAdd, Title: "Kills"}); len(pks) != 0 {
		t.Errorf("an unshown objective drew %d packets", len(pks))
	}
	b.score(attach.Score{Owner: "Steve", Objective: "kills", Value: 3})
	pks := b.display(attach.DisplaySlot{Slot: attach.SlotSidebar, Objective: "kills"})
	if len(pks) != 2 {
		t.Fatalf("display drew %d packets", len(pks))
	}
	if d := pks[0].(*packet.SetDisplayObjective); d.DisplaySlot != "sidebar" || d.DisplayName != "Kills" || d.ObjectiveName != "kills" {
		t.Errorf("display %+v", d)
	}
	if s := pks[1].(*packet.SetScore); len(s.Entries) != 1 || s.Entries[0].DisplayName != "Steve" || s.Entries[0].Score != 3 {
		t.Errorf("lines %+v", s.Entries)
	}
	up := b.score(attach.Score{Owner: "Alex", Objective: "kills", Value: 5})
	if len(up) != 1 || up[0].(*packet.SetScore).Entries[0].EntryID != entryID("kills", "Alex") {
		t.Errorf("score update %+v", up)
	}
	tm := b.team(attach.Team{Name: "red", Method: attach.TeamAdd, Prefix: "[R] ", Color: 12, Players: []string{"Alex"}})
	if len(tm) != 2 || tm[1].(*packet.SetScore).Entries[0].DisplayName == "Alex" && tm[1].(*packet.SetScore).Entries[1].DisplayName == "Steve" {
		found := false
		for _, e := range tm[1].(*packet.SetScore).Entries {
			if e.DisplayName == "§c[R] Alex" {
				found = true
			}
		}
		if !found {
			t.Errorf("team decoration missing: %+v", tm[1].(*packet.SetScore).Entries)
		}
	}
	rm := b.score(attach.Score{Owner: "Steve", Objective: "kills", Reset: true})
	if len(rm) != 1 || rm[0].(*packet.SetScore).ActionType != packet.ScoreboardActionRemove {
		t.Errorf("reset %+v", rm)
	}
	if pks := b.objective(attach.Objective{Name: "kills", Method: attach.ObjRemove}); len(pks) != 1 || pks[0].(*packet.RemoveObjective).ObjectiveName != "kills" {
		t.Errorf("remove %+v", pks)
	}
	if b.display(attach.DisplaySlot{Slot: 5, Objective: "x"}) != nil {
		t.Error("a team-colour slot rendered")
	}
}
