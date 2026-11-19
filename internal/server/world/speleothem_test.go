package world

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
)

func TestSpeleothemProviders(t *testing.T) {
	for _, tc := range []struct {
		name, doc string
		integer   bool
		want      float32
	}{
		{"constant", `{"value":0.25}`, false, .25},
		{"normal lower clamp", `{"value":{"type":"clamped_normal","mean":-20,"deviation":0,"min":0.2,"max":0.8}}`, false, .2},
		{"normal upper clamp", `{"value":{"type":"clamped_normal","mean":20,"deviation":0,"min":0.2,"max":0.8}}`, false, .8},
		{"normal interior", `{"value":{"type":"clamped_normal","mean":0.45,"deviation":0,"min":0.2,"max":0.8}}`, false, .45},
		{"integer truncates", `{"value":{"type":"clamped_normal","mean":-1.75,"deviation":0,"min_inclusive":-3,"max_inclusive":3}}`, true, -1},
		{"integer lower clamp", `{"value":{"type":"clamped_normal","mean":-20,"deviation":0,"min_inclusive":-3,"max_inclusive":3}}`, true, -3},
		{"integer upper clamp", `{"value":{"type":"clamped_normal","mean":20,"deviation":0,"min_inclusive":-3,"max_inclusive":3}}`, true, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rng, reference := levelgen.NewWorldgenRandom(42), levelgen.NewWorldgenRandom(42)
			v := parseJSON(tc.doc)["value"]
			var got float32
			if tc.integer {
				got = float32(compileInt(v)(rng))
			} else {
				got = compileFloat(v)(rng)
			}
			if got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			if tc.name != "constant" {
				reference.NextGaussian()
			}
			if rng.NextLong() != reference.NextLong() {
				t.Fatal("provider consumed the wrong random draws")
			}
		})
	}
}
