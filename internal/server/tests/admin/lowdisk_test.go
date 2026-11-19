package admin

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
)

// lowDiskScene joins an operator and a plain player to a server whose chunk
// saves all fail (the region folder is a file) and whose disk reports free
// bytes usable, then breaks a block so the next auto-save has a dirty chunk to
// fail on. The loop is then held, so the saves come on the ticks the test
// runs. It returns the server, both clients and the log.
func lowDiskScene(t *testing.T, free uint64, level int) (srv *server.Server, op, guest *servertest.Client, logs *servertest.SyncBuffer) {
	t.Helper()
	logs = &servertest.SyncBuffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(logs, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	cfg.SyncChunkWrites = false
	if err := os.WriteFile(filepath.Join(cfg.LevelName, "region"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	opSetup := servertest.WriteOpsFile(t, servertest.OpRecord{
		UUID:  protocol.OfflineUUID("Admin").String(),
		Name:  "Admin",
		Level: level,
	})
	srv, connect := servertest.Start(t, cfg, opSetup, func(s *server.Server) {
		s.SetFreeSpace(func(string) (uint64, error) { return free, nil })
	})
	op, guest = connect(), connect()
	for _, c := range []*servertest.Client{op, guest} {
		name := "Guest"
		if c == op {
			name = "Admin"
		}
		c.Join(name)
	}
	op.BreakBlock(0, 63, 0)
	op.Settle()
	servertest.HoldTicks(t, srv)
	srv.SetAutoSave(time.Second / 20)
	return srv, op, guest, logs
}

// lowDiskSaveTicks is the budget for the auto-save to run: it falls due on
// every tick.
const lowDiskSaveTicks = 5

func isLowDiskWarning(id int32, _ []byte) bool { return id == protocol.IDPlayLowDiskSpaceWarning }

// TestFailedChunkSaveWarnsOperatorsOnLowDisk checks that a chunk save that
// fails with under 64 MiB usable logs the warning and sends the packet to the
// operator only. Not parallel: it swaps the default logger.
// vanilla: MinecraftServer.reportChunkSaveFailure, DedicatedServer.sendLowDiskSpaceWarning
func TestFailedChunkSaveWarnsOperatorsOnLowDisk(t *testing.T) {
	srv, op, guest, logs := lowDiskScene(t, 64<<20-1, 3)
	if data := op.ExpectWithinTicks(srv, lowDiskSaveTicks, protocol.IDPlayLowDiskSpaceWarning); len(data) != 0 {
		t.Errorf("Low Disk Space Warning carried %d bytes, want none", len(data))
	}
	guest.ExpectNoneWithinTicks(srv, 0, isLowDiskWarning)
	if got := logs.String(); !strings.Contains(got, "Low disk space! Might not be able to save the world.") {
		t.Errorf("the log has no low disk space warning:\n%s", got)
	}
}

// TestFailedChunkSaveWithRoomSendsNoWarning checks the 64 MiB threshold: a
// save that fails with exactly that much usable space is a plain failure.
func TestFailedChunkSaveWithRoomSendsNoWarning(t *testing.T) {
	srv, op, _, logs := lowDiskScene(t, 64<<20, 4)
	servertest.UntilTicks(t, srv, lowDiskSaveTicks, "the failed chunk save is logged", func() bool {
		return strings.Contains(logs.String(), "could not save chunk")
	})
	op.ExpectNoneWithinTicks(srv, 0, isLowDiskWarning)
	if strings.Contains(logs.String(), "Low disk space") {
		t.Error("the log warns of low disk space with 64 MiB free")
	}
}

// TestFailedChunkSaveSparesLowerOperators checks the level gate: an operator
// below level 3 gets no warning.
func TestFailedChunkSaveSparesLowerOperators(t *testing.T) {
	srv, op, _, logs := lowDiskScene(t, 0, 2)
	servertest.UntilTicks(t, srv, lowDiskSaveTicks, "the low disk space log line", func() bool {
		return strings.Contains(logs.String(), "Low disk space")
	})
	op.ExpectNoneWithinTicks(srv, 0, isLowDiskWarning)
}
