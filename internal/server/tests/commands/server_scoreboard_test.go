package commands

import (
	"bytes"
	"github.com/golem-mc/golem/internal/server/servertest"
	"path/filepath"
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
)

// expectScoreboardFeedback reads the next System Chat message and fails
// unless it is the success feedback under key; it returns the arguments.
func expectScoreboardFeedback(t *testing.T, c *servertest.Client, key string) []protocol.Text {
	t.Helper()
	got := servertest.FeedbackComponent(t, c)
	if got.Translate != key {
		t.Fatalf("feedback %q, want %q ", got.Translate, key)
	}
	return got.With
}

// expectScoreboardRefusal reads the next System Chat message and fails
// unless it is the red refusal under key.
func expectScoreboardRefusal(t *testing.T, c *servertest.Client, key string) []protocol.Text {
	t.Helper()
	got := servertest.FeedbackComponent(t, c)
	if got.Color != protocol.ColorRed || len(got.Extra) != 1 || got.Extra[0].Translate != key {
		t.Fatalf("refusal %+v, want the red %q", got, key)
	}
	return got.Extra[0].With
}

// bracketedName is the plain name inside an objective's square-bracketed
// feedback form.
func bracketedName(t *testing.T, text protocol.Text) string {
	t.Helper()
	if text.Translate != "chat.square_brackets" || len(text.With) != 1 {
		t.Fatalf("objective argument %+v, want the square-bracketed name", text)
	}
	return text.With[0].Text
}

func number(text protocol.Text) int64 {
	if text.Number == nil {
		return -1 << 62
	}
	return *text.Number
}

// TestScoreboardObjectives runs /scoreboard objectives through an empty
// list, an add, a duplicate add, an unknown criteria, a list and a remove.
func TestScoreboardObjectives(t *testing.T) {
	t.Parallel()
	_, c := servertest.StartExecuteWorld(t, "Testificate")

	c.ChatCommand("scoreboard objectives list")
	expectScoreboardFeedback(t, c, "commands.scoreboard.objectives.list.empty")

	c.ChatCommand("scoreboard objectives add kills dummy")
	with := expectScoreboardFeedback(t, c, "commands.scoreboard.objectives.add.success")
	if len(with) != 1 || bracketedName(t, with[0]) != "kills" {
		t.Errorf("add arguments %+v, want [kills]", with)
	}

	c.ChatCommand("scoreboard objectives add kills dummy")
	expectScoreboardRefusal(t, c, "commands.scoreboard.objectives.add.duplicate")

	c.ChatCommand("scoreboard objectives add other nonsense")
	if got := c.ExpectPlay(protocol.IDPlaySystemChat); !bytes.Equal(got, servertest.CommandErrorMessageForTest("argument.criteria.invalid", "nonsense")) {
		t.Errorf("an unknown criteria answered % X, want argument.criteria.invalid", got)
	}
	c.ExpectPlay(protocol.IDPlaySystemChat) // the context line

	c.ChatCommand("scoreboard objectives add hp health")
	expectScoreboardFeedback(t, c, "commands.scoreboard.objectives.add.success")

	c.ChatCommand("scoreboard objectives list")
	with = expectScoreboardFeedback(t, c, "commands.scoreboard.objectives.list.success")
	if len(with) != 2 || number(with[0]) != 2 {
		t.Errorf("list arguments %+v, want a count of 2", with)
	}

	c.ChatCommand(`scoreboard objectives modify kills displayname {"text":"Kills","color":"red"}`)
	with = expectScoreboardFeedback(t, c, "commands.scoreboard.objectives.modify.displayname")
	if len(with) != 2 || with[1].Translate != "chat.square_brackets" || len(with[1].With) != 1 ||
		with[1].With[0].Text != "Kills" || with[1].With[0].Color != protocol.ColorRed {
		t.Errorf("displayname arguments %+v, want the red Kills", with)
	}

	c.CommandSuggestion(1, "/scoreboard objectives remove ")
	if _, _, _, matches := c.ExpectCommandSuggestions(); !slices.Equal(matches, []string{"hp", "kills"}) {
		t.Errorf("objective suggestions %v, want hp and kills", matches)
	}

	c.ChatCommand("scoreboard objectives remove kills")
	with = expectScoreboardFeedback(t, c, "commands.scoreboard.objectives.remove.success")
	if len(with) != 1 || bracketedName(t, with[0]) != "Kills" {
		t.Errorf("remove arguments %+v, want [Kills]", with)
	}

	c.ChatCommand("scoreboard objectives remove kills")
	expectScoreboardRefusal(t, c, "arguments.objective.notFound")

	c.Disconnect()
}

