package mobs

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// vanilla: EnderDragon.aiStep, checkWalls
func TestADragonsHeadHurtsAPlayerAndRemovesAWall(t *testing.T) {
	t.Parallel()
	cfg := servertest.HostileTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c := servertest.Join(t, cfg, "Explorer", servertest.OpsListing(t, "Explorer"))
	servertest.HoldTicks(t, srv)
	srv.SetSpawning(false)
	srv.SetDifficulty(rules.DifficultyNormal)
	srv.SetGameRule("mob_griefing", true)
	c.MoveTo(8, 128, 5.5, 0, 0)
	c.Settle()
	// A sitting phase keeps the dragon still while its contacts and walls tick.
	c.ChatCommand(`summon minecraft:ender_dragon 8.0 128.0 12.0 {DragonPhase:6,Rotation:[0f,0f]}`)
	c.Settle()
	if !srv.SetBlock(8, 129, 5, registry.BlockStone.DefaultState()) {
		t.Fatal("the head's wall chunk was not loaded")
	}
	// Mob effects queue the block update after the tick's block flush.
	servertest.RunTicks(t, srv, 2)
	var hurt, removed, particles bool
	c.SettleAndCollect(func(id int32, data []byte) {
		r := protocol.NewReader(data)
		switch id {
		case protocol.IDPlaySetHealth:
			hurt = servertest.ExpectHealthReport(c, data).Health == 10
		case protocol.IDPlayBlockUpdate, protocol.IDPlaySectionBlocks:
			for _, ch := range servertest.BlockChangesIn(t, id, data) {
				if ch.X == 8 && ch.Y == 129 && ch.Z == 5 && ch.State == registry.BlockAir.DefaultState() {
					removed = true
				}
			}
		case protocol.IDPlayWorldEvent:
			// vanilla: LevelEvent.PARTICLES_DRAGON_BLOCK_BREAK
			if r.ReadI32() == 2008 {
				particles = true
			}
		}
	})
	if !hurt || srv.PlayerHealth("Explorer") != 10 {
		t.Errorf("head contact: health = %v, received health 10 = %v", srv.PlayerHealth("Explorer"), hurt)
	}
	if !removed || srv.BlockAt(8, 129, 5) != registry.BlockAir.DefaultState() || !particles {
		t.Errorf("wall: received removal = %v, block = %d, event 2008 = %v", removed, srv.BlockAt(8, 129, 5), particles)
	}
}

// TestTeamFriendlyFireGatesDragonContacts: a dragon's head and wing damage
// a player, unless the dragon shares the player's team and the team's
// friendlyFire is off; the wing's push moves the player either way.
// vanilla: Entity.doTeamsAllowDamage, EnderDragon.knockBack, EnderDragon.hurt
func TestTeamFriendlyFireGatesDragonContacts(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		phase int // 6 sits (head contact); a hovering dragon's wing deals no damage, so 0 flies
		x, z  float64
		wing  bool
	}{
		{"head", 6, 8, 5.5, false},
		{"wing", 0, 14.5, 12, true},
	}
	for _, tc := range cases {
		for _, mode := range []string{"control", "teamed", "friendly"} {
			t.Run(tc.name+"/"+mode, func(t *testing.T) {
				t.Parallel()
				cfg := servertest.HostileTestConfig()
				cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
				srv, c := servertest.Join(t, cfg, "Explorer", servertest.OpsListing(t, "Explorer"))
				servertest.HoldTicks(t, srv)
				srv.SetSpawning(false)
				srv.SetDifficulty(rules.DifficultyNormal)
				c.MoveTo(tc.x, 128, tc.z, 0, 0)
				c.Settle()
				// A fresh player's hurt stamp is still inside the wing's grace window.
				servertest.RunTicks(t, srv, 5)
				c.ChatCommand(fmt.Sprintf("summon minecraft:ender_dragon 8.0 128.0 12.0 {DragonPhase:%d,Rotation:[0f,0f]}", tc.phase))
				c.Settle()
				if mode != "control" {
					srv.RunLine("team add t")
					srv.RunLine("team modify t friendlyFire " + fmt.Sprint(mode == "friendly"))
					srv.RunLine("team join t Explorer")
					srv.RunLine("team join t @e[type=minecraft:ender_dragon,limit=1]")
				}
				var moved bool
				for range 3 {
					servertest.RunTicks(t, srv, 1)
					if vx, _, _ := srv.PlayerVelocity("Explorer"); tc.wing && vx != 0 {
						moved = true
					}
				}
				motion := false
				c.SettleAndCollect(func(id int32, data []byte) {
					if id == protocol.IDPlaySetEntityMotion && protocol.NewReader(data).ReadVarInt() == c.PlayerEntityID {
						motion = true
					}
				})
				hurt := srv.PlayerHealth("Explorer") < 20
				// The impulse is server-side; only a landed hurt tells the client.
				if tc.wing && motion != hurt {
					t.Errorf("mode %s: client told of motion = %v, hurt = %v", mode, motion, hurt)
				}
				if hurt != (mode != "teamed") {
					t.Errorf("health %v with mode %s", srv.PlayerHealth("Explorer"), mode)
				}
				if tc.wing && !moved {
					t.Errorf("the wing did not push the player in mode %s", mode)
				}
			})
		}
	}
}
