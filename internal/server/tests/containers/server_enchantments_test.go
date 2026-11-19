package containers

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"math"
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// The enchantment effects, seen from the wire: a stack's minecraft:
// enchantments component coming out of the attacks, the wear, the shots and
// the breath the effects are wired into. The numbers are vanilla's own,
// checked against the vanilla server live in the summary this work ships
// with; the tests here pin the behaviour this server answers with, each of
// them failing on the server this file was written against, where the
// enchantments rode along as unread components.

// nightWorld is the setup the mob tests run in: the day cycle stopped at
// servertest.Midnight, so the daylight burn no mob of these tests is due stands out of
// the way and the burn the enchantments light is the only fire a body
// carries.
func nightWorld(srv *server.Server, c *servertest.Client) {
	// Set through the server rather than /time: the player here is no
	// operator, and a refused command would leave the noon a zombie burns
	// in.
	srv.SetPaused(true)
	srv.SetDayTime(servertest.Midnight)
	c.Settle()
}

// awaitFullDraw waits for the server's count of the draw's ticks to pass the
// twenty of a full draw, the power reading off it being the capped one.
func awaitFullDraw(srv *server.Server, c *servertest.Client, name string) {
	c.T.Helper()
	servertest.UntilTicks(c.T, srv, 100, "the bow draw reaching its full twenty ticks", func() bool {
		return srv.BowTicks(name) >= 24
	})
}

// groundThePlayer reports the player standing on the ground at the spawn,
// the movement a blow is read against: without it the server has never been
// told the body is on its feet, and the swing lands as the falling
// critical's half again.
func groundThePlayer(c *servertest.Client) {
	c.T.Helper()
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()
}

// mobMetadataID is the entity ID a Set Entity Metadata packet opens with,
// the check that decides whether the packet is one of the mob's to read.
func mobMetadataID(data []byte) int32 {
	return protocol.NewReader(data).ReadVarInt()
}

// TestSharpnessAddsItsDamageToAMob swings a sharpness V iron sword at a
// zombie and reads the health the blow left: the sword's own six plus
// sharpness's three, the nine the zombie's two points of armour turn to
// eight and eight hundred fifty-six thousandths of the vanilla formula —
// eleven and a hundred forty-four thousandths left of the twenty, where a
// plain sword would have left more.
func TestSharpnessAddsItsDamageToAMob(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalConfig(), "Slayer", func(s *server.Server) { s.SetTimeSync(0) })
	c.MarkLoaded()
	groundThePlayer(c)
	nightWorld(srv, c)

	srv.GiveSlot("Slayer", player.HotbarSlot, servertest.EnchantedStack(t, "minecraft:iron_sword", "minecraft:sharpness", 5))

	zombieID := srv.SpawnMob("minecraft:zombie", 2.5, 64, 0.5)
	c.ExpectMobSpawn(protocol.EntityTypeZombie)
	servertest.AwaitFullSwing(t, srv, "Slayer")
	c.Attack(zombieID)

	// The first health the zombie is seen carrying below its twenty is
	// the hit's; the retaliation and the walking after it say nothing
	// about what the swing took off.
	c.ReadTicksSkipping(srv, servertest.HostileTicks, servertest.HostileSkip, func(id int32, data []byte) bool {
		if id != protocol.IDPlayEntityMetadata || mobMetadataID(data) != zombieID {
			return false
		}
		m := servertest.ReadZombieMetadata(c, data)
		if !m.HasHealth || m.Health >= 20 {
			return false
		}
		if math.Abs(float64(m.Health)-11.144) > 1e-5 {
			c.T.Errorf("the zombie's health after a sharpness V hit = %v, want the armoured 11.144 of twenty", m.Health)
		}
		return true
	})
}

