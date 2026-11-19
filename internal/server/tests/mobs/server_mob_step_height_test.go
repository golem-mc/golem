package mobs

import (
	"math"
	"strconv"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// TestIronGolemStepsOntoABlockWithoutJumping sends an iron golem after a
// zombie standing on a ledge one block over the floor. The golem's
// step_height of 1 (vanilla: IronGolem.createAttributes) takes it up the
// ledge in its stride; a body that read the living 0.6 would jump, and a
// jump carries its feet well over the ledge top.
func TestIronGolemStepsOntoABlockWithoutJumping(t *testing.T) {
	t.Parallel()
	stone, ok := registry.FindState("minecraft:stone", nil)
	if !ok {
		t.Fatal("the block registry has no stone")
	}
	srv, c := servertest.Join(t, servertest.HostileTestConfig(), "Watcher")
	srv.SetDayTime(18000) // midnight, whose dark is no daylight burn

	// The ledge: x 1..8 by z -2..2, its top at y 65.
	for x := int32(1); x <= 8; x++ {
		for z := int32(-2); z <= 2; z++ {
			if !srv.SetBlock(x, 64, z, stone) {
				t.Fatal("the world did not take the ledge")
			}
		}
	}
	c.MoveToGround(-4.5, 64, 6.5, true)
	c.Settle()
	servertest.HoldTicks(t, srv)

	prey := srv.SpawnMob("minecraft:zombie", 7.5, 65, 0.5)
	if prey == 0 || !srv.SetMobNoAI(prey) {
		t.Fatal("the zombie did not spawn")
	}
	srv.SetMobBaby(prey, false)
	c.ExpectMobSpawn(protocol.EntityTypeZombie)
	golem := srv.SpawnMob("minecraft:iron_golem", -3.5, 64, 0.5)
	if golem == 0 {
		t.Fatal("the iron golem did not spawn")
	}
	c.ExpectMobSpawn(protocol.EntityTypeIronGolem)

	maxY, up := 0.0, false
	c.PumpTicks(srv, 300, func(int32, []byte) bool {
		x, y, _, ok := srv.MobPos(golem)
		if !ok {
			return false
		}
		maxY = math.Max(maxY, y)
		up = x >= 1.5 && y >= 65
		return up
	})
	if !up {
		x, y, z, _ := srv.MobPos(golem)
		t.Fatalf("the golem never came up onto the ledge; it stands at %.2f,%.2f,%.2f", x, y, z)
	}
	if maxY > 65+1e-6 {
		t.Errorf("the golem rose to y=%.3f crossing onto the ledge, want it stepped up to 65 without a jump", maxY)
	}
}

// TestAHorseStepsOneBlock reads a horse's step_height through /attribute:
// vanilla's AbstractHorse.createBaseHorseAttributes gives every equine 1.
func TestAHorseStepsOneBlock(t *testing.T) {
	t.Parallel()
	_, c := servertest.Join(t, servertest.MobTestConfig(), "Rider", servertest.OpsListing(t, "Rider"))
	c.ChatCommand("summon minecraft:horse 1.5 64 0.5 {NoAI:1b}")
	c.ExpectMobSpawn(protocol.EntityTypeHorse)

	c.ChatCommand("attribute @e[type=horse,limit=1] minecraft:step_height get")
	var value string
	c.SettleAndCollect(func(packetID int32, data []byte) {
		if packetID != protocol.IDPlaySystemChat {
			return
		}
		if text, ok := protocol.ReadText(protocol.NewReader(data)); ok && text.Translate == "commands.attribute.value.get.success" {
			value = text.With[len(text.With)-1].Text
		}
	})
	if v, err := strconv.ParseFloat(value, 64); err != nil || v != 1 {
		t.Errorf("horse step_height = %q, want 1", value)
	}
}
