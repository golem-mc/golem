package commands

import (
	"bytes"
	"github.com/golem-mc/golem/internal/server/servertest"
	"path/filepath"
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/scoreboard"
	"github.com/golem-mc/golem/internal/server/world"
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

// TestScoreboardScoreDisplayAutoUpdate checks that an entity holder's score
// carries its display name only in an objective whose display auto-updates.
// vanilla: ScoreAccess.set
func TestScoreboardScoreDisplayAutoUpdate(t *testing.T) {
	t.Parallel()
	_, c := servertest.StartExecuteWorld(t, "Testificate")

	c.ChatCommand("scoreboard objectives add x dummy")
	expectScoreboardFeedback(t, c, "commands.scoreboard.objectives.add.success")
	c.ChatCommand("scoreboard objectives setdisplay sidebar x")
	c.ExpectPlay(protocol.IDPlayUpdateObjectives)
	c.ExpectPlay(protocol.IDPlayDisplayObjective)
	expectScoreboardFeedback(t, c, "commands.scoreboard.objectives.display.set")
	c.ChatCommand(`summon marker ~ ~ ~ {CustomName:"Bob",Tags:["t"]}`)
	expectScoreboardFeedback(t, c, "commands.summon.success")

	c.ChatCommand("scoreboard objectives modify x displayautoupdate true")
	c.ExpectPlay(protocol.IDPlayUpdateObjectives)
	expectScoreboardFeedback(t, c, "commands.scoreboard.objectives.modify.displayAutoUpdate.enable")
	c.ChatCommand("execute as @e[tag=t] run scoreboard players set @s x 1")
	_, name, value, display := readScore(t, c.ExpectPlay(protocol.IDPlayUpdateScore))
	if name != "x" || value != 1 || display == nil || display.Plain() != "Bob" {
		t.Errorf("score in %s %d display %v, want 1 in x shown as Bob", name, value, display)
	}
	expectScoreboardFeedback(t, c, "commands.scoreboard.players.set.success.single")

	c.ChatCommand("scoreboard objectives add y dummy")
	expectScoreboardFeedback(t, c, "commands.scoreboard.objectives.add.success")
	c.ChatCommand("scoreboard objectives setdisplay list y")
	c.ExpectPlay(protocol.IDPlayUpdateObjectives)
	c.ExpectPlay(protocol.IDPlayDisplayObjective)
	expectScoreboardFeedback(t, c, "commands.scoreboard.objectives.display.set")
	c.ChatCommand("execute as @e[tag=t] run scoreboard players set @s y 1")
	if _, name, value, display := readScore(t, c.ExpectPlay(protocol.IDPlayUpdateScore)); name != "y" || value != 1 || display != nil {
		t.Errorf("score in %s %d display %v, want 1 in y with no display name", name, value, display)
	}
	expectScoreboardFeedback(t, c, "commands.scoreboard.players.set.success.single")
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

// TestScoreboardSendsANewScoreAtZero checks that a score created at 0 is
// still sent to the clients showing its objective.
// vanilla: Scoreboard.getOrCreatePlayerScore (requiresSync)
func TestScoreboardSendsANewScoreAtZero(t *testing.T) {
	t.Parallel()
	_, c := servertest.StartExecuteWorld(t, "Testificate")

	c.ChatCommand("scoreboard objectives add kills dummy")
	expectScoreboardFeedback(t, c, "commands.scoreboard.objectives.add.success")
	c.ChatCommand("scoreboard objectives setdisplay sidebar kills")
	c.ExpectPlay(protocol.IDPlayUpdateObjectives)
	c.ExpectPlay(protocol.IDPlayDisplayObjective)
	expectScoreboardFeedback(t, c, "commands.scoreboard.objectives.display.set")

	c.ChatCommand("scoreboard players set Zero kills 0")
	if holder, name, value, _ := readScore(t, c.ExpectPlay(protocol.IDPlayUpdateScore)); holder != "Zero" || name != "kills" || value != 0 {
		t.Errorf("score %s %s %d, want Zero kills 0", holder, name, value)
	}
	expectScoreboardFeedback(t, c, "commands.scoreboard.players.set.success.multiple")

	c.ChatCommand("scoreboard players add One kills 0")
	if holder, _, value, _ := readScore(t, c.ExpectPlay(protocol.IDPlayUpdateScore)); holder != "One" || value != 0 {
		t.Errorf("score %s %d, want One at 0", holder, value)
	}
	expectScoreboardFeedback(t, c, "commands.scoreboard.players.add.success.single")

	c.ChatCommand("scoreboard players operation Two kills = Zero kills")
	if holder, _, value, _ := readScore(t, c.ExpectPlay(protocol.IDPlayUpdateScore)); holder != "Two" || value != 0 {
		t.Errorf("score %s %d, want Two at 0", holder, value)
	}
	expectScoreboardFeedback(t, c, "commands.scoreboard.players.operation.success.single")
	c.Disconnect()
}

// TestScoreboardStyledNumberFormatAndReadOnlySwap checks that a styled number
// format takes the style fields the text model has no word for, and that an
// operation which would write back into a read-only objective fails.
func TestScoreboardStyledNumberFormatAndReadOnlySwap(t *testing.T) {
	t.Parallel()
	_, c := servertest.StartExecuteWorld(t, "Testificate")

	c.ChatCommand("scoreboard objectives add kills dummy")
	expectScoreboardFeedback(t, c, "commands.scoreboard.objectives.add.success")
	c.ChatCommand(`scoreboard objectives modify kills numberformat styled {"bold":true,"strikethrough":true,"font":"minecraft:uniform","color":"red"}`)
	expectScoreboardFeedback(t, c, "commands.scoreboard.objectives.modify.objectiveFormat.set")
	c.ChatCommand(`scoreboard objectives modify kills numberformat styled {"bold":1,"shadow_color":5b}`)
	expectScoreboardFeedback(t, c, "commands.scoreboard.objectives.modify.objectiveFormat.set")
	for _, bad := range []string{`{"bold":"yes"}`, `{"font":"Bad Font"}`, `{"shadow_color":[1.0f,1.0f]}`, `{"shadow_color":[1.0f,"a",1.0f,1.0f]}`} {
		c.ChatCommand("scoreboard objectives modify kills numberformat styled " + bad)
		if got := servertest.FeedbackComponent(t, c); len(got.Extra) != 1 || got.Extra[0].Translate != "argument.style.invalid" {
			t.Errorf("%s answered %+v, want argument.style.invalid", bad, got)
		}
		c.ExpectPlay(protocol.IDPlaySystemChat) // the context line
	}

	c.ChatCommand("scoreboard objectives add hp health")
	expectScoreboardFeedback(t, c, "commands.scoreboard.objectives.add.success")
	c.ChatCommand("scoreboard players set Alice kills 5")
	expectScoreboardFeedback(t, c, "commands.scoreboard.players.set.success.single")
	c.ChatCommand("scoreboard players operation Alice kills >< Alice hp")
	expectScoreboardRefusal(t, c, "command.failed")
	c.Disconnect()
}

// TestScoreboardSwapWritesUnchangedSource checks that a swap sends and checks
// its source even when the swap leaves the source's value as it was.
// vanilla: OperationArgument.getOperation
func TestScoreboardSwapWritesUnchangedSource(t *testing.T) {
	t.Parallel()
	_, c := servertest.StartExecuteWorld(t, "Testificate")

	c.ChatCommand("scoreboard objectives add kills dummy")
	expectScoreboardFeedback(t, c, "commands.scoreboard.objectives.add.success")
	c.ChatCommand("scoreboard objectives setdisplay sidebar kills")
	c.ExpectPlay(protocol.IDPlayUpdateObjectives)
	c.ExpectPlay(protocol.IDPlayDisplayObjective)
	expectScoreboardFeedback(t, c, "commands.scoreboard.objectives.display.set")
	c.ChatCommand("scoreboard players set Zero kills 0")
	c.ExpectPlay(protocol.IDPlayUpdateScore)
	expectScoreboardFeedback(t, c, "commands.scoreboard.players.set.success.multiple")

	c.ChatCommand("scoreboard players operation Zero kills >< Fresh kills")
	if holder, _, value, _ := readScore(t, c.ExpectPlay(protocol.IDPlayUpdateScore)); holder != "Fresh" || value != 0 {
		t.Errorf("score %s %d, want Fresh at 0", holder, value)
	}
	expectScoreboardFeedback(t, c, "commands.scoreboard.players.operation.success.single")

	c.ChatCommand("scoreboard objectives add hp health")
	expectScoreboardFeedback(t, c, "commands.scoreboard.objectives.add.success")
	c.ChatCommand("scoreboard players operation Zero kills >< Zero hp")
	expectScoreboardRefusal(t, c, "command.failed")
	c.Disconnect()
}

// TestScoreboardLoadsVanillaDefaults checks a scoreboard.dat that leaves out
// CriteriaName and RenderType when they hold their defaults, dummy and integer.
// vanilla: Objective.Packed
func TestScoreboardLoadsVanillaDefaults(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()
	text := nbt.String("d")
	data := nbt.Compound{
		"Objectives": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
			nbt.Compound{"Name": nbt.String("d"), "DisplayName": text},
			nbt.Compound{"Name": nbt.String("h"), "DisplayName": nbt.String("h"), "CriteriaName": nbt.String("health"),
				"RenderType": nbt.String("integer")},
		}},
		"PlayerScores": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
			nbt.Compound{"Name": nbt.String("Alice"), "Objective": nbt.String("d"), "Score": nbt.Int(3)},
		}},
		"DisplaySlots": nbt.Compound{"sidebar": nbt.String("d")},
	}
	if err := world.WriteSavedData(cfg.LevelName, scoreboard.File, data); err != nil {
		t.Fatal(err)
	}

	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Testificate"))
	c := connect()
	c.Login("Testificate")
	c.Configure()
	var objectives, scores int
	c.ReadJoin(func(id int32, _ []byte) {
		switch id {
		case protocol.IDPlayUpdateObjectives:
			objectives++
		case protocol.IDPlayUpdateScore:
			scores++
		}
	})
	if objectives != 1 || scores != 1 {
		t.Errorf("join sent %d objectives and %d scores, want the sidebar's d and Alice", objectives, scores)
	}
	c.MarkLoaded()
	c.ChatCommand("scoreboard players get Alice d")
	with := expectScoreboardFeedback(t, c, "commands.scoreboard.players.get.success")
	if len(with) != 3 || number(with[1]) != 3 {
		t.Errorf("get arguments %+v, want 3", with)
	}
	// Any change makes the next save write the loaded objectives back.
	c.ChatCommand("scoreboard players set Bob d 1")
	c.ExpectPlay(protocol.IDPlayUpdateScore)
	expectScoreboardFeedback(t, c, "commands.scoreboard.players.set.success.single")
	c.Disconnect()
	srv.Shutdown()

	saved := servertest.ReadSavedFile(t, filepath.Join(cfg.LevelName, "data", "minecraft", "scoreboard.dat"), "data")
	list, _ := nbt.Get[nbt.List](saved, "Objectives")
	got := map[string]nbt.Compound{}
	for _, tag := range list.Items {
		entry, _ := tag.(nbt.Compound)
		name, _ := nbt.Get[nbt.String](entry, "Name")
		got[string(name)] = entry
	}
	if got["d"]["CriteriaName"] != nbt.String("dummy") {
		t.Errorf("objective d saved %v, want criteria dummy", got["d"])
	}
	if got["h"]["RenderType"] != nbt.String("integer") {
		t.Errorf("objective h saved %v, want render integer", got["h"])
	}
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