// TestSmiteAddsItsDamageToTheUndead swings a smite V iron sword at a zombie
// and at a cow, the blow carrying the enchantment's two and a half a level
// against the undead body and nothing against the cow.
func TestSmiteAddsItsDamageToTheUndead(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalConfig(), "Cleric", func(s *server.Server) { s.SetTimeSync(0) })
	c.MarkLoaded()
	groundThePlayer(c)
	nightWorld(srv, c)

	srv.GiveSlot("Cleric", player.HotbarSlot, servertest.EnchantedStack(t, "minecraft:iron_sword", "minecraft:smite", 5))

	zombieID := srv.SpawnMob("minecraft:zombie", 2.5, 64, 0.5)
	c.ExpectMobSpawn(protocol.EntityTypeZombie)
	servertest.AwaitFullSwing(t, srv, "Cleric")
	c.Attack(zombieID)
	// Smite V: six and twelve and a half, the eighteen and a half the
	// zombie's two points of armour turn to eighteen and two hundred
	// four thousandths - the body standing on the one and seven hundred
	// ninety-six thousandths the blow leaves of its twenty, the way the
	// vanilla one does: an iron sword's smite V is not a killing blow
	// against the armoured body.
	sawLow := false
	c.ReadTicksSkipping(srv, servertest.HostileTicks, servertest.HostileSkip, func(id int32, data []byte) bool {
		if id != protocol.IDPlayEntityMetadata || mobMetadataID(data) != zombieID {
			return false
		}
		m := servertest.ReadZombieMetadata(c, data)
		if !m.HasHealth || m.Health >= 20 {
			return false
		}
		if math.Abs(float64(m.Health)-1.796) > 1e-5 {
			c.T.Errorf("the zombie's health after a smite V hit = %v, want the armoured 1.796 of twenty", m.Health)
		}
		sawLow = true
		return true
	})
	if !sawLow {
		t.Error("a smite V blow never left the zombie standing on its last two health")
	}

	// The cow, outside the tag, takes the plain blow and lives - ten
	// points of health being a cow's whole worth, four after the sword's
	// six.
	cowID := srv.SpawnMob("minecraft:cow", 2.5, 64, 0.5)
	// The struck zombie turns on the player meanwhile, and its flags and
	// steps are the pump's to step over on the way to the cow's arrival.
	cowArrived := false
	c.ReadTicksSkipping(srv, servertest.HostileTicks, servertest.HostileSkip, func(id int32, data []byte) bool {
		if id == protocol.IDPlaySpawnEntity && protocol.NewReader(data).ReadVarInt() == cowID {
			cowArrived = true
		}
		return cowArrived
	})
	if !cowArrived {
		t.Fatal("the cow never arrived")
	}
	servertest.AwaitFullSwing(t, srv, "Cleric")
	c.Attack(cowID)
	c.ReadTicksSkipping(srv, servertest.HostileTicks, servertest.HostileSkip, func(id int32, data []byte) bool {
		if id != protocol.IDPlayEntityMetadata || mobMetadataID(data) != cowID {
			return false
		}
		m := servertest.ReadZombieMetadata(c, data)
		if !m.HasHealth || m.Health >= 10 {
			return false
		}
		if m.Health != 4 {
			c.T.Errorf("the cow's health after a smite V hit = %v, want the plain sword's six off of its ten", m.Health)
		}
		return true
	})
}

// TestProtectionIVTurnsTheBlow punches a player wearing a protection IV
// diamond chestplate and reads their health: the armour's own share of the
// blow, then the enchantment's four points of protection taking four
// twenty-fifths of what is left.
func TestProtectionIVTurnsTheBlow(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.SurvivalConfig())

	victim := connect()
	knightID := victim.JoinAs("Knight")

	attacker := connect()
	attackerID := attacker.JoinAs("Brute")

	victim.ExpectPlayerAdded("Brute")
	victim.ExpectAnnouncement("multiplayer.player.joined", "Brute")

	attacker.MoveToGround(0.5, 64, 0.5, true)
	victim.MoveToGround(3.5, 64, 0.5, true)
	attacker.Settle()
	victim.Settle()
	srv.SetFood("Knight", 17)

	srv.GiveSlot("Knight", player.ArmorSlot+1, servertest.EnchantedStack(t, "minecraft:diamond_chestplate", "minecraft:protection", 4))

	servertest.AwaitFullSwing(t, srv, "Brute")
	attacker.Send(protocol.IDPlayAttackIn, func(b *protocol.Buffer) { b.PutVarInt(knightID) })

	got := victim.ExpectHealthAfterWear()
	// The armour first: eight points and two of toughness against a
	// one-point blow, the points side's seven and six tenths of
	// twenty-fifths taking most of thirty percent off. The protection
	// then: four points, four twenty-fifths of what is left.
	armour := 1 * (1 - 7.6/25)
	want := float32(20 - armour*(1-4.0/25))
	if math.Abs(float64(got.Health-want)) > 1e-5 {
		t.Errorf("the knight's health = %v, want %v (the armour's share, then the enchantment's)", got.Health, want)
	}
	victim.ExpectDamageEvent(knightID, "minecraft:player_attack", attackerID+1)
}

