package redstone

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: PistonMovingBlockEntity.tick, PistonMovingBlockEntity.saveAdditional
func TestMovingPistonDataReportsPreviousProgress(t *testing.T) {
	t.Parallel()
	srv, actor := servertest.AttachActor(t)
	servertest.HoldTicks(t, srv)

	servertest.Setblock(t, actor, 2, 66, 2, "piston[facing=east]")
	servertest.Setblock(t, actor, 3, 66, 2, "stone")
	servertest.Setblock(t, actor, 2, 66, 1, "redstone_block")
	for tick, want := range []string{"0.0f", "0.5f"} {
		servertest.RunTicks(t, srv, 1)
		actor.ChatCommand("data get block 4 66 2 progress")
		feedback := servertest.FeedbackComponent(t, actor)
		if feedback.Translate != "commands.data.block.query" || len(feedback.With) != 4 {
			t.Fatalf("tick %d: /data get returned %q", tick+1, feedback.Plain())
		}
		if got := feedback.With[3].Plain(); got != want {
			t.Errorf("tick %d: progress = %s, want %s", tick+1, got, want)
		}
	}
}