// TestMultiHolderSelectorSpansItsSpaces checks that a score argument taking
// several holders reads a selector whose brackets hold a space as one word.
// vanilla: ScoreHolderArgument.parse
func TestMultiHolderSelectorSpansItsSpaces(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	c.ChatCommand("scoreboard objectives add x dummy")
	servertest.FeedbackComponent(t, c)
	c.ChatCommand("tag @s add a")
	servertest.FeedbackComponent(t, c)

	c.ChatCommand("scoreboard players set @a[tag=a, limit=2] x 5")
	expectScoreboardFeedback(t, c, "commands.scoreboard.players.set.success.single")
	if v, ok := srv.ScoreOf("Testificate", "x"); !ok || v != 5 {
		t.Errorf("score after set = %d, %v, want 5", v, ok)
	}

	c.ChatCommand("team add t")
	awaitFeedbackWith(t, c, "commands.team.add.success")
	c.ChatCommand("team join t @a[tag=a, limit=2]")
	awaitFeedbackWith(t, c, "commands.team.join.success.single")

	c.ChatCommand("execute store result score @a[tag=a, limit=2] x run scoreboard players set @s x 7")
	servertest.FeedbackComponent(t, c)
	if v, ok := srv.ScoreOf("Testificate", "x"); !ok || v != 7 {
		t.Errorf("score after store = %d, %v, want 7", v, ok)
	}

	// The selector's own error, as the plain command gives it: the same key
	// and the cursor after the space inside the bracket.
	const plain = "scoreboard players set @s[ x 1"
	c.ChatCommand(plain)
	first := c.ExpectPlay(protocol.IDPlaySystemChat)
	c.ExpectPlay(protocol.IDPlaySystemChat)
	const stored = "execute store result score @s[ x run say hi"
	c.ChatCommand(stored)
	c.ExpectCommandError(first, servertest.CommandErrorContextForTest(stored, len("execute store result score @s[ ")))
}

