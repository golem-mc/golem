package plugins_test

import (
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/scoreboard"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/plugin"
)

// nextBoardPacket also rejects out-of-order scoreboard traffic during a switch.
func nextBoardPacket(t *testing.T, c *servertest.Client, srv *server.Server, want int32) []byte {
	t.Helper()
	var got []byte
	if !c.ReadTicks(srv, 0, func(id int32, data []byte) bool {
		if !servertest.PacketIs(protocol.IDPlayUpdateObjectives, protocol.IDPlayDisplayObjective, protocol.IDPlayUpdateScore, protocol.IDPlayResetScore, protocol.IDPlaySetPlayerTeam)(id, data) {
			return false
		}
		if id != want {
			t.Fatalf("scoreboard packet %x, want %x", id, want)
		}
		got = data
		return true
	}) {
		t.Fatalf("missing scoreboard packet %x", want)
	}
	return got
}

// vanilla: ServerScoreboard.getStartTrackingPackets, PlayerList.updateEntireScoreboard
func TestPluginScoreboards(t *testing.T) {
	var host plugin.Server
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv := servertest.NewTestServerWithPlugins(t, cfg, []plugin.Registration{{Name: "boards", Plugin: &testPlugin{enable: func(s plugin.Server) error { host = s; return nil }}}})
	c := servertest.DialServer(t, srv)
	c.ViewAsk = config.MinDistance
	c.Join("BoardUser")
	other := servertest.DialServer(t, srv)
	other.ViewAsk = config.MinDistance
	other.Join("OtherUser")
	servertest.HoldTicks(t, srv)
	p, _ := host.PlayerNamed("BoardUser")
	main := host.Scoreboard()
	if !reflect.DeepEqual(p.Scoreboard(), main) {
		t.Fatal("default board is not main")
	}
	if _, err := main.AddObjective("bad", "unknown", plugin.Plain("bad"), plugin.RenderInteger); err == nil {
		t.Fatal("unknown criterion accepted")
	}
	o, err := main.AddObjective("same", "dummy", plugin.Plain("Main"), plugin.RenderInteger)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := main.AddObjective("same", "dummy", plugin.Plain("duplicate"), plugin.RenderInteger); err == nil {
		t.Fatal("duplicate objective accepted")
	}
	o.SetScore("entry", 7)
	for _, viewer := range []*servertest.Client{c, other} {
		viewer.ExpectNoneWithinTicks(srv, 0, servertest.PacketIs(protocol.IDPlayUpdateObjectives, protocol.IDPlayUpdateScore))
	}
	main.SetDisplay(plugin.SlotSidebar, o)
	objective := func(viewer *servertest.Client, mode uint8, title string, render int32) {
		t.Helper()
		r := protocol.NewReader(nextBoardPacket(t, viewer, srv, protocol.IDPlayUpdateObjectives))
		if r.ReadString(32767) != "same" || r.ReadU8() != mode {
			t.Fatal("objective name/mode")
		}
		if mode != 1 {
			text, ok := protocol.ReadText(r)
			if !ok || text.Plain() != title || r.ReadVarInt() != render || r.ReadBool() {
				t.Fatal("objective parameters")
			}
		}
		if r.Err() != nil {
			t.Fatal(r.Err())
		}
	}
	display := func(viewer *servertest.Client) {
		t.Helper()
		r := protocol.NewReader(nextBoardPacket(t, viewer, srv, protocol.IDPlayDisplayObjective))
		if r.ReadVarInt() != 1 || r.ReadString(32767) != "same" || r.Err() != nil {
			t.Fatal("display slot")
		}
	}
	score := func(viewer *servertest.Client, value int32, label string) {
		t.Helper()
		r := protocol.NewReader(nextBoardPacket(t, viewer, srv, protocol.IDPlayUpdateScore))
		if r.ReadString(32767) != "entry" || r.ReadString(32767) != "same" || r.ReadVarInt() != value {
			t.Fatal("score")
		}
		if r.ReadBool() {
			t.Fatal("score entry name was overridden")
		}
		if r.ReadBool() {
			if label == "" || r.ReadVarInt() != int32(scoreboard.FormatFixed) {
				t.Fatal("score number format")
			}
			text, ok := protocol.ReadText(r)
			if !ok || text.Plain() != label {
				t.Fatal("fixed score number")
			}
		} else if label != "" {
			t.Fatal("missing score number format")
		}
		if r.Err() != nil || r.Remaining() != 0 {
			t.Fatal("score packet remainder")
		}
	}
	for _, viewer := range []*servertest.Client{c, other} {
		objective(viewer, 0, "Main", 0)
		display(viewer)
		score(viewer, 7, "")
	}
	team, err := main.AddTeam("same")
	if err != nil {
		t.Fatal(err)
	}
	teamPacket := func(viewer *servertest.Client, mode uint8) *protocol.Reader {
		t.Helper()
		r := protocol.NewReader(nextBoardPacket(t, viewer, srv, protocol.IDPlaySetPlayerTeam))
		if r.ReadString(32767) != "same" || r.ReadU8() != mode || r.Err() != nil {
			t.Fatal("team name/mode")
		}
		return r
	}
	for _, viewer := range []*servertest.Client{c, other} {
		teamPacket(viewer, 0)
		teamPacket(viewer, 2)
	}
	team.AddEntry("entry")
	for _, viewer := range []*servertest.Client{c, other} {
		r := teamPacket(viewer, 3)
		if r.ReadVarInt() != 1 || r.ReadString(32767) != "entry" || r.Err() != nil {
			t.Fatal("team entry")
		}
	}
	private := host.NewScoreboard()
	if _, err := private.AddObjective("unsaved", "dummy", plugin.Plain("Unsaved"), plugin.RenderInteger); err != nil {
		t.Fatal(err)
	}

	po, err := private.AddObjective("same", "dummy", plugin.Plain("Private"), plugin.RenderHearts)
	if err != nil {
		t.Fatal(err)
	}
	po.SetScore("entry", 12)
	private.SetDisplay(plugin.SlotSidebar, po)
	pt, err := private.AddTeam("same")
	if err != nil {
		t.Fatal(err)
	}
	pt.SetPrefix(plugin.Plain("[P]"))
	pt.AddEntry("entry")
	p.SetScoreboard(private)
	teamPacket(c, 1)
	objective(c, 1, "", 0)
	r := teamPacket(c, 0)
	text, _ := protocol.ReadText(r)
	prefix, _ := protocol.ReadText(r)
	if text.Plain() != "same" || prefix.Plain() != "[P]" {
		t.Fatal("private team snapshot")
	}
	objective(c, 0, "Private", 1)
	display(c)
	score(c, 12, "")
	if !reflect.DeepEqual(p.Scoreboard(), private) {
		t.Fatal("private board not assigned")
	}
	p.SetScoreboard(private)
	other.ExpectNoneWithinTicks(srv, 0, servertest.PacketIs(protocol.IDPlayUpdateObjectives, protocol.IDPlayUpdateScore, protocol.IDPlaySetPlayerTeam, protocol.IDPlayDisplayObjective))
	c.ExpectNoneWithinTicks(srv, 0, servertest.PacketIs(protocol.IDPlayUpdateObjectives, protocol.IDPlaySetPlayerTeam))
	po.SetScore("entry", 13)
	score(c, 13, "")
	srv.RunLine("scoreboard players set entry same 8")
	if n, ok := o.Score("entry"); !ok || n != 8 {
		t.Fatal("command and plugin board differ")
	}
	score(other, 8, "")
	c.ExpectNoneWithinTicks(srv, 0, servertest.PacketIs(protocol.IDPlayUpdateScore))
	other.ExpectNoneWithinTicks(srv, 0, servertest.PacketIs(protocol.IDPlayUpdateScore))
	label := plugin.Plain("label")
	po.SetScoreDisplay("entry", &label)
	score(c, 13, "label")
	po.SetScoreDisplay("entry", nil)
	score(c, 13, "")
	po.SetDisplay(plugin.Plain("Renamed"))
	objective(c, 2, "Renamed", 1)
	po.SetRender(plugin.RenderInteger)
	objective(c, 2, "Renamed", 0)
	pt.SetColor("red")
	r = teamPacket(c, 2)
	protocol.ReadText(r)
	protocol.ReadText(r)
	protocol.ReadText(r)
	if r.ReadVarInt() != 0 || r.ReadVarInt() != 0 || !r.ReadBool() || r.ReadVarInt() != 12 || r.ReadU8() != 3 || r.Err() != nil {
		t.Fatal("team parameters")
	}
	private.ResetScores("entry")
	r = protocol.NewReader(nextBoardPacket(t, c, srv, protocol.IDPlayResetScore))
	if r.ReadString(32767) != "entry" || r.ReadBool() || r.Err() != nil {
		t.Fatal("score reset")
	}
	if _, ok := po.Score("entry"); ok {
		t.Fatal("score was not reset")
	}
	p.SetScoreboard(main)
	teamPacket(c, 1)
	objective(c, 1, "", 0)
	teamPacket(c, 0)
	objective(c, 0, "Main", 0)
	display(c)
	score(c, 8, "")
	// Both views use identical names, so stale definitions must leave before new ones arrive.
	pt.Remove()
	po.Remove()
	c.ExpectNoneWithinTicks(srv, 0, servertest.PacketIs(protocol.IDPlayUpdateObjectives, protocol.IDPlaySetPlayerTeam))
	servertest.ReleaseTicks(t, srv)
	srv.RunLine("scoreboard players set entry same 9")
	servertest.HoldTicks(t, srv)
	score(c, 9, "")
	score(other, 9, "")
	if n, ok := o.Score("entry"); !ok || n != 9 {
		t.Fatal("command and plugin board differ")
	}
	// Only the main board is written to scoreboard.dat.
	if _, err := private.AddTeam("unsaved"); err != nil {
		t.Fatal(err)
	}
	servertest.ReleaseTicks(t, srv)
	srv.SaveAll(true)
	servertest.HoldTicks(t, srv)
	data, ok, err := world.ReadSavedData(cfg.LevelName, scoreboard.File, world.MaxSavedData)
	if err != nil || !ok {
		t.Fatalf("saved scoreboard: %v, %v at %s", ok, err, filepath.Join(cfg.LevelName, scoreboard.File))
	}
	saved := scoreboard.New(nil)
	saved.Load(data)
	if got, ok := saved.Score("entry", "same"); !ok || got.Value != 9 {
		t.Fatal("main score not saved")
	}
	if got, ok := saved.Team("same"); !ok || got.Prefix != nil {
		t.Fatalf("private team reached save: %+v", got)
	}
	if _, ok := saved.Objective("unsaved"); ok {
		t.Fatal("private objective was saved")
	}
	if _, ok := saved.Team("unsaved"); ok {
		t.Fatal("private team was saved")
	}
}

