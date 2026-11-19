package commands

import (
	"bytes"
	"path/filepath"
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
)

// teamPacket is a decoded Set Player Team packet.
type teamPacket struct {
	name    string
	mode    uint8
	display protocol.Text
	prefix  protocol.Text
	nameTag int32
	collide int32
	color   int32 // -1 for none
	options uint8
	players []string
}

func readTeamPacket(t *testing.T, data []byte) teamPacket {
	t.Helper()
	r := protocol.NewReader(data)
	p := teamPacket{name: r.ReadString(32767), mode: r.ReadU8(), color: -1}
	if p.mode == 0 || p.mode == 2 {
		p.display, _ = protocol.ReadText(r)
		p.prefix, _ = protocol.ReadText(r)
		protocol.ReadText(r) // suffix
		p.nameTag = r.ReadVarInt()
		p.collide = r.ReadVarInt()
		if r.ReadBool() {
			p.color = r.ReadVarInt()
		}
		p.options = r.ReadU8()
	}
	if p.mode == 0 || p.mode == 3 || p.mode == 4 {
		for n := r.ReadVarInt(); n > 0; n-- {
			p.players = append(p.players, r.ReadString(32767))
		}
	}
	if r.Err() != nil {
		t.Fatalf("decode Set Player Team: %v", r.Err())
	}
	return p
}

func expectTeamPacket(t *testing.T, c *servertest.Client, name string, mode uint8) teamPacket {
	t.Helper()
	p := readTeamPacket(t, c.ExpectPlay(protocol.IDPlaySetPlayerTeam))
	if p.name != name || p.mode != mode {
		t.Fatalf("Set Player Team %q mode %d, want %q mode %d", p.name, p.mode, name, mode)
	}
	return p
}

// teamName is the plain display name inside a team's coloured, bracketed
// feedback form.
func teamNameOf(t *testing.T, text protocol.Text) (name, color string) {
	t.Helper()
	return bracketedName(t, text), text.Color
}

