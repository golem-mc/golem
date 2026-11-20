package commands

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestJfrStartStop checks /jfr records a trace under debug/ and refuses a
// second start and a stop with nothing running. Not parallel: a process has
// one trace.
// vanilla: JfrCommand
func TestJfrStartStop(t *testing.T) {
	dir := t.TempDir()
	// A relative config path, as a launched server has: the server dir is ".".
	t.Chdir(dir)
	_, c := servertest.Join(t, servertest.BlockTestConfig(), "Alice", servertest.OpsListing(t, "Alice"),
		func(s *server.Server) { s.SetConfigFile("server.properties") })

	c.ChatCommand("jfr start")
	c.ExpectCommandFeedback("commands.jfr.started")
	c.ChatCommand("jfr start")
	c.ExpectFailure("commands.jfr.start.failed")
	c.ChatCommand("jfr stop")
	msg := c.ExpectPlay(protocol.IDPlaySystemChat)
	files, _ := filepath.Glob(filepath.Join(dir, "debug", "server-*.trace"))
	if len(files) != 1 {
		t.Fatalf("trace files = %v, want one", files)
	}
	if fi, err := os.Stat(files[0]); err != nil || fi.Size() == 0 {
		t.Errorf("trace %s: %v, %v; want non-empty", files[0], fi, err)
	}
	var want protocol.Buffer
	want.PutText(protocol.Text{Translate: "commands.jfr.stopped", With: []protocol.Text{{
		Text:      filepath.Join("debug", filepath.Base(files[0])),
		Underline: true,
		ClickCopy: files[0],
		HoverText: &protocol.Text{Translate: "chat.copy.click"},
	}}})
	want.PutBool(false)
	if string(msg) != string(want.Bytes()) {
		t.Errorf("/jfr stop answered % X, want % X", msg, want.Bytes())
	}
	c.ChatCommand("jfr stop")
	c.ExpectComponentError("commands.jfr.dump.failed", protocol.Text{Text: "Not currently profiling"})
}

// TestJfrNeedsOwner checks a level 2 operator is not offered /jfr.
func TestJfrNeedsOwner(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	_, connect := servertest.Start(t, cfg, servertest.WriteOpsFile(t,
		servertest.OpRecord{UUID: protocol.OfflineUUID("Gm").String(), Name: "Gm", Level: 2}))
	c := connect()
	c.Login("Gm")
	c.Configure()
	nodes := servertest.DecodeCommands(t, c.AwaitPlay(protocol.IDPlayCommands))
	var names []string
	for _, i := range nodes[0].Children {
		names = append(names, nodes[i].Name)
	}
	if slices.Contains(names, "jfr") || !slices.Contains(names, "tp") {
		t.Errorf("level 2 command tree = %v, want tp but not jfr", names)
	}
}