// joinMinimalOp joins an op at the minimum view and simulation distances.
func joinMinimalOp(t *testing.T) (*server.Server, *servertest.Client) {
	t.Helper()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.ViewDistance = config.MinDistance
	cfg.SimulationDistance = config.MinDistance
	return servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))
}

// expectCommandErrorWithinTicks reads a command error's two System Chat
// messages, each within the tick bound.
func expectCommandErrorWithinTicks(srv *server.Server, c *servertest.Client, message, context []byte) {
	c.T.Helper()
	if got := c.ExpectWithinTicks(srv, 0, protocol.IDPlaySystemChat); !bytes.Equal(got, message) {
		c.T.Errorf("command error message = % X, want % X", got, message)
	}
	if got := c.ExpectWithinTicks(srv, 0, protocol.IDPlaySystemChat); !bytes.Equal(got, context) {
		c.T.Errorf("command error context = % X, want % X", got, context)
	}
}

// TestScoreboardComponentUUIDDiagnostic checks that a show_entity hover with
// a malformed string uuid is refused with the codec's message for the
// exception UUID.fromString throws, and a context that points at the
// component's start.
// vanilla: CommandArgumentParser.withCodec, UUIDUtil.STRING_CODEC
func TestScoreboardComponentUUIDDiagnostic(t *testing.T) {
	t.Parallel()
	srv, c := joinMinimalOp(t)

	const prefix = "Failed to parse either. First: Not a list; Second: Invalid UUID "
	for uuid, reason := range map[string]string{
		"32c1e9d0-a046-34fb-8bc1-3986ea745dc40": "UUID string too large",
		"zz-a046-34fb-8bc1-3986ea745dc4":        `Error at index 0 in: "zz"`,
		"1-2-3-4-8000000000000000":              `Error at index 15 in: "8000000000000000"`,
		"1-2-3":                                 "Invalid UUID string: 1-2-3",
		"1--3-4-5":                              `For input string: "" under radix 16`,
		"1-2-3-4-+":                             `Error at index 1 in: "+"`,
		"1-2-3-4--5":                            "Invalid UUID string: 1-2-3-4--5",
	} {
		component := `{text:"x",hover_event:{action:"show_entity",id:"minecraft:player",name:"S",uuid:"` + uuid + `"}}`
		input := "scoreboard objectives add bad dummy " + component
		c.ChatCommand(input)
		expectCommandErrorWithinTicks(srv, c,
			servertest.CommandErrorMessageForTest("argument.component.invalid", prefix+uuid+": "+reason),
			servertest.CommandErrorContextForTest(input, len(input)-len(component)),
		)
	}
}