// TestTeamCommands runs /team through its subcommands and their refusals.
func TestTeamCommands(t *testing.T) {
	t.Parallel()
	_, c := servertest.StartExecuteWorld(t, "Testificate")

	c.ChatCommand("team list")
	expectScoreboardFeedback(t, c, "commands.team.list.teams.empty")

	c.ChatCommand("team add red")
	expectTeamPacket(t, c, "red", 0)
	expectTeamPacket(t, c, "red", 2)
	with := expectScoreboardFeedback(t, c, "commands.team.add.success")
	if name, _ := teamNameOf(t, with[0]); name != "red" {
		t.Errorf("add feedback names %q, want red", name)
	}
	c.ChatCommand("team add red")
	expectScoreboardRefusal(t, c, "commands.team.add.duplicate")
	c.ChatCommand("team add blue {\"text\":\"Blue Team\"}")
	expectTeamPacket(t, c, "blue", 0)
	if p := expectTeamPacket(t, c, "blue", 2); p.display.Text != "Blue Team" {
		t.Errorf("display name %+v, want Blue Team", p.display)
	}
	expectScoreboardFeedback(t, c, "commands.team.add.success")

	c.ChatCommand("team list")
	with = expectScoreboardFeedback(t, c, "commands.team.list.teams.success")
	if len(with) != 2 || number(with[0]) != 2 {
		t.Errorf("list arguments %+v, want 2 teams", with)
	}
	c.ChatCommand("team remove nope")
	if got := servertest.FeedbackComponent(t, c); got.Extra[0].Translate != "team.notFound" {
		t.Errorf("unknown team answered %+v, want team.notFound", got)
	}

	// Each option changes the team once and refuses to change it to what
	// it already is.
	c.ChatCommand("team modify red color gold")
	if p := expectTeamPacket(t, c, "red", 2); p.color != 6 {
		t.Errorf("color %d, want gold (6)", p.color)
	}
	with = expectScoreboardFeedback(t, c, "commands.team.option.color.success")
	if _, color := teamNameOf(t, with[0]); color != "gold" || with[1].Text != "gold" {
		t.Errorf("color feedback %+v, want the team in gold", with)
	}
	c.ChatCommand("team modify red color gold")
	expectScoreboardRefusal(t, c, "commands.team.option.color.unchanged")
	c.ChatCommand("team modify red friendlyFire false")
	if p := expectTeamPacket(t, c, "red", 2); p.options != 2 {
		t.Errorf("options %d, want only see-friendly-invisibles (2)", p.options)
	}
	expectScoreboardFeedback(t, c, "commands.team.option.friendlyfire.disabled")
	c.ChatCommand("team modify red friendlyFire false")
	expectScoreboardRefusal(t, c, "commands.team.option.friendlyfire.alreadyDisabled")
	c.ChatCommand("team modify red seeFriendlyInvisibles false")
	if p := expectTeamPacket(t, c, "red", 2); p.options != 0 {
		t.Errorf("options %d, want none", p.options)
	}
	expectScoreboardFeedback(t, c, "commands.team.option.seeFriendlyInvisibles.disabled")
	c.ChatCommand("team modify red seeFriendlyInvisibles true")
	expectTeamPacket(t, c, "red", 2)
	expectScoreboardFeedback(t, c, "commands.team.option.seeFriendlyInvisibles.enabled")
	c.ChatCommand("team modify red seeFriendlyInvisibles true")
	expectScoreboardRefusal(t, c, "commands.team.option.seeFriendlyInvisibles.alreadyEnabled")
	c.ChatCommand("team modify red nametagVisibility hideForOwnTeam")
	if p := expectTeamPacket(t, c, "red", 2); p.nameTag != 3 {
		t.Errorf("name tag visibility %d, want hideForOwnTeam (3)", p.nameTag)
	}
	with = expectScoreboardFeedback(t, c, "commands.team.option.nametagVisibility.success")
	if with[1].Translate != "team.visibility.hideForOwnTeam" {
		t.Errorf("visibility feedback %+v", with[1])
	}
	c.ChatCommand("team modify red nametagVisibility hideForOwnTeam")
	expectScoreboardRefusal(t, c, "commands.team.option.nametagVisibility.unchanged")
	c.ChatCommand("team modify red deathMessageVisibility never")
	expectTeamPacket(t, c, "red", 2)
	expectScoreboardFeedback(t, c, "commands.team.option.deathMessageVisibility.success")
	c.ChatCommand("team modify red deathMessageVisibility never")
	expectScoreboardRefusal(t, c, "commands.team.option.deathMessageVisibility.unchanged")
	c.ChatCommand("team modify red collisionRule pushOtherTeams")
	if p := expectTeamPacket(t, c, "red", 2); p.collide != 2 {
		t.Errorf("collision rule %d, want pushOtherTeams (2)", p.collide)
	}
	expectScoreboardFeedback(t, c, "commands.team.option.collisionRule.success")
	c.ChatCommand("team modify red collisionRule pushOtherTeams")
	expectScoreboardRefusal(t, c, "commands.team.option.collisionRule.unchanged")
	c.ChatCommand("team modify red displayName {\"text\":\"Reds\"}")
	expectTeamPacket(t, c, "red", 2)
	with = expectScoreboardFeedback(t, c, "commands.team.option.name.success")
	if name, _ := teamNameOf(t, with[0]); name != "Reds" {
		t.Errorf("name feedback names %q, want Reds", name)
	}
	c.ChatCommand("team modify red displayName {\"text\":\"Reds\"}")
	expectScoreboardRefusal(t, c, "commands.team.option.name.unchanged")
	c.ChatCommand("team modify red prefix {\"text\":\"[R] \"}")
	if p := expectTeamPacket(t, c, "red", 2); p.prefix.Text != "[R] " {
		t.Errorf("prefix %+v, want [R] ", p.prefix)
	}
	expectScoreboardFeedback(t, c, "commands.team.option.prefix.success")
	c.ChatCommand("team modify red color reset")
	if p := expectTeamPacket(t, c, "red", 2); p.color != -1 {
		t.Errorf("color %d after reset, want none", p.color)
	}
	expectScoreboardFeedback(t, c, "commands.team.option.color.clear.success")
	c.ChatCommand("team modify red color reset")
	expectScoreboardRefusal(t, c, "commands.team.option.color.unchanged")

	// Joining takes a player off the team they were on.
	c.ChatCommand("team join red Alice")
	if p := expectTeamPacket(t, c, "red", 3); !slices.Equal(p.players, []string{"Alice"}) {
		t.Errorf("join packet players %v, want Alice", p.players)
	}
	with = expectScoreboardFeedback(t, c, "commands.team.join.success.single")
	if with[0].Text != "Alice" {
		t.Errorf("join feedback %+v, want Alice", with)
	}
	c.ChatCommand("team join blue Alice")
	expectTeamPacket(t, c, "red", 4)
	expectTeamPacket(t, c, "blue", 3)
	expectScoreboardFeedback(t, c, "commands.team.join.success.single")
	c.ChatCommand("team join red")
	if p := expectTeamPacket(t, c, "red", 3); !slices.Equal(p.players, []string{"Testificate"}) {
		t.Errorf("join packet players %v, want the runner", p.players)
	}
	expectScoreboardFeedback(t, c, "commands.team.join.success.single")
	c.ChatCommand("team join red *")
	expectScoreboardRefusal(t, c, "argument.scoreHolder.empty")

	c.ChatCommand("team list blue")
	with = expectScoreboardFeedback(t, c, "commands.team.list.members.success")
	if len(with) != 3 || number(with[1]) != 1 || with[2].Text != "Alice" {
		t.Errorf("members %+v, want Alice alone", with)
	}
	c.ChatCommand("team leave Alice")
	expectTeamPacket(t, c, "blue", 4)
	expectScoreboardFeedback(t, c, "commands.team.leave.success.single")
	c.ChatCommand("team list blue")
	expectScoreboardFeedback(t, c, "commands.team.list.members.empty")
	c.ChatCommand("team empty blue")
	expectScoreboardRefusal(t, c, "commands.team.empty.unchanged")

	c.ChatCommand("team empty red")
	if p := expectTeamPacket(t, c, "red", 4); !slices.Equal(p.players, []string{"Testificate"}) {
		t.Errorf("empty packet players %v, want the runner", p.players)
	}
	with = expectScoreboardFeedback(t, c, "commands.team.empty.success")
	if number(with[0]) != 1 {
		t.Errorf("empty feedback %+v, want 1 member", with)
	}
	c.ChatCommand("team remove red")
	expectTeamPacket(t, c, "red", 1)
	expectScoreboardFeedback(t, c, "commands.team.remove.success")
	c.ChatCommand("team list")
	with = expectScoreboardFeedback(t, c, "commands.team.list.teams.success")
	if len(with) != 2 || number(with[0]) != 1 {
		t.Errorf("list arguments %+v, want one team", with)
	}
	c.Disconnect()
}

