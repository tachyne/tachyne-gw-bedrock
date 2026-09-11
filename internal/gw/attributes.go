package gw

import (
	"github.com/sandertv/gophertunnel/minecraft/protocol"
	attach "github.com/tachyne/tachyne-common/attach"
)

// attributeValue evaluates one attribute of a frame the way vanilla's
// AttributeInstance does: flat additions first, then each multiply-base
// against that sum, then each multiply-total in turn.
func attributeValue(e attach.EntityAttributes, name string) (float64, bool) {
	for _, a := range e.Attrs {
		if a.Name != name {
			continue
		}
		v := a.Base
		for _, m := range a.Modifiers {
			if m.Op == 0 {
				v += m.Amount
			}
		}
		base := v
		for _, m := range a.Modifiers {
			if m.Op == 1 {
				v += base * m.Amount
			}
		}
		for _, m := range a.Modifiers {
			if m.Op == 2 {
				v *= 1 + m.Amount
			}
		}
		return v, true
	}
	return 0, false
}

// bedrockAttributes maps the Java attributes Bedrock's client physics reads
// onto its own names: movement speed and, for the horse family, the jump
// strength that sizes the rider's jump.
func bedrockAttributes(e attach.EntityAttributes) []protocol.Attribute {
	var out []protocol.Attribute
	if v, ok := attributeValue(e, "minecraft:movement_speed"); ok {
		out = append(out, protocol.Attribute{AttributeValue: protocol.AttributeValue{Name: "minecraft:movement", Value: float32(v), Max: 3.4e38}, DefaultMax: 3.4e38, Default: 0.1})
	}
	if v, ok := attributeValue(e, "minecraft:jump_strength"); ok {
		out = append(out, protocol.Attribute{AttributeValue: protocol.AttributeValue{Name: "minecraft:horse.jump_strength", Value: float32(v), Max: 2}, DefaultMax: 2, Default: 0.7})
	}
	return out
}
