package plugins_test

import (
	"fmt"
	"github.com/golem-mc/golem/internal/config"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/plugin"
)

// helpSender is a non-console sender, paged as a player is.
type helpSender struct {
	level int
	lines []string
}

func (s *helpSender) Name() string              { return "Helper" }
func (s *helpSender) OpLevel() int              { return s.level }
func (s *helpSender) SendMessage(t plugin.Text) { s.lines = append(s.lines, t.PlainString()) }
func (s *helpSender) HasPermission(string) bool { return false }
func (s *helpSender) PermissionSet(string) bool { return false }
func (s *helpSender) take() []string            { l := s.lines; s.lines = nil; return l }
func plainLines(out []protocol.Text) (lines []string) {
	for _, t := range out {
		lines = append(lines, t.Plain())
	}
	return lines
}

func bukkitHelpServer(t *testing.T, extra ...plugin.Command) (*server.Server, plugin.Server) {
	t.Helper()
	var host plugin.Server
	srv := servertest.NewTestServerWithPlugins(t, servertest.BlockTestConfig(), []plugin.Registration{{Name: "Warps", Plugin: &testPlugin{enable: func(s plugin.Server) error {
		host = s
		for _, c := range extra {
			if err := s.Commands().Register(c); err != nil {
				return err
			}
		}
		return nil
	}}}})
	return srv, host
}

var warpCommand = plugin.Command{Name: "warp", Aliases: []string{"wp"}, Description: "Go places", Usage: "/<command> <name>", Run: func(*plugin.CommandContext) (int, error) { return 1, nil }}

func TestHelpWithoutPluginsIsBukkits(t *testing.T) {
	srv := servertest.NewTestServer(t, servertest.BlockTestConfig())
	for _, line := range []string{"help", "?", "bukkit:help"} {
		out := plainLines(srv.RunLine(line))
		if len(out) < 3 || !strings.HasPrefix(out[0], "--------- Help: Index") {
			t.Fatalf("%s produced %v", line, out)
		}
	}
	if out := plainLines(srv.RunLine("help Minecraft")); len(out) < 3 || !strings.Contains(strings.Join(out, "\n"), "/seed") {
		t.Fatalf("help Minecraft produced %v", out)
	}
	if out := srv.RunLine("minecraft:help"); !strings.Contains(fmt.Sprint(out), "command.unknown.command") {
		t.Fatalf("minecraft:help produced %v", out)
	}
}

func TestHelpConsoleIsOnePage(t *testing.T) {
	srv, _ := bukkitHelpServer(t, warpCommand)
	for _, line := range []string{"help", "?", "bukkit:help"} {
		out := plainLines(srv.RunLine(line))
		if len(out) < 20 || !strings.HasPrefix(out[0], "--------- Help: Index ---") || out[1] != "Use /help [n] to get page n of help." {
			t.Fatalf("%s produced %q", line, out[:min(len(out), 3)])
		}
		if len(out[0]) != 49 {
			t.Fatalf("header %q is %d wide", out[0], len(out[0]))
		}
		all := strings.Join(out, "\n")
		for _, want := range []string{"Aliases: Lists command aliases", "Bukkit: All commands for Bukkit", "Minecraft: All commands for Minecraft", "Warps: All commands for Warps", "/warp: Go places", "/seed: A Mojang provided command."} {
			if !strings.Contains(all, want) {
				t.Fatalf("%s lacks %q", line, want)
			}
		}
		if strings.Contains(all, "/wp: Alias") {
			t.Fatalf("index lists an alias")
		}
	}
}

func TestHelpPagesForAPlayer(t *testing.T) {
	_, host := bukkitHelpServer(t, warpCommand)
	op := &helpSender{level: 4}
	if _, err := host.Dispatch(op, "help"); err != nil {
		t.Fatal(err)
	}
	page := op.take()
	if len(page) != 10 || !strings.HasPrefix(page[0], "--------- Help: Index") || len(page[0]) != 55 && !strings.Contains(page[0], "-") {
		t.Fatalf("page 1: %q", page)
	}
	total := strings.TrimSuffix(strings.SplitN(strings.SplitN(page[0], "/", 2)[1], ")", 2)[0], "")
	if _, err := host.Dispatch(op, "help 2"); err != nil {
		t.Fatal(err)
	}
	if two := op.take(); len(two) != 10 || !strings.Contains(two[0], "(2/"+total+")") {
		t.Fatalf("page 2: %q", two)
	}
	if _, err := host.Dispatch(op, "help 999"); err != nil {
		t.Fatal(err)
	}
	if last := op.take(); !strings.Contains(last[0], "("+total+"/"+total+")") {
		t.Fatalf("clamped page: %q", last)
	}
	// A page-width topic is not cut off for a non-player but wraps at 55.
	for _, line := range page[1:] {
		if len(line) > 55+4 {
			t.Fatalf("line %q is too wide", line)
		}
	}
}

