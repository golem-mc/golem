package mobs

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// The hostile tests: the first monster, driven the way the survival and mob
// tests drive everything else — through a real connection — with the world
// the flat one the harness grows, which grows no animals of its own, so
// every mob in these worlds is one the test planted. The zombie hunts, the
// sun burns it, the despawn takes it, and its blows go through the same
// player-damage pipeline a player's own do.

// TestABurningZombieBurnsInTheSun checks the sun's half of a zombie's week:
// at noon under an open sky the shared flags' on-fire bit rises, and the
// fire bites a point of the body a second, the health metadata falling with
// it. The player watching is in creative — a zombie busy burning has no
// hunt to ride, and the burn is nobody's to interrupt.
func TestABurningZombieBurnsInTheSun(t *testing.T) {
	t.Parallel()
	cfg := servertest.HostileTestConfig()
	cfg.GameMode = "creative"

	// The noon the zombie burns in is set by a /time, whose runner is
	// listed as an operator.
	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Sunwatcher"), func(s *server.Server) { s.SetTimeSync(0) })
	srv.SetPaused(true)
	c := connect().Join("Sunwatcher")
	c.ChatCommand("time set noon")
	c.Settle()

	zombieID := srv.SpawnMob("minecraft:zombie", 4.5, 64, 4.5)
	onFire := false
	burned := false
	c.ReadTicksSkipping(srv, servertest.HostileTicks, servertest.HostileSkip, func(id int32, data []byte) bool {
		switch id {
		case protocol.IDPlayEntityMetadata:
			m := servertest.ReadZombieMetadata(c, data)
			if m.EntityID != zombieID {
				return false // another body's state, read past
			}
			if m.HasFire && m.OnFire {
				onFire = true
			}
			if onFire && m.HasHealth && m.Health < 20 {
				burned = true
				return true
			}
		}
		return false
	})
	if !onFire {
		t.Error("a zombie in the noonday sun never caught fire")
	}
	if !burned {
		t.Error("the standing fire never hurt the zombie")
	}
}

// TestAZombieReplansAroundABlockPlacedOnItsPath: a wall with one doorway
// raised across a zombie's standing path is walked around, not pressed at.
// vanilla: PathNavigation.shouldRecomputePath
func TestAZombieReplansAroundABlockPlacedOnItsPath(t *testing.T) {
	t.Parallel()
	cfg := servertest.HostileTestConfig()

	// /fill is an operator's command.
	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Builder"))
	srv.SetDayTime(18000)
	c := connect().Join("Builder")
	c.MoveToGround(12.5, 64, 0.5, true)
	c.Settle()

	zombieID := srv.SpawnMob("minecraft:zombie", -5.5, 64, 0.5)
	c.ExpectMobSpawn(protocol.EntityTypeZombie)
	servertest.HoldTicks(t, srv)
	servertest.WaitForTicks(t, srv, 200, func() bool {
		x, _, _, ok := srv.MobPos(zombieID)
		return ok && x > -2
	})
	// The wall goes through the command, as srv.SetBlock never wakes the
	// mobs' paths.
	c.ChatCommand("fill 4 64 -6 4 65 4 minecraft:stone")
	c.ChatCommand("fill 4 64 6 4 65 6 minecraft:stone")
	c.Settle()
	// The zombie crosses in 114 ticks with the replan and 172 without it
	// (it presses at the wall until its path times out), so 140 fails
	// without it.
	servertest.WaitForTicks(t, srv, 140, func() bool {
		x, _, _, ok := srv.MobPos(zombieID)
		return ok && x > 5
	})
}