// TestTeamsSurviveARestart checks that teams and their members are written
// to scoreboard.dat and that a joining player is sent each team with its
// members before anything else.
func TestTeamsSurviveARestart(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()

	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))
	for _, step := range []struct {
		command string
		packets int
	}{
		{"team add red", 2},
		{"team modify red color red", 1},
		{"team modify red friendlyFire false", 1},
		{"team modify red collisionRule never", 1},
		{"team join red Alice", 1},
	} {
		c.ChatCommand(step.command)
		for range step.packets {
			c.ExpectPlay(protocol.IDPlaySetPlayerTeam)
		}
		servertest.FeedbackComponent(t, c)
	}
	c.Disconnect()
	srv.Shutdown()

	data := servertest.ReadSavedFile(t, filepath.Join(cfg.LevelName, "data", "minecraft", "scoreboard.dat"), "data")
	teams, _ := nbt.Get[nbt.List](data, "Teams")
	if len(teams.Items) != 1 {
		t.Fatalf("scoreboard.dat teams = %v, want red", teams)
	}
	entry, _ := teams.Items[0].(nbt.Compound)
	players, _ := nbt.Get[nbt.List](entry, "Players")
	if entry["Name"] != nbt.String("red") || entry["TeamColor"] != nbt.String("red") ||
		entry["AllowFriendlyFire"] != nbt.Byte(0) || entry["CollisionRule"] != nbt.String("never") ||
		len(players.Items) != 1 || players.Items[0] != nbt.String("Alice") {
		t.Errorf("saved team %v, want red in red, no friendly fire, never colliding, with Alice", entry)
	}
	// vanilla: PlayerTeam.Packed.CODEC leaves a default out
	for _, key := range []string{"SeeFriendlyInvisibles", "NameTagVisibility", "MemberNamePrefix"} {
		if _, has := entry[key]; has {
			t.Errorf("saved team has a default %s", key)
		}
	}

	restarted := *cfg
	_, reconnect := servertest.Start(t, &restarted, servertest.OpsListing(t, "Testificate"))
	again := reconnect()
	again.Login("Testificate")
	again.Configure()
	var joined []teamPacket
	again.ReadJoin(func(id int32, data []byte) {
		if id == protocol.IDPlaySetPlayerTeam {
			joined = append(joined, readTeamPacket(t, data))
		}
	})
	if len(joined) != 1 || joined[0].name != "red" || joined[0].mode != 0 || joined[0].color != 12 ||
		joined[0].options != 2 || joined[0].collide != 1 || !slices.Equal(joined[0].players, []string{"Alice"}) {
		t.Fatalf("join sent teams %+v, want red (colour 12, collide never, Alice)", joined)
	}
	again.MarkLoaded()
	again.Disconnect()
}

