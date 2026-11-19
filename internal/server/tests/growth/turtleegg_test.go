package growth

import (
	"github.com/golem-mc/golem/internal/server/servertest"

	"math/rand"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// eggSeed finds a physics seed whose first rolls break the egg: Intn(3)
// zero, then the sound's pitch, then another Intn(3) zero.
func eggSeed(t *testing.T) int64 {
	t.Helper()
	for seed := int64(1); seed < 10000; seed++ {
		r := rand.New(rand.NewSource(seed))
		if r.Intn(3) == 0 {
			r.Float32()
			if r.Intn(3) == 0 {
				return seed
			}
		}
	}
	t.Fatal("no seed rolls two breaks")
	return 0
}

func turtleEggs(t *testing.T, srv *server.Server, x, y, z int32) int {
	t.Helper()
	state := srv.BlockAt(x, y, z)
	name, props, ok := registry.BlockStateName(state)
	if !ok || name != "minecraft:turtle_egg" {
		return 0
	}
	switch registry.ParseStateProps(props)["eggs"] {
	case "1":
		return 1
	case "2":
		return 2
	case "3":
		return 3
	}
	return 4
}

// TestALandingOnTurtleEggsBreaksOne checks the landing: a survival player
// who falls onto a block of three eggs and loses the one-in-three roll leaves
// two. vanilla: TurtleEggBlock.fallOn
func TestALandingOnTurtleEggsBreaksOne(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalConfig(), "Camper", servertest.OpsListing(t, "Camper"))
	srv.StopTick()
	servertest.SeedPhysics(t, srv, eggSeed(t))

	servertest.SetFarmBlock(t, c, srv, 0, 63, 0, "minecraft:turtle_egg", map[string]string{"eggs": "3"})
	c.MoveTo(0.5, 74, 0.5, 0, 0)
	c.MoveTo(0.5, 63.4375, 0.5, 0, 0)

	servertest.AwaitBlockUpdate(t, c, 0, 63, 0, servertest.CropsStateOf(t, "minecraft:turtle_egg", map[string]string{"eggs": "2"}))
	if got := turtleEggs(t, srv, 0, 63, 0); got != 2 {
		t.Errorf("eggs after the landing = %d, want 2", got)
	}
}

// TestSneakingOnTurtleEggsBreaksNone checks the step: over four hundred
// ticks a walker breaks one of three eggs at one in a hundred a tick, and a
// sneaker or a spectator never does. vanilla: TurtleEggBlock.stepOn
func TestSneakingOnTurtleEggsBreaksNone(t *testing.T) {
	t.Parallel()
	for _, who := range []string{"sneaking", "spectating", "walking"} {
		srv, c := servertest.Join(t, servertest.SurvivalConfig(), "Camper", servertest.OpsListing(t, "Camper"))
		srv.StopTick()
		servertest.SeedPhysics(t, srv, 5)

		c.MoveTo(0.5, 63.4375, 0.5, 0, 0)
		servertest.SetFarmBlock(t, c, srv, 0, 63, 0, "minecraft:turtle_egg", map[string]string{"eggs": "3"})
		switch who {
		case "sneaking":
			c.Send(protocol.IDPlayPlayerInput, func(b *protocol.Buffer) { b.PutU8(protocol.InputSneak) })
		case "spectating":
			c.SendChangeGameMode(protocol.GameModeSpectator)
			servertest.PollUntil(t, servertest.ExpectTimeout, func() bool { return srv.PlayerGameMode("Camper") == protocol.GameModeSpectator })
		}
		c.Settle()
		for range 400 {
			srv.TickSurvival()
		}
		got := turtleEggs(t, srv, 0, 63, 0)
		if who == "walking" && got >= 3 {
			t.Errorf("eggs under a walking player = %d, want fewer than 3", got)
		}
		if who != "walking" && got != 3 {
			t.Errorf("eggs under a %s player = %d, want 3", who, got)
		}
	}
}

// awaitSoundNamedIn reads until the named sound arrives and checks its
// category.
func awaitSoundNamedIn(c *servertest.Client, name string, category int32) {
	c.T.Helper()
	for packetID, data := range c.Packets(servertest.ExpectTimeout) {
		if packetID != protocol.IDPlaySoundEffect {
			continue
		}
		sound, got, _, _, _, _, _ := servertest.ReadAmbient(c.T, data)
		if sound == name {
			if got != category {
				c.T.Errorf("%s played in category %d, want %d", name, got, category)
			}
			return
		}
	}
	c.T.Fatalf("the sound %s never arrived within %s", name, servertest.ExpectTimeout)
}

// firstRollHitSeed finds a physics seed whose first step roll hits.
func firstRollHitSeed(t *testing.T) int64 {
	t.Helper()
	for seed := int64(1); seed < 10000; seed++ {
		if rand.New(rand.NewSource(seed)).Intn(world.TurtleEggStepOdds) == 0 {
			return seed
		}
	}
	t.Fatal("no hitting seed")
	return 0
}

// quietSeed finds a physics seed whose first hundred one-in-a-hundred step
// rolls all miss.
func quietSeed(t *testing.T) int64 {
	t.Helper()
	for seed := int64(1); seed < 10000; seed++ {
		r := rand.New(rand.NewSource(seed))
		quiet := true
		for range 100 {
			if r.Intn(world.TurtleEggStepOdds) == 0 {
				quiet = false
				break
			}
		}
		if quiet {
			return seed
		}
	}
	t.Fatal("no quiet seed")
	return 0
}
