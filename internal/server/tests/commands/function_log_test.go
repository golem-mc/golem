package commands

import (
	"bytes"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

type syncBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// TestFunctionSummonsAndSetblocksLogNothingAtInfo runs a function that
// summons a marker and sets a block 100 times: the server log gets no INFO
// line for them, while a player's /summon still sends the operator echo.
// Not parallel: it swaps the default logger.
// vanilla: SummonCommand.spawnEntity, SetBlockCommand.setBlock
func TestFunctionSummonsAndSetblocksLogNothingAtInfo(t *testing.T) {
	logs := &syncBuf{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(logs, &slog.HandlerOptions{Level: slog.LevelInfo})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	body := strings.Repeat("summon marker ~ ~ ~ {Tags:[\"z\"]}\nsetblock ~ ~ ~ stone\nfill ~ ~1 ~ ~ ~1 ~ stone\nclone ~ ~ ~ ~ ~ ~ ~ ~5 ~\n", 100)
	srv, connect := startFunctionServer(t, map[string]string{"t/function/churn.mcfunction": body}, "Testificate")
	c := connect().Join("Testificate")
	c.ChatCommand("function t:churn")
	c.ExpectCommandFeedback("commands.function.scheduled.single", "t:churn")
	servertest.RunTicks(t, srv, 3)
	for _, banned := range []string{"summoned", "block set", "region filled", "region cloned"} {
		if strings.Contains(logs.String(), banned) {
			t.Errorf("the function wrote an INFO line containing %q", banned)
		}
	}

	c.ChatCommand("summon marker ~ ~ ~")
	c.ExpectFeedbackLine("commands.summon.success")
}