// vanilla: PlayerTeam setters and ServerScoreboard.onTeamChanged
func TestPluginScoreboardTeamOptions(t *testing.T) {
	var host plugin.Server
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv := servertest.NewTestServerWithPlugins(t, cfg, []plugin.Registration{{Name: "options", Plugin: &testPlugin{enable: func(s plugin.Server) error { host = s; return nil }}}})
	c := servertest.DialServer(t, srv)
	c.ViewAsk = config.MinDistance
	c.Join("Options")
	servertest.HoldTicks(t, srv)
	board := host.NewScoreboard()
	p, _ := host.PlayerNamed("Options")
	p.SetScoreboard(board)
	team, err := board.AddTeam("options")
	if err != nil {
		t.Fatal(err)
	}
	nextBoardPacket(t, c, srv, protocol.IDPlaySetPlayerTeam)
	nextBoardPacket(t, c, srv, protocol.IDPlaySetPlayerTeam)
	if _, err := board.AddTeam("options"); err == nil {
		t.Fatal("duplicate team accepted")
	}
	team.SetDisplay(plugin.Plain("Display"))
	team.SetPrefix(plugin.Plain("["))
	team.SetSuffix(plugin.Plain("]"))
	team.SetColor("gold")
	team.SetFriendlyFire(false)
	team.SetSeeInvisible(false)
	team.SetNameTags("hideForOwnTeam")
	team.SetCollision("pushOtherTeams")
	var r *protocol.Reader
	for range 8 {
		r = protocol.NewReader(nextBoardPacket(t, c, srv, protocol.IDPlaySetPlayerTeam))
	}
	if r.ReadString(32767) != "options" || r.ReadU8() != 2 {
		t.Fatal("team update")
	}
	display, _ := protocol.ReadText(r)
	prefix, _ := protocol.ReadText(r)
	suffix, _ := protocol.ReadText(r)
	if display.Plain() != "Display" || prefix.Plain() != "[" || suffix.Plain() != "]" || r.ReadVarInt() != 3 || r.ReadVarInt() != 2 || !r.ReadBool() || r.ReadVarInt() != 6 || r.ReadU8() != 0 || r.Err() != nil {
		t.Fatal("team update fields")
	}
	if team.Name() != "options" || team.Display().PlainString() != "Display" || team.Prefix().PlainString() != "[" || team.Suffix().PlainString() != "]" || team.Color() != "gold" || team.FriendlyFire() || team.SeeInvisible() || team.NameTags() != "hideForOwnTeam" || team.Collision() != "pushOtherTeams" {
		t.Fatal("team getters")
	}
	team.SetColor("reset")
	nextBoardPacket(t, c, srv, protocol.IDPlaySetPlayerTeam)
	if team.Color() != "reset" {
		t.Fatal("reset colour")
	}
	team.AddEntry("entry")
	nextBoardPacket(t, c, srv, protocol.IDPlaySetPlayerTeam)
	got, ok := board.TeamOf("entry")
	if !ok || got.Name() != "options" || !slices.Equal(team.Entries(), []string{"entry"}) {
		t.Fatal("team lookup")
	}
	entries := team.Entries()
	entries[0] = "mutated"
	if team.Entries()[0] != "entry" {
		t.Fatal("mutable entries snapshot")
	}
	if got, ok := board.Team("options"); !ok || got.Name() != "options" || len(board.Teams()) != 1 {
		t.Fatal("team enumeration")
	}
	team.RemoveEntry("entry")
	nextBoardPacket(t, c, srv, protocol.IDPlaySetPlayerTeam)
	if _, ok := board.TeamOf("entry"); ok {
		t.Fatal("entry not removed")
	}
	team.Remove()
	nextBoardPacket(t, c, srv, protocol.IDPlaySetPlayerTeam)
	if _, ok := board.Team("options"); ok || len(board.Teams()) != 0 {
		t.Fatal("team not removed")
	}
	objective, err := board.AddObjective("health", "health", plugin.Plain("HP"), plugin.RenderHearts)
	if err != nil {
		t.Fatal(err)
	}
	if objective.Name() != "health" || objective.Criterion() != "health" || objective.Display().PlainString() != "HP" || objective.Render() != plugin.RenderHearts || len(board.Objectives()) != 1 {
		t.Fatal("objective getters")
	}
	if _, ok := board.Display(plugin.SlotSidebar); ok {
		t.Fatal("unset display")
	}
	board.SetDisplay(plugin.SlotSidebar, objective)
	nextBoardPacket(t, c, srv, protocol.IDPlayUpdateObjectives)
	nextBoardPacket(t, c, srv, protocol.IDPlayDisplayObjective)
	if got, ok := board.Display(plugin.SlotSidebar); !ok || got.Name() != "health" {
		t.Fatal("display getter")
	}
	// A read-only objective cannot be written by an explicit score setter.
	objective.SetScore("entry", 100)
	if score, _ := objective.Score("entry"); score != 0 {
		t.Fatal("read-only score was written")
	}
	board.SetDisplay(plugin.SlotSidebar, nil)
	nextBoardPacket(t, c, srv, protocol.IDPlayUpdateObjectives)
	if _, ok := board.Display(plugin.SlotSidebar); ok {
		t.Fatal("display was not cleared")
	}
	objective.Remove()
	if _, ok := board.Objective("health"); ok || len(board.Objectives()) != 0 {
		t.Fatal("objective not removed")
	}
}

