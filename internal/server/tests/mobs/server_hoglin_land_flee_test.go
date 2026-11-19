package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// vanilla: HoglinAi.wasHurtBy, SetWalkTargetAwayFrom.create
func TestAHurtBabyHoglinFleesAlongTheBankInsteadOfIntoWater(t *testing.T) {
	t.Parallel()
	c, _, srv := hoglinWorld2(t)
	for x := int32(-20); x <= 24; x++ {
		for z := int32(-24); z <= 24; z++ {
			state := registry.BlockStone.DefaultState()
			if x >= 3 {
				state = registry.BlockWater.DefaultState()
			}
			srv.SetBlock(x, 64, z, state)
		}
	}
	c.MoveToGround(-1.5, 65, 0.5, true)
	c.Settle()
	baby := srv.SpawnMob(hoglin, 1.5, 65, 0.5)
	srv.SetMobBaby(baby, true)
	c.Attack(baby)
	c.Settle()
	walked := false
	for range 100 {
		servertest.RunTicks(t, srv, 1)
		x, _, z, ok := srv.MobPos(baby)
		if !ok {
			t.Fatal("the hoglin disappeared")
		}
		if x >= 4 {
			t.Fatalf("the retreating hoglin entered the pond at (%v, %v)", x, z)
		}
		walked = walked || z < -2 || z > 3
	}
	if !walked {
		t.Fatal("the hoglin never fled along the dry bank")
	}
}
