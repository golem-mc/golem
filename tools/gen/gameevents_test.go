package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGameEventIDsReserveZeroAndFollowRegistryOrder(t *testing.T) {
	root, ref := fixture(t, `{"minecraft:game_event":{"entries":{
 "minecraft:step":{"protocol_id":1},"minecraft:block_activate":{"protocol_id":0}}}}`)
	if code, _, stderr := runGen(root, ref, "gameevents"); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	got, err := os.ReadFile(filepath.Join(root, "registry", "gameevents_gen.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"type GameEvent uint8", "GameEventNone GameEvent = 0", "const GameEventCount = 3",
		"GameEventBlockActivate GameEvent = 1", "GameEventStep          GameEvent = 2",
		"\"\",\n\t\"minecraft:block_activate\",\n\t\"minecraft:step\",",
	} {
		if !strings.Contains(string(got), want) {
			t.Errorf("generated IDs lack %q:\n%s", want, got)
		}
	}
}