func TestPluginScoreboardHandleLifetimes(t *testing.T) {
	var host plugin.Server
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv := servertest.NewTestServerWithPlugins(t, cfg, []plugin.Registration{{Name: "lifetimes", Plugin: &testPlugin{enable: func(s plugin.Server) error { host = s; return nil }}}})
	c := servertest.DialServer(t, srv)
	c.ViewAsk = config.MinDistance
	c.Join("Lifetimes")
	servertest.HoldTicks(t, srv)
	p, _ := host.PlayerNamed("Lifetimes")
	for _, mode := range []string{"api-main", "api-private", "commands"} {
		t.Run(mode, func(t *testing.T) {
			board := host.Scoreboard()
			if mode == "api-private" {
				board = host.NewScoreboard()
			}
			p.SetScoreboard(board)
			create := func() (plugin.Objective, plugin.Team) {
				t.Helper()
				if mode == "commands" {
					srv.RunLine("scoreboard objectives add same dummy")
					srv.RunLine("team add same")
				} else {
					if _, err := board.AddObjective("same", "dummy", plugin.Plain("Same"), plugin.RenderInteger); err != nil {
						t.Fatal(err)
					}
					if _, err := board.AddTeam("same"); err != nil {
						t.Fatal(err)
					}
				}
				o, ok := board.Objective("same")
				if !ok {
					t.Fatal("missing objective")
				}
				team, ok := board.Team("same")
				if !ok {
					t.Fatal("missing team")
				}
				if board.Objectives()[0] != o || board.Teams()[0] != team {
					t.Fatal("enumerated handles differ from lookups")
				}
				return o, team
			}
			old, oldTeam := create()
			old.SetScore("entry", 1)
			oldTeam.AddEntry("entry")
			if mode == "commands" {
				srv.RunLine("scoreboard objectives remove same")
				srv.RunLine("team remove same")
			} else {
				old.Remove()
				oldTeam.Remove()
			}
			assertStaleReads := func() {
				t.Helper()
				if old.Name() != "" || old.Criterion() != "" || old.Display().PlainString() != "" || old.Render() != 0 {
					t.Fatal("removed objective returned live fields")
				}
				if value, ok := old.Score("entry"); ok || value != 0 {
					t.Fatal("removed objective returned a score")
				}
				if oldTeam.Name() != "" || oldTeam.Display().PlainString() != "" || oldTeam.Prefix().PlainString() != "" || oldTeam.Suffix().PlainString() != "" || oldTeam.Color() != "" || oldTeam.FriendlyFire() || oldTeam.SeeInvisible() || oldTeam.NameTags() != "" || oldTeam.Collision() != "" || len(oldTeam.Entries()) != 0 {
					t.Fatal("removed team returned live fields")
				}
			}
			assertStaleReads()
			o, team := create()
			if old == o || oldTeam == team {
				t.Fatal("removed handles equal their replacements")
			}
			o.SetDisplay(plugin.Plain("Replacement"))
			o.SetRender(plugin.RenderHearts)
			o.SetScore("entry", 13)
			team.SetDisplay(plugin.Plain("Replacement"))
			team.SetPrefix(plugin.Plain("[P]"))
			team.SetSuffix(plugin.Plain("[S]"))
			team.SetColor("gold")
			team.SetNameTags("hideForOwnTeam")
			team.SetCollision("pushOwnTeam")
			team.AddEntry("entry")
			if got, ok := board.TeamOf("entry"); !ok || got != team {
				t.Fatal("member lookup lost team identity")
			}
			keep, err := board.AddObjective("keep", "dummy", plugin.Plain("Keep"), plugin.RenderInteger)
			if err != nil {
				t.Fatal(err)
			}
			board.SetDisplay(plugin.SlotSidebar, keep)
			board.SetDisplay(plugin.SlotBelowName, o)
			c.ReadTicks(srv, 0, func(int32, []byte) bool { return false })
			assertStaleReads()
			old.SetDisplay(plugin.Plain("Stale"))
			old.SetRender(plugin.RenderInteger)
			old.SetScore("entry", 99)
			label := plugin.Plain("Stale")
			old.SetScoreDisplay("entry", &label)
			old.SetScoreDisplay("entry", nil)
			board.SetDisplay(plugin.SlotSidebar, old)
			board.SetDisplay(plugin.SlotList, old)
			old.Remove()
			oldTeam.SetDisplay(label)
			oldTeam.SetPrefix(label)
			oldTeam.SetSuffix(label)
			oldTeam.SetColor("red")
			oldTeam.SetFriendlyFire(false)
			oldTeam.SetSeeInvisible(false)
			oldTeam.SetNameTags("never")
			oldTeam.SetCollision("never")
			oldTeam.AddEntry("other")
			oldTeam.RemoveEntry("entry")
			oldTeam.Remove()
			c.ExpectNoneWithinTicks(srv, 0, servertest.PacketIs(protocol.IDPlayUpdateObjectives, protocol.IDPlayDisplayObjective, protocol.IDPlayUpdateScore, protocol.IDPlayResetScore, protocol.IDPlaySetPlayerTeam))
			if got, ok := board.Objective("same"); !ok || got != o || o.Display().PlainString() != "Replacement" || o.Render() != plugin.RenderHearts {
				t.Fatal("stale handle changed or removed replacement objective")
			}
			if value, ok := o.Score("entry"); !ok || value != 13 {
				t.Fatal("stale handle changed replacement score")
			}
			if got, ok := board.Display(plugin.SlotSidebar); !ok || got != keep {
				t.Fatal("stale handle replaced occupied display slot")
			}
			if _, ok := board.Display(plugin.SlotList); ok {
				t.Fatal("stale handle assigned empty display slot")
			}
			if got, ok := board.Team("same"); !ok || got != team || team.Display().PlainString() != "Replacement" || team.Prefix().PlainString() != "[P]" || team.Suffix().PlainString() != "[S]" || team.Color() != "gold" || !team.FriendlyFire() || !team.SeeInvisible() || team.NameTags() != "hideForOwnTeam" || team.Collision() != "pushOwnTeam" || !slices.Equal(team.Entries(), []string{"entry"}) {
				t.Fatal("stale handle changed or removed replacement team")
			}
			o.Remove()
			keep.Remove()
			team.Remove()
		})
	}
}

