package mobs

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"

	"math"
	"testing"

	server "github.com/golem-mc/golem/internal/server"
)

// The phantom tests: the spawner that stands phantoms up over a player who
// has gone too long without sleeping, and the phantom's own flight. The loop
// is stopped; the spawner's passes are run by hand with a seed each, so a run
// is the same every time.

// insomniac is a rest count far past the 72000 ticks the spawner's roll
// starts to favour a phantom at.
const insomniac = 100_000_000

// phantomPasses bounds the passes a test throws at the spawner: its interval
// is 60 to 120 seconds, 1200 to 2400 ticks, and each interval's roll lands
// with the local difficulty's chance.
const phantomPasses = 60000

// phantomTests is the fixture: a survival player on the hard flat world's
// surface at midnight, the spawner turned on and the loop stopped.
func phantomTests(t *testing.T) (*server.Server, *servertest.Client) {
	t.Helper()
	srv, c := zombieTests(t, servertest.SpawningTestConfig())
	srv.SetDifficulty(3)
	srv.SetDayTime(midnight)
	srv.SetSpawning(true)
	return srv, c
}

// phantomsOf counts the phantoms in the world.
func phantomsOf(srv *server.Server) []server.TestMob {
	var found []server.TestMob
	for _, m := range srv.Mobs() {
		if m.Kind == "minecraft:phantom" {
			found = append(found, m)
		}
	}
	return found
}

// runPhantomPasses runs passes until a phantom stands or the budget is spent.
func runPhantomPasses(srv *server.Server) []server.TestMob {
	for seed := int64(1); seed <= phantomPasses; seed++ {
		srv.PhantomPass(seed)
		if found := phantomsOf(srv); len(found) > 0 {
			return found
		}
	}
	return nil
}

// TestSleeplessPlayerGetsPhantoms checks that a player under the open night
// sky whose rest count is far past 72000 has phantoms stood up 20 to 34
// blocks over them, within ten blocks sideways.
func TestSleeplessPlayerGetsPhantoms(t *testing.T) {
	t.Parallel()
	srv, _ := phantomTests(t)
	srv.SetTimeSinceRest("Watcher", insomniac)

	found := runPhantomPasses(srv)
	if len(found) == 0 {
		t.Fatal("no phantom stood up over a sleepless player in a night's worth of spawner intervals")
	}
	if len(found) > 4 {
		t.Errorf("%d phantoms in one group, want at most 1 + the hard difficulty's 3", len(found))
	}
	for _, m := range found {
		if m.Y < 64+20 || m.Y > 64+34 {
			t.Errorf("a phantom stands at y=%v, want 20 to 34 over the player's 64", m.Y)
		}
		if math.Abs(m.X-0.5) > 10.5 || math.Abs(m.Z-0.5) > 10.5 {
			t.Errorf("a phantom stands at (%v, %v), want within ten blocks of the player", m.X, m.Z)
		}
	}
}

// TestRestedPlayerGetsNoPhantoms checks the same night over a player whose
// rest count is nothing: the spawner finds the roll against it never lands.
func TestRestedPlayerGetsNoPhantoms(t *testing.T) {
	t.Parallel()
	srv, _ := phantomTests(t)
	srv.SetTimeSinceRest("Watcher", 0)

	if found := runPhantomPasses(srv); len(found) != 0 {
		t.Errorf("%d phantoms stood up over a player who had just rested", len(found))
	}
}

// TestSleepingResetsTheRestCount puts a sleepless player to bed: the count
// goes to nothing, and the night after it brings no phantom.
func TestSleepingResetsTheRestCount(t *testing.T) {
	t.Parallel()
	srv, c := phantomTests(t)
	srv.StopTick()
	srv.SetTimeSinceRest("Watcher", insomniac)
	if found := runPhantomPasses(srv); len(found) == 0 {
		t.Fatal("the control: a sleepless player drew no phantom")
	}
	// Clear the control's phantoms away: the sleeper's night counts only
	// what is made after it.
	before := len(phantomsOf(srv))

	c.ExpectSleepStarted(srv, 6.5, 0.5, 0)
	if got := srv.TimeSinceRest("Watcher"); got != 0 {
		t.Fatalf("the rest count after lying down is %d, want 0", got)
	}
	// Standing again under the open sky at night.
	for seed := int64(1); seed <= phantomPasses; seed++ {
		srv.PhantomPass(seed)
	}
	if after := len(phantomsOf(srv)); after != before {
		t.Errorf("%d phantoms came after a sleep, want none", after-before)
	}
}

