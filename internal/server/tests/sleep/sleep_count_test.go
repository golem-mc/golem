package sleep

import (
	"bytes"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
)

// sleepPair joins two players with the main loop stopped and returns the
// server and both clients.
func sleepPair(t *testing.T, setup ...func(*server.Server)) (*server.Server, *servertest.Client, *servertest.Client) {
	t.Helper()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig(), append([]func(*server.Server){servertest.NoTimeSync}, setup...)...)
	srv.StopTick()
	a := connect().Join("Actor")
	b := connect().Join("Watcher")
	return srv, a, b
}

// expectSleepChatKey reads system chats until one names the translation key,
// returning its payload.
func expectSleepChatKey(t *testing.T, c *servertest.Client, key string) []byte {
	t.Helper()
	for {
		data := c.NextSystemChat()
		if bytes.Contains(data, []byte(key)) {
			return data
		}
	}
}

// passTheNight runs the sleeping pass long enough for a sleeper to be deep.
func passTheNight(srv *server.Server) {
	for range sleepTicksToSkip + 1 {
		srv.TickSleeping()
	}
}

func requireNightHeld(t *testing.T, srv *server.Server) {
	t.Helper()
	if _, dayTime := srv.Time(); dayTime == 24000 {
		t.Fatal("the night passed")
	}
}

// TestAJoinWhileSomeoneSleepsRaisesTheShareNeeded has a second player join
// while the first lies: the recount makes the night need both, says so, and
// the one sleeper no longer passes it.
func TestAJoinWhileSomeoneSleepsRaisesTheShareNeeded(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig(), servertest.NoTimeSync)
	srv.StopTick()
	a := connect().Join("Actor")
	a.ExpectSleepStarted(srv, 6.5, 0.5, 0)

	connect().Join("Watcher")

	want := servertest.SystemChatPayload("sleep.players_sleeping", true, protocol.Text{Text: "1"}, protocol.Text{Text: "2"})
	if got := expectSleepChatKey(t, a, "sleep.players_sleeping"); !bytes.Equal(got, want) {
		t.Errorf("the sleeper was told % X, want % X", got, want)
	}
	passTheNight(srv)
	requireNightHeld(t, srv)
	if !srv.Sleeping("Actor") {
		t.Error("the sleeper was woken though the night did not pass")
	}
}

// TestALeaveWhileSomeoneSleepsLetsTheNightPass has the awake player leave: the
// recount leaves the sleeper alone in the world, says the night will pass, and
// it does.
func TestALeaveWhileSomeoneSleepsLetsTheNightPass(t *testing.T) {
	t.Parallel()
	srv, a, b := sleepPair(t)
	a.ExpectSleepStarted(srv, 6.5, 0.5, 0)
	b.Disconnect()

	expectSleepChatKey(t, a, "sleep.skipping_night")
	passTheNight(srv)
	if _, dayTime := srv.Time(); dayTime != 24000 {
		t.Errorf("the night left the clock at %d, want 24000", dayTime)
	}
}

// TestAPlayerInTheNetherIsNotCountedForTheNight has the other player stand in
// the nether: the overworld holds one player, who passes the night, and the
// message does not reach the nether.
func TestAPlayerInTheNetherIsNotCountedForTheNight(t *testing.T) {
	t.Parallel()
	srv, a, b := sleepPair(t)
	srv.MovePlayerToDimension("Watcher", world.NetherName, 85, 40, 85)
	b.Settle()
	b.SystemChatsBeforeSettle()
	a.ExpectSleepStarted(srv, 6.5, 0.5, 0)

	for _, chat := range b.SystemChatsBeforeSettle() {
		if bytes.Contains(chat, []byte("sleep.")) {
			t.Errorf("the nether was told of the overworld's sleep: % X", chat)
		}
	}
	passTheNight(srv)
	if _, dayTime := srv.Time(); dayTime != 24000 {
		t.Errorf("the night left the clock at %d, want 24000", dayTime)
	}
}

// TestACrossingOutOfTheOverworldLetsTheNightPass has the awake player leave
// the overworld after the sleep began: the recount at the crossing drops them.
func TestACrossingOutOfTheOverworldLetsTheNightPass(t *testing.T) {
	t.Parallel()
	srv, a, b := sleepPair(t)
	a.ExpectSleepStarted(srv, 6.5, 0.5, 0)
	srv.MovePlayerToDimension("Watcher", world.NetherName, 85, 40, 85)

	expectSleepChatKey(t, a, "sleep.skipping_night")
	passTheNight(srv)
	if _, dayTime := srv.Time(); dayTime != 24000 {
		t.Errorf("the night left the clock at %d, want 24000", dayTime)
	}
	_ = b
}

// TestASpectatorSwitchLetsTheNightPass has the awake player become a
// spectator, who is not counted.
func TestASpectatorSwitchLetsTheNightPass(t *testing.T) {
	t.Parallel()
	srv, a, b := sleepPair(t, servertest.OpsListing(t, "Watcher"))
	a.ExpectSleepStarted(srv, 6.5, 0.5, 0)
	b.ChatCommand("gamemode spectator Watcher")

	expectSleepChatKey(t, a, "sleep.skipping_night")
	passTheNight(srv)
	if _, dayTime := srv.Time(); dayTime != 24000 {
		t.Errorf("the night left the clock at %d, want 24000", dayTime)
	}
}

// TestAnEmptyOverworldKeepsTheLastCount has the lone sleeper leave and a
// newcomer join: the level held no player at the leave, so no recount ran, and
// the newcomer's join is a change from the sleeper still counted.
// vanilla: ServerLevel.updateSleepingPlayerList
func TestAnEmptyOverworldKeepsTheLastCount(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig(), servertest.NoTimeSync)
	srv.StopTick()
	a := connect().Join("Actor")
	a.ExpectSleepStarted(srv, 6.5, 0.5, 0)
	a.Disconnect()

	c := connect()
	c.Login("Newcomer")
	c.Configure()
	var told [][]byte
	c.ReadJoin(func(id int32, data []byte) {
		if id == protocol.IDPlaySystemChat {
			told = append(told, data)
		}
	})
	want := servertest.SystemChatPayload("sleep.players_sleeping", true, protocol.Text{Text: "0"}, protocol.Text{Text: "1"})
	found := false
	for _, chat := range told {
		found = found || bytes.Equal(chat, want)
	}
	if !found {
		t.Errorf("the newcomer was never told 0 of 1 asleep, got % X", told)
	}
}

// TestAJoinWithNobodySleepingSaysNothing checks the recount stays quiet when
// no one slept before or sleeps now, whatever the player count did.
func TestAJoinWithNobodySleepingSaysNothing(t *testing.T) {
	t.Parallel()
	_, a, _ := sleepPair(t)
	for _, chat := range a.SystemChatsBeforeSettle() {
		if bytes.Contains(chat, []byte("sleep.")) {
			t.Errorf("a join with nobody asleep was told as % X", chat)
		}
	}
}