// TestAPeacefulWorldHasNoZombies checks the difficulty's own word: the
// monster a peaceful world finds itself with is discarded at once, its body
// taken out of every view it was in and nothing laid down behind it.
func TestAPeacefulWorldHasNoZombies(t *testing.T) {
	t.Parallel()
	cfg := servertest.HostileTestConfig()

	srv, c := servertest.Join(t, cfg, "Pacifist")

	zombieID := srv.SpawnMob("minecraft:zombie", 4.5, 64, 4.5)
	c.ExpectMobSpawn(protocol.EntityTypeZombie)

	srv.SetDifficulty(0) // peaceful

	removed := false
	c.ReadTicksSkipping(srv, servertest.HostileTicks, servertest.HostileSkip, func(id int32, data []byte) bool {
		if id == protocol.IDPlayRemoveEntities {
			r := protocol.NewReader(data)
			if got := r.ReadVarInt(); got != 1 {
				c.T.Errorf("removed entities = %d, want 1", got)
			}
			if got := r.ReadVarInt(); got != zombieID {
				c.T.Errorf("removed entity = %d, want the zombie's %d", got, zombieID)
			}
			removed = true
			return true
		}
		return false
	})
	if !removed {
		t.Error("the peaceful world never took its zombie away")
	}
	if n := srv.MobCount(); n != 0 {
		t.Errorf("the peaceful world keeps %d mobs, want the zombie gone from the herd too", n)
	}
}

