package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestAFallingCowIsToldItsMotion pins that the client learns a mob's velocity
// as it changes: the client draws its own splash particles from that value,
// so a mob it was never told about drifts them by a stale one.
func TestAFallingCowIsToldItsMotion(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Farmer")

	id := srv.SpawnMob("minecraft:cow", 5.5, 80, 5.5)
	cow := c.ExpectMobSpawn(protocol.EntityTypeCow)
	if cow.EntityID != id {
		t.Fatalf("spawned cow %d, want %d", cow.EntityID, id)
	}
	servertest.HoldTicks(t, srv)

	var falls int
	c.ReadTicks(srv, 120, func(pid int32, data []byte) bool {
		if pid != protocol.IDPlaySetEntityMotion {
			return false
		}
		r := protocol.NewReader(data)
		if r.ReadVarInt() != id {
			return false
		}
		_, vy, _ := r.ReadLpVec3()
		if vy < 0 {
			falls++
		}
		return false
	})
	if falls == 0 {
		t.Error("the falling cow was never told a downward motion")
	}
}
