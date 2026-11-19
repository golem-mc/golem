package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
)

// TestRaiderWaveReadsIntLikeVanilla loads a pillager whose Wave is a float,
// which getIntOr floors.
// vanilla: Raider.readAdditionalSaveData
func TestRaiderWaveReadsIntLikeVanilla(t *testing.T) {
	t.Parallel()
	cfg := servertest.HostileTestConfig()
	cfg.LevelName = t.TempDir()
	pillager := servertest.FixtureUUID(11, 22, 33, 45)
	servertest.PlantChunkEntities(t, cfg.LevelName, world.ChunkPos{X: 0, Z: 0}, nbt.Compound{
		"id":   nbt.String("minecraft:pillager"),
		"UUID": servertest.UuidWords(pillager),
		"Pos": nbt.List{Elem: nbt.TypeDouble, Items: []nbt.Tag{
			nbt.Double(8.5), nbt.Double(64), nbt.Double(8.5),
		}},
		"NoAI":                nbt.Byte(1),
		"NoGravity":           nbt.Byte(1),
		"PersistenceRequired": nbt.Byte(1),
		"Invulnerable":        nbt.Byte(1),
		"Wave":                nbt.Float(-1.5),
	})
	srv, connect := servertest.Start(t, cfg)
	c := connect()
	c.Login("Keeper")
	c.Configure()
	seen := c.JoinSeeingMobs("Keeper", pillager)
	servertest.HoldTicks(t, srv)
	servertest.RunTicks(t, srv, 2)
	if _, wave := srv.MobRaid(seen[pillager].EntityID); wave != -2 {
		t.Errorf("wave after loading Wave:-1.5f = %d, want -2", wave)
	}
}
