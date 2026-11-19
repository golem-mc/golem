package rules

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
)

// vanillaOrderPlayerPose is PlayerPose in vanilla's order, swimming box first.
// vanilla: Player.updatePlayerPose
func vanillaOrderPlayerPose(desired Pose, free bool, fit func(Pose) bool) (Pose, bool) {
	if !fit(protocol.PoseSwimming) {
		return desired, false
	}
	if free || fit(desired) {
		return desired, true
	}
	if fit(protocol.PoseSneaking) {
		return protocol.PoseSneaking, true
	}
	return protocol.PoseSwimming, true
}

// TestPlayerPoseMatchesVanillaOrder checks that trying the desired box first
// gives vanilla's pose for every desired pose and every fit answer a world can
// give, where a box that contains the swimming box fits only if it does.
func TestPlayerPoseMatchesVanillaOrder(t *testing.T) {
	desiredPoses := []Pose{
		protocol.PoseStanding, protocol.PoseSleeping, protocol.PoseFallFlying,
		protocol.PoseSwimming, protocol.PoseSpinAttack, protocol.PoseSneaking,
		protocol.PoseDying, // no entry in the table: the standing shape
	}
	sw := PlayerDimensionsOf(protocol.PoseSwimming)
	for _, desired := range desiredPoses {
		d := PlayerDimensionsOf(desired)
		covers := d.Width >= sw.Width && d.Height >= sw.Height
		for mask := range 8 {
			swimFits, desiredFits, crouchFits := mask&1 != 0, mask&2 != 0, mask&4 != 0
			switch desired {
			case protocol.PoseSwimming:
				desiredFits = swimFits
			case protocol.PoseSneaking:
				desiredFits = crouchFits
			}
			if covers && desiredFits && !swimFits {
				continue // the desired box contains the swimming box
			}
			fit := func(p Pose) bool {
				switch p {
				case protocol.PoseSwimming:
					return swimFits
				case protocol.PoseSneaking:
					return crouchFits
				case desired:
					return desiredFits
				}
				t.Fatalf("desired %d: fit asked of pose %d", desired, p)
				return false
			}
			for _, free := range []bool{false, true} {
				wantPose, wantOK := vanillaOrderPlayerPose(desired, free, fit)
				gotPose, gotOK := PlayerPose(desired, free, fit)
				if gotPose != wantPose || gotOK != wantOK {
					t.Errorf("desired %d, free %v, swim %v desired %v crouch %v: got (%d, %v), want (%d, %v)",
						desired, free, swimFits, desiredFits, crouchFits, gotPose, gotOK, wantPose, wantOK)
				}
			}
		}
	}
}

// TestPlayerPoseStandingFreeFitsOnce checks that a standing body in open
// space costs one fit.
func TestPlayerPoseStandingFreeFitsOnce(t *testing.T) {
	calls := 0
	pose, ok := PlayerPose(protocol.PoseStanding, false, func(Pose) bool { calls++; return true })
	if pose != protocol.PoseStanding || !ok || calls != 1 {
		t.Fatalf("got (%d, %v) after %d fits, want (%d, true) after 1", pose, ok, calls, protocol.PoseStanding)
	}
}

func TestPlayerPoseFreeOnlyChecksSwimming(t *testing.T) {
	for _, swimFits := range []bool{false, true} {
		calls := 0
		pose, ok := PlayerPose(protocol.PoseStanding, true, func(p Pose) bool {
			calls++
			if p != protocol.PoseSwimming {
				t.Fatalf("free body checked pose %d, want only swimming", p)
			}
			return swimFits
		})
		if pose != protocol.PoseStanding || ok != swimFits || calls != 1 {
			t.Fatalf("swimming fits %v: got (%d, %v) after %d fits, want (%d, %v) after 1",
				swimFits, pose, ok, calls, protocol.PoseStanding, swimFits)
		}
	}
}
