package datapack

import (
	"testing"
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
