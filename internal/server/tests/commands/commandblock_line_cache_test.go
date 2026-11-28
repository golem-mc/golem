package commands

import (
	"fmt"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// scoreOf reads a holder's score through RCON.
func scoreOf(t *testing.T, srv *server.Server, holder string) int {
	t.Helper()
	out := srv.RunRconLine("scoreboard players get " + holder + " s")
	if len(out) != 1 || len(out[0].With) < 2 || out[0].With[1].Number == nil {
		t.Fatalf("score of %s: no value in %d lines", holder, len(out))
	}
	return int(*out[0].With[1].Number)
}

// TestRepeatingCommandBlockLineCache runs a repeating block whose line forks
// execute over three entities: each gains one per tick however often the line
// has run before; reprogramming the block runs the new line on the next tick;
// a line that does not parse keeps its error in LastOutput on every run.
func TestRepeatingCommandBlockLineCache(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	servertest.HoldTicks(t, srv)
	srv.RunRconLine("scoreboard objectives add s dummy")
	srv.RunRconLine("scoreboard players set #y s 0")
	for range 3 {
		srv.RunRconLine(`summon armor_stand 4 64 4 {Tags:["x"]}`)
	}
	servertest.RunTicks(t, srv, 1)
	servertest.Setblock(t, c, 2, 64, 0,
		`repeating_command_block[facing=north,conditional=false]{auto:1b,TrackOutput:1b,Command:"execute as @e[tag=x] run scoreboard players add @s s 1"}`)
	servertest.RunTicks(t, srv, 3)
	srv.RunRconLine("scoreboard players set @e[tag=x] s 0")
	servertest.RunTicks(t, srv, 1)
	for range 3 {
		srv.RunRconLine("execute store result score #n s if entity @e[tag=x,scores={s=1}]")
		if got := scoreOf(t, srv, "#n"); got != 3 {
			t.Fatalf("%d stands scored 1 after one tick, want 3", got)
		}
		srv.RunRconLine("scoreboard players set @e[tag=x] s 0")
		servertest.RunTicks(t, srv, 1)
	}

	srv.RunRconLine(`data merge block 2 64 0 {Command:"scoreboard players add #y s 1"}`)
	servertest.RunTicks(t, srv, 2)
	if got := scoreOf(t, srv, "#y"); got < 1 {
		t.Errorf("#y = %d after one tick of the new line, want 1", got)
	}

	for i := range 3 {
		srv.RunRconLine(`data merge block 2 64 0 {Command:"nosuchcommand x"}`)
		servertest.RunTicks(t, srv, 2)
		out := fmt.Sprintf("%+v", srv.RunRconLine("data get block 2 64 0 LastOutput"))
		if !strings.Contains(out, "nosuchcommand") {
			t.Fatalf("run %d: LastOutput lacks the failed line: %s", i, out[max(len(out)-700, 0):])
		}
		srv.RunRconLine(`data merge block 2 64 0 {LastOutput:""}`)
	}
}
