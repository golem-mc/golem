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