// TestNoPhantomsWithTheRuleOff checks that spawn_phantoms off keeps the
// spawner quiet over the sleepless.
func TestNoPhantomsWithTheRuleOff(t *testing.T) {
	t.Parallel()
	srv, _ := phantomTests(t)
	srv.SetTimeSinceRest("Watcher", insomniac)
	srv.SetGameRule("spawn_phantoms", false)

	if found := runPhantomPasses(srv); len(found) != 0 {
		t.Errorf("%d phantoms stood up with spawn_phantoms off", len(found))
	}
}

func TestPhantomsHonorLatchedEnemyFlag(t *testing.T) {
	t.Parallel()
	srv, _ := phantomTests(t)
	srv.SetTimeSinceRest("Watcher", insomniac)
	srv.SetGameRule("spawn_mobs", false)
	srv.SetDifficulty(3)
	srv.SetGameRule("spawn_mobs", true)
	if found := runPhantomPasses(srv); len(found) != 0 {
		t.Fatalf("%d phantoms with enemy spawning latched off", len(found))
	}
	srv.SetGameRule("spawn_monsters", true)
	if found := runPhantomPasses(srv); len(found) == 0 {
		t.Fatal("no phantoms after refreshing the enemy flag")
	}
}

// TestNoPhantomsInDaylight checks that a bright sky keeps the spawner quiet.
func TestNoPhantomsInDaylight(t *testing.T) {
	t.Parallel()
	srv, _ := phantomTests(t)
	srv.SetTimeSinceRest("Watcher", insomniac)
	srv.SetDayTime(noon)

	if found := runPhantomPasses(srv); len(found) != 0 {
		t.Errorf("%d phantoms stood up at noon", len(found))
	}
}

// TestRestCountClimbs checks that the count rises a tick at a time
func TestRestCountClimbs(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	srv, _ := servertest.Join(t, cfg, "Actor", servertest.NoTimeSync)
	srv.StopTick()

	srv.SetTimeSinceRest("Actor", 100)
	for range 25 {
		srv.TickSurvival()
	}
	if got := srv.TimeSinceRest("Actor"); got != 125 {
		t.Fatalf("the rest count is %d after 25 ticks from 100, want 125", got)
	}
}

// TestPhantomBitesItsTarget checks the flight: a phantom put over a player
// at night finds them, circles, swoops and bites within a minute of ticks.
func TestPhantomBitesItsTarget(t *testing.T) {
	t.Parallel()
	srv, _ := phantomTests(t)
	srv.SetDayTime(midnight)
	srv.SpawnMob("minecraft:phantom", 0.5, 84, 0.5)

	was := srv.PlayerHealth("Watcher")
	bitten := false
	for range 120 {
		srv.TickMobs(10)
		if srv.PlayerHealth("Watcher") < was {
			bitten = true
			break
		}
	}
	if !bitten {
		t.Fatalf("the phantom never bit its target in 1200 ticks, the player is still at %v", was)
	}
	if got := was - srv.PlayerHealth("Watcher"); got != 3 {
		t.Errorf("default phantom bite on hard dealt %v health, want 3", got)
	}
}

// vanilla: Phantom.PhantomAttackPlayerTargetGoal, TargetingConditions.test
func TestPhantomIgnoresDistantInvisiblePlayer(t *testing.T) {
	t.Parallel()
	for _, invisible := range []bool{false, true} {
		name := "visible"
		if invisible {
			name = "invisible"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			srv, c := phantomTargetScene(t)
			if invisible {
				c.ChatCommand("effect give @s minecraft:invisibility 100 0")
				c.Settle()
			}
			c.ChatCommand("summon minecraft:phantom ~ ~20 ~")
			id := c.ExpectSummonedMob(protocol.EntityTypePhantom)
			c.Settle()
			if invisible {
				servertest.RunTicks(t, srv, 200)
				if got := srv.MobTargetID(id); got != 0 {
					t.Fatalf("phantom targets %d through invisibility twenty blocks above an unarmoured player", got)
				}
				return
			}
			servertest.UntilTicks(t, srv, 200, "phantom targeting the visible player", func() bool {
				return srv.MobTargetID(id) == c.PlayerEntityID
			})
		})
	}
}

