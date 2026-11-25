package blocks

import (
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
)

// vanilla: BaseSpawner.load, BaseSpawner.save, SpawnData.CODEC
func TestSpawnerLoadsDefaultPotentials(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, data, want string
	}{
		{
			name: "omitted",
			data: `SpawnData:{entity:{id:"pig"}}`,
			want: `[{data: {entity: {id: "minecraft:pig"}}, weight: 1}]`,
		},
		{
			name: "empty",
			data: `SpawnData:{entity:{id:"minecraft:pig"}},SpawnPotentials:[]`,
			want: `[]`,
		},
		{
			name: "supplied",
			data: `SpawnData:{entity:{id:"minecraft:pig"}},SpawnPotentials:[{data:{entity:{id:"minecraft:cow"}},weight:3}]`,
			want: `[{data: {entity: {id: "minecraft:cow"}}, weight: 3}]`,
		},
		{
			name: "no_spawn_data",
			data: `Delay:20s`,
			want: `[{data: {entity: {}}, weight: 1}]`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := servertest.BlockTestConfig()
			cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
			srv, _ := servertest.Join(t, cfg, "Spawnwatcher")
			servertest.HoldTicks(t, srv)
			srv.RunLine(`setblock 2 64 3 minecraft:spawner{` + tc.data + `}`)
			check := func() {
				t.Helper()
				replies := srv.RunLine(`data get block 2 64 3 SpawnPotentials`)
				if len(replies) != 1 || replies[0].Translate != "commands.data.block.query" || len(replies[0].With) != 4 {
					t.Fatalf("data get replies = %+v", replies)
				}
				if got := replies[0].With[3].Plain(); got != tc.want {
					t.Errorf("SpawnPotentials = %s, want %s", got, tc.want)
				}
			}
			check()
			replies := srv.RunLine(`data merge block 2 64 3 {x:99}`)
			if len(replies) != 1 || replies[0].Translate != "commands.data.block.modified" {
				t.Fatalf("data merge replies = %+v", replies)
			}
			check()
		})
	}
}

// vanilla: MonsterRoomFeature.place, BaseSpawner.setEntityId, BaseSpawner.save
func TestGeneratedDungeonSpawnerSavesEmptyPotentials(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	// Seed 0's chunk (3,-1) holds a dungeon whose spawner is at 59 -12 -13.
	cfg.LevelName = (servertest.RegionFixture{
		Seed: 0, GenerateStructures: true,
		Min: world.ChunkPos{X: 3, Z: -1}, Max: world.ChunkPos{X: 3, Z: -1},
	}).Copy(t)
	srv, _ := servertest.Join(t, cfg, "Spawnwatcher")
	servertest.HoldTicks(t, srv)
	t.Cleanup(srv.HoldChunk(59, -13))
	replies := srv.RunLine(`data get block 59 -12 -13`)
	if len(replies) != 1 || replies[0].Translate != "commands.data.block.query" {
		t.Fatalf("data get replies = %+v", replies)
	}
	got := replies[0].With[3].Plain()
	for _, want := range []string{`SpawnPotentials: []`, `minecraft:spider`} {
		if !strings.Contains(got, want) {
			t.Errorf("generated spawner = %s, want it to contain %s", got, want)
		}
	}
}
