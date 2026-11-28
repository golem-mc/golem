package entities

import (
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// vanilla: CampfireBlock.entityInside, ArmorStand.hurtServer
func TestArmorStandBurnsInLitCampfire(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		block registry.Block
	}{{"campfire", registry.BlockCampfire}, {"soul_campfire", registry.BlockSoulCampfire}} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.Join(t, servertest.MobTestConfig(), "Watcher", servertest.OpsListing(t, "Watcher"))
			servertest.HoldTicks(t, srv)
			srv.SetBlock(5, 64, 5, tc.block.DefaultState())
			c.ChatCommand(`summon minecraft:armor_stand 5.5 64 5.5 {Tags:["campcheck"]}`)
			stand := c.ExpectArmorStandSpawn()
			get := func(path, key string) string {
				t.Helper()
				return standCommandText(t, c, "data get entity @e[tag=campcheck,limit=1] "+path, key).With[1].Plain()
			}
			servertest.RunTicks(t, srv, 3)
			if got := get("Fire", "commands.data.entity.query"); got != "98s" {
				t.Fatalf("Fire = %s, want 98s", got)
			}
			if got := get("Health", "commands.data.entity.query"); !strings.HasPrefix(got, "15.7") {
				t.Fatalf("Health = %s, want 15.7", got)
			}
			servertest.AwaitRemovalsOfTicks(t, c, srv, 57, stand.EntityID)
		})
	}
}

func TestArmorStandIgnoresUnlitCampfire(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Watcher", servertest.OpsListing(t, "Watcher"))
	servertest.HoldTicks(t, srv)
	c.ChatCommand(`setblock 5 64 5 minecraft:campfire[lit=false]`)
	c.ChatCommand(`summon minecraft:armor_stand 5.5 64 5.5 {Tags:["campcheck"]}`)
	c.ExpectArmorStandSpawn()
	servertest.RunTicks(t, srv, 60)
	got := standCommandText(t, c, "data get entity @e[tag=campcheck,limit=1] Health", "commands.data.entity.query").With[1].Plain()
	if !strings.HasPrefix(got, "20") {
		t.Fatalf("Health = %s, want 20", got)
	}
}