// TestFeatherFallingCushionsTheFall drops a player wearing feather falling
// IV boots the six blocks a plain body takes three half-hearts for and reads
// what is left: the fall's twelve points of enchantment protection,
// twenty-fifths off a fall the armour never sees.
func TestFeatherFallingCushionsTheFall(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalConfig(), "Jumper")
	c.MarkLoaded()
	srv.SetFood("Jumper", 17)

	srv.GiveSlot("Jumper", player.ArmorSlot+3, servertest.EnchantedStack(t, "minecraft:diamond_boots", "minecraft:feather_falling", 4))

	// Six blocks up, then down, a step at a time the way a falling body
	// reports them: the three free blocks and the three that cost half a
	// heart apiece, whose damage the boots' twelve points of enchantment
	// protection take their share of.
	c.MoveToGround(0.5, 70, 0.5, false)
	c.MoveToGround(0.5, 67, 0.5, false)
	c.MoveToGround(0.5, 64, 0.5, true)

	// No settle here: it would read past the hurt's health when that comes
	// in ahead of the ping's answer, and the wait below reads to it anyway.
	c.AwaitPlay(protocol.IDPlayLevelParticles)
	got := c.ExpectHealthAfterWear()
	// Fall damage of six blocks is three; the enchantment's twelve
	// points take twelve twenty-fifths off it.
	want := float32(20 - 3*(1-12.0/25))
	if math.Abs(float64(got.Health-want)) > 1e-5 {
		t.Errorf("the jumper's health = %v, want %v (the fall's three, cut by the boots' twelve points)", got.Health, want)
	}
}

// findFirstRoll walks the seeds until the first roll of one lands on the
// side of the bound the test asks for, the first Float64 of the source
// being what a single point of wear rolls against.
func findFirstRoll(below bool, bound float64) int64 {
	for seed := range int64(100000) {
		roll := rand.New(rand.NewSource(seed)).Float64()
		if (roll < bound) == below {
			return seed
		}
	}
	return -1
}

// TestUnbreakingSparesTheWear digs two stone blocks with an unbreaking III
// iron pickaxe, the wear dice seeded: the first dig's point of wear dropped
// by the roll, the second's kept. A plain pickaxe would have taken its one
// both times.
func TestUnbreakingSparesTheWear(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalConfig(), "Quarrier")
	c.MarkLoaded()
	groundThePlayer(c)

	srv.GiveSlot("Quarrier", player.HotbarSlot, servertest.EnchantedStack(t, "minecraft:iron_pickaxe", "minecraft:unbreaking", 3))

	// The tool's chance is three quarters at level three; a seed whose
	// first roll lands under it drops the dig's point of wear.
	spared := findFirstRoll(true, 0.75)
	taken := findFirstRoll(false, 0.75)

	srv.SetWearSeed(spared)
	c.BreakBlock(1, 63, 1)
	// An iron pickaxe takes eight ticks to the stone, so the break and the
	// wear it lands land behind the click.
	c.WaitBlockChanged()
	if stack := srv.InventorySlot("Quarrier", player.HotbarSlot); stack.Damage != 0 {
		t.Errorf("the pickaxe's wear after a spared dig = %d, want none", stack.Damage)
	}

	srv.SetWearSeed(taken)
	c.BreakBlock(2, 63, 2)
	c.WaitBlockChanged()
	if stack := srv.InventorySlot("Quarrier", player.HotbarSlot); stack.Damage != 1 {
		t.Errorf("the pickaxe's wear after a kept dig = %d, want the one the roll kept", stack.Damage)
	}
}

