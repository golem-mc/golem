package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// batTestType is the entity type ID of the bat.
func batTestType(t *testing.T) int32 {
	t.Helper()
	id, ok := registry.EntityTypeID("minecraft:bat")
	if !ok {
		t.Fatal("the registry has no bat")
	}
	return id
}

// TestABatIsToldHangingAndWakesForAPlayerWithinFourBlocks hangs a bat under a
// stone block, checks its spawn carries the resting bit of its flags byte
// (the byte at index 16, which the harness reads as the golem flags), keeps
// the player out of its four blocks while it stays asleep, then walks the
// player in and expects the cleared bit and the takeoff level event.
// vanilla: Bat.customServerAiStep
func TestABatIsToldHangingAndWakesForAPlayerWithinFourBlocks(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.MobTestConfig(), "Spelunker", servertest.OpsListing(t, "Spelunker"))

	stone, ok := registry.FindState("minecraft:stone", nil)
	if !ok {
		t.Fatal("the block registry has no stone")
	}
	srv.SetBlock(6, 66, 0, stone)
	id := srv.SpawnMob("minecraft:bat", 6.5, 65.1, 0.5)
	bat := c.ExpectMobSpawn(batTestType(t))
	if bat.EntityID != id || !bat.HasGolemFlags || bat.GolemFlags&protocol.EntityFlagClimbing == 0 {
		t.Fatalf("spawned bat = %+v, want the resting bit set", bat)
	}

	woke := pumpTicks(c, srv, 40, batWorldTraffic, func(pid int32, data []byte) bool {
		return pid == protocol.IDPlayEntityMetadata && protocol.NewReader(data).ReadVarInt() == id
	})
	if woke {
		t.Fatal("the bat changed its flags with the player six blocks away")
	}

	c.ChatCommand("tp @s 4.5 65 0.5")
	cleared, took := false, false
	pumpTicks(c, srv, 40, batWorldTraffic, func(pid int32, data []byte) bool {
		switch pid {
		case protocol.IDPlayEntityMetadata:
			if protocol.NewReader(data).ReadVarInt() == id {
				m := servertest.ReadMonsterMetadata(c, data)
				cleared = cleared || (m.HasClimb && !m.Climbing)
			}
		case protocol.IDPlayWorldEvent:
			r := protocol.NewReader(data)
			took = took || r.ReadI32() == 1025
		}
		return cleared && took
	})
	if !cleared || !took {
		t.Errorf("after the player came within four blocks: resting bit cleared %v, takeoff event %v", cleared, took)
	}
}

// batWorldTraffic is the world traffic a bat test steps over, the movement
// packets and world events kept.
func batWorldTraffic(id int32) bool {
	switch id {
	case protocol.IDPlayUpdateTime, protocol.IDPlayUpdateLight, protocol.IDPlayBlockUpdate,
		protocol.IDPlayChunkData, protocol.IDPlayChunkBatchStart, protocol.IDPlayChunkBatchFinished,
		protocol.IDPlayCenterChunk, protocol.IDPlayUnloadChunk, protocol.IDPlaySetEntityMotion,
		protocol.IDPlayEntityPosition, protocol.IDPlayEntityPositionRot, protocol.IDPlayEntityRotation,
		protocol.IDPlayHeadRotation, protocol.IDPlayTeleportEntity,
		protocol.IDPlayPingResponse:
		return true
	}
	return false
}
