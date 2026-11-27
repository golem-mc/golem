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

// vanilla: BaseSpawner.serverTick
func TestSpawnerCapIgnoresFarMobs(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Spawnwatcher")
	servertest.HoldTicks(t, srv)
	srv.SetDifficulty(2)
	srv.RunLine("time set midnight")
	srv.RunLine("gamerule max_entity_cramming 0")
	srv.RunLine("fill 0 64 -2 8 68 6 air")
	srv.RunLine("fill 0 63 -2 8 63 6 stone")
	srv.RunLine(`summon minecraft:zombie 5.5 64 2.5 {NoAI:1b,Tags:["beside"]}`)
	for i := range 50 {
		srv.RunLine(fmt.Sprintf(`summon minecraft:zombie %d.5 64 %d.5 {NoAI:1b,Tags:["far"]}`, 44+i%10, 2+i/10))
	}
	srv.RunLine(`setblock 4 64 2 spawner{Delay:0s,MinSpawnDelay:1s,MaxSpawnDelay:2s,SpawnCount:1s,MaxNearbyEntities:1s,SpawnRange:4s,SpawnData:{entity:{id:"minecraft:zombie",NoAI:1b},custom_spawn_rules:{block_light_limit:[0,15],sky_light_limit:[0,15]}}}`)

	countZombies := func() int {
		n := 0
		for _, m := range srv.Mobs() {
			if m.Kind == "minecraft:zombie" {
				n++
			}
		}
		return n
	}
	if got := countZombies(); got != 51 {
		t.Fatalf("initial zombie count = %d, want 51", got)
	}
	servertest.RunTicks(t, srv, 100)
	if got := countZombies(); got != 51 {
		t.Fatalf("zombie count after 100 ticks with beside mob = %d, want 51", got)
	}
	srv.RunLine("kill @e[tag=beside]")
	// 20 ticks for the beside corpse's death animation, plus the next spawn round.
	servertest.RunTicks(t, srv, 30)
	if got := countZombies(); got <= 50 {
		t.Fatalf("zombie count after killing beside mob = %d, want > 50", got)
	}
	c.Disconnect()
}

// vanilla: BaseSpawner.serverTick, Mob.checkSpawnObstruction
func TestSpawnerEdgeMobBlocksSpawn(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Spawnwatcher")
	servertest.HoldTicks(t, srv)
	srv.SetDifficulty(2)
	srv.RunLine("time set midnight")
	srv.RunLine("fill 1 62 3 8 67 10 stone")
	srv.RunLine("fill 5 64 5 7 66 7 air")
	srv.RunLine(`summon minecraft:ravager 6.0 64 6.5 {NoAI:1b,Tags:["blocker"]}`)
	srv.RunLine(`setblock 4 64 6 spawner{Delay:0s,MinSpawnDelay:1s,MaxSpawnDelay:2s,SpawnCount:16s,MaxNearbyEntities:10s,SpawnRange:2s,SpawnData:{entity:{id:"minecraft:zombie",NoAI:1b},custom_spawn_rules:{block_light_limit:[0,15],sky_light_limit:[0,15]}}}`)

	countZombies := func() int {
		n := 0
		for _, m := range srv.Mobs() {
			if m.Kind == "minecraft:zombie" {
				n++
			}
		}
		return n
	}
	servertest.RunTicks(t, srv, 40)
	if got := countZombies(); got != 0 {
		t.Fatalf("zombie count with edge blocker = %d, want 0", got)
	}
	srv.RunLine("kill @e[tag=blocker]")
	servertest.RunTicks(t, srv, 50)
	if got := countZombies(); got == 0 {
		t.Fatal("no zombie spawned after removing edge blocker")
	}
	c.Disconnect()
}

// entityPlaceLog records the entity_place events a server posts.
type entityPlaceLog struct {
	mu  sync.Mutex
	pos [][3]float64
}
