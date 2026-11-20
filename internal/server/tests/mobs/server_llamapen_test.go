package mobs

import (
	"fmt"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestSummoningAThousandLlamasIntoAPenKeepsTheTickShort: one command summons
// 1024 llamas beside 1024 others, and the ticks that follow, each llama
// looking for a caravan among the crowd, stay far under the client's 30s
// read timeout.
// vanilla: LlamaFollowCaravanGoal.canUse
func TestSummoningAThousandLlamasIntoAPenKeepsTheTickShort(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Herder")
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()
	for i := range 1024 {
		srv.RunRconLine(fmt.Sprintf("summon llama %d 64 %d {PersistenceRequired:1b}", i%32-16, i/32-16))
	}
	start := time.Now()
	if got := len(srv.RunRconLine("execute as @e[type=llama] at @s run summon llama ~ ~ ~32 {PersistenceRequired:1b}")); got != 1024 {
		t.Fatalf("the command answered %d summons, want 1024", got)
	}
	servertest.RunTicks(t, srv, 5)
	// A whole-herd caravan scan per llama takes about 40s here; the bound
	// leaves room for a loaded runner.
	if took := time.Since(start); took > 10*time.Second {
		t.Errorf("the summon and five ticks took %v, want well under the 30s read timeout", took)
	}
}
