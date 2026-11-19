package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// goatPoses reads the pose a goat's Set Entity Metadata carries.
func goatPoses(c *servertest.Client, goat int32, data []byte) (pose int32, found bool) {
	r := protocol.NewReader(data)
	if r.ReadVarInt() != goat {
		return 0, false
	}
	for r.Err() == nil {
		index := r.ReadU8()
		if index == protocol.MetadataEnd {
			break
		}
		fieldType := r.ReadVarInt()
		if index == protocol.MetadataIndexPose {
			pose, found = r.ReadVarInt(), true
			continue
		}
		servertest.SkipMetadataField(c, r, fieldType)
	}
	return pose, found
}

// TestAGoatLongJumpsAcrossAGap: on a stone platform in the sky with another
// three blocks off, a goat whose long jump cooldown is gone stands, crosses
// and lands on the far platform. The leap sounds long_jump, draws the goat in
// the long jumping pose until it lands, and the landing sounds step, after
// which the cooldown is 600 to 1200 ticks.
// vanilla: LongJumpToRandomPos, LongJumpMidJump
func TestAGoatLongJumpsAcrossAGap(t *testing.T) {
	t.Parallel()
	srv, c := servertest.BreedingPlayer(t, "survival", goatOp, "", 0, servertest.OpsListing(t, goatOp))
	servertest.HoldTicks(t, srv)
	c.ChatCommand("fill 0 99 0 2 99 2 minecraft:stone")
	c.ChatCommand("fill 6 99 0 8 99 2 minecraft:stone")
	c.ChatCommand("tp @s 0.5 100 0.5")
	c.ReadTicks(srv, 2, func(int32, []byte) bool { return false })
	id := srv.SpawnMobSeeded("minecraft:goat", 1.5, 100, 1.5, 3)
	c.ExpectMobSpawn(protocol.EntityTypeGoat)
	srv.SetGoatRamCooldown(id, 100000)
	c.ReadTicks(srv, 3, func(int32, []byte) bool { return false })
	srv.SetGoatLongJumpCooldown(id, 0)

	var w goatWatch
	poses := map[int32]bool{}
	landed := false
	c.ReadTicks(srv, 400, func(packetID int32, data []byte) bool {
		readGoat(t, c, id, &w, data, packetID)
		if packetID == protocol.IDPlayEntityMetadata {
			if pose, ok := goatPoses(c, id, data); ok {
				poses[pose] = true
				landed = landed || (pose == protocol.PoseStanding && poses[protocol.PoseLongJumping])
			}
		}
		return landed
	})
	if w.sounds["minecraft:entity.goat.long_jump"] == 0 {
		t.Error("no long_jump sound")
	}
	if !poses[protocol.PoseLongJumping] || !landed {
		t.Fatalf("poses seen %v, want the long jumping pose and then standing", poses)
	}
	c.ReadTicks(srv, 2, func(packetID int32, data []byte) bool {
		readGoat(t, c, id, &w, data, packetID)
		return false
	})
	if w.sounds["minecraft:entity.goat.step"] == 0 {
		t.Error("no step sound at the landing")
	}
	x, y, _, _ := srv.MobPos(id)
	if x < 5.5 || y < 99.9 {
		t.Errorf("the goat stands at x=%.2f y=%.2f, want it on the far platform", x, y)
	}
	if got := srv.GoatLongJumpCooldown(id); got < 590 || got > 1200 {
		t.Errorf("the long jump cooldown after landing = %d, want 600 to 1200", got)
	}
}

// TestAGoatOnACooldownDoesNotLongJump: the cooldown holds the leap back.
func TestAGoatOnACooldownDoesNotLongJump(t *testing.T) {
	t.Parallel()
	srv, c := servertest.BreedingPlayer(t, "survival", goatOp, "", 0, servertest.OpsListing(t, goatOp))
	servertest.HoldTicks(t, srv)
	c.ChatCommand("fill 0 99 0 2 99 2 minecraft:stone")
	c.ChatCommand("fill 6 99 0 8 99 2 minecraft:stone")
	c.ChatCommand("tp @s 0.5 100 0.5")
	c.ReadTicks(srv, 2, func(int32, []byte) bool { return false })
	id := srv.SpawnMobSeeded("minecraft:goat", 1.5, 100, 1.5, 3)
	c.ExpectMobSpawn(protocol.EntityTypeGoat)
	srv.SetGoatRamCooldown(id, 100000)
	c.ReadTicks(srv, 3, func(int32, []byte) bool { return false })
	srv.SetGoatLongJumpCooldown(id, 1000)
	var w goatWatch
	c.ReadTicks(srv, 200, func(packetID int32, data []byte) bool {
		readGoat(t, c, id, &w, data, packetID)
		return false
	})
	if w.sounds["minecraft:entity.goat.long_jump"] != 0 {
		t.Error("a goat on cooldown long jumped")
	}
}
