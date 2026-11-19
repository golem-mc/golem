package redstone

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

func TestFourthShriekSummonsAnEmergingWarden(t *testing.T) {
	t.Parallel()
	srv, c := shriekerSceneWithWarning(t, "true", 3)
	srv.SetDifficulty(2)
	wardenType, ok := registry.EntityTypeID("minecraft:warden")
	if !ok {
		t.Fatal("no warden registry entry")
	}
	var spawned servertest.CapturedEntity
	pose := int32(protocol.PoseStanding)
	placeStoneAt(c, 2)
	c.ReadTicks(srv, 100, func(id int32, data []byte) bool {
		r := protocol.NewReader(data)
		switch id {
		case protocol.IDPlaySpawnEntity:
			entityID := r.ReadVarInt()
			identity := r.ReadUUID()
			if r.ReadVarInt() != wardenType {
				return false
			}
			if spawned.EntityID != 0 {
				t.Fatal("a shriek summoned two wardens")
			}
			spawned.EntityID, spawned.Uuid = entityID, identity
			spawned.X, spawned.Y, spawned.Z = r.ReadF64(), r.ReadF64(), r.ReadF64()
			r.ReadLpVec3()
			r.ReadAngle()
			r.ReadAngle()
			r.ReadAngle()
			spawned.Data = r.ReadVarInt()
			if r.Err() != nil || r.Remaining() != 0 {
				t.Fatal("invalid warden spawn packet")
			}
		case protocol.IDPlayEntityMetadata:
			if spawned.EntityID == 0 || r.ReadVarInt() != spawned.EntityID {
				return false
			}
			entries, ok := servertest.ReadMetadata(r)
			if !ok || r.Err() != nil || r.Remaining() != 0 {
				t.Fatal("invalid warden metadata")
			}
			for _, e := range entries {
				if e.Index == protocol.MetadataIndexPose {
					pose = e.VarintVal
				}
			}
		}
		return false
	})
	if spawned.EntityID == 0 || spawned.Data != 1 || pose != protocol.PoseEmerging {
		t.Fatalf("spawn=%+v pose=%d, want emerging warden with spawn data 1", spawned, pose)
	}
	if spawned.X < 1.5 || spawned.X > 11.5 || spawned.Z < -0.5 || spawned.Z > 9.5 {
		t.Fatalf("warden outside shrieker's spawn range: %+v", spawned)
	}
	// An existing warden suppresses warning and shrieking, even after the player's cooldown.
	c.BreakBlock(2, 64, 4)
	c.Settle()
	servertest.RunTicks(t, srv, 110)
	placeStoneAt(c, 2)
	servertest.RunTicks(t, srv, 8)
	expectShrieker(t, srv, "true", "false")
	if len(srv.Mobs()) != 1 {
		t.Fatalf("mobs=%v, want one warden", srv.Mobs())
	}
	// The initial emergence has ended; darkness still pulses for a nearby survivor.
	c.SendChangeGameMode(protocol.GameModeSurvival)
	c.Settle()
	_, counts, _ := servertest.CollectSoundsTicks(c, srv, 120)
	if counts[protocol.IDPlayUpdateMobEffect] == 0 {
		t.Fatal("warden did not refresh darkness")
	}
}
