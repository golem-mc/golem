package mobs

import (
	"fmt"
	"math"
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// The hostile tests: the first monster, driven the way the survival and mob
// tests drive everything else — through a real connection — with the world
// the flat one the harness grows, which grows no animals of its own, so
// every mob in these worlds is one the test planted. The zombie hunts, the
// sun burns it, the despawn takes it, and its blows go through the same
// player-damage pipeline a player's own do.

// TestABabyZombieCarriesTheBabyStateAndTheHaste turns the spawned zombie's
// age down and reads what the two tells of the wire carry: the metadata's
// own index a boolean on, and the Update Attributes the flip re-sends with
// the haste's modifier riding the pace — the name and the amount and the
// operation the vanilla modifier carries — and, grown again, the boolean
// off and the pace back to the bare base with no modifier at all.
func TestABabyZombieCarriesTheBabyStateAndTheHaste(t *testing.T) {
	t.Parallel()
	cfg := servertest.HostileTestConfig()

	srv, c, _ := servertest.JoinAs(t, cfg, "Watcher")

	zombieID := srv.SpawnMob("minecraft:zombie", 4.5, 64, 4.5)
	zombie := c.ExpectMobSpawn(protocol.EntityTypeZombie)
	if zombie.HasBaby {
		t.Error("a grown zombie's spawn metadata carried the baby flag")
	}

	srv.SetMobBaby(zombieID, true)

	// The flip's own tell: the metadata boolean on, and the attributes
	// re-sent with the haste's modifier beside the base.
	babySeen := false
	hasteSeen := false
	c.ReadTicksSkipping(srv, servertest.HostileTicks, servertest.HostileSkip, func(id int32, data []byte) bool {
		switch id {
		case protocol.IDPlayEntityMetadata:
			m := servertest.ReadZombieMetadata(c, data)
			if m.EntityID != zombieID {
				return false
			}
			if m.Baby {
				babySeen = true
			}
		case protocol.IDPlayUpdateAttributes:
			r := protocol.NewReader(data)
			if got := r.ReadVarInt(); got != zombieID {
				return false // another body's pace, read past
			}
			if got := r.ReadVarInt(); got != 1 {
				c.T.Fatalf("attribute count = %d, want the pace's one", got)
			}
			if got := r.ReadVarInt(); got != protocol.AttributeMovementSpeed {
				c.T.Errorf("attribute = %d, want %d (movement_speed)", got, protocol.AttributeMovementSpeed)
			}
			base := r.ReadF64()
			if base != float64(float32(0.23)) {
				c.T.Errorf("the baby zombie's base pace = %v, want the vanilla 0.23 the modifier rides", base)
			}
			modifiers := r.ReadVarInt()
			if modifiers != 1 {
				c.T.Errorf("the baby zombie's attribute carried %d modifiers, want the haste's one", modifiers)
			}
			for range modifiers {
				name := r.ReadString(256)
				if name != "minecraft:baby" {
					c.T.Errorf("the modifier's name = %q, want the vanilla baby's", name)
				}
				amount := r.ReadF64()
				if amount != 0.5 {
					c.T.Errorf("the baby modifier's amount = %v, want the vanilla half again", amount)
				}
				if op := r.ReadVarInt(); op != 1 {
					c.T.Errorf("the baby modifier's operation = %d, want the multiplied base's 1", op)
				}
			}
			if err := r.Err(); err != nil {
				c.T.Fatalf("the baby zombie's attributes: %v", err)
			}
			if r.Remaining() != 0 {
				c.T.Errorf("the baby zombie's attributes have %d bytes left over", r.Remaining())
			}
			hasteSeen = true
		}
		return babySeen && hasteSeen
	})
	if !babySeen {
		t.Error("the flip to a baby sent no metadata saying so")
	}
	if !hasteSeen {
		t.Error("the flip to a baby re-sent no attributes carrying the haste")
	}

	srv.SetMobBaby(zombieID, false)
	grownSeen := false
	calmSeen := false
	c.ReadTicksSkipping(srv, servertest.HostileTicks, servertest.HostileSkip, func(id int32, data []byte) bool {
		switch id {
		case protocol.IDPlayEntityMetadata:
			m := servertest.ReadZombieMetadata(c, data)
			if m.EntityID != zombieID {
				return false
			}
			if m.HasBaby && !m.Baby {
				grownSeen = true
			}
		case protocol.IDPlayUpdateAttributes:
			r := protocol.NewReader(data)
			if got := r.ReadVarInt(); got != zombieID {
				return false
			}
			if got := r.ReadVarInt(); got != 1 {
				c.T.Fatalf("attribute count = %d, want the pace's one", got)
			}
			if got := r.ReadVarInt(); got != protocol.AttributeMovementSpeed {
				c.T.Errorf("attribute = %d, want %d (movement_speed)", got, protocol.AttributeMovementSpeed)
			}
			if base := r.ReadF64(); base != float64(float32(0.23)) {
				c.T.Errorf("the grown zombie's base pace = %v, want the vanilla 0.23", base)
			}
			if got := r.ReadVarInt(); got != 0 {
				c.T.Errorf("the grown zombie's pace carried %d modifiers, want none", got)
			}
			calmSeen = true
		}
		return grownSeen && calmSeen
	})
	if !grownSeen {
		t.Error("the growing sent no metadata saying the flag off")
	}
	if !calmSeen {
		t.Error("the growing re-sent no attributes leaving the bare base")
	}
}

// TestAZombieHuntsAPlayerNearIt walks the whole hunt over the wire: the
// zombie put five blocks from a survival player takes them for its target —
// the aggressive bit of the Mob flags rising with the hunt — closes the
// distance, and lands its blow through the same pipeline a player's own
// hits run: the health update, the damage event naming the zombie, the
// flinch.
func TestAZombieHuntsAPlayerNearIt(t *testing.T) {
	t.Parallel()
	cfg := servertest.HostileTestConfig()

	srv, c, playerID := servertest.JoinAs(t, cfg, "Prey")

	zombieID := srv.SpawnMob("minecraft:zombie", 4.5, 64, 4.5)
	zombie := c.ExpectMobSpawn(protocol.EntityTypeZombie)
	if zombie.EntityID != zombieID {
		t.Fatalf("spawned zombie's entity ID = %d, want the one SpawnMob reported (%d)", zombie.EntityID, zombieID)
	}
	if zombie.Health != 20 {
		t.Errorf("spawned zombie's health = %v, want the vanilla twenty", zombie.Health)
	}
	if zombie.SpeedAttribute != float64(float32(0.23)) {
		t.Errorf("spawned zombie's speed = %v, want the vanilla 0.23", zombie.SpeedAttribute)
	}

	// The hunt: the aggressive bit rises, then the blow lands and the
	// pipeline says so — the health update, the damage event naming the
	// zombie.
	aggressive := false
	hurt := false
	damageEvent := false
	c.ReadTicksSkipping(srv, servertest.HostileTicks, servertest.HostileSkip, func(id int32, data []byte) bool {
		switch id {
		case protocol.IDPlayEntityMetadata:
			m := servertest.ReadZombieMetadata(c, data)
			if m.EntityID != zombieID {
				return false // another body's state, read past
			}
			if m.HasFlags && m.Flags&protocol.EntityFlagAggressive != 0 {
				aggressive = true
			}
		case protocol.IDPlaySetHealth:
			h := servertest.ExpectHealthReport(c, data)
			// The first blow's own health update is the one asserted; the
			// regeneration and the further blows after it say nothing
			// about the first.
			if h.Health < 20 && !hurt {
				hurt = true
				if h.Health != 17.5 {
					c.T.Errorf("the zombie's blow left the player at %v, want the easy difficulty's 17.5 of 20", h.Health)
				}
			}
		case protocol.IDPlayDamageEvent:
			r := protocol.NewReader(data)
			victim := r.ReadVarInt()
			if victim == playerID {
				if got, want := r.ReadVarInt(), registry.MustID("minecraft:damage_type", rules.DamageTypeMobAttack.Name()); got != want {
					c.T.Errorf("damage type ID = %d, want %d", got, want)
				}
				if got := r.ReadVarInt(); got != zombieID+1 {
					c.T.Errorf("damage cause ID = %d, want the zombie's %d", got, zombieID+1)
				}
				if got := r.ReadVarInt(); got != zombieID+1 {
					c.T.Errorf("damage direct ID = %d, want the zombie's %d", got, zombieID+1)
				}
				if got := r.ReadBool(); got {
					c.T.Error("damage event carries a position, which only /damage gives")
				}
				damageEvent = true
			}
		}
		return aggressive && hurt && damageEvent
	})
	if !aggressive {
		t.Error("the hunt never raised the aggressive bit of the Mob flags")
	}
	if !hurt {
		t.Error("the zombie never landed its blow")
	}
	if !damageEvent {
		t.Error("the blow never came with its damage event")
	}
}

// TestAZombiesWeaponRaisesItsBlow holds a monster's melee blow to the weapon
// in its main hand: a zombie summoned with an iron sword lands the kind's own
// three plus the blade's five, scaled by the easy difficulty to five, while a
// bare-handed one lands its base three alone. vanilla: Mob.doHurtTarget
// reading ATTACK_DAMAGE, LivingEntity.collectEquipmentChanges
func TestAZombiesWeaponRaisesItsBlow(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		held string
		want float32
	}{
		{"bare hand", "", 17.5},
		{"iron sword", "minecraft:iron_sword", 15},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := zombieBlowOnPreyHealth(t, tc.held); got != tc.want {
				t.Errorf("the %s zombie left its prey at %v of twenty, want %v", tc.name, got, tc.want)
			}
		})
	}
}