// TestFireAspectSetsTheVictimBurning swings a fire aspect II sword at a
// zombie and waits for the on-fire bit the burn shows in: the eight seconds
// of fire the enchantment's post-attack ignite grants.
func TestFireAspectSetsTheVictimBurning(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalConfig(), "Ember", func(s *server.Server) { s.SetTimeSync(0) })
	c.MarkLoaded()

	srv.GiveSlot("Ember", player.HotbarSlot, servertest.EnchantedStack(t, "minecraft:iron_sword", "minecraft:fire_aspect", 2))

	zombieID := srv.SpawnMob("minecraft:zombie", 2.5, 64, 0.5)
	c.ExpectMobSpawn(protocol.EntityTypeZombie)
	servertest.AwaitFullSwing(t, srv, "Ember")
	c.Attack(zombieID)

	burning := false
	c.ReadTicksSkipping(srv, servertest.HostileTicks, servertest.HostileSkip, func(id int32, data []byte) bool {
		if id != protocol.IDPlayEntityMetadata || mobMetadataID(data) != zombieID {
			return false
		}
		m := servertest.ReadZombieMetadata(c, data)
		if m.HasFire && m.OnFire {
			burning = true
		}
		return burning
	})
	if !burning {
		t.Error("a fire aspect II hit left the zombie unburnt")
	}
}

// TestBaneOfArthropodsSlowsTheArthropodItsBlowLandsOn swings a bane of
// arthropods III sword at a spider and reads the post-attack slowness the
// data pack's apply_mob_effect leaves on it: Slowness IV, its seconds rolled
// between the effect's ends — thirty to fifty ticks at level three — told to
// whoever sees the spider as the swarm of particles its metadata carries, and
// in no Update Mob Effect, which a mob never gets. A zombie hit with the same
// sword takes the plain blow and is left nothing.
func TestBaneOfArthropodsSlowsTheArthropodItsBlowLandsOn(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.MobTestConfig(), "Squasher", func(s *server.Server) { s.SetTimeSync(0) })
	nightWorld(srv, c)

	srv.GiveSlot("Squasher", player.HotbarSlot,
		servertest.EnchantedStack(t, "minecraft:iron_sword", "minecraft:bane_of_arthropods", 3))

	spiderID := srv.SpawnMob("minecraft:spider", 2.5, 64, 0.5)
	spider := c.ExpectMobSpawn(protocol.EntityTypeSpider)
	if spider.EntityID != spiderID || spider.Health != 16 {
		t.Fatalf("the planted spider spawned as %+v (id %d), want the sixteen of health it was planted with", spider, spiderID)
	}
	zombieID := srv.SpawnMob("minecraft:zombie", -1.5, 64, 0.5)
	c.ExpectMobSpawn(protocol.EntityTypeZombie)

	servertest.AwaitFullSwing(t, srv, "Squasher")
	srv.StopTick()
	c.Attack(spiderID)

	// Slowness's own swarm: the effect's colour at full alpha, 0x8BAFE0 in
	// the table MobEffects registers it with.
	slowness := [2]int32{protocol.ParticleEntityEffect, -0x1000000 | 0x8BAFE0}
	bodies := map[int32]bool{spiderID: true, zombieID: true}
	swarmTold, effectPackets := false, 0
	var swarm [][2]int32
	c.SettleAndCollect(func(id int32, data []byte) {
		switch id {
		case protocol.IDPlayEntityMetadata:
			if m, mine := servertest.ReadBodyMetadata(c, data, bodies); mine && m.EntityID == spiderID && m.HasParticles {
				swarm, swarmTold = m.Particles, true
			}
		case protocol.IDPlayUpdateMobEffect:
			effectPackets++
		}
	})
	if !swarmTold {
		t.Error("the bane of arthropods blow left no slowness swarm on the spider's metadata")
	} else if len(swarm) != 1 || swarm[0] != slowness {
		t.Errorf("the slowed spider's particles = %v, want slowness's own %v", swarm, slowness)
	}
	if effectPackets != 0 {
		t.Errorf("the slowed spider was sent %d effect packets, which a mob never gets", effectPackets)
	}
	if duration, amplifier, ok := srv.MobEffectOf(spiderID, "minecraft:slowness"); !ok {
		t.Error("the bane of arthropods blow left no slowness on the spider")
	} else if amplifier != 3 || duration < 30 || duration > 50 {
		t.Errorf("the spider's slowness = amplifier %d for %d ticks, want Slowness IV for the 30 to 50 of a level III roll",
			amplifier, duration)
	}

	// The zombie, outside the tag, takes the plain blow and is left
	// nothing: no slowness in the body, and no swarm in its metadata.
	servertest.RecoverByHand(t, srv, "Squasher")
	c.Attack(zombieID)
	zombieHurt, zombieSwarmTold := false, false
	c.SettleAndCollect(func(id int32, data []byte) {
		if id != protocol.IDPlayEntityMetadata {
			return
		}
		if m, mine := servertest.ReadBodyMetadata(c, data, bodies); mine && m.EntityID == zombieID {
			zombieHurt = zombieHurt || m.HasHealth && m.Health < 20
			zombieSwarmTold = zombieSwarmTold || m.HasParticles && len(m.Particles) > 0
		}
	})
	if !zombieHurt {
		t.Fatal("the bane of arthropods blow never landed on the zombie")
	}
	if zombieSwarmTold {
		t.Error("the zombie outside the tag was told a slowness swarm")
	}
	if _, _, ok := srv.MobEffectOf(zombieID, "minecraft:slowness"); ok {
		t.Error("the zombie outside the tag took a slowness from the bane of arthropods sword")
	}
}

