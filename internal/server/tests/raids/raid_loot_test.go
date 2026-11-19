package raids

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// vanilla: Raider.hasRaid, RaiderPredicate.matches
func TestACaptainNearARaidDropsNoOminousBottle(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.HostileTestConfig(), "Hero", servertest.OpsListing(t, "Hero"))
	servertest.HoldTicks(t, srv)
	srv.SetDifficulty(rules.DifficultyNormal)
	bell := [3]int32{3, 64, 0}
	if !srv.SetBlock(bell[0], bell[1], bell[2], registry.MustState("minecraft:bell", nil)) {
		t.Fatal("the bell was not placed")
	}
	spawnVillager(t, srv, bell)
	startRaid(t, c, srv, 0)
	id := srv.SpawnCaptain("minecraft:pillager", float64(bell[0])+0.5, float64(bell[1]), float64(bell[2])+0.5)
	c.ChatCommand("loot give @s kill @e[type=minecraft:pillager,limit=1]")
	servertest.AwaitCommandFeedbackText(t, c, "commands.drop.success.multiple_with_table",
		servertest.NumberText(0), protocol.Text{Text: "minecraft:entities/pillager"})
	if !srv.KillMob(id) {
		t.Fatal("the captain was not killed")
	}
	servertest.RunTicks(t, srv, 2)
	for _, it := range srv.ItemsInDimension("minecraft:overworld") {
		if it.Item == "minecraft:ominous_bottle" {
			t.Fatalf("a captain inside the raid dropped %d ominous bottles", it.Count)
		}
	}
}
