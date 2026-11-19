package mobs

import (
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