// TestComponentArgumentsShowTheCodecDiagnostic checks that the component and
// style arguments refuse with the codec's message and a cursor at the
// argument's start, as a vanilla 26.3 server answers them.
// vanilla: CommandArgumentParser.withCodec
func TestComponentArgumentsShowTheCodecDiagnostic(t *testing.T) {
	t.Parallel()
	srv, c := joinMinimalOp(t)

	c.ChatCommand("scoreboard objectives add kills dummy")
	c.ExpectWithinTicks(srv, 0, protocol.IDPlaySystemChat)
	for _, tc := range []struct{ key, prefix, arg, want string }{
		{"argument.component.invalid", "scoreboard objectives add bad dummy ", `{text:"x",color:"nope",bold:"yes"}`, "Not a number; Invalid color name: nope"},
		{"argument.component.invalid", "scoreboard objectives add bad dummy ", `[1,2]`, "Failed to parse either. First: Failed to parse either. First: Not a string; Second: Failed to parse either. First: Failed to parse either. First: Not a string; Second: Not a list: 2; Second: Not a map: 2; Failed to parse either. First: Failed to parse either. First: Not a string; Second: Not a list: 1; Second: Not a map: 1; List must have contents; Second: Not a map: [1,2]"},
		{"argument.component.invalid", "scoreboard objectives add bad dummy ", `{text:"x",extra:[1,{text:"y",color:"nope"}]}`, "Invalid color name: nope; Failed to parse either. First: Failed to parse either. First: Not a string; Second: Not a list: 1; Second: Not a map: 1"},
		{"argument.component.invalid", "scoreboard objectives add bad dummy ", `{text:"x",click_event:{action:"open_url",url:"nope"},hover_event:{action:"nope"}}`, "Unknown element name:nope; Missing protocol in URI: nope: nope"},
		{"argument.component.invalid", "scoreboard objectives add bad dummy ", `{text:"x",type:"nope"}`, "Failed to parse either. First: Failed to parse either. First: Not a string; Second: Not a list: {text:\"x\",type:\"nope\"}; Second: Unknown element id: nope"},
		{"argument.component.invalid", "scoreboard objectives modify kills displayname ", `{text:1}`, "Failed to parse either. First: Failed to parse either. First: Not a string; Second: Not a list: {text:1}; Second: No matching codec found"},
		{"argument.component.invalid", "tellraw @a ", `{text:"x",color:"nope"}`, "Invalid color name: nope"},
		{"argument.component.invalid", "tellraw @a ", `{translate:"x",fallback:1,color:"nope"}`, "Invalid color name: nope"},
		{"argument.component.invalid", "tellraw @a ", `{text:"x",font:":BAD"}`, "Not a valid resource location: :BAD Non [a-z0-9/._-] character in path of location: minecraft:BAD"},
		{"argument.component.invalid", "tellraw @a ", `{text:"x",hover_event:{action:"show_entity",id:":player",uuid:"bad"}}`, "Failed to parse either. First: Not a list; Second: Invalid UUID bad: Invalid UUID string: bad"},
		{"argument.component.invalid", "title @a title ", `{text:1}`, "Failed to parse either. First: Failed to parse either. First: Not a string; Second: Not a list: {text:1}; Second: No matching codec found"},
		{"argument.component.invalid", "datapack create zz ", `{text:1}`, "Failed to parse either. First: Failed to parse either. First: Not a string; Second: Not a list: {text:1}; Second: No matching codec found"},
		{"argument.style.invalid", "scoreboard objectives modify kills numberformat styled ", `{color:"nope",bold:"x"}`, "Not a number; Invalid color name: nope"},
		{"argument.style.invalid", "scoreboard objectives modify kills numberformat styled ", `5`, "Not a map: 5"},
		{"argument.style.invalid", "scoreboard objectives modify kills numberformat styled ", `{italic:1,hover_event:{action:"nope"}}`, "Unknown element name:nope"},
	} {
		input := tc.prefix + tc.arg
		c.ChatCommand(input)
		expectCommandErrorWithinTicks(srv, c,
			servertest.CommandErrorMessageForTest(tc.key, tc.want),
			servertest.CommandErrorContextForTest(input, len(tc.prefix)),
		)
	}
}

