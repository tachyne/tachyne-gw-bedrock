package gw

import (
	"hash/fnv"
	"sync"

	"github.com/sandertv/gophertunnel/minecraft/protocol"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
	attach "github.com/tachyne/tachyne-common/attach"
)

// Scoreboards. Java keeps objectives, the slots they show in, per-owner
// scores and teams (whose prefix and suffix decorate an owner's name);
// Bedrock shows an objective in a slot and takes each line as a fake
// player entry with a display name and a score. The board keeps enough
// Java state to rebuild a slot's lines whenever a score or a team
// changes, and every entry gets a stable id per objective and owner.

// javaSlots are the Java display slot ids Bedrock has a twin for.
var javaSlots = map[int32]string{
	attach.SlotList:      packet.ScoreboardSlotList,
	attach.SlotSidebar:   packet.ScoreboardSlotSidebar,
	attach.SlotBelowName: packet.ScoreboardSlotBelowName,
}

// teamColours are Bedrock's colour codes by Java's ChatFormatting ordinal.
var teamColours = []string{"§0", "§1", "§2", "§3", "§4", "§5", "§6", "§7", "§8", "§9", "§a", "§b", "§c", "§d", "§e", "§f"}

type sbObjective struct {
	title  string
	hearts bool
	scores map[string]int32 // by owner
	// display holds the owners whose score carries its own display name
	// (Java's set_score display override), already flattened to § text.
	display map[string]string
}

type sbTeam struct {
	prefix, suffix, colour string
	players                map[string]bool
}

// scoreboard is the session's view of the Java scoreboard.
type scoreboard struct {
	mu         sync.Mutex
	objectives map[string]*sbObjective
	slots      map[int32]string // Java slot → objective shown there
	teams      map[string]*sbTeam
}

func newScoreboard() *scoreboard {
	return &scoreboard{objectives: map[string]*sbObjective{}, slots: map[int32]string{}, teams: map[string]*sbTeam{}}
}

// entryID is a stable id for one owner's line in one objective.
func entryID(objective, owner string) int64 {
	h := fnv.New64a()
	h.Write([]byte(objective))
	h.Write([]byte{0})
	h.Write([]byte(owner))
	return int64(h.Sum64() >> 1)
}

// decorate is an owner's name as its team shows it.
func (b *scoreboard) decorate(owner string) string {
	return b.decorateAs(owner, owner)
}

// decorateAs is an owner's line text: the score's own display name when it
// has one (else the owner), wrapped in the owner's team prefix and suffix
// — vanilla's sidebar formats ownerName() (display or owner) for the team.
func (b *scoreboard) decorateAs(owner, shown string) string {
	for _, t := range b.teams {
		if t.players[owner] {
			return t.colour + t.prefix + shown + t.suffix
		}
	}
	return shown
}

// line is one owner's text in one objective.
func (b *scoreboard) line(o *sbObjective, owner string) string {
	if d, ok := o.display[owner]; ok {
		return b.decorateAs(owner, d)
	}
	return b.decorate(owner)
}

// chatColours are Bedrock's colour codes by Java's ChatFormatting colour name.
var chatColours = map[string]string{
	"black": "§0", "dark_blue": "§1", "dark_green": "§2", "dark_aqua": "§3", "dark_red": "§4",
	"dark_purple": "§5", "gold": "§6", "gray": "§7", "dark_gray": "§8", "blue": "§9",
	"green": "§a", "aqua": "§b", "red": "§c", "light_purple": "§d", "yellow": "§e", "white": "§f",
}

// plainText flattens a Java text component into Bedrock § text: literal
// text, a translate key's fallback (or the key itself), named colours as
// § codes, and the extra siblings in order.
func plainText(t attach.Text) string {
	var s string
	if c, ok := chatColours[t.Color]; ok {
		s = c
	}
	switch {
	case t.Translate != "" && t.Fallback != "":
		s += t.Fallback
	case t.Translate != "":
		s += t.Translate
	default:
		s += t.Text
	}
	for _, x := range t.Extra {
		s += plainText(x)
	}
	return s
}

// objective folds an objective frame in; the packets re-show it where it
// is displayed (a removed objective clears its slots).
func (b *scoreboard) objective(e attach.Objective) []packet.Packet {
	b.mu.Lock()
	defer b.mu.Unlock()
	switch e.Method {
	case attach.ObjRemove:
		delete(b.objectives, e.Name)
		var out []packet.Packet
		for slot, name := range b.slots {
			if name == e.Name {
				delete(b.slots, slot)
				out = append(out, &packet.RemoveObjective{ObjectiveName: e.Name})
			}
		}
		return out
	default:
		o := b.objectives[e.Name]
		if o == nil {
			o = &sbObjective{scores: map[string]int32{}, display: map[string]string{}}
			b.objectives[e.Name] = o
		}
		o.title, o.hearts = e.Title, e.Hearts
		return b.showLocked(e.Name)
	}
}

