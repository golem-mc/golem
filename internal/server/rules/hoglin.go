package rules

import "math"

// HoglinBlowDamage is the damage of a grown hoglin's or zoglin's blow: half
// its attack plus the roll, a number below the attack's integer part.
// vanilla: HoglinBase.hurtAndThrowTarget
func HoglinBlowDamage(attack float64, roll int) float64 {
	return attack/2 + float64(roll)
}

// HoglinThrow is the velocity a hoglin's blow adds to a target standing
// (dx, dz) from it with the attacker's attack_knockback and the target's
// knockback resistance given: the horizontal push
// away from the hoglin, turned by the angle (radians, as vanilla passes it)
// and scaled by h, and the vertical push scaled by v. ok is false when the
// resistance cancels the throw.
// vanilla: HoglinBase.throwTarget
func HoglinThrow(knockback, resistance, dx, dz, angle, h, v float64) (vx, vy, vz float64, ok bool) {
	power := knockback - resistance
	if power <= 0 {
		return 0, 0, 0, false
	}
	length := math.Hypot(dx, dz)
	if length < 1e-5 {
		dx, dz, length = 0, 0, 1
	}
	px, pz := dx/length*power*h, dz/length*power*h
	cos, sin := math.Cos(angle), math.Sin(angle)
	return px*cos + pz*sin, power * v, pz*cos - px*sin, true
}