// vanilla: ScoreboardCommand.setScore, CommandResponseTracker.ElementType.NON_ZERO
func TestScoreboardSetCountsNonZeroValues(t *testing.T) {
	t.Parallel()
	srv, c := servertest.StartExecuteWorld(t, "Testificate")

	c.ChatCommand("scoreboard objectives add t dummy")
	expectScoreboardFeedback(t, c, "commands.scoreboard.objectives.add.success")

	for _, tc := range []struct {
		target, value, form string
		count               int64
		score               int32
	}{
		{"a", "0", "multiple", 0, 0},
		{"a", "7", "single", 1, 7},
		{"a", "7", "single", 1, 7}, // An unchanged nonzero value still counts.
		{"a", "-7", "single", 1, -7},
		{"b", "0", "multiple", 0, 0},
		{"*", "0", "multiple", 0, 0},
		{"*", "7", "multiple", 2, 7},
		{"*", "0", "multiple", 0, 0},
	} {
		c.ChatCommand("scoreboard players set " + tc.target + " t " + tc.value)
		with := expectScoreboardFeedback(t, c, "commands.scoreboard.players.set.success."+tc.form)
		if len(with) != 3 {
			t.Fatalf("set %s t %s arguments %+v, want objective, holder/count, value", tc.target, tc.value, with)
		}
		if bracketedName(t, with[0]) != "t" || number(with[2]) != int64(tc.score) {
			t.Errorf("set %s t %s arguments %+v", tc.target, tc.value, with)
		}
		if tc.form == "single" {
			if with[1].Text != tc.target {
				t.Errorf("holder %+v, want %s", with[1], tc.target)
			}
		} else if number(with[1]) != tc.count {
			t.Errorf("count %+v, want %d", with[1], tc.count)
		}
		holders := []string{tc.target}
		if tc.target == "*" {
			holders = []string{"a", "b"}
		}
		for _, holder := range holders {
			if got, ok := srv.ScoreOf(holder, "t"); !ok || got != tc.score {
				t.Errorf("score for %s = %d, %v; want %d", holder, got, ok, tc.score)
			}
		}
	}
}