// vanilla: StyleArgument, Style.Serializer.MAP_CODEC, CommandArgumentParser.withCodec.
func TestScoreboardStyleCodecDiagnostics(t *testing.T) {
	t.Parallel()
	srv, c := joinMinimalOp(t)
	c.ChatCommand("scoreboard objectives add kills dummy")
	c.ExpectWithinTicks(srv, 0, protocol.IDPlaySystemChat)
	const prefix = "scoreboard objectives modify kills numberformat styled "
	for _, tc := range []struct{ arg, want string }{
		{`{shadow_color:[1,"x",2,3]}`, `Failed to parse either. First: Not a number; Second: Not a number; Input is not a list of 4 elements`},
		{`{shadow_color:[I;1,2]}`, `Failed to parse either. First: Not a number; Second: Input is not a list of 4 elements`},
		{`{hover_event:{action:"show_item",id:1}}`, `Not a string`},
		{`{hover_event:{action:"show_item",id:"stone",count:0,components:5}}`, `Not a map: 5; Value must be within range [1;99]: 0`},
		{`{click_event:{action:"custom",id:1}}`, `Not a string`},
		{`{click_event:{action:"show_dialog"}}`, `No key dialog in MapLike[{action:"show_dialog"}]`},
		{`{hover_event:{action:"show_text",value:{type:"text",text:1}}}`, `Failed to parse either. First: Failed to parse either. First: Not a string; Second: Not a list: {text:1,type:"text"}; Second: Not a string`},
		{`{font:"BAD",bold:"yes",click_event:{action:"custom",id:1}}`, `Not a valid resource location: BAD Non [a-z0-9/._-] character in path of location: minecraft:BAD; Not a string; Not a number`},
	} {
		input := prefix + tc.arg
		c.ChatCommand(input)
		expectCommandErrorWithinTicks(srv, c,
			servertest.CommandErrorMessageForTest("argument.style.invalid", tc.want),
			servertest.CommandErrorContextForTest(input, len(prefix)))
	}
}