func TestHelpCommandAndPluginTopics(t *testing.T) {
	srv, _ := bukkitHelpServer(t, warpCommand)
	out := plainLines(srv.RunLine("help warp"))
	want := []string{"--------- Help: /warp ---", "Go places", "Description: Go places", "Usage: /warp <name>", "Aliases: wp"}
	if len(out) < 4 {
		t.Fatalf("help warp: %q", out)
	}
	if !strings.HasPrefix(out[0], want[0]) || strings.Contains(out[0], "(") {
		t.Fatalf("header %q", out[0])
	}
	if got := strings.Join(out[1:], "\n"); !strings.Contains(got, want[2]) || !strings.Contains(got, want[3]) || !strings.Contains(got, want[4]) {
		t.Fatalf("help warp: %q", out)
	}
	if got := plainLines(srv.RunLine("help /warp")); len(got) != len(out) {
		t.Fatalf("help /warp: %q", got)
	}
	plug := plainLines(srv.RunLine("help Warps"))
	if len(plug) != 3 || !strings.HasPrefix(plug[0], "--------- Help: Warps ---") || plug[1] != "Below is a list of all Warps commands:" || plug[2] != "/warp: Go places" {
		t.Fatalf("help Warps: %q", plug)
	}
	if got := plainLines(srv.RunLine("help wp")); len(got) < 2 || got[1] != "Alias for /warp" {
		t.Fatalf("help wp: %q", got)
	}
	aliases := strings.Join(plainLines(srv.RunLine("help Aliases")), "\n")
	if !strings.Contains(aliases, "/?: Alias for /help") || !strings.Contains(aliases, "/wp: Alias for /warp") {
		t.Fatalf("help Aliases: %q", aliases)
	}
	if got := plainLines(srv.RunLine("help nonsense-topic-xyz")); len(got) != 1 || got[0] != "No help for nonsense-topic-xyz" {
		t.Fatalf("unknown topic: %q", got)
	}
	// A near miss searches.
	if got := plainLines(srv.RunLine("help wrap")); len(got) < 3 || !strings.HasPrefix(got[0], "--------- Help: Search ---") || got[1] != "Search for: wrap" {
		t.Fatalf("help wrap: %q", got)
	}
}

func TestHelpHidesWhatTheSenderMayNotUse(t *testing.T) {
	secret := plugin.Command{Name: "secret", Description: "Hidden", Permission: "warps.secret", Run: func(*plugin.CommandContext) (int, error) { return 1, nil }}
	srv, host := bukkitHelpServer(t, warpCommand, secret)
	if got := strings.Join(plainLines(srv.RunLine("help")), "\n"); !strings.Contains(got, "/secret: Hidden") {
		t.Fatalf("console does not see /secret: %q", got)
	}
	user := &helpSender{}
	if _, err := host.Dispatch(user, "help Warps"); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(user.take(), "\n"); strings.Contains(got, "/secret") || !strings.Contains(got, "/warp") {
		t.Fatalf("player sees %q", got)
	}
	for _, topic := range []string{"secret", "seed"} {
		if _, err := host.Dispatch(user, "help "+topic); err != nil {
			t.Fatal(err)
		}
		if got := user.take(); len(got) != 1 || got[0] != "No help for "+topic {
			t.Fatalf("help %s: %q", topic, got)
		}
	}
}

func TestHelpYAML(t *testing.T) {
	t.Chdir(t.TempDir())
	yml := `general-topics:
    Rules:
        shortText: The rules
        fullText: Be &akind
index-topics:
    Staff:
        shortText: Staff things
        preamble: Staff commands
        commands: ["/warp"]
amended-topics:
    /warp:
        shortText: "<text> (amended)"
        fullText: "<text>\nExtra"
ignore-plugins: []
command-topics-in-master-index: false
`
	if err := os.WriteFile("help.yml", []byte(yml), 0o644); err != nil {
		t.Fatal(err)
	}
	srv, _ := bukkitHelpServer(t, warpCommand)
	index := strings.Join(plainLines(srv.RunLine("help")), "\n")
	for _, want := range []string{"Rules: The rules", "Staff: Staff things"} {
		if !strings.Contains(index, want) {
			t.Fatalf("index lacks %q: %s", want, index)
		}
	}
	if strings.Contains(index, "/warp") || strings.Contains(index, "/seed") {
		t.Fatalf("index lists commands: %s", index)
	}
	if got := plainLines(srv.RunLine("help Rules")); len(got) != 3 || got[1] != "The rules" || got[2] != "Be kind" {
		t.Fatalf("help Rules: %q", got)
	}
	if got := plainLines(srv.RunLine("help Staff")); len(got) != 3 || got[2] != "/warp: Go places (amended)" {
		t.Fatalf("help Staff: %q", got)
	}
	if got := strings.Join(plainLines(srv.RunLine("help warp")), "\n"); !strings.HasSuffix(got, "Extra") {
		t.Fatalf("help warp: %q", got)
	}
}