// TestScoreboardPlayers runs /scoreboard players: set, add, remove and get
// on a name that is no player, the empty get, an operation and its division
// by zero, a read-only objective, enabling a trigger, a reset and a list.
func TestScoreboardPlayers(t *testing.T) {
	t.Parallel()
	_, c := servertest.StartExecuteWorld(t, "Testificate")

	c.ChatCommand("scoreboard objectives add points dummy")
	expectScoreboardFeedback(t, c, "commands.scoreboard.objectives.add.success")

	c.ChatCommand("scoreboard players set Alice points 5")
	with := expectScoreboardFeedback(t, c, "commands.scoreboard.players.set.success.single")
	if len(with) != 3 || with[1].Text != "Alice" || number(with[2]) != 5 {
		t.Errorf("set arguments %+v, want points, Alice, 5", with)
	}

	c.ChatCommand("scoreboard players add Alice points 3")
	with = expectScoreboardFeedback(t, c, "commands.scoreboard.players.add.success.single")
	if len(with) != 4 || number(with[3]) != 8 {
		t.Errorf("add arguments %+v, want a new score of 8", with)
	}

	c.ChatCommand("scoreboard players remove Alice points 10")
	with = expectScoreboardFeedback(t, c, "commands.scoreboard.players.remove.success.single")
	if len(with) != 4 || number(with[3]) != -2 {
		t.Errorf("remove arguments %+v, want a new score of -2", with)
	}

	c.ChatCommand("scoreboard players get Alice points")
	with = expectScoreboardFeedback(t, c, "commands.scoreboard.players.get.success")
	if len(with) != 3 || with[0].Text != "Alice" || number(with[1]) != -2 {
		t.Errorf("get arguments %+v, want Alice at -2", with)
	}

	c.ChatCommand("scoreboard players get Bob points")
	expectScoreboardRefusal(t, c, "commands.scoreboard.players.get.null")

	c.ChatCommand("scoreboard players set Bob points 0")
	expectScoreboardFeedback(t, c, "commands.scoreboard.players.set.success.multiple")

	c.ChatCommand("scoreboard players operation Alice points *= Alice points")
	with = expectScoreboardFeedback(t, c, "commands.scoreboard.players.operation.success.single")
	if len(with) != 3 || number(with[2]) != 4 {
		t.Errorf("operation arguments %+v, want a new score of 4", with)
	}

	c.ChatCommand("scoreboard players operation Alice points /= Bob points")
	expectScoreboardRefusal(t, c, "arguments.operation.div0")

	// Floor division and modulus, not Go's truncating ones.
	c.ChatCommand("scoreboard players set Bob points -3")
	expectScoreboardFeedback(t, c, "commands.scoreboard.players.set.success.single")
	c.ChatCommand("scoreboard players operation Alice points %= Bob points")
	with = expectScoreboardFeedback(t, c, "commands.scoreboard.players.operation.success.single")
	if len(with) != 3 || number(with[2]) != -2 {
		t.Errorf("4 %% -3 = %+v, want -2", with)
	}

	c.ChatCommand("scoreboard objectives add hp health")
	expectScoreboardFeedback(t, c, "commands.scoreboard.objectives.add.success")
	c.ChatCommand("scoreboard players set Alice hp 1")
	expectScoreboardRefusal(t, c, "arguments.objective.readonly")

	c.ChatCommand("scoreboard objectives add press trigger")
	expectScoreboardFeedback(t, c, "commands.scoreboard.objectives.add.success")
	c.ChatCommand("scoreboard players enable Alice press")
	expectScoreboardFeedback(t, c, "commands.scoreboard.players.enable.success.single")
	c.ChatCommand("scoreboard players enable Alice press")
	expectScoreboardRefusal(t, c, "commands.scoreboard.players.enable.failed")
	c.ChatCommand("scoreboard players enable Alice points")
	expectScoreboardRefusal(t, c, "commands.scoreboard.players.enable.invalid")

	c.ChatCommand("scoreboard players reset Alice points")
	expectScoreboardFeedback(t, c, "commands.scoreboard.players.reset.specific.single")
	c.ChatCommand("scoreboard players get Alice points")
	expectScoreboardRefusal(t, c, "commands.scoreboard.players.get.null")

	c.ChatCommand("scoreboard players list")
	with = expectScoreboardFeedback(t, c, "commands.scoreboard.players.list.success")
	if len(with) != 2 || number(with[0]) != 2 {
		t.Errorf("list arguments %+v, want Alice and Bob", with)
	}

	c.Disconnect()
}

// readObjective decodes an Update Objectives packet's name, mode and, for
// an add, its display name.
func readObjective(t *testing.T, data []byte) (name string, mode uint8, display string) {
	t.Helper()
	r := protocol.NewReader(data)
	name = r.ReadString(32767)
	mode = r.ReadU8()
	if mode != 1 {
		text, ok := protocol.ReadText(r)
		if !ok {
			t.Fatalf("objective display name: %v", r.Err())
		}
		display = text.Text
		r.ReadVarInt()
		if r.ReadBool() {
			t.Errorf("objective %q carries a number format, want none", name)
		}
	}
	if r.Err() != nil {
		t.Fatalf("decode objective: %v", r.Err())
	}
	return name, mode, display
}

func readDisplay(t *testing.T, data []byte) (slot int32, objective string) {
	t.Helper()
	r := protocol.NewReader(data)
	slot = r.ReadVarInt()
	objective = r.ReadString(32767)
	if r.Err() != nil {
		t.Fatalf("decode display objective: %v", r.Err())
	}
	return slot, objective
}

func readScore(t *testing.T, data []byte) (holder, objective string, value int32, display *protocol.Text) {
	t.Helper()
	r := protocol.NewReader(data)
	holder = r.ReadString(32767)
	objective = r.ReadString(32767)
	value = r.ReadVarInt()
	if r.ReadBool() {
		text, ok := protocol.ReadText(r)
		if !ok {
			t.Fatalf("score of %s: undecodable display name", holder)
		}
		display = &text
	}
	if r.ReadBool() {
		t.Errorf("score of %s carries a number format, want none", holder)
	}
	if r.Err() != nil {
		t.Fatalf("decode score: %v", r.Err())
	}
	return holder, objective, value, display
}