// vanilla: Dialog.CODEC, CommonDialogData.MAP_CODEC, ActionButton.CODEC.
// Literal diagnostics also checked with the registry-aware vanilla codec.
func TestScoreboardDialogCodecDiagnostics(t *testing.T) {
	t.Parallel()
	srv, c := joinMinimalOp(t)
	c.ChatCommand("scoreboard objectives add kills dummy")
	c.ExpectWithinTicks(srv, 0, protocol.IDPlaySystemChat)
	for _, mode := range []struct{ prefix, key, start string }{
		{"scoreboard objectives add bad dummy ", "argument.component.invalid", `{text:"x",`},
		{"scoreboard objectives modify kills numberformat styled ", "argument.style.invalid", `{`},
	} {
		for _, tc := range []struct{ dialog, want string }{
			{`5`, `Not a map: 5`},
			{`"BAD"`, `Not a map: "BAD"`},
			{`"missing"`, `Failed to get element minecraft:missing from registry minecraft:dialog`},
			{`{}`, `No key type in MapLike[{}]`},
			{`{type:5}`, `Not a string`},
			{`{type:"bad"}`, `Unknown registry key in ResourceKey[minecraft:root / minecraft:dialog_type]: minecraft:bad`},
			{`{type:"notice"}`, `No key title in MapLike[{type:"notice"}]`},
			{`{type:"notice",title:5,pause:"x"}`, `Not a number; Failed to parse either. First: Failed to parse either. First: Not a string; Second: Not a list: 5; Second: Not a map: 5`},
			{`{type:"notice",title:"ok",action:{label:5,width:0}}`, `Value must be within range [1;1024]: 0; Failed to parse either. First: Failed to parse either. First: Not a string; Second: Not a list: 5; Second: Not a map: 5`},
			{`{type:"confirmation",title:"x"}`, `No key no in MapLike[{title:"x",type:"confirmation"}]; No key yes in MapLike[{title:"x",type:"confirmation"}]`},
		} {
			input := mode.prefix + mode.start + `click_event:{action:"show_dialog",dialog:` + tc.dialog + `}}`
			c.ChatCommand(input)
			expectCommandErrorWithinTicks(srv, c,
				servertest.CommandErrorMessageForTest(mode.key, tc.want),
				servertest.CommandErrorContextForTest(input, len(mode.prefix)))
		}
	}
}