// TestTeamDeathMessageVisibility: a player's death in chat reaches whom the
// victim's team deathMessageVisibility allows, while the victim's own Combat
// Death always carries it.
// vanilla: ServerPlayer.die
func TestTeamDeathMessageVisibility(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		option         string
		teammate, rest bool
	}{
		{"always", true, true},
		{"hideForOtherTeams", true, false},
		{"hideForOwnTeam", false, true},
		{"never", false, false},
	} {
		t.Run(tc.option, func(t *testing.T) {
			t.Parallel()
			cfg := servertest.SurvivalTestConfig()
			cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
			srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "A"))
			a := connect().Join("A")
			b := connect().Join("B")
			other := connect().Join("C")
			servertest.HoldTicks(t, srv)
			a.ChatCommand("team add red")
			a.ChatCommand("team join red A")
			a.ChatCommand("team join red B")
			a.ChatCommand("team modify red deathMessageVisibility " + tc.option)
			a.Settle()
			b.Settle()
			other.Settle()

			a.ChatCommand("kill A")
			heard := func(c *servertest.Client) (chat, combat bool) {
				c.SettleAndCollect(func(id int32, data []byte) {
					has := bytes.Contains(data, []byte("death.attack.genericKill"))
					chat = chat || id == protocol.IDPlaySystemChat && has
					combat = combat || id == protocol.IDPlayCombatDeath && has
				})
				return chat, combat
			}
			// A is the runner: its feedback chat is no death message.
			if chat, combat := heard(a); !combat {
				t.Errorf("victim: chat %v combat %v, want a Combat Death", chat, combat)
			}
			if chat, _ := heard(b); chat != tc.teammate {
				t.Errorf("teammate heard the death %v, want %v", chat, tc.teammate)
			}
			if chat, _ := heard(other); chat != tc.rest {
				t.Errorf("other team heard the death %v, want %v", chat, tc.rest)
			}
		})
	}
}
