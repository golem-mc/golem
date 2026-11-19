package mobs

import (
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/internal/server/rules"

	"github.com/golem-mc/golem/internal/server/servertest"

	"github.com/golem-mc/golem/internal/config"
)

// TestAHurtBodyWithInfestedLetsSilverfishOut hurts a grid of cows twice, the
// second time under the infested effect: the first round lets nothing out,
// the second one or two silverfish for about a tenth of the cows.
// vanilla: InfestedMobEffect.onMobHurt
func TestAHurtBodyWithInfestedLetsSilverfishOut(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, c := servertest.Dial(t, cfg, servertest.OpsListing(t, "Infester"))
	c.Login("Infester")
	c.Configure()
	c.SpawnedAt()
	c.SkipJoin()
	srv.StopTick()
	srv.SetDifficulty(2)
	var cows []int32
	for i := range 100 {
		x, z := 3+float64(i%10)*1.5, 3+float64(i/10)*1.5
		cows = append(cows, srv.SpawnMob("minecraft:cow", x, 64, z))
	}
	for _, id := range cows {
		srv.HurtMobByWorld(id, 1, "minecraft:magic")
	}
	srv.TickMobs(1)
	if n := countKind(srv, silverfishKind); n != 0 {
		t.Fatalf("%d silverfish came out of cows without the effect", n)
	}
	c.ChatCommand("effect give @e[type=minecraft:cow] minecraft:infested 1000")
	c.Settle()
	srv.TickMobs(25)
	want := 0
	for i, id := range cows {
		srv.SeedMobInfested(id, int64(i))
		want += infestedReplay(rand.New(rand.NewSource(int64(i))))
		srv.HurtMobByWorld(id, 1, "minecraft:magic")
	}
	srv.TickMobs(1)
	if want == 0 {
		t.Fatal("the seeds let no silverfish out, the test proves nothing")
	}
	if n := countKind(srv, silverfishKind); n != want {
		t.Errorf("%d silverfish came out of 100 hurt cows, want %d from the seeded rolls", n, want)
	}
	c.Disconnect()
}

// TestASilverfishStaysOnTopOfPowderSnow sinks a silverfish into a walled
// column of powder snow: it jumps until it stands on the snow's surface.
// vanilla: ClimbOnTopOfPowderSnowGoal
func TestASilverfishStaysOnTopOfPowderSnow(t *testing.T) {
	t.Parallel()
	srv, c := silverfishTests(t, "survival", true)
	bedrock, snow := blockNamed(t, "minecraft:bedrock"), servertest.MustState(t, "minecraft:powder_snow", nil)
	for x := int32(5); x <= 9; x++ {
		for z := int32(5); z <= 9; z++ {
			srv.SetBlock(x, 64, z, bedrock)
			if x == 7 && z == 7 {
				srv.SetBlock(x, 65, z, snow)
			} else {
				srv.SetBlock(x, 65, z, bedrock)
			}
		}
	}
	id := srv.SpawnMob(silverfishKind, 7.5, 65.2, 7.5)
	top := 0.0
	for range 60 {
		srv.TickMobs(1)
		if _, y, _, ok := srv.MobPos(id); ok && y > top {
			top = y
		}
	}
	if top < 66 {
		t.Errorf("the silverfish rose to y = %.2f, want out of the snow onto its surface at 66", top)
	}
	c.Disconnect()
}

// TestAHurtPlayerWithInfestedLetsSilverfishOut lands 800 hurts, each a hair
// stronger than the last so the window passes the excess, on a player under
// the effect with the dice seeded: the silverfish the seeded rolls give.
// vanilla: InfestedMobEffect.onMobHurt
func TestAHurtPlayerWithInfestedLetsSilverfishOut(t *testing.T) {
	t.Parallel()
	srv, c := silverfishTests(t, "survival", true)
	for i := 1; i <= 800; i++ {
		srv.Hurt("Miner", float64(i)*0.0005)
	}
	if n := len(silverfishes(srv)); n != 0 {
		t.Fatalf("%d silverfish came out of a player without the effect", n)
	}
	srv.GiveEffect("Miner", "minecraft:infested", 1000, 0, true)
	srv.SeedInfested(7)
	r := rand.New(rand.NewSource(7))
	want := 0
	for i := 1; i <= 800; i++ {
		want += infestedReplay(r)
		srv.Hurt("Miner", 0.4+float64(i)*0.0005)
	}
	if n := len(silverfishes(srv)); n != want || want == 0 {
		t.Errorf("%d silverfish came out of 800 hurts, want %d from the seeded rolls", n, want)
	}
	c.Disconnect()
}

// infestedReplay draws what one hurt's roll draws from the dice and answers
// how many silverfish it lets out.
func infestedReplay(r *rand.Rand) int {
	if r.Float32() > rules.InfestedChance {
		return 0
	}
	n := rules.InfestedCount(r)
	for range n {
		rules.InfestedLaunch(r, 0, 0, 0)
	}
	return n
}