// zombieBlowOnPreyHealth stands a survival player, summons a zombie within its
// melee reach holding the named item — empty for a bare hand — and returns the
// health the prey's first blow left. The summoned zombie skips the spawn's own
// equipment roll, so its hand holds exactly what the tag names.
func zombieBlowOnPreyHealth(t *testing.T, held string) float32 {
	t.Helper()
	srv, connect := servertest.Start(t, servertest.HostileTestConfig(), servertest.OpsListing(t, "Prey"))
	srv.SetDayTime(18000) // midnight, whose dark keeps the daylight burn off the zombie

	c := connect()
	c.JoinAs("Prey")
	c.Settle()

	pos := srv.PlayerPosition("Prey")
	if pos == (player.Position{}) {
		t.Fatal("the player never arrived in the world")
	}
	equipment := ""
	if held != "" {
		equipment = fmt.Sprintf(` {equipment:{mainhand:{id:"%s",count:1}}}`, held)
	}
	c.ChatCommand(fmt.Sprintf("summon minecraft:zombie %.1f %.1f %.1f%s", pos.X+1.0, pos.Y, pos.Z, equipment))

	health := float32(20)
	c.ReadTicksSkipping(srv, servertest.HostileTicks, servertest.HostileSkip, func(id int32, data []byte) bool {
		if id != protocol.IDPlaySetHealth {
			return false
		}
		if h := servertest.ExpectHealthReport(c, data); h.Health < health {
			health = h.Health
		}
		return health < 20
	})
	return health
}

