package mobs

import (
	"math/rand"
	"testing"

	"github.com/golem-mc/golem/internal/server/entity"
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
		want += infestedReplay(entity.NewDice(int64(i)))
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
