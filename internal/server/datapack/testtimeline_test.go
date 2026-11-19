package datapack

import (
	"testing"

	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

func TestDecodeTestTimeline(t *testing.T) {
	for _, tc := range []struct {
		name, json string
		wantErr    bool
	}{
		{"empty", `{"clock":"overworld"}`, false},
		{"no clock", `{}`, true},
		{"unknown clock", `{"clock":"pk:missing"}`, true},
		{"zero period", `{"clock":"overworld","period_ticks":0}`, true},
		{"unknown attribute", `{"clock":"overworld","tracks":{"pk:missing":{}}}`, true},
		{"no frames", `{"clock":"overworld","tracks":{"visual/sun_angle":{"keyframes":[]}}}`, true},
		{"unordered", `{"clock":"overworld","tracks":{"visual/sun_angle":{"keyframes":[{"ticks":2,"value":2},{"ticks":1,"value":1}]}}}`, true},
		{"beyond period", `{"clock":"overworld","period_ticks":2,"tracks":{"visual/sun_angle":{"keyframes":[{"ticks":3,"value":1}]}}}`, true},
		{"at period", `{"clock":"overworld","period_ticks":2,"tracks":{"visual/sun_angle":{"keyframes":[{"ticks":2,"value":1}]}}}`, false},
		{"twice on tick", `{"clock":"overworld","tracks":{"visual/sun_angle":{"keyframes":[{"ticks":1,"value":1},{"ticks":1,"value":2}]}}}`, false},
		{"three on tick", `{"clock":"overworld","tracks":{"visual/sun_angle":{"keyframes":[{"ticks":1,"value":1},{"ticks":1,"value":2},{"ticks":1,"value":3}]}}}`, true},
		{"unknown modifier", `{"clock":"overworld","tracks":{"visual/sun_angle":{"modifier":"nope","keyframes":[{"ticks":0,"value":1}]}}}`, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tag, err := JSONTag([]byte(tc.json))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := decodeTestTimeline(tag); (err != nil) != tc.wantErr {
				t.Fatalf("decode = %v, want error %v", err, tc.wantErr)
			}
		})
	}
}

func TestTestTimelineFloatModifiersAndEasing(t *testing.T) {
	for _, tc := range []struct {
		modifier, value string
		want            float32
	}{
		{"override", "2", 2}, {"add", "2", 12}, {"subtract", "2", 8},
		{"multiply", "2", 20}, {"minimum", "2", 2}, {"maximum", "2", 10},
		{"alpha_blend", `{"value":2,"alpha":0.25}`, 8}, {"alpha_blend", "2", 2},
	} {
		t.Run(tc.modifier+tc.value, func(t *testing.T) {
			tag, err := JSONTag([]byte(`{"clock":"overworld","tracks":{"visual/sun_angle":{"modifier":"` + tc.modifier +
				`","keyframes":[{"ticks":0,"value":` + tc.value + `}]}}}`))
			if err != nil {
				t.Fatal(err)
			}
			tl, err := decodeTestTimeline(tag)
			if err != nil {
				t.Fatal(err)
			}
			if got := tl.Tracks["minecraft:visual/sun_angle"].Sample(0, tl.Period).Apply(10); got != tc.want {
				t.Errorf("modifier = %v, want %v", got, tc.want)
			}
		})
	}
	for _, tc := range []struct {
		ease string
		want float32
	}{
		{`"linear"`, 5}, {`"constant"`, 0}, {`{"cubic_bezier":[0,0,1,1]}`, 5},
	} {
		tag, err := JSONTag([]byte(`{"clock":"overworld","period_ticks":20,"tracks":{"visual/sun_angle":{"ease":` + tc.ease +
			`,"keyframes":[{"ticks":0,"value":0},{"ticks":10,"value":10}]}}}`))
		if err != nil {
			t.Fatal(err)
		}
		tl, err := decodeTestTimeline(tag)
		if err != nil {
			t.Fatal(err)
		}
		if got := tl.Tracks["minecraft:visual/sun_angle"].Sample(25, tl.Period).Value; got != tc.want {
			t.Errorf("ease %s = %v, want %v", tc.ease, got, tc.want)
		}
	}
	// A pack's override takes precedence over the generated built-in tracks.
	t.Cleanup(func() { registry.ApplyPackRegistries(nil) })
	registry.ApplyPackRegistries(map[string]map[string]nbt.Tag{"minecraft:timeline": {
		"minecraft:day": nbt.Compound{"clock": nbt.String("the_end")},
	}})
	tl, err := decodeTestTimeline(nbt.String("day"))
	if err != nil || tl.Clock != "minecraft:the_end" || len(tl.Tracks) != 0 {
		t.Fatalf("overridden day = %+v, %v", tl, err)
	}
}