// TestAZombieChasesAcrossItsFollowRange holds the hunt to the follow
// range its path may wander: a zombie named a target twenty-five blocks
// off is to close the distance rather than stand and stare at
// twenty-five and a half. The flat world the harness grows carries the
// whole chase, so the hunt does not depend on the ground any one seed
// lays.
func TestAZombieChasesAcrossItsFollowRange(t *testing.T) {
	t.Parallel()
	cfg := servertest.HostileTestConfig()

	srv, c, _ := servertest.JoinAs(t, cfg, "Prey")

	// Midnight keeps the daylight burn out of the chase.
	srv.SetDayTime(18000)

	pos := srv.PlayerPosition("Prey")
	if pos == (player.Position{}) {
		t.Fatal("the player never arrived in the world")
	}

	zombieID := srv.SpawnMob("minecraft:zombie", pos.X+25, pos.Y, pos.Z)
	c.ExpectMobSpawn(protocol.EntityTypeZombie)

	// The hunt, read off the herd while the wire is kept alive: the
	// zombie is to come within the reach its blow needs of the player the
	// wire left standing still, over a distance only a plan made the full
	// way across can walk.
	if !c.PumpTicks(srv, 700, func(int32, []byte) bool {
		x, _, z, ok := srv.MobPos(zombieID)
		if !ok {
			t.Fatal("the zombie left the world before the hunt ended")
		}
		return math.Abs(x-pos.X) < 1.5 && math.Abs(z-pos.Z) < 1.5
	}) {
		x, y, z, _ := srv.MobPos(zombieID)
		t.Fatalf("the zombie never closed the distance; it stands at %.2f,%.2f,%.2f", x, y, z)
	}
}

