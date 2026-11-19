package mobs

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// shulkerTests is the flat world with the loop stopped and the difficulty
// normal; the shulkers stand on the grass at the player's feet level.
func shulkerTests(t *testing.T) (*server.Server, *servertest.Client, float64) {
	t.Helper()
	srv, c := zombieTests(t, servertest.SpawningTestConfig())
	srv.SetDifficulty(2)
	c.Settle()
	return srv, c, srv.PlayerPosition("Watcher").Y
}

func shulkerType(t *testing.T) int32 {
	t.Helper()
	id, ok := registry.EntityTypeID("minecraft:shulker")
	if !ok {
		t.Fatal("no shulker entity type")
	}
	return id
}

// TestAShulkerNearAPlayerOpensItsShell: a player in reach makes the shulker
// tell a fully open shell, and the spawn rests on the grass below.
// vanilla: Shulker.ShulkerAttackGoal.start
func TestAShulkerNearAPlayerOpensItsShell(t *testing.T) {
	t.Parallel()
	srv, c, y := shulkerTests(t)
	at := srv.PlayerPosition("Watcher")
	id := srv.SpawnMob("minecraft:shulker", at.X+4, y, at.Z)
	srv.TickMobs(1)
	got := c.ExpectMobSpawn(shulkerType(t))
	if got.EntityID != id {
		t.Fatalf("the spawn names entity %d, want %d", got.EntityID, id)
	}
	if got.ShulkerFace != 0 || got.ShulkerPeek != 0 {
		t.Errorf("a fresh shulker told face %d peek %d, want down and shut", got.ShulkerFace, got.ShulkerPeek)
	}
	peek := uint8(0)
	for range 80 {
		srv.TickMobs(1)
		c.SettleAndCollect(func(pid int32, data []byte) {
			if pid == protocol.IDPlayEntityMetadata && protocol.NewReader(data).ReadVarInt() == id {
				c.ReadMobMetadataInto(&got, data)
				peek = max(peek, got.ShulkerPeek)
			}
		})
	}
	if peek != 100 {
		t.Errorf("the shell opened to %d for a player four blocks away, want 100", peek)
	}
}

// TestAShulkerWhoseBlockIsBrokenTeleports: with nothing left to cling to the
// shulker lands within eight blocks of where it was, on a new face.
// vanilla: Shulker.findNewAttachment, teleportSomewhere
func TestAShulkerWhoseBlockIsBrokenTeleports(t *testing.T) {
	t.Parallel()
	srv, c, y := shulkerTests(t)
	at := srv.PlayerPosition("Watcher")
	x, z := int32(math.Floor(at.X))+6, int32(math.Floor(at.Z))
	stone, _ := registry.FindState("minecraft:stone", nil)
	// A pillar two blocks above the grass with the shulker on top, out of
	// the player's sight line height.
	top := int32(y) + 2
	for yy := int32(y) - 1; yy < top; yy++ {
		srv.SetBlock(x, yy, z, stone)
	}
	id := srv.SpawnMob("minecraft:shulker", float64(x)+0.5, float64(top), float64(z)+0.5)
	srv.TickMobs(5)
	c.Settle()
	if _, py, _, ok := srv.MobPos(id); !ok || py != float64(top) {
		t.Fatalf("the shulker stands at y %v (alive %v), want %d on its pillar", py, ok, top)
	}
	srv.SetBlock(x, top-1, z, 0)
	moved := false
	for range 60 {
		srv.TickMobs(1)
		if mx, my, mz, ok := srv.MobPos(id); ok && (mx != float64(x)+0.5 || my != float64(top) || mz != float64(z)+0.5) {
			moved = true
			if math.Abs(mx-float64(x)-0.5) > 8 || math.Abs(my-float64(top)) > 8 || math.Abs(mz-float64(z)-0.5) > 8 {
				t.Errorf("the shulker landed at %v,%v,%v, more than eight blocks from its pillar", mx, my, mz)
			}
			break
		}
	}
	if !moved {
		t.Error("the shulker never left the cell whose block was broken")
	}
}

// TestShulkersLeaveShellsInHalfTheirDeaths: the shell drops at the 50%
// chance the loot table rolls.
// vanilla: entities/shulker.json
func TestShulkersLeaveShellsInHalfTheirDeaths(t *testing.T) {
	t.Parallel()
	srv, _, y := shulkerTests(t)
	at := srv.PlayerPosition("Watcher")
	for i := range 60 {
		id := srv.SpawnMob("minecraft:shulker", at.X+3+float64(i%10), y, at.Z-3-float64(i/10))
		srv.KillMob(id)
	}
	srv.TickMobs(2)
	shells := 0
	for _, it := range srv.ItemsInDimension("minecraft:overworld") {
		if it.Item == "minecraft:shulker_shell" {
			shells += int(it.Count)
		}
	}
	if shells < 15 || shells > 45 {
		t.Errorf("60 dead shulkers left %d shells, want about 30", shells)
	}
}
