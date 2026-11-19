package entity

import "testing"

// vanilla: AbstractWindCharge.makeBoundingBox, Projectile.onAboveBubbleColumn
func TestWindChargeOffsetTouchesBubbleSurface(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		kind ThrownKind
	}{
		{"wind charge", ThrownWindCharge},
		{"breeze wind charge", ThrownBreezeWindCharge},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w := newTestWorld()
			w.set(0, 63, 0, bubbleColumnState(t, false))
			charge := NewThrown(1, tc.kind, 0.5, 64.05, 0.5, 0, 0, 0, 0, 1)
			events := charge.Step(w, nil, nil)
			if !closeEnough(charge.VY, 0.1) {
				t.Errorf("surface lift = %v, want 0.1", charge.VY)
			}
			if len(events) != 1 || events[0].Kind != ThrownBubbleSurface ||
				events[0].X != 0 || events[0].Y != 63 || events[0].Z != 0 {
				t.Errorf("surface events = %#v, want one at (0, 63, 0)", events)
			}
		})
	}
}