// TestScoreboardSidebarPackets checks what the clients are told: nothing
// while an objective is shown nowhere, its objective, slot and scores once
// the sidebar shows it, each change after, and its removal when the sidebar
// is cleared.
func TestScoreboardSidebarPackets(t *testing.T) {
	t.Parallel()
	_, c := servertest.StartExecuteWorld(t, "Testificate")

	c.ChatCommand("scoreboard objectives add kills dummy")
	expectScoreboardFeedback(t, c, "commands.scoreboard.objectives.add.success")
	c.ChatCommand("scoreboard players set Alice kills 7")
	expectScoreboardFeedback(t, c, "commands.scoreboard.players.set.success.single")

	c.ChatCommand("scoreboard objectives setdisplay sidebar kills")
	if name, mode, display := readObjective(t, c.ExpectPlay(protocol.IDPlayUpdateObjectives)); name != "kills" || mode != 0 || display != "kills" {
		t.Errorf("objective %q mode %d display %q, want kills added", name, mode, display)
	}
	// vanilla: DisplaySlot.SIDEBAR has id 1
	if slot, name := readDisplay(t, c.ExpectPlay(protocol.IDPlayDisplayObjective)); slot != 1 || name != "kills" {
		t.Errorf("display slot %d objective %q, want kills in the sidebar", slot, name)
	}
	if holder, name, value, _ := readScore(t, c.ExpectPlay(protocol.IDPlayUpdateScore)); holder != "Alice" || name != "kills" || value != 7 {
		t.Errorf("score %s %s %d, want Alice kills 7", holder, name, value)
	}
	expectScoreboardFeedback(t, c, "commands.scoreboard.objectives.display.set")

	c.ChatCommand("scoreboard players add Alice kills 2")
	if holder, _, value, _ := readScore(t, c.ExpectPlay(protocol.IDPlayUpdateScore)); holder != "Alice" || value != 9 {
		t.Errorf("score %s %d, want Alice at 9", holder, value)
	}
	expectScoreboardFeedback(t, c, "commands.scoreboard.players.add.success.single")

	// A holder with another score left loses the one; a holder whose last
	// score goes is removed from every objective.
	// vanilla: Scoreboard.resetSinglePlayerScore
	c.ChatCommand("scoreboard objectives add deaths dummy")
	expectScoreboardFeedback(t, c, "commands.scoreboard.objectives.add.success")
	c.ChatCommand("scoreboard players set Alice deaths 1")
	expectScoreboardFeedback(t, c, "commands.scoreboard.players.set.success.single")
	c.ChatCommand("scoreboard players reset Alice kills")
	r := protocol.NewReader(c.ExpectPlay(protocol.IDPlayResetScore))
	holder := r.ReadString(32767)
	hasObjective := r.ReadBool()
	objective := r.ReadString(32767)
	if r.Err() != nil || holder != "Alice" || !hasObjective || objective != "kills" {
		t.Errorf("reset %q %v %q (%v), want Alice in kills", holder, hasObjective, objective, r.Err())
	}
	expectScoreboardFeedback(t, c, "commands.scoreboard.players.reset.specific.single")
	c.ChatCommand("scoreboard players reset Alice deaths")
	r = protocol.NewReader(c.ExpectPlay(protocol.IDPlayResetScore))
	holder = r.ReadString(32767)
	hasObjective = r.ReadBool()
	if r.Err() != nil || holder != "Alice" || hasObjective {
		t.Errorf("last reset %q %v (%v), want Alice removed everywhere", holder, hasObjective, r.Err())
	}
	expectScoreboardFeedback(t, c, "commands.scoreboard.players.reset.specific.single")

	c.ChatCommand("scoreboard objectives setdisplay sidebar")
	if name, mode, _ := readObjective(t, c.ExpectPlay(protocol.IDPlayUpdateObjectives)); name != "kills" || mode != 1 {
		t.Errorf("objective %q mode %d, want kills removed", name, mode)
	}
	expectScoreboardFeedback(t, c, "commands.scoreboard.objectives.display.cleared")

	c.ChatCommand("scoreboard objectives setdisplay sidebar")
	expectScoreboardRefusal(t, c, "commands.scoreboard.objectives.display.alreadyEmpty")

	c.Disconnect()
}

