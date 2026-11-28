package commands

import (
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestCommandBlockFeedbackOnlyWhereSomeoneReadsIt runs `data get` in an
// impulse command block that tracks its output and in one that does not: the
// first records the query line, the second records none yet succeeds. A
// function-style store through execute still gets the value.
// vanilla: CommandSourceStack.sendSuccess, BaseCommandBlock.createSource
func TestCommandBlockFeedbackOnlyWhereSomeoneReadsIt(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	srv.RunRconLine("summon marker 0 64 5 {Tags:[\"m\"]}")
	const get = `execute as @e[type=marker,limit=1] run data get entity @s Pos[0]`
	servertest.Setblock(t, c, 2, 64, 0,
		`command_block[facing=north,conditional=false]{auto:1b,TrackOutput:1b,Command:"data get entity @e[type=marker,limit=1] Pos[0]"}`)
	servertest.Setblock(t, c, 4, 64, 0,
		`command_block[facing=north,conditional=false]{auto:1b,TrackOutput:0b,Command:"data get entity @e[type=marker,limit=1] Pos[0]"}`)
	servertest.Setblock(t, c, 6, 64, 0,
		`command_block[facing=north,conditional=false]{auto:1b,TrackOutput:0b,Command:"`+get+`"}`)
	servertest.RunTicks(t, srv, 5)

	if out := srv.RunRconLine("data get block 2 64 0 LastOutput"); len(out) != 1 || len(out[0].With) < 4 ||
		!strings.Contains(out[0].With[3].Plain(), "commands.data.entity.query") {
		t.Errorf("LastOutput lacks commands.data.entity.query: %+v", out)
	}
	for _, x := range []string{"4", "6"} {
		// No LastOutput is kept, so the get is refused.
		if out := srv.RunRconLine("data get block " + x + " 64 0 LastOutput"); len(out) != 1 || len(out[0].With) != 0 {
			t.Errorf("block at %s recorded output: %+v", x, out)
		}
		out := srv.RunRconLine("data get block " + x + " 64 0 SuccessCount")
		if len(out) != 1 || out[0].With[3].Plain() != "1" {
			t.Errorf("block at %s SuccessCount: %+v", x, out)
		}
	}

	srv.RunRconLine("scoreboard objectives add s dummy")
	srv.RunRconLine("execute store result score #x s run data get entity @e[type=marker,limit=1] Pos[1]")
	out := srv.RunRconLine("scoreboard players get #x s")
	if len(out) != 1 || number(out[0].With[1]) != 64 {
		t.Errorf("stored score: %+v", out)
	}
}
