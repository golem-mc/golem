package mobs

import (
	"io"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/console"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// The bee over the wire: neutral until struck, then it stings once, poisons
// by difficulty, and dies of the sting.
// vanilla: Bee, NeutralMob

const bee = "minecraft:bee"

// beeWorld starts a held world with an op named Slayer standing at
// (2.5, 64, 0.5) on the given difficulty.
func beeWorld(t *testing.T, difficulty uint8) (*servertest.Client, int32, *server.Server) {
	t.Helper()
	srv, connect := servertest.Start(t, servertest.HostileTestConfig(), servertest.OpsListing(t, "Slayer"))
	srv.SetDayTime(18000)
	srv.SetDifficulty(difficulty)
	c := connect()
	id := c.JoinAs("Slayer")
	c.MoveToGround(2.5, 64, 0.5, true)
	c.Settle()
	servertest.HoldTicks(t, srv)
	return c, id, srv
}

// strikeBee spawns a bee beside the player and hits it once.
func strikeBee(t *testing.T, c *servertest.Client, srv *server.Server, x, z float64) int32 {
	t.Helper()
	id := srv.SpawnMob(bee, x, 64, z)
	servertest.AwaitFullSwing(t, srv, "Slayer")
	c.Attack(id)
	c.Settle()
	return id
}

func TestAStruckBeeStingsOnceAndPoisonsByDifficulty(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		difficulty uint8
		damage     float64
		poison     int32
	}{
		{"easy", rules.DifficultyEasy, 2, 0},
		{"normal", rules.DifficultyNormal, 2, 10 * 20},
		{"hard", rules.DifficultyHard, 3, 18 * 20},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c, playerID, srv := beeWorld(t, tc.difficulty)
			id := strikeBee(t, c, srv, 3.5, 0.5)
			servertest.UntilTicks(t, srv, 5, "the bee's target", func() bool { return srv.MobTargetID(id) == playerID })
			if got := srv.MobAngerTicks(id); got != 0 {
				// the timer starts on the tick after the goal names the target
				t.Errorf("anger on the tick the target is named = %d", got)
			}
			servertest.RunTicks(t, srv, 1)
			if got := srv.MobAngerTicks(id); got < 400 || got > 780 {
				t.Errorf("anger after the blow = %d ticks, want 400 to 780", got)
			}
			servertest.UntilTicks(t, srv, 100, "the sting", func() bool { return srv.PlayerHealth("Slayer") < 20 })
			if got, want := srv.PlayerHealth("Slayer"), 20-tc.damage; got != want {
				t.Errorf("health after the sting = %v, want %v", got, want)
			}
			if tc.poison > 0 {
				poison, _ := registry.MobEffectID("minecraft:poison")
				effect := c.AwaitMobEffect()
				if effect.Effect != int32(poison) || effect.Duration != tc.poison || effect.Amplifier != 0 {
					t.Errorf("effect = %+v, want poison 0 for %d ticks", effect, tc.poison)
				}
			}
			if flags, _ := srv.MobBeeFlags(id); flags&4 == 0 {
				t.Errorf("flags after the sting = %#x, want the has-stung bit", flags)
			}
			if got := srv.MobAngerTicks(id); got != 0 {
				t.Errorf("anger after the sting = %d, want none", got)
			}
			if got := srv.MobTargetID(id); got != 0 {
				t.Errorf("target after the sting = %d, want none", got)
			}
			// A bee that has stung never hunts again.
			servertest.RunTicks(t, srv, 100)
			if got := srv.MobTargetID(id); got != 0 {
				t.Errorf("a stung bee targets %d", got)
			}
		})
	}
}

func TestStrikingABeeCallsTheBeesInReachButNotTheFarOnes(t *testing.T) {
	t.Parallel()
	c, playerID, srv := beeWorld(t, rules.DifficultyNormal)
	near := srv.SpawnMob(bee, 8.5, 64, 4.5)
	far := srv.SpawnMob(bee, 60.5, 64, 0.5)
	strikeBee(t, c, srv, 3.5, 0.5)
	servertest.UntilTicks(t, srv, 10, "the neighbour's target", func() bool { return srv.MobTargetID(near) == playerID })
	servertest.RunTicks(t, srv, 20)
	if got := srv.MobTargetID(far); got != 0 {
		t.Errorf("a bee 57 blocks away targets %d, want nobody", got)
	}
	if got := srv.MobAngerTicks(far); got != 0 {
		t.Errorf("a bee 57 blocks away has %d ticks of anger", got)
	}
}

