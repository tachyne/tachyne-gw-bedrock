package gw

import (
	"strconv"
	"testing"

	"github.com/sandertv/gophertunnel/minecraft/protocol/login"
)

// Floodgate's identity: UUID(0, xuid) and a "."-prefixed name.
func TestJavaIdentity(t *testing.T) {
	xuid := strconv.FormatUint(0x000901f2a3b4c5d6, 10)
	name, uuid := javaIdentity(login.IdentityData{XUID: xuid, Identity: "ignored", DisplayName: "Edge ZA1951"})
	if name != ".Edge_ZA1951" {
		t.Errorf("name %q", name)
	}
	if uuid != "00000000-0000-0000-0009-01f2a3b4c5d6" {
		t.Errorf("uuid %q", uuid)
	}
	if got := xuidUUID(0x000901f2a3b4c5d6); got != "00000000-0000-0000-0009-01f2a3b4c5d6" {
		t.Errorf("xuidUUID = %s", got)
	}
	if n, _ := javaIdentity(login.IdentityData{XUID: "1", DisplayName: "AVeryLongGamerTag"}); len(n) != 16 {
		t.Errorf("name %q is longer than a Java name may be", n)
	}
	// No Xbox sign-in (dev): the client's own identity.
	if n, u := javaIdentity(login.IdentityData{Identity: "11111111-2222-3333-4444-555555555555", DisplayName: "dev"}); n != "dev" || u != "11111111-2222-3333-4444-555555555555" {
		t.Errorf("unauthenticated identity %q %q", n, u)
	}
}