// display folds a slot binding in.
func (b *scoreboard) display(e attach.DisplaySlot) []packet.Packet {
	b.mu.Lock()
	defer b.mu.Unlock()
	bedrockSlot, ok := javaSlots[e.Slot]
	if !ok {
		return nil
	}
	var out []packet.Packet
	if prev := b.slots[e.Slot]; prev != "" && prev != e.Objective {
		out = append(out, &packet.RemoveObjective{ObjectiveName: prev})
	}
	if e.Objective == "" {
		delete(b.slots, e.Slot)
		return out
	}
	b.slots[e.Slot] = e.Objective
	o := b.objectives[e.Objective]
	if o == nil {
		o = &sbObjective{scores: map[string]int32{}, display: map[string]string{}}
		b.objectives[e.Objective] = o
	}
	out = append(out, b.showIn(bedrockSlot, e.Objective, o)...)
	return out
}

// score folds one score in and updates the line where the objective shows.
func (b *scoreboard) score(e attach.Score) []packet.Packet {
	b.mu.Lock()
	defer b.mu.Unlock()
	o := b.objectives[e.Objective]
	if o == nil {
		return nil
	}
	if e.Reset {
		delete(o.scores, e.Owner)
		delete(o.display, e.Owner)
	} else {
		o.scores[e.Owner] = e.Value
		// The packet's display replaces the override outright: absent clears
		// it, as the vanilla client's handleSetScore does.
		if e.Display != nil {
			o.display[e.Owner] = plainText(*e.Display)
		} else {
			delete(o.display, e.Owner)
		}
	}
	if !b.shown(e.Objective) {
		return nil
	}
	entry := protocol.ScoreboardEntry{EntryID: entryID(e.Objective, e.Owner), ObjectiveName: e.Objective, Score: e.Value,
		IdentityType: protocol.ScoreboardIdentityFakePlayer, DisplayName: b.line(o, e.Owner)}
	if e.Reset { // a removal is an entry of its own kind since 1.26.50
		entry.IdentityType = protocol.ScoreboardIdentityRemove
	}
	return []packet.Packet{&packet.SetScore{Entries: []protocol.ScoreboardEntry{entry}}}
}

// team folds a team frame in; every shown objective is redrawn, since a
// team decorates the owners' names.
func (b *scoreboard) team(e attach.Team) []packet.Packet {
	b.mu.Lock()
	defer b.mu.Unlock()
	t := b.teams[e.Name]
	switch e.Method {
	case attach.TeamRemove:
		delete(b.teams, e.Name)
	case attach.TeamAddPlayers:
		if t == nil {
			return nil
		}
		for _, p := range e.Players {
			t.players[p] = true
		}
	case attach.TeamRemovePlayers:
		if t == nil {
			return nil
		}
		for _, p := range e.Players {
			delete(t.players, p)
		}
	default: // add / update
		if t == nil {
			t = &sbTeam{players: map[string]bool{}}
			b.teams[e.Name] = t
		}
		t.prefix, t.suffix, t.colour = e.Prefix, e.Suffix, ""
		if e.Color >= 0 && int(e.Color) < len(teamColours) {
			t.colour = teamColours[e.Color]
		}
		if e.Method == attach.TeamAdd {
			t.players = map[string]bool{}
		}
		for _, p := range e.Players {
			t.players[p] = true
		}
	}
	var out []packet.Packet
	for _, name := range b.slots {
		out = append(out, b.showLocked(name)...)
	}
	return out
}

func (b *scoreboard) shown(name string) bool {
	for _, n := range b.slots {
		if n == name {
			return true
		}
	}
	return false
}

// showLocked redraws an objective in every slot it occupies (lock held).
func (b *scoreboard) showLocked(name string) []packet.Packet {
	o := b.objectives[name]
	if o == nil {
		return nil
	}
	var out []packet.Packet
	for slot, n := range b.slots {
		if n == name {
			out = append(out, b.showIn(javaSlots[slot], name, o)...)
		}
	}
	return out
}

// showIn draws an objective into a Bedrock slot: the objective, then
// every line.
func (b *scoreboard) showIn(slot, name string, o *sbObjective) []packet.Packet {
	title := o.title
	if title == "" {
		title = name
	}
	out := []packet.Packet{&packet.SetDisplayObjective{DisplaySlot: slot, ObjectiveName: name, DisplayName: title,
		CriteriaName: "dummy", SortOrder: packet.ScoreboardSortOrderDescending}}
	var entries []protocol.ScoreboardEntry
	for owner, v := range o.scores {
		entries = append(entries, protocol.ScoreboardEntry{EntryID: entryID(name, owner), ObjectiveName: name, Score: v,
			IdentityType: protocol.ScoreboardIdentityFakePlayer, DisplayName: b.line(o, owner)})
	}
	if len(entries) > 0 {
		out = append(out, &packet.SetScore{Entries: entries})
	}
	return out
}
