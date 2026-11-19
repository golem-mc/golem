package blocks

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
)

// vanilla: TrialSpawnerStateData.Packed.MAP_CODEC, TrialSpawnerConfig.DIRECT_CODEC,
// TrialSpawnerStateData.getOrCreateNextSpawnData
func TestATrialSpawnerSavesTheWayItsCodecsWrite(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Actor", servertest.OpsListing(t, "Actor"), func(s *server.Server) {
		servertest.FreezeUntilHeld(t, s)
	})
	servertest.HoldTicks(t, srv)
	keys := func(tag nbt.Compound) []string {
		var out []string
		for k := range tag {
			out = append(out, k)
		}
		slices.Sort(out)
		return out
	}

	servertest.Setblock(t, c, 0, 64, 4, "trial_spawner{required_player_range:5}")
	servertest.RunTicks(t, srv, 5)
	tag, ok := srv.BlockEntityTagAt(0, 64, 4)
	if !ok {
		t.Fatal("no trial spawner data")
	}
	if got, want := keys(tag), []string{"components", "id", "required_player_range", "spawn_data", "x", "y", "z"}; !slices.Equal(got, want) {
		t.Errorf("keys = %v, want %v", got, want)
	}
	if tag["components"] == nil {
		t.Error("components missing")
	}
	data, _ := nbt.Get[nbt.Compound](tag, "spawn_data")
	entity, ok := nbt.Get[nbt.Compound](data, "entity")
	if !ok || len(entity) != 0 {
		t.Errorf("spawn_data = %v, want {entity: {}}", tag["spawn_data"])
	}

	servertest.Setblock(t, c, 0, 64, 4, "trial_spawner{normal_config:{total_mobs:4,simultaneous_mobs:2}}")
	servertest.RunTicks(t, srv, 5)
	tag, _ = srv.BlockEntityTagAt(0, 64, 4)
	if got, want := keys(tag), []string{"components", "id", "normal_config", "spawn_data", "x", "y", "z"}; !slices.Equal(got, want) {
		t.Errorf("keys = %v, want %v", got, want)
	}
	cfg, _ := nbt.Get[nbt.Compound](tag, "normal_config")
	if got, want := keys(cfg), []string{"total_mobs"}; !slices.Equal(got, want) {
		t.Errorf("normal_config keys = %v, want %v", got, want)
	}
	if cfg["total_mobs"] != nbt.Float(4) {
		t.Errorf("total_mobs = %v, want 4.0f", cfg["total_mobs"])
	}
}