func TestPluginPrivateScoreboardCleanupAndRestart(t *testing.T) {
	var host plugin.Server
	var p plugin.Player
	observer := &testPlugin{disable: func() {
		if !reflect.DeepEqual(p.Scoreboard(), host.Scoreboard()) {
			t.Error("disabled plugin left its private board assigned")
		}
	}}
	owner := &testPlugin{enable: func(s plugin.Server) error { host = s; return nil }}
	cfg := servertest.BlockTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv := servertest.NewTestServerWithPlugins(t, cfg, []plugin.Registration{{Name: "observer", Plugin: observer}, {Name: "owner", Plugin: owner}})
	c := servertest.DialServer(t, srv)
	c.ViewAsk = config.MinDistance
	c.Join("Restart")
	servertest.HoldTicks(t, srv)
	p, _ = host.PlayerNamed("Restart")
	o, err := host.Scoreboard().AddObjective("main", "dummy", plugin.Plain("Saved"), plugin.RenderInteger)
	if err != nil {
		t.Fatal(err)
	}
	o.SetScore("entry", 42)
	host.Scoreboard().SetDisplay(plugin.SlotSidebar, o)
	nextBoardPacket(t, c, srv, protocol.IDPlayUpdateObjectives)
	nextBoardPacket(t, c, srv, protocol.IDPlayDisplayObjective)
	nextBoardPacket(t, c, srv, protocol.IDPlayUpdateScore)
	private := host.NewScoreboard()
	unsaved, err := private.AddObjective("private", "dummy", plugin.Plain("Unsaved"), plugin.RenderInteger)
	if err != nil {
		t.Fatal(err)
	}
	private.SetDisplay(plugin.SlotSidebar, unsaved)
	p.SetScoreboard(private)
	nextBoardPacket(t, c, srv, protocol.IDPlayUpdateObjectives)
	nextBoardPacket(t, c, srv, protocol.IDPlayUpdateObjectives)
	nextBoardPacket(t, c, srv, protocol.IDPlayDisplayObjective)
	servertest.ReleaseTicks(t, srv)
	srv.Shutdown()
	// Restart without plugins: the main board and its vanilla join snapshot survive.
	srv = servertest.NewTestServer(t, cfg)
	again := servertest.DialServer(t, srv)
	again.ViewAsk = config.MinDistance
	again.Login("Restart")
	again.Configure()
	var seen []int32
	again.ReadJoin(func(id int32, data []byte) {
		switch id {
		case protocol.IDPlayUpdateObjectives:
			r := protocol.NewReader(data)
			if r.ReadString(32767) != "main" || r.ReadU8() != 0 {
				t.Fatal("private objective survived restart")
			}
			text, _ := protocol.ReadText(r)
			if text.Plain() != "Saved" {
				t.Fatal("main display not saved")
			}
		case protocol.IDPlayDisplayObjective:
			r := protocol.NewReader(data)
			if r.ReadVarInt() != 1 || r.ReadString(32767) != "main" {
				t.Fatal("saved slot")
			}
		case protocol.IDPlayUpdateScore:
			r := protocol.NewReader(data)
			if r.ReadString(32767) != "entry" || r.ReadString(32767) != "main" || r.ReadVarInt() != 42 {
				t.Fatal("saved score")
			}
		default:
			return
		}
		seen = append(seen, id)
	})
	if !slices.Equal(seen, []int32{protocol.IDPlayUpdateObjectives, protocol.IDPlayDisplayObjective, protocol.IDPlayUpdateScore}) {
		t.Fatalf("join scoreboard packets: %v", seen)
	}
	again.MarkLoaded()
}