// vanilla: ComponentArgument.parse, SnbtGrammar.
func TestScoreboardComponentSNBTDiagnostics(t *testing.T) {
	t.Parallel()
	srv, c := joinMinimalOp(t)
	c.ChatCommand("scoreboard objectives add kills dummy")
	c.ExpectWithinTicks(srv, 0, protocol.IDPlaySystemChat)
	for _, prefix := range []string{
		"scoreboard objectives add bad dummy ",
		"scoreboard objectives modify kills displayname ",
		"scoreboard objectives modify kills numberformat fixed ",
		"scoreboard objectives modify kills numberformat styled ",
	} {
		for _, arg := range []string{`{text:`, `{text:}`} {
			input := prefix + arg
			c.ChatCommand(input)
			expectCommandErrorWithinTicks(srv, c,
				servertest.CommandErrorMessageForTest("snbt.parser.expected_unquoted_string"),
				servertest.CommandErrorContextForTest(input, len(prefix)+6))
		}
	}
}

// vanilla: Style.Serializer.MAP_CODEC, ClientboundSetObjectivePacket.write.
func TestScoreboardStyledNumberFormatPreservesEvents(t *testing.T) {
	t.Parallel()
	srv, c := joinMinimalOp(t)
	c.ChatCommand("scoreboard objectives add kills dummy")
	c.ExpectWithinTicks(srv, 0, protocol.IDPlaySystemChat)
	c.ChatCommand("scoreboard objectives setdisplay sidebar kills")
	c.ExpectWithinTicks(srv, 0, protocol.IDPlayUpdateObjectives)
	c.ExpectWithinTicks(srv, 0, protocol.IDPlayDisplayObjective)
	c.ExpectWithinTicks(srv, 0, protocol.IDPlaySystemChat)
	const style = `{italic:0b,shadow_color:[I;1,2,3,4],hover_event:{action:"show_item",id:"stone"},click_event:{action:"custom",id:"test:click",payload:{value:1}}}`
	// Non-style keys are ignored even when they would be invalid component fields.
	c.ChatCommand(`scoreboard objectives modify kills numberformat styled {text:1,extra:5,` + style[1:])
	r := protocol.NewReader(c.ExpectWithinTicks(srv, 0, protocol.IDPlayUpdateObjectives))
	if name, mode := r.ReadString(32767), r.ReadU8(); name != "kills" || mode != 2 {
		t.Fatalf("objective %s mode %d, want kills changed", name, mode)
	}
	if _, ok := protocol.ReadText(r); !ok {
		t.Fatal("invalid display name")
	}
	if render, has := r.ReadVarInt(), r.ReadBool(); render != 0 || !has || r.ReadVarInt() != int32(scoreboard.FormatStyled) {
		t.Fatal("missing styled number format")
	}
	got, _, err := nbt.DecodeNetwork(r.Rest())
	want, _, perr := nbt.ParseValueTag(style)
	if err != nil || perr != nil || nbt.Stringify(got) != nbt.Stringify(want) {
		t.Fatalf("style %s (%v), want %s", nbt.Stringify(got), err, nbt.Stringify(want))
	}
	c.ExpectWithinTicks(srv, 0, protocol.IDPlaySystemChat)
}