// TestAMeleeHuntPressesIntoItsTarget hunts a zombie at a player standing still
// on the open flat and holds it to vanilla's opening search: MeleeAttackGoal's
// canUse plans createPath(target, 0) to the target's own cell, and
// PathNavigation.createPath answers every replan with that standing path while
// it walks the same block and is not done, so the zombie comes to rest in body
// contact rather than stopping at the navigation's usual reach of one.
func TestAMeleeHuntPressesIntoItsTarget(t *testing.T) {
	t.Parallel()
	cfg := servertest.HostileTestConfig()

	srv, connect := servertest.Start(t, cfg)
	srv.SetDayTime(18000) // midnight keeps the daylight burn out of the hunt
	c := connect()
	c.JoinAs("Prey")

	pos := srv.PlayerPosition("Prey")
	if pos == (player.Position{}) {
		t.Fatal("the player never arrived in the world")
	}

	// Twenty-five blocks off, the same span the follow-range chase crosses,
	// so the hunt is a path and not a single step.
	zombieID := srv.SpawnMob("minecraft:zombie", pos.X+25, pos.Y, pos.Z)
	c.ExpectMobSpawn(protocol.EntityTypeZombie)
	servertest.HoldTicks(t, srv)

	// The hunt closes the distance; once it has arrived, let it settle a
	// hundred ticks before reading the resting place, not while the walk is
	// still passing through the reach.
	settled := func() bool {
		x, _, z, ok := srv.MobPos(zombieID)
		return ok && math.Hypot(x-pos.X, z-pos.Z) <= 1.5
	}
	if !c.PumpTicks(srv, 700, func(int32, []byte) bool { return settled() }) {
		t.Fatal("the zombie never closed the distance to the player")
	}
	c.PumpTicks(srv, 100, func(int32, []byte) bool { return false })

	x, _, z, ok := srv.MobPos(zombieID)
	if !ok {
		t.Fatal("the zombie left the world before the hunt settled")
	}
	away := math.Hypot(x-pos.X, z-pos.Z)
	if away > 0.85 {
		t.Errorf("the hunt came to rest %.3f from the player, want the opening path to press it into body contact", away)
	}
	if away < 0.2 {
		t.Errorf("the hunt came to rest %.3f from the player, want the bodies kept apart", away)
	}
}

// TestAZombieHuntsThePlayerItsEyesRankNearest walks the hunt's choice of
// target over the wire between two players: the search ranks its candidates
// by the distance from the zombie's eye point to each player's feet, so the
// player five deep in a shaft loses the target to one six out on the floor —
// nearer by the eye's measure, farther by the feet — and the blow the
// zombie lands at the end of the hunt is the floor player's.
func TestAZombieHuntsThePlayerItsEyesRankNearest(t *testing.T) {
	t.Parallel()
	cfg := servertest.HostileTestConfig()

	srv, connect := servertest.Start(t, cfg)

	floorClient := connect()
	floorClient.JoinAs("Floor")
	floorClient.MoveToGround(7.5, 64, 0.5, true)

	// The shaft dug two cells east of where the zombie will stand, five
	// deep — past the three a path may drop, so nothing walks down it.
	// Dug after a join: an empty server has no chunks loaded to dig.
	for x := int32(2); x <= 3; x++ {
		for y := int32(59); y <= 63; y++ {
			if !srv.SetBlock(x, y, 0, 0) {
				t.Fatalf("digging the shaft's cell (%d, %d, 0) failed", x, y)
			}
		}
	}

	// One block a step keeps the descent the move steps report fall-less.
	shaftClient := connect()
	shaftClient.JoinAs("Below")
	shaftClient.MoveToGround(3.5, 64, 0.5, true)
	for y := 63.0; y >= 59; y-- {
		shaftClient.MoveToGround(3.5, y, 0.5, true)
	}

	// Midnight keeps the daylight burn out of the hunt.
	srv.SetDayTime(18000)

	srv.SpawnMob("minecraft:zombie", 1.5, 64, 0.5)

	// The hunt the wire reads: the zombie is to walk the six blocks to the
	// player on the floor and land its blow on them.
	if !floorClient.PumpTicks(srv, 250, func(id int32, data []byte) bool {
		if id != protocol.IDPlaySetHealth {
			return false
		}
		if h := servertest.ExpectHealthReport(floorClient, data); h.Health < 20 {
			return true
		}
		return false
	}) {
		t.Fatal("the zombie never bit the player on the floor, want the hunt to have taken them")
	}

	// And the player down the shaft is untouched: closer by the feet,
	// the shaft's depth loses them the ranking.
	shaftClient.ReadTicksSkipping(srv, 60, nil, func(id int32, data []byte) bool {
		if id == protocol.IDPlaySetHealth {
			if h := servertest.ExpectHealthReport(shaftClient, data); h.Health < 20 {
				t.Error("the zombie bit the player down the shaft, want the one on the floor")
			}
		}
		return false
	})
}

