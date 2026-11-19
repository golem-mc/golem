package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
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

// vanilla: Mob.baseTick, EnderDragon.getAmbientSound, getHurtSound,
// getSoundVolume, getSoundSource
func TestTheDragonGrowlsAndIsHeardWhenHurt(t *testing.T) {
	t.Parallel()
	cfg := servertest.HostileTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c := servertest.Join(t, cfg, "Listener", servertest.OpsListing(t, "Listener"))
	servertest.HoldTicks(t, srv)
	srv.SetSpawning(false)
	c.MoveTo(8, 128, 5.5, 0, 0)
	c.Settle()
	c.ChatCommand(`summon minecraft:ender_dragon 8.0 128.0 13.5 {DragonPhase:6}`)
	c.Settle()
	// One tick of AI positions the parts, which a NoAI dragon keeps.
	srv.TickMobs(1)
	c.ChatCommand(`data merge entity @e[type=minecraft:ender_dragon,limit=1] {NoAI:1b}`)
	c.Settle()
	ids := srv.MobIDsOfKind("minecraft:ender_dragon")
	if len(ids) != 1 {
		t.Fatalf("dragons = %v, want one", ids)
	}
	id := ids[0]

	// check reports whether data is the named dragon sound, hostile at volume 5.
	check := func(data []byte, want string) bool {
		name, category, _, _, _, volume, _ := servertest.ReadAmbient(t, data)
		if name != want {
			return false
		}
		if category != protocol.SoundCategoryHostile || volume != 5 {
			t.Errorf("%s played in category %d at volume %v, want %d at 5", name, category, volume, protocol.SoundCategoryHostile)
		}
		return true
	}
	growled := false
	for tick := 0; tick < servertest.AmbientTicks && !growled; tick += 10 {
		srv.TickMobs(10)
		c.ReadTicks(srv, 0, func(id int32, data []byte) bool {
			if id == protocol.IDPlaySoundEffect && check(data, "minecraft:entity.ender_dragon.ambient") {
				growled = true
			}
			return false
		})
	}
	if !growled {
		t.Fatalf("the dragon never growled within %d ticks", servertest.AmbientTicks)
	}

	// The body is the dragon's fourth part.
	c.Attack(id + 3)
	hurt := false
	c.ReadTicks(srv, 2, func(id int32, data []byte) bool {
		hurt = hurt || id == protocol.IDPlaySoundEffect && check(data, "minecraft:entity.ender_dragon.hurt")
		return hurt
	})
	if !hurt {
		t.Error("a blow on the dragon's body sent no entity.ender_dragon.hurt")
	}
}

// vanilla: Entity.move (noPhysics clears the collision flags), DragonChargePlayerPhase.doServerTick
func TestADragonChargingThroughAWallKeepsCharging(t *testing.T) {
	t.Parallel()
	cfg := servertest.HostileTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c := servertest.Join(t, cfg, "Charger", servertest.OpsListing(t, "Charger"))
	servertest.HoldTicks(t, srv)
	srv.SetSpawning(false)
	srv.SetGameRule("mob_griefing", false)
	c.MoveTo(8, 128, 5, 0, 0)
	c.Settle()
	c.ChatCommand("fill 4 124 31 12 132 39 minecraft:stone")
	c.Settle()
	id := srv.SpawnMob("minecraft:ender_dragon", 8, 128, 35)
	if !srv.SetDragonPhase(id, entity.DragonCharging, "Charger") {
		t.Fatal("the dragon did not spawn")
	}
	servertest.RunTicks(t, srv, 15)
	if phase := srv.MobDragonPhase(id); phase != entity.DragonCharging {
		t.Fatalf("dragon phase after 15 ticks in a wall = %d, want charging (%d)", phase, entity.DragonCharging)
	}
}