// TestScoreboardContentFamilyCodecDiagnostics checks that components of the
// selector, score, nbt and object content types are refused with their own
// codec's message and a cursor at the argument's start.
// vanilla: SelectorContents, ScoreContents, NbtContents, ObjectContents, CommandArgumentParser.withCodec
func TestScoreboardContentFamilyCodecDiagnostics(t *testing.T) {
	t.Parallel()
	srv, c := joinMinimalOp(t)

	const prefix = "scoreboard objectives add bad dummy "
	const typed = "Failed to parse either. First: Failed to parse either. First: Not a string; Second: Not a list: "
	for _, tc := range []struct{ arg, want string }{
		{`{type:"selector",selector:1}`, typed + `{selector:1,type:"selector"}; Second: Not a string`},
		{`{type:"selector",selector:"@a x"}`, typed + `{selector:"@a x",type:"selector"}; Second: Invalid selector component: @a x: Trailing data found: @a[ x]`},
		{`{type:"selector",selector:"@e["}`, typed + `{selector:"@e[",type:"selector"}; Second: Invalid selector component: @e[: Expected end of options at position 3: @e[<--[HERE]`},
		{`{type:"selector",selector:1,color:"nope"}`, typed + `{color:"nope",selector:1,type:"selector"}; Second: Invalid color name: nope; Not a string`},
		{`{type:"score",score:5}`, typed + `{score:5,type:"score"}; Second: Not a map: 5`},
		{`{type:"score",score:{}}`, typed + `{score:{},type:"score"}; Second: No key objective in MapLike[{}]; No key name in MapLike[{}]`},
		{`{type:"score",score:{name:1,objective:2}}`, typed + `{score:{name:1,objective:2},type:"score"}; Second: Not a string; Failed to parse either. First: Not a string; Second: Not a string`},
		{`{type:"nbt",nbt:"a[",entity:"@s"}`, typed + `{entity:"@s",nbt:"a[",type:"nbt"}; Second: Invalid NBT path: a[: Invalid NBT path element at position 2: a[<--[HERE]`},
		{`{type:"nbt",nbt:"a",entity:"@s",interpret:1b,plain:1b}`, typed + `{entity:"@s",interpret:1b,nbt:"a",plain:1b,type:"nbt"}; Second: 'interpret' and 'plain' flags can't be both on`},
		{`{type:"nbt",nbt:"a",source:"storage",storage:"BAD"}`, typed + `{nbt:"a",source:"storage",storage:"BAD",type:"nbt"}; Second: Not a valid resource location: BAD Non [a-z0-9/._-] character in path of location: minecraft:BAD`},
		{`{type:"object",object:"atlas"}`, typed + `{object:"atlas",type:"object"}; Second: No key sprite in MapLike[{object:"atlas",type:"object"}]`},
		{`{type:"object",object:"nope",sprite:"a"}`, typed + `{object:"nope",sprite:"a",type:"object"}; Second: Unknown element id: nope`},
		// Without a type the codec searches every content type.
		{`{score:{name:"@s"}}`, typed + `{score:{name:"@s"}}; Second: No matching codec found`},
		{`{object:"atlas"}`, typed + `{object:"atlas"}; Second: No matching codec found`},
	} {
		input := prefix + tc.arg
		c.ChatCommand(input)
		expectCommandErrorWithinTicks(srv, c,
			servertest.CommandErrorMessageForTest("argument.component.invalid", tc.want),
			servertest.CommandErrorContextForTest(input, len(prefix)))
	}
}

// TestScoreboardComponentSNBTAndContentDiagnostics checks that unreadable
// SNBT shows the parser's own error at its offset, and that the typed
// content codecs' reasons reach the message.
// vanilla: ComponentArgument.parse, SnbtGrammar, ComponentSerialization.CODEC
func TestScoreboardComponentSNBTAndContentDiagnostics(t *testing.T) {
	t.Parallel()
	srv, c := joinMinimalOp(t)
	const prefix = "scoreboard objectives add bad dummy "

	input := prefix + `{text:`
	c.ChatCommand(input)
	expectCommandErrorWithinTicks(srv, c,
		servertest.CommandErrorMessageForTest("snbt.parser.expected_unquoted_string"),
		servertest.CommandErrorContextForTest(input, len(input)))

	const either = "Failed to parse either. First: Failed to parse either. First: Not a string; Second: Not a list: "
	for _, tc := range []struct{ arg, want string }{
		{`{type:"text",text:1}`, either + `{text:1,type:"text"}; Second: Not a string`},
		{`{type:"keybind",keybind:1}`, either + `{keybind:1,type:"keybind"}; Second: Not a string`},
		{`{selector:1}`, either + `{selector:1}; Second: No matching codec found`},
		{`{type:"selector",selector:"@e[",separator:1}`, either + `{selector:"@e[",separator:1,type:"selector"}; Second: Failed to parse either. First: Failed to parse either. First: Not a string; Second: Not a list: 1; Second: Not a map: 1; Invalid selector component: @e[: Expected end of options at position 3: @e[<--[HERE]`},
		{`{type:"score",score:{name:"a"}}`, either + `{score:{name:"a"},type:"score"}; Second: No key objective in MapLike[{name:"a"}]`},
		{`{type:"object",object:"atlas"}`, either + `{object:"atlas",type:"object"}; Second: No key sprite in MapLike[{object:"atlas",type:"object"}]`},
	} {
		input := prefix + tc.arg
		c.ChatCommand(input)
		expectCommandErrorWithinTicks(srv, c,
			servertest.CommandErrorMessageForTest("argument.component.invalid", tc.want),
			servertest.CommandErrorContextForTest(input, len(prefix)))
	}
}