// TestAZombiesBlowMeetsTheArmourWorn checks the armour's share: the same
// blow against a player wearing a diamond chestplate leaves more of them
// than it would leave one wearing nothing.
func TestAZombiesBlowMeetsTheArmourWorn(t *testing.T) {
	t.Parallel()
	cfg := servertest.HostileTestConfig()

	srv, c := servertest.Join(t, cfg, "Armoured")

	chestplate, ok := registry.ItemID("minecraft:diamond_chestplate")
	if !ok {
		t.Fatal("the item registry has no diamond chestplate")
	}
	srv.GiveSlot("Armoured", player.ArmorSlot+1, protocol.ItemStack{ItemID: chestplate, Count: 1})

	srv.SpawnMob("minecraft:zombie", 4.5, 64, 4.5)

	hurt := false
	c.ReadTicksSkipping(srv, servertest.HostileTicks, servertest.HostileSkip, func(id int32, data []byte) bool {
		if id == protocol.IDPlaySetHealth {
			h := servertest.ExpectHealthReport(c, data)
			if h.Health < 20 {
				hurt = true
				// The easy difficulty's two and a half, with the
				// chestplate's eight points taking their vanilla share
				// off it: 18.2 left, give or take the rounding of the
				// wire's float.
				if h.Health <= 17.5 || h.Health >= 19 {
					c.T.Errorf("an armoured player took the blow down to %v, want the armour's share off 2.5", h.Health)
				}
				return true
			}
		}
		return false
	})
	if !hurt {
		t.Error("the zombie never landed its blow on the armoured player")
	}
}

// TestAZombiesBlowGrowsWithTheDifficulty checks the scaling: the same zombie
// against the same player on hard lands more than the easy world's two and
// a half, vanilla's half again.
func TestAZombiesBlowGrowsWithTheDifficulty(t *testing.T) {
	t.Parallel()
	cfg := servertest.HostileTestConfig()

	srv, connect := servertest.Start(t, cfg)
	srv.SetDifficulty(3) // hard
	c := connect().Join("Braver")

	srv.SpawnMob("minecraft:zombie", 4.5, 64, 4.5)

	hurt := false
	c.ReadTicksSkipping(srv, servertest.HostileTicks, servertest.HostileSkip, func(id int32, data []byte) bool {
		if id == protocol.IDPlaySetHealth {
			h := servertest.ExpectHealthReport(c, data)
			if h.Health < 20 {
				hurt = true
				if h.Health != 15.5 {
					c.T.Errorf("hard scaled the blow to %v of health left, want the 4.5 damage's 15.5", h.Health)
				}
				return true
			}
		}
		return false
	})
	if !hurt {
		t.Error("the zombie never landed its blow on the hard world")
	}
}

// TestACreativePlayerIsIgnored checks the modes a monster may not hunt: a
// creative player standing in the zombie's way draws no hunt, no aggressive
// bit and no blow, for as long as the test cares to watch.
func TestACreativePlayerIsIgnored(t *testing.T) {
	t.Parallel()
	cfg := servertest.HostileTestConfig()
	cfg.GameMode = "creative"

	srv, c := servertest.Join(t, cfg, "Untouchable")

	srv.SpawnMob("minecraft:zombie", 4.5, 64, 4.5)
	c.ExpectMobSpawn(protocol.EntityTypeZombie)

	c.ReadTicksSkipping(srv, 120, servertest.HostileSkip, func(id int32, data []byte) bool {
		switch id {
		case protocol.IDPlayEntityMetadata:
			m := servertest.ReadZombieMetadata(c, data)
			if m.HasFlags && m.Flags&protocol.EntityFlagAggressive != 0 {
				t.Error("a zombie turned aggressive on a creative player")
				return true
			}
		case protocol.IDPlaySetHealth:
			h := servertest.ExpectHealthReport(c, data)
			if h.Health < 20 {
				t.Errorf("a creative player was hurt down to %v, want them untouched", h.Health)
				return true
			}
		}
		return false
	})
}

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

