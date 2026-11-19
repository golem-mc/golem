package server

import (
	"bytes"
	"testing"

	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/world"
)

func TestTrackedBodyReadsAggregateAndDynamicRangeOncePerPass(t *testing.T) {
	ent, tick, in := mobMetadataTracking(entity.TypeCow)
	k := mobTracker
	reads, ranges, passengers := 0, 0, 0
	aggregate := k.readBody
	k.readBody = func(ent *mobEntity) (*bodyTrack[mobShown], *world.World, int32, bodyPose) {
		reads++
		return aggregate(ent)
	}
	k.track = func(*mobEntity) *bodyTrack[mobShown] { t.Fatal("separate track read"); return nil }
	k.world = func(*mobEntity) *world.World { t.Fatal("separate world read"); return nil }
	k.id = func(*mobEntity) int32 { t.Fatal("separate id read"); return 0 }
	k.pose = func(*mobEntity) bodyPose { t.Fatal("separate pose read"); return bodyPose{} }
	k.rangeOf = func(*mobEntity) int { ranges++; return 1 }
	k.passengerRange = func(*mobEntity) int { return passengers }
	c := in.inWorld[tick.w][0]
	body := k.body(ent)
	for range 20 {
		if body.sees(c, 100, 0, 10) {
			t.Fatal("short range saw distant watcher")
		}
	}
	if reads != 1 || ranges != 1 {
		t.Fatalf("aggregate/range reads = %d/%d", reads, ranges)
	}
	passengers = 8
	body = k.body(ent)
	if !body.sees(c, 100, 0, 10) || reads != 2 || ranges != 2 {
		t.Fatal("next pass missed dynamic passenger range")
	}
	k.rangeOf = func(*mobEntity) int { return 0 }
	body = k.body(ent)
	for range 2 {
		if body.sees(c, 0, 0, 10) {
			t.Fatal("disabled kind acquired passenger range")
		}
	}
}

func TestTrackedMovePacketKeepsMoveDiffPacketsAndBaselines(t *testing.T) {
	ent, _, _ := mobMetadataTracking(entity.TypeCow)
	initial := mobTracker.body(ent)
	base := initial.p
	poses := []bodyPose{base, base, base, base, base, base}
	poses[1].x += 1.0 / 4096
	poses[2].x += .5
	poses[3].yaw += 90
	poses[4].onGround = !base.onGround
	poses[5].x += 100
	for _, pose := range poses {
		for flags := range 64 {
			mt := moveTick{gate: true, forced: flags&1 != 0, resync: flags&2 != 0, teleported: flags&4 != 0, rotationOnly: flags&8 != 0, dismounted: flags&16 != 0}
			k := mobTracker
			k.alwaysPosition = flags&32 != 0
			body := k.body(ent)
			body.k, body.p = &k, pose
			was := toldBody[mobShown]{trackedPosition: base.tracked()}
			expectedTick := mt
			expectedTick.forced = expectedTick.forced || k.alwaysPosition
			want, packet, sent := moveDiff(body.id, was.trackedPosition, pose.tracked(), pose, expectedTick)
			got, ok := body.movePacket(&was, mt)
			if ok != sent || got.id != packet.id || !bytes.Equal(got.data, packet.data) || was.trackedPosition != want {
				t.Fatalf("pose %+v flags %d: packet/baseline diverged", pose, flags)
			}
		}
	}
}

func TestMovementScheduleReadyFlagPreservesCadenceAndCallbacks(t *testing.T) {
	for _, interval := range []int{1, 2, 3, 5, 60} {
		var callback, ready movementSchedule
		for tick := range 1500 {
			synced := tick%7 == 0
			calls := 0
			want := callback.step(interval, func() bool { calls++; return synced })
			got := ready.stepDue(interval, synced)
			if got != want || ready != callback {
				t.Fatalf("interval %d tick %d: cadence diverged", interval, tick)
			}
			expectedCalls := 0
			if tick%interval != 0 {
				expectedCalls = 1
			}
			if calls != expectedCalls {
				t.Fatal("callback ran on a scheduled tick")
			}
		}
	}
}
