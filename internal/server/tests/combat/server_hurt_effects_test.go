package combat

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
)

// Status effects on the mob hurt path: Resistance scales a sword blow, Fire
// Resistance drops fire damage.

// TestResistanceTurnsAsideAMobsSwing plants a cow under Resistance and
// strikes it with a whole sword blow. At level V (amplifier 4) vanilla's
// Resistance scales the blow by nought, so the cow keeps its untouched ten
// although the blow still lands and is still told of — the damage event, the
// flinch, the shove and the metadata with it.
// vanilla: LivingEntity.getDamageAfterMagicAbsorb
func TestResistanceTurnsAsideAMobsSwing(t *testing.T) {
	t.Parallel()
	srv, c, attacker := servertest.JoinAs(t, servertest.CombatTestConfig(), "Juggler", servertest.OpsListing(t, "Juggler"))
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()

	mobID := srv.SpawnMob("minecraft:cow", 5.5, 64, 5.5)
	c.ExpectMobSpawn(protocol.EntityTypeCow)
	c.Give("effect give @e[type=minecraft:cow] minecraft:resistance 999 4", mobID)

	srv.GiveSlot("Juggler", player.HotbarSlot, protocol.ItemStack{
		ItemID: servertest.MustItemID(t, "minecraft:diamond_sword"), Count: 1,
	})
	servertest.AwaitFullSwing(t, srv, "Juggler")
	if got := c.Strike(mobID, attacker); math.Abs(float64(got)-10) > 0.01 {
		t.Errorf("the cow with Resistance V kept %v health after a whole sword blow, want its untouched 10", got)
	}
}

// TestResistanceScalesAMobsSwing is the weaker case of the same: at the
// first level vanilla's Resistance leaves four fifths of the blow, so the
// sword's whole force lands as that share.
// vanilla: LivingEntity.getDamageAfterMagicAbsorb
func TestResistanceScalesAMobsSwing(t *testing.T) {
	t.Parallel()
	srv, c, attacker := servertest.JoinAs(t, servertest.CombatTestConfig(), "Duelist", servertest.OpsListing(t, "Duelist"))
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()

	mobID := srv.SpawnMob("minecraft:cow", 5.5, 64, 5.5)
	c.ExpectMobSpawn(protocol.EntityTypeCow)
	c.Give("effect give @e[type=minecraft:cow] minecraft:resistance 999 0", mobID)

	srv.GiveSlot("Duelist", player.HotbarSlot, protocol.ItemStack{
		ItemID: servertest.MustItemID(t, "minecraft:diamond_sword"), Count: 1,
	})
	servertest.AwaitFullSwing(t, srv, "Duelist")
	want := 10 - 0.8*servertest.SwordDamage()
	if got := c.Strike(mobID, attacker); math.Abs(float64(got)-want) > 0.01 {
		t.Errorf("the cow with Resistance kept %v health after a whole sword blow, want %v", got, want)
	}
}

// burnTwoCows plants a cow under Fire Resistance and a plain one beside it,
// sets both alight the same way, and returns their entity IDs and the health
// metadata each told over the ticks asked for.
func burnTwoCows(t *testing.T, c *servertest.Client, srv *server.Server, setFire func(resistant, plain int32), ticks int) (resistant, plain int32, healths map[int32][]float32) {
	t.Helper()
	resistant = srv.SpawnMob("minecraft:cow", 4.5, 64, 0.5)
	c.ExpectMobSpawn(protocol.EntityTypeCow)
	srv.SetMobNoAI(resistant)
	// The effect is given while this is the only cow, so the selector cannot
	// reach the plain one planted next.
	c.Give("effect give @e[type=minecraft:cow] minecraft:fire_resistance 999 0", resistant)
	plain = srv.SpawnMob("minecraft:cow", 12.5, 64, 0.5)
	c.ExpectMobSpawn(protocol.EntityTypeCow)
	srv.SetMobNoAI(plain)

	srv.StopTick()
	setFire(resistant, plain)

	healths = map[int32][]float32{}
	bodies := map[int32]bool{resistant: true, plain: true}
	for range ticks {
		srv.TickMobs(1)
		c.SettleAndCollect(func(id int32, data []byte) {
			if id != protocol.IDPlayEntityMetadata {
				return
			}
			if m, mine := servertest.ReadBodyMetadata(c, data, bodies); mine && m.HasHealth {
				healths[m.EntityID] = append(healths[m.EntityID], m.Health)
			}
		})
	}
	return resistant, plain, healths
}

// TestFireResistanceSavesABurningMob burns two cows side by side, one under
// Fire Resistance, and holds that the burning bites only the plain one: the
// resistant body takes none of the on_fire ticks, so no health is told of it
// at all, while the plain cow loses the hearts the bites inside the ticks
// come to.
// vanilla: LivingEntity.hurtServer, LivingEntity.baseTick
func TestFireResistanceSavesABurningMob(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Arsonist", servertest.OpsListing(t, "Arsonist"))
	c.Settle()

	resistant, plain, healths := burnTwoCows(t, c, srv,
		func(resistant, plain int32) { srv.SetMobFire(resistant, 600); srv.SetMobFire(plain, 600) }, 45)
	if got := healths[resistant]; len(got) != 0 {
		t.Errorf("the fire-resistant cow took burning damage %v, want none", got)
	}
	t.Logf("plain burning cow healths: %v", healths[plain])
	if got := healths[plain]; len(got) == 0 || got[len(got)-1] >= 10 {
		t.Errorf("the plain burning cow's health = %v, want it bitten below its ten", got)
	}
}

// TestFireResistanceSavesAMobInTheFlames is the same for the fire a block
// lights under a standing body, the in_fire damage rather than the burning
// tick.
// vanilla: BaseFireBlock.entityInside, LivingEntity.hurtServer
func TestFireResistanceSavesAMobInTheFlames(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Kindler", servertest.OpsListing(t, "Kindler"))
	c.Settle()

	resistant, plain, healths := burnTwoCows(t, c, srv, func(resistant, plain int32) {
		if !srv.SetBlock(4, 64, 0, servertest.BlockState(t, "minecraft:fire")) {
			t.Fatal("the resistant cow's fire chunk is not loaded")
		}
		if !srv.SetBlock(12, 64, 0, servertest.BlockState(t, "minecraft:fire")) {
			t.Fatal("the plain cow's fire chunk is not loaded")
		}
	}, 45)
	if got := healths[resistant]; len(got) != 0 {
		t.Errorf("the fire-resistant cow standing in fire took damage %v, want none", got)
	}
	t.Logf("plain cow in flames healths: %v", healths[plain])
	if got := healths[plain]; len(got) == 0 || got[len(got)-1] >= 10 {
		t.Errorf("the plain cow standing in fire kept health %v, want it bitten below its ten", got)
	}
}
