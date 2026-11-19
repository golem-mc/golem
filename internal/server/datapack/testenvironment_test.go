package datapack

import (
	"reflect"
	"testing"

	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// testEnvs knows one entry, pk:rain, and two rules: a boolean and an
// integer from 0 to 10.
func testEnvs(t *testing.T) TestEnvironments {
	rain, err := JSONTag([]byte(`{"type":"weather","weather":"rain"}`))
	if err != nil {
		t.Fatal(err)
	}
	return TestEnvironments{
		Entry: func(id string) (nbt.Tag, bool) { return rain, id == "pk:rain" },
		Rule: func(id string) (bool, int32, int32, bool) {
			switch id {
			case "flag":
				return true, 0, 0, true
			case "count":
				return false, 0, 10, true
			}
			return false, 0, 0, false
		},
	}
}

// TestDecodeTestEnvironment reads each definition type and nested holders,
// and refuses what vanilla's codec refuses.
// vanilla: TestEnvironmentDefinition.CODEC
func TestDecodeTestEnvironment(t *testing.T) {
	envs := testEnvs(t)
	day, _ := registry.EnvTimelineByName("minecraft:day")
	for _, tc := range []struct {
		json string
		want *TestEnvironment // nil: refused
	}{
		{`"pk:rain"`, &TestEnvironment{Type: "minecraft:weather", Weather: 1}},
		{`"pk:none"`, nil},
		{`{"type":"all_of","definitions":["pk:rain",{"type":"difficulty","difficulty":"easy"}]}`,
			&TestEnvironment{Type: "minecraft:all_of", AllOf: []TestEnvironment{
				{Type: "minecraft:weather", Weather: 1}, {Type: "minecraft:difficulty", Difficulty: 1}}}},
		{`{"type":"all_of"}`, nil},
		{`{"type":"clock_time","clock":"overworld","time":6000}`,
			&TestEnvironment{Type: "minecraft:clock_time", Clock: "minecraft:overworld", Time: 6000}},
		{`{"type":"clock_time","clock":"overworld","time":-1}`, nil},
		{`{"type":"clock_time","clock":"overworld","time":-0.5}`,
			&TestEnvironment{Type: "minecraft:clock_time", Clock: "minecraft:overworld"}},
		{`{"type":"clock_time","clock":"pk:nope","time":1}`, nil},
		{`{"type":"difficulty","difficulty":"brutal"}`, nil},
		{`{"type":"function","teardown":"pk:down"}`, &TestEnvironment{Type: "minecraft:function", Teardown: "pk:down"}},
		{`{"type":"game_rules","rules":{"minecraft:flag":true,"count":7}}`,
			&TestEnvironment{Type: "minecraft:game_rules", Rules: []TestRule{{"count", 7}, {"flag", 1}}}},
		{`{"type":"game_rules","rules":{"count":11}}`, nil},
		{`{"type":"game_rules","rules":{"count":10.5}}`,
			&TestEnvironment{Type: "minecraft:game_rules", Rules: []TestRule{{"count", 10}}}},
		{`{"type":"game_rules","rules":{"flag":256}}`,
			&TestEnvironment{Type: "minecraft:game_rules", Rules: []TestRule{{"flag", 1}}}},
		{`{"type":"game_rules","rules":{"flag":0.5}}`,
			&TestEnvironment{Type: "minecraft:game_rules", Rules: []TestRule{{"flag", 1}}}},
		{`{"type":"game_rules","rules":{"pk:flag":true}}`, nil},
		{`{"type":"game_rules","rules":{"other":true}}`, nil},
		{`{"type":"timeline_attributes","timelines":["day"]}`,
			&TestEnvironment{Type: "minecraft:timeline_attributes", Timelines: []registry.EnvTimeline{day}}},
		{`{"type":"timeline_attributes","timelines":["pk:nope"]}`, nil},
		{`{"type":"weather","weather":"snow"}`, nil},
		{`{"type":"pk:custom"}`, nil},
	} {
		tag, err := JSONTag([]byte(tc.json))
		if err != nil {
			t.Fatal(err)
		}
		got, err := DecodeTestEnvironment(tag, envs)
		switch {
		case tc.want == nil && err == nil:
			t.Errorf("%s decoded to %+v, want refused", tc.json, got)
		case tc.want != nil && err != nil:
			t.Errorf("%s refused: %v", tc.json, err)
		case tc.want != nil && !reflect.DeepEqual(got, *tc.want):
			t.Errorf("%s = %+v, want %+v", tc.json, got, *tc.want)
		}
	}
}

// TestDecodeTestEnvironmentStopsACycle: entries naming each other fail
// rather than recurse forever.
func TestDecodeTestEnvironmentStopsACycle(t *testing.T) {
	loop, err := JSONTag([]byte(`{"type":"all_of","definitions":["pk:loop"]}`))
	if err != nil {
		t.Fatal(err)
	}
	envs := testEnvs(t)
	envs.Entry = func(string) (nbt.Tag, bool) { return loop, true }
	if _, err := DecodeTestEnvironment(nbt.String("pk:loop"), envs); err == nil {
		t.Error("a cycle decoded")
	}
}