// TestABeesFlagsAreSavedAndLoaded: the nectar and sting flags and the anger
// survive a save, and a calm bee writes the -1 sentinel.
// vanilla: Bee.addAdditionalSaveData, NeutralMob.addPersistentAngerSaveData
func TestABeesFlagsAreSavedAndLoaded(t *testing.T) {
	t.Parallel()
	cfg := servertest.HostileTestConfig()
	srv, _ := servertest.Start(t, cfg)
	script := strings.Join([]string{
		`summon minecraft:bee 100.5 64 0.5 {NoAI:1b,HasNectar:1b,HasStung:1b,anger_end_time:5000L,angry_at:[I;1,2,3,4]}`,
		`summon minecraft:bee 100.5 64 8.5 {NoAI:1b}`,
	}, "\n") + "\n"
	console.New(strings.NewReader(script), io.Discard, io.Discard, srv).Serve()
	srv.Shutdown()

	var marked, calm nbt.Compound
	for _, compound := range servertest.ReadChunkEntities(t, cfg.LevelName, world.ChunkPos{X: world.ChunkOf(100.5), Z: world.ChunkOf(0.5)}) {
		if _, ok := compound["angry_at"]; ok {
			marked = compound
		} else {
			calm = compound
		}
	}
	for _, key := range []string{"HasNectar", "HasStung"} {
		if v, _ := marked[key].(nbt.Byte); v != 1 {
			t.Errorf("a marked bee saved %s %v, want 1", key, marked[key])
		}
		if v, ok := calm[key].(nbt.Byte); !ok || v != 0 {
			t.Errorf("a calm bee saved %s %v, want 0", key, calm[key])
		}
	}
	if end, _ := marked["anger_end_time"].(nbt.Long); end <= 0 {
		t.Errorf("an angry bee saved anger_end_time %v, want a time in the future", marked["anger_end_time"])
	}
	if end, ok := calm["anger_end_time"].(nbt.Long); !ok || end != -1 {
		t.Errorf("a calm bee saved anger_end_time %v, want -1", calm["anger_end_time"])
	}
}

// stingerMetadata reports whether a packet is the player's metadata carrying
// the stinger count, and the count.
func stingerMetadata(t *testing.T, playerID, id int32, data []byte) (int32, bool) {
	t.Helper()
	if id != protocol.IDPlayEntityMetadata {
		return 0, false
	}
	r := protocol.NewReader(data)
	if r.ReadVarInt() != playerID {
		return 0, false
	}
	entries, ok := servertest.ReadMetadata(r)
	if !ok {
		t.Fatal("unreadable entity metadata")
	}
	if e := servertest.MetaOf(entries, protocol.MetadataIndexStingerCount); e != nil {
		return e.VarintVal, true
	}
	return 0, false
}

// vanilla: Bee.doHurtTarget, LivingEntity.tick (20 * (30 - count) ticks)
func TestABeeStingLeavesAStingerThatFallsOut(t *testing.T) {
	t.Parallel()
	c, playerID, srv := beeWorld(t, rules.DifficultyEasy)
	strikeBee(t, c, srv, 3.5, 0.5)
	counts := func(want int32, ticks int) bool {
		return c.ReadTicks(srv, ticks, func(id int32, data []byte) bool {
			n, ok := stingerMetadata(t, playerID, id, data)
			return ok && n == want
		})
	}
	if !counts(1, 100) {
		t.Fatal("the sting did not tell a stinger count of 1")
	}
	if counts(0, 400) {
		t.Fatal("the stinger fell out before its 580 ticks")
	}
	if !counts(0, 300) {
		t.Fatal("the stinger did not fall out after 580 ticks")
	}
}
