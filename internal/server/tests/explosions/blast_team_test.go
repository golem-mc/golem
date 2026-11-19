package explosions

import (
	"fmt"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// A primed TNT discards its team before exploding; a TNT minecart explodes
// while still on its team, sparing teammates when friendly fire is off.
// vanilla: PrimedTnt.tick, MinecartTNT.explode, Entity.doTeamsAllowDamage
func TestABlastSparesTheTeamOfItsSource(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"minecraft:tnt", "minecraft:tnt_minecart"} {
		for _, friendlyFire := range []bool{false, true} {
			t.Run(fmt.Sprint(kind, "/friendlyFire=", friendlyFire), func(t *testing.T) {
				t.Parallel()
				srv, c, _ := servertest.JoinAs(t, servertest.HostileTestConfig(), "Alice")
				servertest.HoldTicks(t, srv)
				srv.RunLine("team add t")
				srv.RunLine(fmt.Sprint("team modify t friendlyFire ", friendlyFire))
				srv.RunLine("team join t Alice")
				zombie := srv.SpawnMob("minecraft:zombie", 2.5, 64, 2.5)
				zombieUUID, ok := srv.MobUUID(zombie)
				if !ok {
					t.Fatal("SpawnMob came to nothing")
				}
				srv.RunLine("team join t " + zombieUUID)
				srv.RunLine("summon " + kind + " 2.5 64 0.5 {fuse:2,Team:\"t\"}")

				var boom servertest.Explosion
				seen := c.ReadTicksSkipping(srv, 60, tntSkip, func(id int32, data []byte) bool {
					if id == protocol.IDPlayExplosion {
						boom = servertest.ReadExplosion(c, data)
						return true
					}
					return false
				})
				if !seen {
					t.Fatal("the TNT never went up")
				}
				if !boom.Pushed || boom.KX == 0 && boom.KY == 0 && boom.KZ == 0 {
					t.Errorf("Explosion knockback = %v (%v, %v, %v), want a push either way", boom.Pushed, boom.KX, boom.KY, boom.KZ)
				}
				spared := kind == "minecraft:tnt_minecart" && !friendlyFire
				health := srv.PlayerHealth("Alice")
				if spared && health != 20 {
					t.Errorf("health = %v, want 20: the blast spares its own team", health)
				}
				if !spared && health >= 20 {
					t.Errorf("health = %v, want below 20: the source has no team immunity", health)
				}
				zombieHealth, _ := srv.MobHealth(zombie)
				if spared && zombieHealth != 20 {
					t.Errorf("zombie health = %v, want 20: the blast spares its own team", zombieHealth)
				}
				if !spared && zombieHealth >= 20 {
					t.Errorf("zombie health = %v, want below 20: the source has no team immunity", zombieHealth)
				}
			})
		}
	}
}

// TestATeamedBlastSparesNonlivingTeammates sets off a primed TNT, a TNT
// minecart and an end crystal, each on a team, beside an item and a
// painting on that team. A still-teamed source with friendly fire off spares
// and pushes the item; otherwise the blast destroys it. The painting breaks
// either way: a spared body is still pushed, and any push breaks one.
// vanilla: ServerExplosion.hurtEntities, Entity.doTeamsAllowDamage,
// BlockAttachedEntity.push, MinecartTNT.explode, EndCrystal.hurtServer
func TestATeamedBlastSparesNonlivingTeammates(t *testing.T) {
	t.Parallel()
	sources := []struct {
		// summon stands the source where the blast centres; set sets it
		// off once its team is joined. ticks is how long the blast takes
		// to land after that.
		summon string
		set    func(c *servertest.Client)
		ticks  int
	}{
		{"summon minecraft:tnt 4.5 64 2.5 {fuse:1}", nil, 2},
		{"summon minecraft:tnt_minecart 4.5 64 2.5 {fuse:1}", nil, 2},
		{"summon minecraft:end_crystal 4.5 64 2.5 {ShowBottom:0b}", func(c *servertest.Client) {
			c.Attack(c.AwaitSpawn(protocol.EntityTypeEndCrystal).EntityID)
			c.Settle()
		}, 1},
	}
	for _, src := range sources {
		for _, friendlyFire := range []bool{false, true} {
			t.Run(fmt.Sprint(strings.Fields(src.summon)[1], "/friendlyFire=", friendlyFire), func(t *testing.T) {
				t.Parallel()
				srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Watcher")
				c.MoveToGrounded(4.5, 64, 0.5, 0, 0, true)
				c.Settle()
				servertest.HoldTicks(t, srv)
				// Obsidian, so the blast leaves the wall the painting hangs on.
				srv.SetBlock(4, 64, 5, registry.BlockObsidian.DefaultState())
				srv.RunLine("team add t")
				srv.RunLine(fmt.Sprint("team modify t friendlyFire ", friendlyFire))
				item := srv.SpawnItemEntity(6.5, 64, 2.5, "minecraft:stone", 1)
				srv.RunLine(`summon minecraft:painting 4.5 64 4.5 {facing:2b,variant:"minecraft:kebab"}`)
				srv.RunLine(src.summon)
				srv.RunLine("team join t @e[type=!minecraft:player]")
				if src.set != nil {
					src.set(c)
				}
				servertest.RunTicks(t, srv, src.ticks)

				var stack *server.TestItem
				for _, it := range srv.ItemsInDimension("minecraft:overworld") {
					if it.ID == item {
						stack = &it
					}
				}
				spared := !friendlyFire && strings.Fields(src.summon)[1] != "minecraft:tnt"
				switch {
				case spared && stack == nil:
					t.Error("the blast destroyed its teammate item, want it spared")
				case spared && stack.X <= 6.5:
					t.Errorf("the spared item stands at x %v, want pushed east of 6.5", stack.X)
				case !spared && stack != nil:
					t.Error("the spared item survived, want the blast to destroy it")
				}
				if n := srv.PaintingCount(); n != 0 {
					t.Errorf("%d paintings hang after the blast, want the push to break the teammate", n)
				}
			})
		}
	}
}