// bowShotDraws fires a bow the way a client plays one: the step to where the
// shot is taken from, facing the yaw given, the use beginning the draw - its
// packet carrying the same aim, the one the server reads the shot's
// direction from - the wait taking it past the twenty ticks of a full draw,
// the release letting go.
func bowShotDraws(srv *server.Server, c *servertest.Client, name string, x, z float64, yaw float32) {
	c.T.Helper()
	c.Send(protocol.IDPlayPositionAndRotation, func(b *protocol.Buffer) {
		b.PutF64(x)
		b.PutF64(64)
		b.PutF64(z)
		b.PutF32(yaw)
		b.PutF32(0)
		b.PutU8(0x01)
	})
	c.Settle()
	c.Send(protocol.IDPlayUseItem, func(b *protocol.Buffer) {
		b.PutVarInt(protocol.HandMain)
		b.PutVarInt(1) // sequence
		b.PutF32(yaw)  // the aim the shot leaves along
		b.PutF32(0)
	})
	awaitFullDraw(srv, c, name)
	c.ReleaseUseItem()
}

// TestPowerBowAddsItsDamage shoots a power V bow at a zombie at the full
// draw and reads what is left standing: the shot's own six-to-eleven blown
// past the fifteen the power's three adds its three to the arrow's base
// damage for. A plain bow's control shot at a second zombie stays where the
// base puts it.
func TestPowerBowAddsItsDamage(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalConfig(), "Archer", func(s *server.Server) { s.SetTimeSync(0) })
	c.MarkLoaded()
	nightWorld(srv, c)

	srv.GiveSlot("Archer", player.HotbarSlot, servertest.EnchantedStack(t, "minecraft:bow", "minecraft:power", 5))
	arrow, _ := registry.ItemID("minecraft:arrow")
	srv.GiveSlot("Archer", player.HotbarSlot+1, protocol.ItemStack{ItemID: arrow, Count: 16})

	// The first zombie stands east of the archer; the shot is drawn full
	// and loosed at it.
	powerZombie := srv.SpawnMob("minecraft:zombie", 2.5, 64, 0.5)
	c.ExpectMobSpawn(protocol.EntityTypeZombie)
	bowShotDraws(srv, c, "Archer", 0.5, 0.5, -90)

	// The damage is fifteen and more, the crit roll's zero to seven on
	// top: through the zombie's two points of armour the no-crit roll
	// leaves no more than the five and a quarter of the vanilla formula,
	// and a roll past twenty-one is the body dead of the blow.
	// A blow that kills reads health nought before the death's own event,
	// and the pump waits for that event too, so it does not land among
	// the reads of the control below.
	low, killed, dead := false, false, false
	c.ReadTicksSkipping(srv, servertest.HostileTicks, servertest.HostileSkip, func(id int32, data []byte) bool {
		switch id {
		case protocol.IDPlayEntityMetadata:
			if mobMetadataID(data) != powerZombie {
				return false
			}
			m := servertest.ReadZombieMetadata(c, data)
			if m.HasHealth && m.Health <= 5.24+1e-5 {
				low = true
				killed = killed || m.Health <= 0
			}
		case protocol.IDPlayEntityEvent:
			r := protocol.NewReader(data)
			if got := r.ReadI32(); got == powerZombie && r.ReadU8() == protocol.EntityStatusDeath {
				low, dead = true, true
			}
		}
		return low && (!killed || dead)
	})
	if !low {
		t.Error("a power V shot left the zombie standing above what its armour turns fifteen to")
	}

	// The control: a plain bow - the same shot, no enchantment - leaves a
	// second zombie standing above the nine its six-to-eleven cannot go
	// under. The archer takes twelve steps north first, out of the first
	// zombie's reach and off its line, and the second zombie stands four
	// blocks north of them there, the shot facing north (yaw 180).
	c.MoveToGround(0.5, 64, -10.5, true)
	c.Settle()
	plainZombie := srv.SpawnMob("minecraft:zombie", 0.5, 64, -14.5)
	// The first zombie, if the power shot left it standing, hunts the
	// archer meanwhile, and its blows' traffic is the pump's to step over.
	spawned := false
	c.ReadTicksSkipping(srv, servertest.HostileTicks, servertest.HostileSkip, func(id int32, data []byte) bool {
		if id == protocol.IDPlaySpawnEntity && protocol.NewReader(data).ReadVarInt() == plainZombie {
			spawned = true
		}
		return spawned
	})
	if !spawned {
		t.Fatal("the control zombie never arrived")
	}
	bow, _ := registry.ItemID("minecraft:bow")
	srv.GiveSlot("Archer", player.HotbarSlot, protocol.ItemStack{ItemID: bow, Count: 1})
	bowShotDraws(srv, c, "Archer", 0.5, -10.5, 180)

	high := false
	c.ReadTicksSkipping(srv, servertest.HostileTicks, servertest.HostileSkip, func(id int32, data []byte) bool {
		if id != protocol.IDPlayEntityMetadata || mobMetadataID(data) != plainZombie {
			return false
		}
		m := servertest.ReadZombieMetadata(c, data)
		if !m.HasHealth || m.Health >= 20 {
			return false
		}
		if m.Health < 9 {
			c.T.Errorf("the plain bow's shot left the zombie at %v, want nine or more", m.Health)
		}
		high = true
		return true
	})
	if !high {
		t.Error("the plain bow's control shot never landed")
	}
}

