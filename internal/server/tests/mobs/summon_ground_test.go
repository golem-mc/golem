package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
)

// vanilla: Entity.load, LivingEntity.aiStep, Mob.isEffectiveAi
func TestASummonedNoAIMobKeepsItsGroundStateForFrostWalker(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		tag    string
		ground bool
	}{
		{"grounded", ",OnGround:1b", true},
		{"airborne", ",OnGround:0b", false},
		{"absent", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.Join(t, servertest.MobTestConfig(), "Grounded", servertest.OpsListing(t, "Grounded"))
			t.Cleanup(srv.HoldChunk(5, 5))
			servertest.HoldTicks(t, srv)
			c.Settle()
			srv.SetBlock(5, 63, 5, servertest.BlockState(t, "minecraft:stone"))
			srv.SetBlock(6, 63, 5, servertest.BlockState(t, "minecraft:water"))
			c.ChatCommand(`summon zombie 5.5 64 5.5 {NoAI:1b` + tc.tag + `}`)
			zombie := c.ExpectMobSpawn(protocol.EntityTypeZombie)
			c.Settle()
			if got := srv.MobOnGround(zombie.EntityID); got != tc.ground {
				t.Errorf("summoned OnGround = %v, want %v", got, tc.ground)
			}
			uuid, _ := srv.MobUUID(zombie.EntityID)
			c.ChatCommand(`item replace entity ` + uuid + ` armor.feet with diamond_boots[enchantments={"minecraft:frost_walker":2}]`)
			c.Settle()
			servertest.RunTicks(t, srv, 1)
			wantBlock := "minecraft:water"
			if tc.ground {
				wantBlock = "minecraft:frosted_ice"
			}
			servertest.ExpectBlockNamed(t, srv, 6, 63, 5, wantBlock, nil)
			x, y, z, _ := srv.MobPos(zombie.EntityID)
			if x != 5.5 || y != 64 || z != 5.5 {
				t.Errorf("NoAI zombie moved to (%g,%g,%g)", x, y, z)
			}
			tag, ok := srv.MobSaveTag(zombie.EntityID)
			if !ok || !nbt.BoolField(tag, "NoAI") || nbt.BoolField(tag, "OnGround") != tc.ground {
				t.Errorf("saved NoAI/OnGround = %v/%v, want true/%v", tag["NoAI"], tag["OnGround"], tc.ground)
			}
		})
	}
}