// vanilla: ServerPlayer.doTick, awardStat, die
func TestPluginPrivateScoreboardGameplayCriteria(t *testing.T) {
	srv, host := pluginAdminServer(t)
	c := pluginJoin(t, srv, "Criteria")
	other := pluginJoin(t, srv, "Observer")
	servertest.HoldTicks(t, srv)
	p, _ := host.PlayerNamed("Criteria")
	private := host.NewScoreboard()
	for _, board := range []plugin.Scoreboard{host.Scoreboard(), private} {
		for _, name := range []string{"health", "deathCount", "minecraft.custom:minecraft.play_time", "teamkill.red", "teamkill.blue", "totalKillCount"} {
			if _, err := board.AddObjective(name, name, plugin.Plain(name), plugin.RenderInteger); err != nil {
				t.Fatal(err)
			}
		}
	}
	health, _ := private.Objective("health")
	private.SetDisplay(plugin.SlotSidebar, health)
	p.SetScoreboard(private)
	servertest.RunTicks(t, srv, 1)
	assert := func(board plugin.Scoreboard, criterion string, want int) {
		t.Helper()
		o, _ := board.Objective(criterion)
		if n, ok := o.Score("Criteria"); !ok || n != want {
			t.Fatalf("%s = %d, %v, want %d", criterion, n, ok, want)
		}
	}
	assert(private, "health", 20)
	assert(private, "minecraft.custom:minecraft.play_time", 1)
	if !c.ReadTicks(srv, 0, func(id int32, data []byte) bool {
		if id != protocol.IDPlayUpdateScore {
			return false
		}
		r := protocol.NewReader(data)
		if r.ReadString(32767) != "Criteria" || r.ReadString(32767) != "health" {
			return false
		}
		if r.ReadVarInt() != 20 || r.Err() != nil {
			t.Fatal("private health packet")
		}
		return true
	}) {
		t.Fatal("missing private health packet")
	}
	other.ExpectNoneWithinTicks(srv, 0, servertest.PacketIs(protocol.IDPlayUpdateScore))
	servertest.RunTicks(t, srv, 1)
	assert(private, "minecraft.custom:minecraft.play_time", 2)
	// A newly created private board samples unchanged vitals on its first tick.
	late := host.NewScoreboard()
	lateHealth, _ := late.AddObjective("health", "health", plugin.Plain("Late"), plugin.RenderInteger)
	servertest.RunTicks(t, srv, 1)
	if v, ok := lateHealth.Score("Criteria"); !ok || v != 20 {
		t.Fatal("late board did not sample health")
	}
	mainTeam, _ := host.Scoreboard().AddTeam("victims")
	mainTeam.SetColor("blue")
	mainTeam.AddEntry("Observer")
	privateTeam, _ := private.AddTeam("victims")
	privateTeam.SetColor("red")
	privateTeam.AddEntry("Observer")
	servertest.ReleaseTicks(t, srv)
	srv.RunLine("damage Observer 100 minecraft:generic_kill by Criteria")
	servertest.HoldTicks(t, srv)
	assert(private, "teamkill.red", 1)
	assert(host.Scoreboard(), "teamkill.blue", 1)
	assert(private, "totalKillCount", 1)
	servertest.ReleaseTicks(t, srv)
	srv.RunLine("kill Criteria")
	servertest.HoldTicks(t, srv)
	assert(private, "deathCount", 1)
	assert(host.Scoreboard(), "deathCount", 1)
}