// TestFlameBowSetsTheVictimBurning shoots a flame bow at a zombie and waits
// for the on-fire bit: the burn the burning shaft grants the body it lands
// on, five seconds of fire the vanilla hit gives.
func TestFlameBowSetsTheVictimBurning(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalConfig(), "Torch", func(s *server.Server) { s.SetTimeSync(0) })
	c.MarkLoaded()
	nightWorld(srv, c)

	srv.GiveSlot("Torch", player.HotbarSlot, servertest.EnchantedStack(t, "minecraft:bow", "minecraft:flame", 1))
	arrow, _ := registry.ItemID("minecraft:arrow")
	srv.GiveSlot("Torch", player.HotbarSlot+1, protocol.ItemStack{ItemID: arrow, Count: 16})

	zombieID := srv.SpawnMob("minecraft:zombie", 2.5, 64, 0.5)
	c.ExpectMobSpawn(protocol.EntityTypeZombie)
	bowShotDraws(srv, c, "Torch", 0.5, 0.5, -90)

	burning := false
	c.ReadTicksSkipping(srv, servertest.HostileTicks, servertest.HostileSkip, func(id int32, data []byte) bool {
		if id != protocol.IDPlayEntityMetadata || mobMetadataID(data) != zombieID {
			return false
		}
		m := servertest.ReadZombieMetadata(c, data)
		if m.HasFire && m.OnFire {
			burning = true
		}
		return burning
	})
	if !burning {
		t.Error("a flame bow's shot left the zombie unburnt")
	}
}

