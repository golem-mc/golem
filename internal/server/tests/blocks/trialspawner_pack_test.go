package blocks

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
)

const chickensConfig = `{
	"total_mobs":1,"simultaneous_mobs":1,"total_mobs_added_per_player":0,
	"simultaneous_mobs_added_per_player":0,"ticks_between_spawn":2,
	"spawn_potentials":[{"weight":1,"data":{"entity":{"id":"minecraft:chicken"}}}]}`

// TestATrialSpawnerNamingAPackConfigSpawnsItsMobs: a spawner whose
// normal_config names a data pack's trial_spawner entry spawns that entry's
// mob when a player is in range. The registry tables are process-wide, so the
// test is not parallel and puts them back.
// vanilla: RegistryDataLoader.WORLD_REGISTRIES, TrialSpawner.Config
func TestATrialSpawnerNamingAPackConfigSpawnsItsMobs(t *testing.T) {
	t.Cleanup(func() { entity.ApplyPackTrialSpawnerConfigs(nil) })
	cfg := servertest.MobTestConfig()
	cfg.LevelName = t.TempDir()
	pack := filepath.Join(cfg.LevelName, "datapacks", "p")
	for file, body := range map[string]string{
		"pack.mcmeta":                           `{"pack":{"description":"p"}}`,
		"data/pack/trial_spawner/chickens.json": chickensConfig,
	} {
		path := filepath.Join(pack, file)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	srv, c, _ := servertest.JoinAs(t, cfg, "Trialist", servertest.OpsListing(t, "Trialist"),
		func(s *server.Server) { s.SetKeepAlive(time.Hour, time.Hour) })
	servertest.HoldTicks(t, srv)
	c.MoveToGround(2.5, 64, 5.5, true)
	// Chickens spawn only on grass.
	stone := servertest.MustState(t, "minecraft:grass_block", map[string]string{"snowy": "false"})
	air := servertest.MustState(t, "minecraft:air", nil)
	for x := int32(-4); x <= 12; x++ {
		for z := int32(-6); z <= 10; z++ {
			srv.SetBlock(x, 63, z, stone)
			for y := int32(64); y <= 67; y++ {
				srv.SetBlock(x, y, z, air)
			}
		}
	}
	srv.SetDifficulty(2)
	srv.SetBlock(trialX, trialY, trialZ, trialState(t, "inactive"))
	srv.SetBlockEntityTagAt(trialX, trialY, trialZ, "minecraft:trial_spawner",
		nbt.Compound{"normal_config": nbt.String("pack:chickens")})

	for range 400 {
		servertest.RunTicks(t, srv, 1)
		for _, m := range srv.Mobs() {
			if m.Kind == "minecraft:chicken" {
				return
			}
		}
	}
	t.Fatalf("no chicken after 400 ticks; mobs = %v, state %s, tag %v", srv.Mobs(), trialStateOf(t, srv), trialTag(t, srv))
}