// TestAKillingAZombieDropsRottenFleshAndPlaysItsDeath kills a zombie by the
// sword and watches what the death says: the death sound of its kind, the
// corpse's twenty ticks, and the loot table's plain path — rotten flesh,
// none to two of it, with the iron and the vegetables of the rare pool
// staying in it. The hand on the sword is a creative one: a survival
// gravedigger would be hunted in return, and the loot wants only the kill.
func TestAKillingAZombieDropsRottenFleshAndPlaysItsDeath(t *testing.T) {
	t.Parallel()
	cfg := servertest.HostileTestConfig()
	cfg.GameMode = "creative"

	srv, connect := servertest.Start(t, cfg)
	srv.SetDayTime(18000) // midnight, whose dark is no daylight burn
	c := connect().Join("Gravedigger")

	sword, ok := registry.ItemID("minecraft:diamond_sword")
	if !ok {
		t.Fatal("the item registry has no diamond sword")
	}
	srv.GiveSlot("Gravedigger", player.HotbarSlot, protocol.ItemStack{ItemID: sword, Count: 1})
	// The sword's arrival in the hand spends the swing's recovery back to
	// nought; the killing waits the cooldown out before the first blow, so
	// every swing lands whole.
	servertest.AwaitFullSwing(t, srv, "Gravedigger")

	// The unenchanted loot roll can be empty. Seed the named sequence for a
	// roll with rotten flesh.
	srv.LootSequenceSeed("minecraft:entities/zombie", 1)
	zombieID := srv.SpawnMob("minecraft:zombie", 2.5, 64, 1.5)
	c.ExpectMobSpawn(protocol.EntityTypeZombie)

	// Three swings of the sword are the zombie's twenty points of health,
	// spaced past the half-second of invulnerability a landed hit buys.
	// The loop is stopped and each recovery ticked by hand, so the zombie
	// wanders for those ticks alone; a landed blow's knockback shoves it
	// a few blocks back, so the hand steps up to where it now stands
	// before the next — vanilla refuses a blow past the entity
	// interaction's reach.
	srv.StopTick()
	for range 3 {
		servertest.RecoverByHand(t, srv, "Gravedigger")
		x, _, z, ok := srv.MobPos(zombieID)
		if !ok {
			t.Fatalf("the zombie is gone before its blows")
		}
		c.MoveToGround(x, 64, z-1.5, true)
		c.Attack(zombieID)
		servertest.SwingLanded(t, srv, "Gravedigger")
	}
	srv.StartTick()

	dead := false
	deathSound := false
	var drops []protocol.ItemStack
	c.ReadTicksSkipping(srv, servertest.HostileTicks, servertest.HostileSkip, func(id int32, data []byte) bool {
		switch id {
		case protocol.IDPlayEntityEvent:
			r := protocol.NewReader(data)
			r.ReadI32()
			if r.ReadU8() == protocol.EntityStatusDeath {
				dead = true
			}
		case protocol.IDPlaySoundEffect:
			// The death sound: the registry reference the server sends
			// sounds by, in the hostile bracket the monsters' sounds play
			// in.
			r := protocol.NewReader(data)
			sound := r.ReadVarInt()
			var name string
			if sound == 0 {
				name = r.ReadString(32767) // an inline definition
			} else {
				name, _ = servertest.SoundEventName(sound - 1)
			}
			if name == "minecraft:entity.zombie.death" {
				deathSound = true
				if got := r.ReadVarInt(); got != protocol.SoundCategoryHostile {
					c.T.Errorf("the death sound played in category %d, want the hostile bracket", got)
				}
			}
		case protocol.IDPlaySpawnEntity:
			if d := servertest.ParseItemSpawn(data); d.EntityID != 0 {
				// The drop's stack rides the metadata that follows it.
				c.ReadDropStack(&d)
				drops = append(drops, d.Stack)
			}
		}
		return dead && deathSound && len(drops) > 0
	})
	if !dead {
		t.Error("the zombie never died")
	}
	if !deathSound {
		t.Error("the death never played the zombie's death sound")
	}
	if len(drops) == 0 {
		t.Fatal("the dead zombie laid nothing down")
	}
	// The flesh, and the player kill's rare drops beside it (an iron ingot,
	// a carrot or a potato, one in forty), which the table rolls too.
	flesh, _ := registry.ItemID("minecraft:rotten_flesh")
	rare := map[string]bool{"minecraft:iron_ingot": true, "minecraft:carrot": true, "minecraft:potato": true}
	fleshDropped := false
	for _, stack := range drops {
		name, _ := registry.ItemName(stack.ItemID)
		if stack.ItemID != flesh {
			if !rare[name] || stack.Count != 1 {
				t.Errorf("the dead zombie dropped %d %s, want rotten flesh or one of the rare drops", stack.Count, name)
			}
			continue
		}
		fleshDropped = true
		if stack.Count < 1 || stack.Count > 2 {
			t.Errorf("the dead zombie dropped %d rotten flesh, want the table's range of one to two", stack.Count)
		}
	}
	if !fleshDropped {
		t.Error("the dead zombie dropped no rotten flesh")
	}

	// The corpse's twenty ticks: the body goes out of the world once the
	// death animation has had its say. The orb the player's kill dropped
	// may have been picked up before the corpse went, and its own removal
	// rides in ahead of the body's, so the wait is for a removal that
	// names the zombie rather than for the first one that comes.
	c.ExpectHostilePacket(srv, protocol.IDPlayRemoveEntities, func(c *servertest.Client, data []byte) {
		r := protocol.NewReader(data)
		count := r.ReadVarInt()
		ids := make([]int32, 0, count)
		for range count {
			ids = append(ids, r.ReadVarInt())
		}
		if !slices.Contains(ids, zombieID) {
			// Someone else's removal - an orb's, say - is background
			// here, and the pump goes on waiting for the corpse's.
			c.Pushback = append(c.Pushback, servertest.PushedPacket{Id: protocol.IDPlayRemoveEntities, Data: data})
			return
		}
	})
}

// TestAZombieFarFromEveryPlayerDespawns checks the despawn distance: a
// zombie whose chunk a test holds open, standing two hundred blocks from
// the only player, is gone within the tick, with nothing left in the herd.
func TestAZombieFarFromEveryPlayerDespawns(t *testing.T) {
	t.Parallel()
	cfg := servertest.HostileTestConfig()

	srv, _ := servertest.Join(t, cfg, "Lighthouse")

	// The chunk is held, since a chunk nobody plays in is given up to its
	// entity file within the tick and the despawn never gets its word in.
	release := srv.HoldChunk(200, 0)
	defer release()

	srv.SpawnMob("minecraft:zombie", 200.5, 64, 0.5)
	if n := srv.MobCount(); n != 1 {
		t.Fatalf("the held chunk keeps %d mobs, want the planted zombie", n)
	}

	servertest.UntilTicks(t, srv, servertest.HostileTicks, "the mob to despawn", func() bool { return srv.MobCount() == 0 })
}
