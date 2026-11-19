package entity

import "testing"

func BenchmarkHoglinAndZoglinAmbientVoice(b *testing.B) {
	for _, tc := range []struct {
		name   string
		kind   MobType
		target int32
	}{
		{"idle hoglin", TypeHoglin, 0},
		{"fighting hoglin", TypeHoglin, 1},
		{"idle zoglin", TypeZoglin, 0},
		{"fighting zoglin", TypeZoglin, 1},
	} {
		b.Run(tc.name, func(b *testing.B) {
			m := NewMob(tc.kind, 2, 0.5, 64, 0.5, 5840)
			m.ZombificationImmune = true
			m.TargetID = tc.target
			b.ReportAllocs()
			for b.Loop() {
				m.AmbientVoice()
			}
		})
	}
}
