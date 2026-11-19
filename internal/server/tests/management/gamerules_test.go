package management

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

func ruleParam(key string, value any) []any {
	return []any{map[string]any{"key": key, "value": value}}
}

// vanilla: GameRulesService, MinecraftGameRuleServiceImpl.updateGameRule,
// JsonRpcNotificationService.onGameRuleChanged
func TestGameRulesMethods(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.ManagementConfig(offline()))
	c := servertest.ManagementClientFor(t, srv)

	var rules []json.RawMessage
	if err := json.Unmarshal(result(t, c, "gamerules", nil), &rules); err != nil {
		t.Fatal(err)
	}
	if len(rules) != 58 {
		t.Errorf("gamerules returned %d rules, want 58", len(rules))
	}
	all := make([]string, len(rules))
	for i, r := range rules {
		all[i] = compact(t, r)
	}
	joined := strings.Join(all, "\n")
	for _, want := range []string{
		`{"key":"minecraft:keep_inventory","type":"boolean","value":false}`,
		`{"key":"minecraft:random_tick_speed","type":"integer","value":3}`,
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("gamerules lacks %s", want)
		}
	}
	if strings.Contains(joined, "max_minecart_speed") {
		t.Error("gamerules offers the feature-gated max_minecart_speed")
	}

	const keepTrue = `{"key":"minecraft:keep_inventory","type":"boolean","value":true}`
	res, updated := callNotified(t, c, "minecraft:gamerules/update", ruleParam("keep_inventory", true), "gamerules/updated")
	sameJSON(t, "update", res, keepTrue)
	if len(updated) != 1 {
		t.Fatalf("update sent %d gamerules/updated, want 1", len(updated))
	}
	sameJSON(t, "updated", updated[0], `[`+keepTrue+`]`)
	if !srv.GameRule("keep_inventory") {
		t.Error("keep_inventory is false after the update")
	}

	errorData(t, c, "gamerules/update", ruleParam("random_tick_speed", -1), -32602,
		"Value -1 outside of range [0:2147483647]")
	errorData(t, c, "gamerules/update", ruleParam("nope", 1), -32602,
		"Unknown registry key in ResourceKey[minecraft:root / minecraft:game_rule]: minecraft:nope")
	errorData(t, c, "gamerules/update", ruleParam("keep_inventory", 1), -32602, "Not a boolean: 1")
	errorData(t, c, "gamerules/update", []any{map[string]any{"key": "keep_inventory"}}, -32602,
		`No key value in MapLike[{"key":"keep_inventory"}]`)

	srv.RunLine("gamerule keep_inventory false")
	sameJSON(t, "command updated", c.ExpectNotification("minecraft:notification/gamerules/updated"),
		`[{"key":"minecraft:keep_inventory","type":"boolean","value":false}]`)

	alice := connect()
	alice.JoinAs("Alice")
	sameJSON(t, "immediate_respawn", result(t, c, "gamerules/update", ruleParam("immediate_respawn", true)),
		`{"key":"minecraft:immediate_respawn","type":"boolean","value":true}`)
	if ev := alice.ExpectWithinTicks(srv, 20, protocol.IDPlayGameEvent); ev[0] != protocol.GameEventImmediateRespawn {
		t.Errorf("game event %d, want immediate respawn", ev[0])
	}
}

func compact(t *testing.T, raw json.RawMessage) string {
	t.Helper()
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(v)
	return string(b)
}
