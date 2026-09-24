package gw

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/sandertv/gophertunnel/minecraft/protocol/login"
)

// javaIdentity is who a Bedrock player is to the world and to everyone on
// Java, in Floodgate's scheme: the UUID is the Xbox account id (XUID) in the
// low 64 bits (UUID(0, xuid)) — stable for the account and never a Java
// account's — and the name is the gamertag behind a "." (spaces as "_",
// sixteen characters at most, as a Java name must be), so it cannot collide
// with a Java player's. Without Xbox authentication (a dev gateway) there is
// no XUID: the client's own identity and name stand.
func javaIdentity(id login.IdentityData) (name, uuid string) {
	x, err := strconv.ParseUint(id.XUID, 10, 64)
	if id.XUID == "" || err != nil {
		return id.DisplayName, id.Identity
	}
	name = "." + strings.ReplaceAll(id.DisplayName, " ", "_")
	if len(name) > 16 {
		name = name[:16]
	}
	return name, xuidUUID(x)
}

// xuidUUID is UUID(0, xuid) in the dashed form.
func xuidUUID(xuid uint64) string {
	return fmt.Sprintf("00000000-0000-0000-%04x-%012x", xuid>>48, xuid&0xffffffffffff)
}
