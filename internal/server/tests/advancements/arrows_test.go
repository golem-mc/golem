package advancements

import (
	"path/filepath"
	"testing"

	"github.com/golem-mc/golem/internal/config"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// The arrow triggers: what a player's shaft hurts and kills.

// archerAt joins an archer at the origin facing east with the loop held and
// a mob of the kind four blocks off on the line of the shot, at the health
// given.
func archerAt(t *testing.T, kind string, health float64) (*server.Server, *servertest.Client, int32) {
	t.Helper()
	srv, c, _ := servertest.JoinAs(t, servertest.BowTestConfig(), "Archer")
	servertest.HoldTicks(t, srv)
	id := srv.SpawnMob(kind, 4.5, 64, 0.5)
	srv.SetMobNoAI(id)
	srv.SetMobHealth(id, health)
	c.MoveTo(0.5, 64, 0.5, -90, 12)
	return srv, c, id
}

// flyTicks runs n held ticks, the flight of a shot.
func flyTicks(c *servertest.Client, srv *server.Server, n int) {
	c.T.Helper()
	c.ReadTicks(srv, n, func(int32, []byte) bool { return false })
}

// TestAnArrowThatHurtsAMobObtainsTakeAim: a bow's arrow landing on a mob is
// a player_hurt_entity by an arrow; the same shot at a mob that dies from it
// is not a crossbow kill.
// vanilla: LivingEntity.hurtServer, PlayerHurtEntityTrigger
func TestAnArrowThatHurtsAMobObtainsTakeAim(t *testing.T) {
	t.Parallel()
	srv, c, id := archerAt(t, "minecraft:cow", 10)
	srv.GiveSlot("Archer", 36, servertest.ItemStack(t, "minecraft:bow", 1))
	srv.GiveSlot("Archer", 37, servertest.ItemStack(t, "minecraft:arrow", 10))
	const shoot = "minecraft:adventure/shoot_arrow"
	if srv.AdvancementObtained("Archer", shoot, "shot_arrow") {
		t.Fatal("shot_arrow was obtained before any shot")
	}
	servertest.DrawBow(c, -90, 12)
	if got := srv.BowTicks("Archer"); got != 1 {
		t.Fatalf("draw ticks before advancing the held loop = %d, want 1", got)
	}
	servertest.HoldDraw(t, srv, "Archer", 24)
	servertest.ReleaseBow(c)
	flyTicks(c, srv, 20)
	if hp, _ := srv.MobHealth(id); hp >= 10 {
		t.Fatalf("the cow has %v health, want the arrow's hurt", hp)
	}
	if !srv.AdvancementObtained("Archer", shoot, "shot_arrow") {
		t.Error("an arrow that hurt a cow did not obtain shoot_arrow")
	}
	if srv.AdvancementObtained("Archer", "minecraft:adventure/throw_trident", "shot_trident") {
		t.Error("an arrow obtained throw_trident")
	}
}

// shootPlayer joins an archer at the origin facing east and a survival
// target four blocks off facing it, runs ready on the target with the loop
// held, and shoots one full-drawn arrow at it.
func shootPlayer(t *testing.T, cfg *config.Config, ready func(srv *server.Server, target *servertest.Client)) *server.Server {
	t.Helper()
	srv, c, _ := servertest.JoinAs(t, cfg, "Archer")
	target := servertest.DialServer(t, srv).Join("Target")
	servertest.HoldTicks(t, srv)
	target.MoveTo(4.5, 64, 0.5, 90, 0)
	target.SettleKeeping()
	srv.SetPlayerGameMode("Target", protocol.GameModeSurvival)
	ready(srv, target)
	c.MoveTo(0.5, 64, 0.5, -90, 12)
	srv.GiveSlot("Archer", 36, servertest.ItemStack(t, "minecraft:bow", 1))
	srv.GiveSlot("Archer", 37, servertest.ItemStack(t, "minecraft:arrow", 10))
	servertest.DrawBow(c, -90, 12)
	servertest.HoldDraw(t, srv, "Archer", 24)
	servertest.ReleaseBow(c)
	flyTicks(c, srv, 20)
	return srv
}

// TestAnArrowThatKillsAPlayerFiresKilledByArrow: a pack's killed_by_arrow
// naming a player victim is obtained by the shot that kills one.
// vanilla: AbstractArrow.onHitEntity, KilledByArrowTrigger
func TestAnArrowThatKillsAPlayerFiresKilledByArrow(t *testing.T) {
	cfg, data := packConfig(t)
	writePackFile(t, filepath.Join(data, "pk", "advancement", "pvp.json"),
		`{"criteria":{"k":{"trigger":"minecraft:killed_by_arrow","conditions":{"victims":[[{"condition":"minecraft:entity_properties","entity":"this","predicate":{"minecraft:entity_type":"minecraft:player"}}]]}}}}`)
	srv := shootPlayer(t, cfg, func(srv *server.Server, _ *servertest.Client) {
		srv.SetHealth("Target", 1)
	})
	if hp := srv.PlayerHealth("Target"); hp > 0 {
		t.Fatalf("the target has %v health, want the arrow's kill", hp)
	}
	if !srv.AdvancementObtained("Archer", "pk:pvp", "k") {
		t.Error("an arrow that killed a player did not obtain a killed_by_arrow naming a player")
	}
}

// shootPhantoms has the archer shoot a crossbow along a line of phantoms
// one health from death, the arrow piercing as many as the enchantment
// allows, and waits for the first to die.
func shootPhantoms(t *testing.T, pierce int) *server.Server {
	t.Helper()
	srv, c, _ := servertest.JoinAs(t, servertest.BowTestConfig(), "Archer")
	servertest.HoldTicks(t, srv)
	// Night, so the sun does not burn the phantoms down first.
	srv.SetDayTime(18000)
	ids := make([]int32, 2)
	for i := range ids {
		ids[i] = srv.SpawnMob("minecraft:phantom", 4.5+2*float64(i), 65.2, 0.5)
		srv.SetMobNoAI(ids[i])
		srv.SetMobHealth(ids[i], 1)
	}
	crossbow := servertest.ItemStack(t, "minecraft:crossbow", 1)
	if pierce > 0 {
		crossbow = servertest.EnchantedStack(t, "minecraft:crossbow", "minecraft:piercing", int32(pierce))
	}
	srv.GiveSlot("Archer", 36, crossbow)
	srv.GiveSlot("Archer", 37, servertest.ItemStack(t, "minecraft:arrow", 10))
	c.MoveTo(0.5, 64, 0.5, -90, 0)
	servertest.DrawBow(c, -90, 0)
	servertest.HoldDraw(t, srv, "Archer", 27)
	servertest.ReleaseBow(c)
	c.Settle()
	if got := srv.BowTicks("Archer"); got != 0 {
		t.Fatalf("the crossbow draw ran %d ticks after the release, want 0", got)
	}
	servertest.DrawBow(c, -90, 0)
	flyTicks(c, srv, 20)
	return srv
}

// shootSkeleton has the archer shoot a one-health skeleton four blocks off
// at the pitch given, stand back blocks further away while the arrow flies,
// and waits for the arrow to land.
func shootSkeleton(t *testing.T, back float64, pitch float32) *server.Server {
	t.Helper()
	srv, c, _ := servertest.JoinAs(t, servertest.BowTestConfig(), "Archer")
	servertest.HoldTicks(t, srv)
	// Night, so the sun does not burn the skeleton down first.
	srv.SetDayTime(18000)
	id := srv.SpawnMob("minecraft:skeleton", 4.5, 64, 0.5)
	srv.SetMobNoAI(id)
	srv.SetMobHealth(id, 1)
	srv.GiveSlot("Archer", 36, servertest.ItemStack(t, "minecraft:bow", 1))
	srv.GiveSlot("Archer", 37, servertest.ItemStack(t, "minecraft:arrow", 10))
	c.MoveTo(0.5, 64, 0.5, -90, pitch)
	servertest.DrawBow(c, -90, pitch)
	servertest.HoldDraw(t, srv, "Archer", 24)
	servertest.ReleaseBow(c)
	if back > 0 {
		c.MoveTo(0.5-back, 64, 0.5, -90, pitch)
	}
	flyTicks(c, srv, 40)
	return srv
}
