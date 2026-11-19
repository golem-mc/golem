package blocks

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// vanilla: Ocelot.checkSpawnObstruction, BaseSpawner.serverTick
func TestAnOcelotSpawnerRequiresGrassOrLeaves(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, ground string
		want         bool
	}{
		{"stone", "stone", false},
		{"grass", "grass_block", true},
		{"leaves", "jungle_leaves", true},
	} {
		for _, custom := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/custom=%v", tc.name, custom), func(t *testing.T) {
				t.Parallel()
				srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Spawnwatcher")
				servertest.HoldTicks(t, srv)
				srv.RunLine("time set noon")
				srv.RunLine("fill 3 62 3 5 66 5 air")
				srv.RunLine("setblock 4 63 4 " + tc.ground)
				rules := ""
				if custom {
					rules = `,custom_spawn_rules:{block_light_limit:[0,15],sky_light_limit:[0,15]}`
				}
				srv.RunLine(`setblock 4 64 2 spawner{Delay:0s,MinSpawnDelay:1s,MaxSpawnDelay:2s,SpawnCount:1s,SpawnData:{entity:{id:"minecraft:ocelot",Pos:[4.5d,64d,4.5d],NoAI:1b}` + rules + `}}`)
				servertest.RunTicks(t, srv, 40)
				spawned := false
				for _, m := range srv.Mobs() {
					if m.Kind == "minecraft:ocelot" {
						spawned = true
					}
				}
				if spawned != tc.want {
					t.Fatalf("ocelot spawned = %v, want %v", spawned, tc.want)
				}
				c.Disconnect()
			})
		}
	}
}

// vanilla: BaseSpawner.serverTick, Monster.checkAnyLightMonsterSpawnRules
func TestABlazeSpawnerSpawnsBesideLava(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.NetherTestConfig(t, servertest.BlockTestConfig()), "Spawnwatcher")
	servertest.HoldTicks(t, srv)
	srv.MovePlayerToDimension("Spawnwatcher", world.NetherName, 5.5, 181, 11.5)
	// Chunk delivery runs independently of the held tick loop.
	servertest.PollUntil(t, 20*time.Second, func() bool { return srv.ChunkLoadedInDimension(world.NetherName, 5, 7) })
	srv.SetDifficulty(2)
	for _, command := range []string{
		"fill 0 180 0 10 185 12 minecraft:air",
		"fill 0 179 0 10 179 12 minecraft:stone",
		"fill 0 180 0 10 180 12 minecraft:light[level=11]",
	} {
		srv.RunLine("execute in minecraft:the_nether run " + command)
	}
	// Relighting, like chunk delivery, runs outside the tick loop.
	servertest.PollUntil(t, 5*time.Second, func() bool { return srv.BlockLightInDimension(world.NetherName, 5, 181, 7) == 10 })
	if got := srv.BlockLightInDimension(world.NetherName, 5, 181, 7); got != 10 {
		t.Fatalf("spawn cell block light = %d, want 10", got)
	}
	srv.RunLine(`execute in minecraft:the_nether run setblock 5 181 5 minecraft:spawner{Delay:0s,SpawnCount:1s,SpawnData:{entity:{id:"minecraft:blaze",Pos:[5.5d,181.0d,7.5d],NoAI:1b}}}`)
	servertest.RunTicks(t, srv, 5)
	for _, m := range srv.MobsInDimension(world.NetherName) {
		if m.Kind == "minecraft:blaze" {
			data, ok := servertest.ReadUntil(t, c, servertest.ExpectTimeout, protocol.IDPlaySpawnEntity)
			if !ok {
				t.Fatal("no spawn entity reached the client")
			}
			r := protocol.NewReader(data)
			r.ReadVarInt()
			r.ReadUUID()
			if got := r.ReadVarInt(); got != int32(registry.EntityTypeBlaze) {
				t.Fatalf("spawned entity type = %d, want blaze", got)
			}
			if err := r.Err(); err != nil {
				t.Fatal(err)
			}
			return
		}
	}
	t.Fatal("no blaze appeared at block light 10 in the Nether")
}

// entityPlaceLog records the entity_place events a server posts.
type entityPlaceLog struct {
	mu  sync.Mutex
	pos [][3]float64
}
