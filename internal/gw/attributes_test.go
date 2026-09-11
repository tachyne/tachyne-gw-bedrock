package gw

import (
	"testing"

	attach "github.com/tachyne/tachyne-common/attach"
)

func TestAttributeValueOps(t *testing.T) {
	e := attach.EntityAttributes{EID: 1, Attrs: []attach.AttributeSnapshot{{Name: "minecraft:movement_speed", Base: 0.1, Modifiers: []attach.AttributeModifier{
		{ID: "a", Amount: 0.1, Op: 0}, {ID: "b", Amount: 0.5, Op: 1}, {ID: "c", Amount: 0.2, Op: 2},
	}}, {Name: "minecraft:jump_strength", Base: 0.8}}}
	v, ok := attributeValue(e, "minecraft:movement_speed")
	if !ok || v < 0.3599 || v > 0.3601 { // (0.1+0.1) → +50% → 0.3 → ×1.2
		t.Fatalf("value %v ok %v", v, ok)
	}
	attrs := bedrockAttributes(e)
	if len(attrs) != 2 || attrs[0].Name != "minecraft:movement" || attrs[1].Name != "minecraft:horse.jump_strength" || attrs[1].Value != 0.8 {
		t.Fatalf("bedrock attrs %+v", attrs)
	}
	if _, ok := attributeValue(e, "minecraft:max_health"); ok {
		t.Fatal("an absent attribute is absent")
	}
}