// TestScoreboardSurvivesARestart checks that the board outlives the process:
// the shutdown writes data/minecraft/scoreboard.dat, and a player joining a
// second server on the same world is sent the displayed objective and its
// scores.
func TestScoreboardSurvivesARestart(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()

	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))
	c.ChatCommand("scoreboard objectives add kills dummy")
	expectScoreboardFeedback(t, c, "commands.scoreboard.objectives.add.success")
	c.ChatCommand("scoreboard players set Alice kills 7")
	expectScoreboardFeedback(t, c, "commands.scoreboard.players.set.success.single")
	c.ChatCommand("scoreboard objectives setdisplay list kills")
	c.ExpectPlay(protocol.IDPlayUpdateObjectives)
	c.ExpectPlay(protocol.IDPlayDisplayObjective)
	c.ExpectPlay(protocol.IDPlayUpdateScore)
	expectScoreboardFeedback(t, c, "commands.scoreboard.objectives.display.set")
	c.Disconnect()
	srv.Shutdown()

	data := servertest.ReadSavedFile(t, filepath.Join(cfg.LevelName, "data", "minecraft", "scoreboard.dat"), "data")
	objectives, _ := nbt.Get[nbt.List](data, "Objectives")
	if len(objectives.Items) != 1 {
		t.Fatalf("scoreboard.dat objectives = %v, want kills", objectives)
	}
	if entry, _ := objectives.Items[0].(nbt.Compound); entry["Name"] != nbt.String("kills") || entry["CriteriaName"] != nbt.String("dummy") {
		t.Errorf("saved objective %v, want kills on dummy", entry)
	}
	scores, _ := nbt.Get[nbt.List](data, "PlayerScores")
	if len(scores.Items) != 1 {
		t.Fatalf("scoreboard.dat scores = %v, want Alice's", scores)
	}
	if entry, _ := scores.Items[0].(nbt.Compound); entry["Name"] != nbt.String("Alice") || entry["Score"] != nbt.Int(7) {
		t.Errorf("saved score %v, want Alice at 7", entry)
	}
	slots, _ := nbt.Get[nbt.Compound](data, "DisplaySlots")
	if slots["list"] != nbt.String("kills") {
		t.Errorf("saved display slots %v, want kills under list", slots)
	}

	restarted := *cfg
	_, reconnect := servertest.Start(t, &restarted, servertest.OpsListing(t, "Testificate"))
	again := reconnect()
	again.Login("Testificate")
	again.Configure()
	var seen []int32
	var score []byte
	again.ReadJoin(func(id int32, data []byte) {
		switch id {
		case protocol.IDPlayUpdateObjectives, protocol.IDPlayDisplayObjective:
			seen = append(seen, id)
		case protocol.IDPlayUpdateScore:
			seen = append(seen, id)
			score = data
		}
	})
	want := []int32{protocol.IDPlayUpdateObjectives, protocol.IDPlayDisplayObjective, protocol.IDPlayUpdateScore}
	if len(seen) != len(want) || seen[0] != want[0] || seen[1] != want[1] || seen[2] != want[2] {
		t.Fatalf("join sent scoreboard packets % X, want % X", seen, want)
	}
	if holder, name, value, _ := readScore(t, score); holder != "Alice" || name != "kills" || value != 7 {
		t.Errorf("join score %s %s %d, want Alice kills 7", holder, name, value)
	}
	again.MarkLoaded()
	again.ChatCommand("scoreboard players get Alice kills")
	with := expectScoreboardFeedback(t, again, "commands.scoreboard.players.get.success")
	if len(with) != 3 || number(with[1]) != 7 {
		t.Errorf("get arguments %+v after the restart, want 7", with)
	}
	again.Disconnect()
}

// TestScoreboardPlayersResultIsTheScore checks that /execute store result
// takes the score /scoreboard players get reads and the sum add leaves.
// vanilla: ScoreboardCommand.getScore, addScore
func TestScoreboardPlayersResultIsTheScore(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.ViewDistance = config.MinDistance
	cfg.SimulationDistance = config.MinDistance

	_, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	c.ChatCommand("scoreboard objectives add o dummy")
	expectScoreboardFeedback(t, c, "commands.scoreboard.objectives.add.success")
	c.ChatCommand("scoreboard players set x o 42")
	expectScoreboardFeedback(t, c, "commands.scoreboard.players.set.success.single")

	c.ChatCommand("execute store result storage t:a v int 1 run scoreboard players get x o")
	expectScoreboardFeedback(t, c, "commands.scoreboard.players.get.success")
	c.ChatCommand("data get storage t:a v")
	c.ExpectCommandFeedback("commands.data.storage.query", "t:a", "42")

	c.ChatCommand("execute store result storage t:a w int 1 run scoreboard players add x o 3")
	expectScoreboardFeedback(t, c, "commands.scoreboard.players.add.success.single")
	c.ChatCommand("data get storage t:a w")
	c.ExpectCommandFeedback("commands.data.storage.query", "t:a", "45")
}