// TestInfinitySpendsNoArrow shoots an infinity bow and reads the hotbar
// back: the arrow the draw charged still there, where a plain bow's shot
// would have spent it.
func TestInfinitySpendsNoArrow(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalConfig(), "Bottomless", func(s *server.Server) { s.SetTimeSync(0) })
	c.MarkLoaded()
	nightWorld(srv, c)

	srv.GiveSlot("Bottomless", player.HotbarSlot, servertest.EnchantedStack(t, "minecraft:bow", "minecraft:infinity", 1))
	arrow, _ := registry.ItemID("minecraft:arrow")
	srv.GiveSlot("Bottomless", player.HotbarSlot+1, protocol.ItemStack{ItemID: arrow, Count: 16})

	srv.SpawnMob("minecraft:zombie", 0.5, 64, 4.5)
	bowShotDraws(srv, c, "Bottomless", 0.5, 0.5, 0)

	if got := srv.InventorySlot("Bottomless", player.HotbarSlot+1); got.Count != 16 {
		t.Errorf("the arrow stack after an infinity shot = %d, want the sixteen it started with", got.Count)
	}
}

// TestRespirationSparesTheBreath stands a diver wearing a respiration III
// helmet under two blocks of water for forty survival ticks and reads the
// air the metadata carries: the breath the vanilla oxygen-bonus dice spare,
// a tick's air kept where its roll against one over bonus-plus-one comes up
// short, against the dice the test seeds.
func TestRespirationSparesTheBreath(t *testing.T) {
	t.Parallel()
	cfg := servertest.EnvironmentConfig()
	srv, connect := servertest.Start(t, cfg, func(s *server.Server) { s.SetTimeSync(0) })
	tick := servertest.StopTicking(t, srv)
	c := connect()
	entityID := c.JoinAs("Diver")
	// The fed body's own regeneration would heal between the drowning
	// bites; a bar below the regeneration line keeps the breath alone.
	srv.SetFood("Diver", 17)

	// Two blocks of water over the ground, the eyes of a body standing in
	// it inside the second.
	water := servertest.BlockState(t, "minecraft:water")
	if !srv.SetBlock(0, 64, 50, water) || !srv.SetBlock(0, 65, 50, water) {
		t.Fatal("the water's chunk was not loaded")
	}
	c.MoveToGround(0.5, 64, 50.5, true)
	c.Settle()

	srv.GiveSlot("Diver", player.ArmorSlot, servertest.EnchantedStack(t, "minecraft:diamond_helmet", "minecraft:respiration", 3))

	// The dice are the test's to place. The first tick's breath runs before
	// its equipment refresh puts the helmet's oxygen_bonus on, so it spends
	// a breath without a roll; the other thirty-nine roll the bonus, each
	// spending the tick's breath only where its roll lands under one in four.
	// vanilla: LivingEntity.tick (baseTick before detectEquipmentUpdates)
	const seed = 4242
	srv.SetEnvironmentSeed(seed)
	r := rand.New(rand.NewSource(seed))
	spent := 1
	for range 39 {
		if r.Float64() < 1.0/4.0 {
			spent++
		}
	}
	tick(40)

	air := int32(-1)
	sawAir := false
	c.SettleAndCollect(func(id int32, data []byte) {
		if id != protocol.IDPlayEntityMetadata {
			return
		}
		m := servertest.ReadBurningMetadata(t, data)
		if m.EntityID == entityID && m.HasAir {
			air, sawAir = m.Air, true
		}
	})
	if !sawAir {
		t.Fatal("forty ticks under water sent no air metadata")
	}
	if want := int32(300 - spent); air != want {
		t.Errorf("the diver's air after forty ticks = %d, want %d (the rolls of the seeded dice)", air, want)
	}
}