func TestHelpYAMLIsWrittenOnFirstStart(t *testing.T) {
	t.Chdir(t.TempDir())
	servertest.NewTestServer(t, servertest.BlockTestConfig())
	data, err := os.ReadFile("help.yml")
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"general-topics", "index-topics", "amended-topics", "ignore-plugins", "command-topics-in-master-index: true"} {
		if !strings.Contains(string(data), key) {
			t.Fatalf("help.yml lacks %q", key)
		}
	}
}

func TestHelpOverrideWins(t *testing.T) {
	override := plugin.Command{Name: "help", Override: true, Run: func(ctx *plugin.CommandContext) (int, error) {
		ctx.Sender.SendMessage(plugin.Plain("mine"))
		return 1, nil
	}}
	srv, host := bukkitHelpServer(t, override)
	if got := plainLines(srv.RunLine("help")); len(got) != 1 || got[0] != "mine" {
		t.Fatalf("help: %q", got)
	}
	if got := plainLines(srv.RunLine("bukkit:help")); len(got) < 2 || !strings.HasPrefix(got[0], "--------- Help: Index") {
		t.Fatalf("bukkit:help: %q", got)
	}
	if got := plainLines(srv.RunLine("?")); len(got) < 2 || !strings.HasPrefix(got[0], "--------- Help: Index") {
		t.Fatalf("?: %q", got)
	}
	_ = host
}

func TestHelpMapAPI(t *testing.T) {
	_, host := bukkitHelpServer(t, warpCommand)
	m := host.Help()
	m.Add(plugin.NewHelpTopic("Lore", "Story", "Once upon a time", ""))
	topic, ok := m.Topic("lore")
	if !ok || topic.Name() != "Lore" || topic.ShortText() != "Story" {
		t.Fatalf("topic: %v %v", topic, ok)
	}
	if _, ok := m.Topic("/warp"); !ok {
		t.Fatal("no /warp topic")
	}
	if idx, ok := m.Topic(""); !ok || idx.Name() != "Index" {
		t.Fatal("no index")
	}
	topics := m.Topics()
	if topics[0].Name() != "Aliases" || !strings.HasPrefix(topics[len(topics)-1].Name(), "/") {
		t.Fatalf("topics out of order: first %s last %s", topics[0].Name(), topics[len(topics)-1].Name())
	}
}

// joinedRoots is the root names of the command tree a joining player is sent.
func joinedRoots(t *testing.T, srv *server.Server) []string {
	t.Helper()
	var names []string
	c := servertest.DialServer(t, srv)
	c.ViewAsk = config.MinDistance
	c.Login("Reader")
	c.Configure()
	c.ReadJoin(func(id int32, data []byte) {
		if id == protocol.IDPlayCommands {
			nodes := servertest.DecodeCommands(t, data)
			for _, child := range nodes[0].Children {
				names = append(names, nodes[child].Name)
			}
		}
	})
	return names
}

func TestHelpRootsInTheCommandTree(t *testing.T) {
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	plain := joinedRoots(t, servertest.NewTestServer(t, cfg))
	for _, name := range []string{"bukkit:help", "?", "help"} {
		if !slices.Contains(plain, name) {
			t.Fatalf("a server without plugins lacks %s: %v", name, plain)
		}
	}
	if slices.Contains(plain, "minecraft:help") || slices.Contains(plain, "minecraft:list") {
		t.Fatalf("a server without plugins offers a minecraft: root: %v", plain)
	}
	srv, _ := bukkitHelpServer(t)
	with := joinedRoots(t, srv)
	for _, name := range []string{"bukkit:help", "?", "help"} {
		if !slices.Contains(with, name) {
			t.Fatalf("a server with a plugin lacks %s: %v", name, with)
		}
	}
	if slices.Contains(with, "minecraft:help") {
		t.Fatalf("minecraft:help is in the tree: %v", with)
	}
}
