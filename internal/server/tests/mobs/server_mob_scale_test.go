package mobs

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestAScaledZombieHasADoubledBox checks that the scale attribute scales a
// mob's box on the server.
// vanilla: LivingEntity.getDimensions
func TestAScaledZombieHasADoubledBox(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.HostileTestConfig(), "Sizer", servertest.OpsListing(t, "Sizer"))

	id := srv.SpawnMob("minecraft:zombie", 3.5, 64, 3.5)
	srv.SetMobBaby(id, false)
	srv.SetMobNoAI(id)
	c.ChatCommand("attribute @e[type=minecraft:zombie,limit=1] minecraft:scale base set 2")
	servertest.AwaitSystemChatSaying(t, c, "commands.attribute.base_value.set.success")
	servertest.RunTicks(t, srv, 1)

	minX, minY, _, maxX, maxY, _, ok := srv.MobBox(id)
	if !ok {
		t.Fatal("zombie is gone")
	}
	if w, h := maxX-minX, maxY-minY; math.Abs(w-1.2) > 1e-6 || math.Abs(h-3.9) > 1e-6 {
		t.Errorf("scaled zombie box = %v wide, %v tall, want 1.2 by 3.9", w, h)
	}
}

// vanilla: LivingEntity.onAttributeUpdated, Entity.fudgePositionAfterSizeChange
func TestAnAirbornePigGrowsAroundItsOldCenter(t *testing.T) {
	t.Parallel()
	const name = "Sizer"
	srv, c := servertest.Join(t, servertest.MobTestConfig(), name, servertest.OpsListing(t, name))
	servertest.HoldTicks(t, srv)
	c.ChatCommand(`summon minecraft:pig 3.5 200 3.5 {NoGravity:1b,NoAI:1b}`)
	spawn := protocol.NewReader(c.ExpectMobPacket(protocol.IDPlaySpawnEntity))
	pigID := spawn.ReadVarInt()
	spawn.ReadUUID()
	if kind := spawn.ReadVarInt(); kind != protocol.EntityTypePig {
		t.Fatalf("spawned entity type = %d, want pig", kind)
	}
	servertest.RunTicks(t, srv, 1)
	if _, y, _, ok := srv.MobPos(pigID); !ok || y != 200 {
		t.Fatalf("pig's initial feet Y = %v (present %v), want 200", y, ok)
	}

	c.ChatCommand("attribute @e[type=minecraft:pig,limit=1] minecraft:scale base set 2")
	servertest.AwaitSystemChatSaying(t, c, "commands.attribute.base_value.set.success")
	servertest.RunTicks(t, srv, 1)

	const half = float64(float32(0.9))
	wantY := 200 - half/2
	if x, y, z, ok := srv.MobPos(pigID); !ok || x != 3.5 || math.Abs(y-wantY) > 1e-6 || z != 3.5 {
		t.Errorf("scaled pig position = (%v, %v, %v) (present %v), want (3.5, %v, 3.5)", x, y, z, ok, wantY)
	}
	minX, minY, minZ, maxX, maxY, maxZ, ok := srv.MobBox(pigID)
	got := [6]float64{minX, minY, minZ, maxX, maxY, maxZ}
	want := [6]float64{3.5 - half, wantY, 3.5 - half, 3.5 + half, wantY + 2*half, 3.5 + half}
	for i := range want {
		if !ok || math.Abs(got[i]-want[i]) > 1e-6 {
			t.Fatalf("scaled pig box = %v (present %v), want %v", got, ok, want)
		}
	}
}