func TestPhantomIgnoresPlayerOnItsTeam(t *testing.T) {
	t.Parallel()
	srv, c := phantomTargetScene(t)
	c.ChatCommand("summon minecraft:phantom ~ ~20 ~")
	id := c.ExpectSummonedMob(protocol.EntityTypePhantom)
	c.ChatCommand("team add friends")
	c.ChatCommand("team join friends @s")
	c.ChatCommand("team join friends @e[type=minecraft:phantom]")
	c.Settle()
	for range 200 {
		servertest.RunTicks(t, srv, 1)
		if got := srv.MobTargetID(id); got != 0 {
			t.Fatalf("phantom targets its teammate %d", got)
		}
	}
}

// phantomTargetScene holds a survival player under the open midnight sky.
func phantomTargetScene(t *testing.T) (*server.Server, *servertest.Client) {
	t.Helper()
	srv, c := servertest.Join(t, servertest.HostileTestConfig(), "Watcher",
		servertest.OpsListing(t, "Watcher"), func(s *server.Server) { servertest.FreezeUntilHeld(t, s) })
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()
	srv.SetDayTime(midnight)
	servertest.HoldTicks(t, srv)
	return srv, c
}

// TestNoPhantomsOutsideTheOverworld carries a sleepless player into the
// nether and the end: only the overworld runs a phantom spawner.
// vanilla: MinecraftServer.createLevels
func TestNoPhantomsOutsideTheOverworld(t *testing.T) {
	t.Parallel()
	for _, dim := range []struct {
		name    string
		x, y, z float64
	}{
		{"minecraft:the_nether", 0.5, 70, 0.5},
		{"minecraft:the_end", 0.5, 70, 0.5},
	} {
		t.Run(dim.name, func(t *testing.T) {
			t.Parallel()
			srv, _ := phantomTests(t)
			srv.MovePlayerToDimension("Watcher", dim.name, dim.x, dim.y, dim.z)
			srv.SetTimeSinceRest("Watcher", insomniac)
			for seed := int64(1); seed <= phantomPasses; seed++ {
				srv.PhantomPass(seed)
			}
			for _, m := range srv.MobsInDimension(dim.name) {
				if m.Kind == "minecraft:phantom" {
					t.Fatalf("a phantom stood up at (%v, %v, %v) in %s", m.X, m.Y, m.Z, dim.name)
				}
			}
		})
	}
}

// TestNoPhantomsOverASpectator checks that the spawner passes a spectator by.
func TestNoPhantomsOverASpectator(t *testing.T) {
	t.Parallel()
	srv, _ := phantomTests(t)
	srv.SetTimeSinceRest("Watcher", insomniac)
	srv.RunLine("gamemode spectator Watcher")

	if found := runPhantomPasses(srv); len(found) != 0 {
		t.Errorf("%d phantoms stood up over a spectator", len(found))
	}
}

// TestDeathResetsTheRestCount checks that dying puts the count back to
// nothing.
// vanilla: ServerPlayer.die
func TestDeathResetsTheRestCount(t *testing.T) {
	t.Parallel()
	srv, _ := phantomTests(t)
	srv.SetTimeSinceRest("Watcher", insomniac)
	srv.RunLine("kill Watcher")

	if got := srv.TimeSinceRest("Watcher"); got != 0 {
		t.Errorf("the rest count after a death is %d, want 0", got)
	}
}

