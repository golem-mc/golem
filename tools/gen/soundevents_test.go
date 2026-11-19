package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSoundIDsReserveZeroAndFollowRegistryOrder(t *testing.T) {
	root, ref := fixture(t, `{"minecraft:sound_event":{"entries":{
 "minecraft:entity.pig.hurt":{"protocol_id":1},"minecraft:block.stone.break":{"protocol_id":0}}}}`)
	if code, _, stderr := runGen(root, ref, "soundevents"); code != 0 {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	got, err := os.ReadFile(filepath.Join(root, "registry", "soundevents_gen.go"))
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Join(strings.Fields(string(got)), " ")
	for _, want := range []string{"SoundNone Sound = 0", "SoundBlockStoneBreak Sound = 1", "SoundEntityPigHurt Sound = 2"} {
		if !strings.Contains(text, want) {
			t.Errorf("generated IDs lack %q", want)
		}
	}
}

func TestSoundIDsRefuseNamingCollisions(t *testing.T) {
	for _, report := range []string{
		`{"minecraft:sound_event":{"entries":{"minecraft:block.a_b":{"protocol_id":0},"minecraft:block.a.b":{"protocol_id":1}}}}`,
		`{"minecraft:sound_event":{"entries":{"minecraft:none":{"protocol_id":0}}}}`,
	} {
		root, ref := fixture(t, report)
		if code, _, _ := runGen(root, ref, "soundevents"); code == 0 {
			t.Fatal("ambiguous constants were generated")
		}
		if _, err := os.Stat(filepath.Join(root, "registry", "soundevents_gen.go")); !os.IsNotExist(err) {
			t.Fatal("failed generation wrote a file")
		}
	}
}
