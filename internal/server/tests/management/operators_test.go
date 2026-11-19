package management

import (
	"strconv"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

func opJSON(name string, level int, bypass bool) string {
	return `{"player":` + playerJSON(name) + `,"permissionLevel":` + strconv.Itoa(level) +
		`,"bypassesPlayerLimit":` + strconv.FormatBool(bypass) + `}`
}

// vanilla: OperatorService, MinecraftOperatorListServiceImpl, PlayerList.op,
// deop, ServerOpList.add, remove, clear
func TestOperatorsMethods(t *testing.T) {
	t.Parallel()
	opsFile := servertest.WriteOpsFile(t)
	srv, connect := servertest.Start(t, servertest.ManagementConfig(offline()), opsFile)
	c := servertest.ManagementClientFor(t, srv)
	alice := connect()
	aliceID := alice.JoinAs("Alice")

	res, added := callNotified(t, c, "minecraft:operators/add",
		[]any{[]any{map[string]any{"player": map[string]any{"name": "Alice"}, "permissionLevel": 2}}}, "operators/added")
	sameJSON(t, "add", res, `[`+opJSON("Alice", 2, false)+`]`)
	if len(added) != 1 {
		t.Fatalf("add sent %d operators/added, want 1", len(added))
	}
	sameJSON(t, "added", added[0], `[`+opJSON("Alice", 2, false)+`]`)
	alice.ExpectPlay(protocol.IDPlayCommands)
	alice.ExpectEntityEvent(aliceID, protocol.EntityStatusOpLevelBase+2)

	res, removed := callNotified(t, c, "minecraft:operators/remove", []any{[]any{map[string]any{"name": "Alice"}}}, "operators/removed")
	sameJSON(t, "remove", res, `[]`)
	if len(removed) != 1 {
		t.Fatalf("remove sent %d operators/removed, want 1", len(removed))
	}
	sameJSON(t, "removed", removed[0], `[`+opJSON("Alice", 2, false)+`]`)
	alice.ExpectPlay(protocol.IDPlayCommands)
	alice.ExpectEntityEvent(aliceID, protocol.EntityStatusOpLevelBase+0)

	srv.RunLine("op Bob")
	sameJSON(t, "command added", c.ExpectNotification("minecraft:notification/operators/added"), `[`+opJSON("Bob", 4, false)+`]`)

	sameJSON(t, "clamped", result(t, c, "operators/add", []any{[]any{map[string]any{
		"player": map[string]any{"name": "Carol"}, "permissionLevel": 9, "bypassesPlayerLimit": true}}}),
		`[`+opJSON("Bob", 4, false)+`,`+opJSON("Carol", 4, true)+`]`)

	// set deops before it ops, so a missing bypass flag is false, not Carol's current one.
	sameJSON(t, "set", result(t, c, "operators/set", []any{[]any{map[string]any{
		"player": map[string]any{"name": "Carol"}}}}), `[`+opJSON("Carol", 4, false)+`]`)
	if got := servertest.ReadListFile[servertest.OpRecord](t, srv.OpsFile()); len(got) != 1 || got[0].Name != "Carol" ||
		got[0].Level != 4 || got[0].BypassesPlayerLimit {
		t.Errorf("ops.json after set = %+v, want only Carol at level 4 without bypass", got)
	}

	res, removed = callNotified(t, c, "minecraft:operators/clear", nil, "operators/removed")
	sameJSON(t, "clear", res, `[]`)
	if len(removed) != 1 {
		t.Errorf("clear sent %d operators/removed, want 1", len(removed))
	}
	if got := servertest.ReadListFile[servertest.OpRecord](t, srv.OpsFile()); len(got) != 0 {
		t.Errorf("ops.json = %+v, want []", got)
	}
}
