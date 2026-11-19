package commands

import (
	"fmt"
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestCommandBlockFunctionSuccessCount runs /function and /return from
// command blocks and reads each block's success count: only a returned
// value counts, so a function that ends without a return, one that returns
// fail and a bare return fail all leave 0.
// vanilla: BaseCommandBlock.performCommand, FunctionCommand.queueFunctionsNoReturn,
// ReturnCommand
func TestCommandBlockFunctionSuccessCount(t *testing.T) {
	t.Parallel()
	srv, connect := startFunctionServer(t, map[string]string{
		"t/function/void.mcfunction": "say void\n",
		"t/function/five.mcfunction": "return 5\n",
		"t/function/fail.mcfunction": "return fail\n",
	}, "Testificate")
	c := connect().Join("Testificate")
	c.ChatCommand("gamerule command_block_output false")
	servertest.AwaitCommandFeedback(t, c, "commands.gamerule.set", "command_block_output", "false")

	pos := c.Pos()
	x, y, z := int(math.Floor(pos.X)), int(math.Floor(pos.Y))+3, int(math.Floor(pos.Z))
	cases := []struct {
		command string
		want    string
	}{
		{"function t:void", "0"},
		{"function t:five", "1"},
		{"function t:fail", "0"},
		{"return fail", "0"},
	}
	for i, tc := range cases {
		bx := x + 3*i
		c.ChatCommand(fmt.Sprintf(`setblock %d %d %d command_block{Command:"%s"}`, bx, y, z, tc.command))
		c.ChatCommand(fmt.Sprintf("setblock %d %d %d redstone_block", bx, y+1, z))
	}
	c.Settle()
	// The power schedules the block's run one tick on.
	servertest.RunTicks(t, srv, 3)
	c.Settle()
	for i, tc := range cases {
		bx := x + 3*i
		c.ChatCommand(fmt.Sprintf("data get block %d %d %d SuccessCount", bx, y, z))
		c.ExpectCommandFeedback("commands.data.block.query",
			fmt.Sprint(bx), fmt.Sprint(y), fmt.Sprint(z), tc.want)
	}
}