// TestASummonedPhantomKeepsItsSize checks Phantom's size tag: the metadata
// int, the saved tag and the 6 + size attack_damage base.
func TestASummonedPhantomKeepsItsSize(t *testing.T) {
	t.Parallel()
	cfg := servertest.HostileTestConfig()
	cfg.GameMode = "creative"
	srv, c := servertest.Join(t, cfg, "Watcher", servertest.OpsListing(t, "Watcher"))
	c.ChatCommand("summon minecraft:phantom ~ ~10 ~ {size:5,NoAI:1b}")
	phantom := c.WatchVariant(srv, "minecraft:phantom")
	if got := phantom.Meta[protocol.MetadataIndexPhantomSize]; len(got) != 1 || got[0] != 5 {
		t.Fatalf("a phantom summoned with size:5 sends size %v, want 5", got)
	}
	answer := func(cmd, key string) string {
		c.ChatCommand(cmd)
		var got string
		c.ReadTicks(srv, 3, func(id int32, data []byte) bool {
			if id != protocol.IDPlaySystemChat {
				return false
			}
			text, ok := protocol.ReadText(protocol.NewReader(data))
			if !ok || text.Translate != key || len(text.With) == 0 {
				return false
			}
			got = text.With[len(text.With)-1].Plain()
			return true
		})
		return got
	}
	if got := answer("data get entity @e[type=minecraft:phantom,limit=1] size", "commands.data.entity.query"); got != "5" {
		t.Errorf("/data get size = %q, want 5", got)
	}
	if got := answer("attribute @e[type=minecraft:phantom,limit=1] minecraft:attack_damage base get", "commands.attribute.base_value.get.success"); got != "11.0" {
		t.Errorf("attack_damage base = %q, want 11.0", got)
	}
	// A resize of a mob whose map is installed moves the base with it.
	// vanilla: Phantom.updatePhantomSizeInfo
	c.ChatCommand("data merge entity @e[type=minecraft:phantom,limit=1] {size:10}")
	if e, ok := c.MetaEntryWithinTicks(srv, 5, phantom.EntityID, protocol.MetadataIndexPhantomSize); !ok || e.VarintVal != 10 {
		t.Errorf("the metadata after a resize to 10 carries size %d (sent: %v), want 10", e.VarintVal, ok)
	}
	if got := answer("attribute @e[type=minecraft:phantom,limit=1] minecraft:attack_damage base get", "commands.attribute.base_value.get.success"); got != "16.0" {
		t.Errorf("attack_damage base after a resize to 10 = %q, want 16.0", got)
	}
	// The merged compound's attributes list carries an edited base, which
	// vanilla reads before the size, so the new size still wins.
	c.ChatCommand("attribute @e[type=minecraft:phantom,limit=1] minecraft:attack_damage base set 20")
	c.ChatCommand("data merge entity @e[type=minecraft:phantom,limit=1] {size:12}")
	if got := answer("attribute @e[type=minecraft:phantom,limit=1] minecraft:attack_damage base get", "commands.attribute.base_value.get.success"); got != "18.0" {
		t.Errorf("attack_damage base after an edit and a resize to 12 = %q, want 18.0", got)
	}
}

// vanilla: Phantom.defineSynchedData, Phantom.onSyncedDataUpdated
func TestPhantomAttackDamageFollowsSizeChanges(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, tag, want string }{
		{"default", "", "2.0"},
		{"zero", ",size:0", "2.0"},
		{"nonzero", ",size:5", "11.0"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := servertest.HostileTestConfig()
			cfg.GameMode = "creative"
			srv, c := servertest.Join(t, cfg, "Watcher", servertest.OpsListing(t, "Watcher"))
			servertest.HoldTicks(t, srv)
			c.ChatCommand("summon minecraft:phantom ~ ~10 ~ {NoAI:1b" + tc.tag + "}")
			c.ExpectSummonedMob(protocol.EntityTypePhantom)
			const target = "@e[type=minecraft:phantom,limit=1]"
			check := func(want string) {
				t.Helper()
				c.ChatCommand("attribute " + target + " minecraft:attack_damage base get")
				var text protocol.Text
				if !c.ReadTicks(srv, 5, func(id int32, data []byte) bool {
					if id != protocol.IDPlaySystemChat {
						return false
					}
					var ok bool
					text, ok = protocol.ReadText(protocol.NewReader(data))
					return ok && text.Translate == "commands.attribute.base_value.get.success" && len(text.With) > 0
				}) {
					t.Fatal("no attack_damage base response")
				}
				if got := text.With[len(text.With)-1].Plain(); got != want {
					t.Fatalf("attack_damage base = %q, want %s", got, want)
				}
			}
			check(tc.want)
			c.ChatCommand("data merge entity " + target + " {size:5}")
			check("11.0")
			c.ChatCommand("data merge entity " + target + " {size:0}")
			check("6.0")
			c.ChatCommand("attribute " + target + " minecraft:attack_damage base set 20")
			c.ChatCommand("data merge entity " + target + " {size:0}")
			check("20.0")
			c.ChatCommand("data merge entity " + target + " {size:10}")
			check("16.0")
		})
	}
}
