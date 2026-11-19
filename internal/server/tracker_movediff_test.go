package server

import "testing"

func TestMoveDiffRotationOnlyFollowsTheVehicle(t *testing.T) {
	was := trackedPosition{x: 10, y: 20, z: 30, yaw: 5, onGround: true}
	now := trackedPosition{x: 4000, y: 20, z: -900, yaw: 5, onGround: false}

	told, _, ok := moveDiff(1, was, now, bodyPose{}, moveTick{rotationOnly: true})
	if ok {
		t.Fatal("an unturned passenger is owed no packet")
	}
	if told.x != now.x || told.y != now.y || told.z != now.z || told.onGround != was.onGround {
		t.Fatalf("baseline = %+v, want position of %+v and onGround of %+v", told, now, was)
	}

	now.yaw = 9
	told, _, ok = moveDiff(1, was, now, bodyPose{}, moveTick{rotationOnly: true})
	if !ok || told.x != now.x || told.z != now.z || told.yaw != 9 || told.onGround != was.onGround {
		t.Fatalf("turned: ok=%v baseline=%+v", ok, told)
	}
}