// TestAZombieHuntsToTheFootOfAPillarItCannotClimb walks the unreachable hunt
// over the wire: a survival player standing four blocks up a stone pillar
// keeps the zombie's hunt, and the search that cannot reach them still
// leaves the way to the closest point of it — so the zombie crosses the ten
// open blocks and stands at the pillar's foot, too far below for a blow.
func TestAZombieHuntsToTheFootOfAPillarItCannotClimb(t *testing.T) {
	t.Parallel()
	cfg := servertest.HostileTestConfig()

	srv, connect := servertest.Start(t, cfg)
	srv.SetDayTime(18000) // midnight, whose dark keeps the daylight burn out of the hunt
	c := connect().Join("Perched")

	// A pillar four high on the flat ground: a top to stand on, and no
	// walker's way up it.
	stone, ok := registry.FindState("minecraft:stone", nil)
	if !ok {
		t.Fatal("the block registry has no stone")
	}
	for y := int32(64); y <= 67; y++ {
		srv.SetBlock(4, y, 0, stone)
	}
	c.MoveToGround(4.5, 68, 0.5, true)
	c.Settle()

	zombieID := srv.SpawnMob("minecraft:zombie", -5.5, 64, 0.5)
	c.ExpectMobSpawn(protocol.EntityTypeZombie)

	// The hunt closes the ten blocks to the pillar's foot, and stands
	// there — on the ground below, never on the unclimbable top.
	servertest.WaitForTicks(t, srv, 400, func() bool {
		x, y, z, ok := srv.MobPos(zombieID)
		return ok && y < 66 && math.Hypot(x-4.5, z-0.5) < 2.5
	})
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

// TestAZombieReplansAroundAFenceGateRedstoneCloses: a fence gate the
// redstone wake shuts across a zombie's standing path is walked around, not
// pressed at. The torch's own cell has no collision either way, so only the
// gate, a cell the physics cascade writes, can start the replan.
// vanilla: ServerLevel.sendBlockUpdated, PathNavigation.shouldRecomputePath
func TestAZombieReplansAroundAFenceGateRedstoneCloses(t *testing.T) {
	t.Parallel()
	cfg := servertest.HostileTestConfig()

	// /fill and /setblock are operator's commands.
	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Builder"))
	srv.SetDayTime(18000)
	c := connect().Join("Builder")
	c.MoveToGround(12.5, 64, 0.5, true)
	c.Settle()

	// A wall at x=4 with a gap at z=5 and the gate at z=0, which the torch
	// past it holds open.
	c.ChatCommand("fill 4 64 -6 4 65 6 minecraft:stone")
	c.ChatCommand("fill 4 64 5 4 65 5 minecraft:air")
	servertest.Setblock(t, c, 5, 64, 0, "redstone_torch")
	servertest.Setblock(t, c, 4, 64, 0, "oak_fence_gate[facing=east,open=true,powered=true]")
	servertest.Setblock(t, c, 4, 65, 0, "air")

	zombieID := srv.SpawnMob("minecraft:zombie", -5.5, 64, 0.5)
	c.ExpectMobSpawn(protocol.EntityTypeZombie)
	servertest.HoldTicks(t, srv)
	servertest.WaitForTicks(t, srv, 200, func() bool {
		x, _, _, ok := srv.MobPos(zombieID)
		return ok && x > -2
	})
	c.ChatCommand("setblock 5 64 0 minecraft:air")
	c.Settle()
	servertest.ExpectBlockNamed(t, srv, 4, 64, 0, "minecraft:oak_fence_gate", map[string]string{"open": "false"})
	// The zombie crosses in 78 ticks with the replan and is still pressing
	// at the gate after 400 without it.
	servertest.WaitForTicks(t, srv, 120, func() bool {
		x, _, _, ok := srv.MobPos(zombieID)
		return ok && x > 5
	})
}

// TestARoofedZombieDoesNotBurn checks the sun's other half: the same noon,
// with the zombie penned under a stone roof that takes the sky light its
// eye reads away, lights nothing at all.
func TestARoofedZombieDoesNotBurn(t *testing.T) {
	t.Parallel()
	cfg := servertest.HostileTestConfig()
	cfg.GameMode = "creative"

	// The noon the test stands the zombie in is set by a /time, whose
	// runner is listed as an operator.
	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Cellar"), func(s *server.Server) { s.SetTimeSync(0) })
	srv.SetPaused(true)
	c := connect().Join("Cellar")
	c.ChatCommand("time set noon")
	c.Settle()

	// A sealed pen: two-high walls on the ring of the zombie's own cell
	// and a stone roof over the lot, so the zombie inside has neither the
	// sky light to burn by nor a stroll that carries it back out into the
	// sun.
	stone, ok := registry.FindState("minecraft:stone", nil)
	if !ok {
		t.Fatal("the block registry has no stone")
	}
	for dx := int32(3); dx <= 5; dx++ {
		for dz := int32(3); dz <= 5; dz++ {
			edge := dx == 3 || dx == 5 || dz == 3 || dz == 5
			if edge {
				srv.SetBlock(dx, 64, dz, stone)
				srv.SetBlock(dx, 65, dz, stone)
			}
			srv.SetBlock(dx, 66, dz, stone)
		}
	}
	srv.RelightAt(4, 4)

	zombieID := srv.SpawnMob("minecraft:zombie", 4.5, 64, 4.5)
	c.ReadTicksSkipping(srv, 160, servertest.HostileSkip, func(id int32, data []byte) bool {
		if id == protocol.IDPlayEntityMetadata {
			m := servertest.ReadZombieMetadata(c, data)
			if m.EntityID != zombieID {
				return false
			}
			if m.HasFire && m.OnFire {
				t.Error("a zombie under a roof caught fire")
				return true
			}
		}
		return false
	})
}

// TestASunburningZombieFollowsTheDusksLight checks the hour's half of the
// daylight burn: the dusk's open still lights the eye, its first darkening
// leaves a raw light of twelve that the float32 curve still burns at, and a
// later darkening under that raw light spares the zombie — as does the
// dawn's. The clock is held still, so the tick the test names is the tick the
// burn is read at.
func TestASunburningZombieFollowsTheDusksLight(t *testing.T) {
	t.Parallel()
	for _, k := range []struct {
		name string
		tick int64
		burn bool
	}{
		{"the dusk still lights", 12100, true},
		{"the dusk's twelve still burns", 12400, true},
		{"the dusk's dark spares", 12530, false},
		{"the dawn's twelve still burns", 23600, true},
		{"the dawn's dark spares", 23470, false},
	} {
		t.Run(k.name, func(t *testing.T) {
			cfg := servertest.HostileTestConfig()
			cfg.GameMode = "creative"
			srv, connect := servertest.Start(t, cfg)
			srv.StopTick()
			srv.SetPaused(true)
			srv.SetDayTime(k.tick)
			release := srv.HoldChunk(4, 4)
			t.Cleanup(release)

			c := connect().Join("Sunwatcher")

			zombieID := srv.SpawnMob("minecraft:zombie", 4.5, 64, 4.5)

			// The fire only catches on the dice's good face; a few hundred
			// ticks of open sky cover the light that carries.
			srv.TickMobs(400)
			lit := false
			c.SettleAndCollect(func(id int32, data []byte) {
				if id != protocol.IDPlayEntityMetadata {
					return
				}
				if m := servertest.ReadZombieMetadata(c, data); m.EntityID == zombieID && m.HasFire && m.OnFire {
					lit = true
				}
			})
			if lit != k.burn {
				t.Errorf("at tick %d a zombie under open sky lit = %v, want %v", k.tick, lit, k.burn)
			}
		})
	}
}

// TestAStandingZombieKeepsItsNoondayFire checks the refresh half of the
// burn: the eight seconds of igniteForSeconds(8) would run down on their
// own, but a body still in the sun rolls isSunBurnTick again each tick, so
// the on-fire bit never falls while the noon stands.
func TestAStandingZombieKeepsItsNoondayFire(t *testing.T) {
	t.Parallel()
	cfg := servertest.HostileTestConfig()
	cfg.GameMode = "creative"
	srv, connect := servertest.Start(t, cfg)
	srv.StopTick()
	srv.SetPaused(true)
	srv.SetDayTime(6000) // noon
	release := srv.HoldChunk(4, 4)
	t.Cleanup(release)

	c := connect().Join("Noonkeeper")

	zombieID := srv.SpawnMob("minecraft:zombie", 4.5, 64, 4.5)

	// Light it: the fire only catches on the dice's good face.
	lit := false
	for i := 0; i < 60 && !lit; i++ {
		srv.TickMobs(10)
		c.SettleAndCollect(func(id int32, data []byte) {
			if id != protocol.IDPlayEntityMetadata {
				return
			}
			if m := servertest.ReadZombieMetadata(c, data); m.EntityID == zombieID && m.HasFire && m.OnFire {
				lit = true
			}
		})
	}
	if !lit {
		t.Fatal("the noonday sun never lit the zombie")
	}

	// Keep it in the noon: the on-fire bit must never fall, which the fire
	// would do after its eight seconds were the burn not rolled afresh.
	out := false
	srv.TickMobs(340)
	c.SettleAndCollect(func(id int32, data []byte) {
		if id != protocol.IDPlayEntityMetadata {
			return
		}
		if m := servertest.ReadZombieMetadata(c, data); m.EntityID == zombieID && m.HasFire && !m.OnFire {
			out = true
		}
	})
	if out {
		t.Error("the zombie's noonday fire ran down while the sun still stood")
	}
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

	zombieID := srv.SpawnMob("minecraft:zombie", 2.5, 64, 1.5)
	// The loot rolls from a stream of its own, which the test's seed fixes
	// however long the zombie wanders first; this one rolls a body's worth
	// of flesh to watch.
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
	for _, stack := range drops {
		name, _ := registry.ItemName(stack.ItemID)
		if stack.ItemID != flesh {
			if !rare[name] || stack.Count != 1 {
				t.Errorf("the dead zombie dropped %d %s, want rotten flesh or one of the rare drops", stack.Count, name)
			}
			continue
		}
		if stack.Count < 1 || stack.Count > 2 {
			t.Errorf("the dead zombie dropped %d rotten flesh, want the table's range of one to two", stack.Count)
		}
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

// TestAShulkerFarFromEveryPlayerDoesNotDespawn: a shulker is a golem, so the
// wait that removes the zombie above leaves it standing two hundred blocks
// from the only player.
// vanilla: AbstractGolem.removeWhenFarAway
func TestAShulkerFarFromEveryPlayerDoesNotDespawn(t *testing.T) {
	t.Parallel()
	cfg := servertest.HostileTestConfig()

	srv, _ := servertest.Join(t, cfg, "Lighthouse")

	// The chunk is held, since a chunk nobody plays in is given up to its
	// entity file within the tick and the despawn never gets its word in.
	release := srv.HoldChunk(200, 0)
	defer release()

	servertest.HoldTicks(t, srv)
	srv.SpawnMob("minecraft:shulker", 200.5, 64, 0.5)
	if n := srv.MobCount(); n != 1 {
		t.Fatalf("the held chunk keeps %d mobs, want the planted shulker", n)
	}

	servertest.RunTicks(t, srv, servertest.HostileTicks)
	if ids := srv.MobIDsOfKind("minecraft:shulker"); len(ids) != 1 {
		t.Fatalf("after %d ticks the world keeps shulkers %v, want the planted one", servertest.HostileTicks, ids)
	}
}

// TestAZombieBesideASpectatorStillDespawns checks that the despawn leaves a
// spectator out of its nearest-player measurement: a zombie beside a
// spectator is still past the despawn distance from the only non-spectator
// player and goes at once.
// vanilla: Mob.checkDespawn's getNearestPlayer with EntitySelector.NO_SPECTATORS
func TestAZombieBesideASpectatorStillDespawns(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.HostileTestConfig(), servertest.OpsListing(t, "Keeper"))
	// Night, so the zombie's own death by daylight is not what empties the
	// herd.
	srv.SetDayTime(18000)

	keeper := connect().Join("Keeper")

	watcher := connect().Join("Watcher")

	// The chunk is held, since a chunk nobody plays in is given up to its
	// entity file within the tick and the despawn never gets its word in.
	release := srv.HoldChunk(200, 0)
	defer release()

	// The spectator stands beside the zombie; the only non-spectator player
	// is two hundred blocks away at the spawn.
	keeper.ChatCommand("gamemode spectator Watcher")
	keeper.Settle()
	keeper.ChatCommand("tp Watcher 200.5 64 1.5")
	keeper.Settle()
	watcher.Settle()
	servertest.WaitFor(t, srv, 1200, func() bool {
		return srv.PlayerGameMode("Watcher") == protocol.GameModeSpectator &&
			srv.PlayerPosition("Watcher").X > 199
	})

	srv.SpawnMob("minecraft:zombie", 200.5, 64, 2.5)
	if n := srv.MobCount(); n != 1 {
		t.Fatalf("the held chunk keeps %d mobs, want the planted zombie", n)
	}

	servertest.UntilTicks(t, srv, servertest.HostileTicks, "the mob to despawn", func() bool { return srv.MobCount() == 0 })
}
