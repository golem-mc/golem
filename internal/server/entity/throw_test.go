package entity

import (
	"math"
	"math/rand"
	"testing"
)

// The throw's maths as ordinary unit tests: the direction a yaw and a
// pitch ask for, and the velocity a thrown item leaves the thrower with,
// rise and spread included. Nothing here steps an item; these are the
// checks of the geometry the spawn and the throw share.

// TestTheLookVectorOfAFewYawAndPitchValues checks the direction a player
// is looking in for the readings that tell the geometry apart: south at
// yaw zero, west at ninety, the verticals of a pitch at either pole, and
// a body turned and tilted together.
func TestTheLookVectorOfAFewYawAndPitchValues(t *testing.T) {
	tests := []struct {
		yaw, pitch float32
		x, y, z    float64
	}{
		{0, 0, 0, 0, 1},    // south, +Z
		{90, 0, -1, 0, 0},  // west, -X
		{180, 0, 0, 0, -1}, // north, -Z
		{-90, 0, 1, 0, 0},  // east, +X
		{0, -90, 0, 1, 0},  // looking straight up
		{0, 90, 0, -1, 0},  // looking straight down
		// West of south and tilted up: half the look in the rise, the
		// other half split between -X and +Z.
		{45, -30, -math.Sqrt(6) / 4, 0.5, math.Sqrt(6) / 4},
	}
	for _, test := range tests {
		x, y, z := lookVector(test.yaw, test.pitch)
		if !closeEnough(x, test.x) || !closeEnough(y, test.y) || !closeEnough(z, test.z) {
			t.Errorf("lookVector(%v, %v) = (%v, %v, %v), want (%v, %v, %v)",
				test.yaw, test.pitch, x, y, z, test.x, test.y, test.z)
		}
		if length := math.Sqrt(x*x + y*y + z*z); !closeEnough(length, 1) {
			t.Errorf("lookVector(%v, %v) has length %v, want a unit vector",
				test.yaw, test.pitch, length)
		}
	}
}

// TestThrowVelocityIsTheVanillaDrop checks the velocity a thrown item
// leaves the thrower with, draw for draw against a seeded source: the
// look direction at the throw strength, raised by the tenth of a block
// every drop climbs on its own, and spread by up to two hundredths of a
// block in a random direction and a triangle tenth in the rise. The
// constants and the draw order are spelled out here, so the old formula
// without them fails against the same seed.
// vanilla: LivingEntity.createItemStackToDrop
func TestThrowVelocityIsTheVanillaDrop(t *testing.T) {
	tests := []struct {
		name       string
		yaw, pitch float32
	}{
		// South and level: 0.3 along +Z under the rise and spread.
		{"south and level", 0, 0},
		// West and level: the 0.3 runs along -X.
		{"west and level", 90, 0},
		// Straight up: no look left, the climb plus the rise.
		{"straight up", 0, -90},
		// Straight down: the fall against the rise.
		{"straight down", 180, 90},
	}
	for _, test := range tests {
		// The draws a vanilla drop spends, in its order: the sideways
		// scatter's direction, its size, then the rise's triangle pair.
		r := rand.New(rand.NewSource(1))
		dir := r.Float64() * 2 * math.Pi
		size := 0.02 * r.Float64()
		rise := (r.Float64() - r.Float64()) * 0.1
		radYaw, radPitch := float64(test.yaw)*math.Pi/180, float64(test.pitch)*math.Pi/180
		sinPitch, cosPitch := math.Sincos(radPitch)
		sinYaw, cosYaw := math.Sincos(radYaw)
		wantX := -sinYaw*cosPitch*0.3 + math.Cos(dir)*size
		wantY := -sinPitch*0.3 + 0.1 + rise
		wantZ := cosYaw*cosPitch*0.3 + math.Sin(dir)*size

		vx, vy, vz := ThrowVelocity(test.yaw, test.pitch, rand.New(rand.NewSource(1)))
		if !closeEnough(vx, wantX) || !closeEnough(vy, wantY) || !closeEnough(vz, wantZ) {
			t.Errorf("%s: ThrowVelocity = (%v, %v, %v), want (%v, %v, %v)",
				test.name, vx, vy, vz, wantX, wantY, wantZ)
		}
	}
}

// TestThrowVelocityRollsItsSpread checks that the spread is rolled, not
// fixed: two throws of one look apart in their source land apart in their
// rise, as the triangle draw (r - r) varies.
func TestThrowVelocityRollsItsSpread(t *testing.T) {
	firstX, firstY, firstZ := ThrowVelocity(0, 0, rand.New(rand.NewSource(1)))
	sameX, sameY, sameZ := ThrowVelocity(0, 0, rand.New(rand.NewSource(1)))
	if firstX != sameX || firstY != sameY || firstZ != sameZ {
		t.Errorf("the same source rolled (%v, %v, %v) and (%v, %v, %v), want the same throw twice",
			firstX, firstY, firstZ, sameX, sameY, sameZ)
	}

	distinct := map[[3]float64]bool{}
	for seed := range int64(20) {
		x, y, z := ThrowVelocity(0, 0, rand.New(rand.NewSource(seed)))
		distinct[[3]float64{x, y, z}] = true
	}
	if len(distinct) < 2 {
		t.Errorf("twenty sources gave %v, want the spread to vary the throw", distinct)
	}
}

// TestDropVelocityIsTheVanillaToss checks the toss a randomly scattered
// drop leaves with, draw for draw against a seeded source: the speed drawn
// before the heading, up to half a block a tick along the heading, and the
// fifth of a block a random drop rises with. The constants and the draw
// order are spelled out here, so a formula with the heading first fails
// against the same seed.
// vanilla: LivingEntity.createItemStackToDrop
func TestDropVelocityIsTheVanillaToss(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	speed := r.Float64() * 0.5
	heading := r.Float64() * 2 * math.Pi
	wantX, wantY, wantZ := -math.Sin(heading)*speed, 0.2, math.Cos(heading)*speed

	vx, vy, vz := DropVelocity(rand.New(rand.NewSource(1)))
	if !closeEnough(vx, wantX) || !closeEnough(vy, wantY) || !closeEnough(vz, wantZ) {
		t.Errorf("DropVelocity = (%v, %v, %v), want (%v, %v, %v)", vx, vy, vz, wantX, wantY, wantZ)
	}
}
