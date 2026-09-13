package gw

import (
	"github.com/go-gl/mathgl/mgl32"
	"github.com/sandertv/gophertunnel/minecraft/protocol/packet"
	"github.com/tachyne/tachyne-common/attach"
)

// Bedrock has no per-stage crack overlay: it is told a total break time
// and animates the cracks itself (level events 3600/3602 carry
// 65535/ticks-remaining, 3601 clears). Java sends stages, so the gateway
// estimates the pace the way Geyser does — the first stage starts a slow
// crack, each later stage re-times it from the ticks since the last one.

type crackKey struct{ x, y, z int32 }

type crackStage struct {
	tick  int64 // the client tick the stage arrived
	stage int8
}

// blockCrackTracker keeps one estimate per cracked block.
type blockCrackTracker struct {
	last map[crackKey]crackStage
}

// packet turns a stage into the level event to send, or nil for nothing.
func (t *blockCrackTracker) packet(e attach.BlockBreakProgress, now int64) *packet.LevelEvent {
	if t.last == nil {
		t.last = map[crackKey]crackStage{}
	}
	key := crackKey{e.X, e.Y, e.Z}
	pos := mgl32.Vec3{float32(e.X), float32(e.Y), float32(e.Z)}
	if e.Progress < 0 || e.Progress > 9 {
		delete(t.last, key)
		return &packet.LevelEvent{EventType: packet.LevelEventStopBlockCracking, Position: pos}
	}
	prev, seen := t.last[key]
	t.last[key] = crackStage{tick: now, stage: e.Progress}
	if !seen {
		return &packet.LevelEvent{EventType: packet.LevelEventStartBlockCracking, Position: pos, EventData: 65535 / 6000}
	}
	ticksSince := int32(now - prev.tick)
	stagesSince := int32(e.Progress - prev.stage)
	ticksPerStage := ticksSince
	if stagesSince > 0 {
		ticksPerStage = ticksSince / stagesSince
	}
	if ticksPerStage < 1 {
		ticksPerStage = 1
	}
	remaining := int32(10 - e.Progress)
	if remaining < 1 {
		remaining = 1
	}
	return &packet.LevelEvent{EventType: packet.LevelEventUpdateBlockCracking, Position: pos, EventData: 65535 / remaining * ticksPerStage}
}
