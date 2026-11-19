package commands

import (
	"strconv"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// joinOpForStore joins an operator at the smallest view and simulation
// distance with the loop held: the commands under test answer from the
// connection, so no chunk or tick beyond the join is needed. The players in
// others join before the hold: a join waits for ticks to wake its chunks.
func joinOpForStore(t *testing.T, others ...string) (*server.Server, *servertest.Client) {
	t.Helper()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c := servertest.Join(t, cfg, "Alice", servertest.OpsListing(t, "Alice"))
	for _, name := range others {
		servertest.DialServer(t, srv).Join(name)
	}
	servertest.HoldTicks(t, srv)
	return srv, c
}

// storedResult runs cmd under /execute store result into a storage int and
// returns what /data get reads back.
func storedResult(t *testing.T, srv *server.Server, c *servertest.Client, cmd string) string {
	t.Helper()
	return stored(t, srv, c, "result", cmd)
}

// storedSuccess is storedResult for /execute store success.
func storedSuccess(t *testing.T, srv *server.Server, c *servertest.Client, cmd string) string {
	t.Helper()
	return stored(t, srv, c, "success", cmd)
}

// stored is storedResult with the store kind, result or success, given.
func stored(t *testing.T, srv *server.Server, c *servertest.Client, kind, cmd string) string {
	t.Helper()
	c.ChatCommand("execute store " + kind + " storage t:a r int 1 run " + cmd)
	c.ChatCommand("data get storage t:a r")
	var got string
	c.ReadTicks(srv, 0, func(id int32, data []byte) bool {
		if id != protocol.IDPlaySystemChat {
			return false
		}
		r := protocol.NewReader(data)
		text, ok := protocol.ReadText(r)
		if !ok || r.Err() != nil || text.Translate != "commands.data.storage.query" || len(text.With) < 2 {
			return false
		}
		arg := text.With[1]
		got = arg.Text
		for _, e := range arg.Extra {
			got += e.Text
		}
		if arg.Number != nil {
			got = strconv.FormatInt(*arg.Number, 10)
		}
		return true
	})
	if got == "" {
		t.Fatalf("%q stored nothing", cmd)
	}
	return got
}

// expectStored checks each command's stored result against its want.
func expectStored(t *testing.T, srv *server.Server, c *servertest.Client, cases [][2]string) {
	t.Helper()
	for _, tc := range cases {
		if got := storedResult(t, srv, c, tc[0]); got != tc[1] {
			t.Errorf("%s stored %s, want %s", tc[0], got, tc[1])
		}
	}
}

// TestExperienceResults checks the values /experience answers.
// vanilla: ExperienceCommand.queryExperience, addExperience, setExperience
func TestExperienceResults(t *testing.T) {
	t.Parallel()
	srv, c := joinOpForStore(t, "Bob")
	c.ChatCommand("experience set @s 7 levels")
	c.ChatCommand("experience set @s 3 points")
	expectStored(t, srv, c, [][2]string{
		{"experience query @s levels", "7"},
		{"experience query @s points", "3"},
		{"experience add @a 4 points", "2"},
		{"experience set @a 2 levels", "2"},
		{"experience add @s 1 levels", "1"},
	})
}

// TestWorldborderResults checks the values /worldborder answers.
// vanilla: WorldBorderCommand
func TestWorldborderResults(t *testing.T) {
	t.Parallel()
	srv, c := joinOpForStore(t)
	c.ChatCommand("worldborder set 100")
	expectStored(t, srv, c, [][2]string{
		{"worldborder get", "100"},
		{"worldborder set 150.7", "50"},  // 150.7 - 100, truncated
		{"worldborder add -50.5", "-50"}, // 100.2 - 150.7, truncated toward zero
		{"worldborder set 120 10", "19"}, // 120 - 100.2: the lerp's delta
		{"worldborder center 4 8", "0"},  // success without a value
		{"worldborder damage amount 2.9", "2"},
		{"worldborder damage buffer 7.5", "7"},
		{"worldborder warning distance 13", "13"},
		{"worldborder warning time 2s", "40"},
	})
}

// TestWeatherResults checks the values /weather answers.
// vanilla: WeatherCommand.setClear, setRain, setThunder
func TestWeatherResults(t *testing.T) {
	t.Parallel()
	srv, c := joinOpForStore(t)
	expectStored(t, srv, c, [][2]string{
		{"weather clear 600", "600"},
		{"weather rain 1200", "1200"},
		{"weather thunder 30", "30"},
		{"weather clear", "-1"},
		{"weather rain", "-1"},
		{"weather thunder", "-1"},
	})
}