func TestPluginPrivateScoreboardCreationDuringMovement(t *testing.T) {
	srv, host := pluginAdminServer(t)
	c := pluginJoin(t, srv, "Walker")
	servertest.HoldTicks(t, srv)
	c.SendPosition(0.5, 64, 0.5, true)
	c.Settle()
	addWalk := func(board plugin.Scoreboard) plugin.Objective {
		t.Helper()
		o, err := board.AddObjective("walk", "minecraft.custom:minecraft.walk_one_cm", plugin.Plain("Walk"), plugin.RenderInteger)
		if err != nil {
			t.Error(err)
		}
		return o
	}
	main := addWalk(host.Scoreboard())
	var private []plugin.Objective
	task := host.Scheduler().RunTimer(1, 1, func() {
		for range 4 {
			private = append(private, addWalk(host.NewScoreboard()))
		}
	})
	// Network handlers award statistics independently of the held tick loop.
	for range 16 {
		for step := range 32 {
			c.SendPosition(0.5+float64((step+1)%2)*0.25, 64, 0.5, true)
		}
		servertest.RunTicks(t, srv, 1)
		c.Settle()
	}
	task.Cancel()
	if value, ok := main.Score("Walker"); !ok || value != 16*32*25 {
		t.Fatalf("movement score = %d, %v", value, ok)
	}
	if len(private) != 16*4 {
		t.Fatalf("created %d boards, want 64", len(private))
	}
	before := make([]int, len(private))
	for i, o := range private {
		before[i], _ = o.Score("Walker")
	}
	c.SendPosition(0.75, 64, 0.5, true)
	c.Settle()
	for i, o := range private {
		if value, ok := o.Score("Walker"); !ok || value != before[i]+25 {
			t.Errorf("board %d movement score = %d, %v, want %d", i, value, ok, before[i]+25)
		}
	}
}
