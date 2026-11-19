package advancements

import (
	"slices"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/advancement"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// outcome is what a command and the ticks after it put on the wire.
type outcome struct {
	updates []sentUpdate
	chats   []protocol.Text
}

// run sends a command as the player and reads the next few ticks.
func run(t *testing.T, srv *server.Server, c *servertest.Client, cmd string) outcome {
	t.Helper()
	c.ChatCommand(cmd)
	var o outcome
	c.ReadTicks(srv, 5, func(id int32, data []byte) bool {
		switch id {
		case protocol.IDPlayUpdateAdvancements:
			o.updates = append(o.updates, parseUpdate(t, data))
		case protocol.IDPlaySystemChat:
			text, ok := protocol.ReadText(protocol.NewReader(data))
			if !ok {
				t.Fatalf("system chat % X does not decode", data)
			}
			o.chats = append(o.chats, text)
		}
		return false
	})
	return o
}

// key is the translation key of a chat line, looking inside the red wrapper
// a refusal arrives in.
func key(msg protocol.Text) string {
	if msg.Translate == "" && len(msg.Extra) == 1 {
		return msg.Extra[0].Translate
	}
	return msg.Translate
}

func isAnnouncement(msg protocol.Text) bool {
	return strings.HasPrefix(key(msg), "chat.type.advancement.")
}

// feedback returns the chat lines that are not announcements.
func (o outcome) feedback() []protocol.Text {
	var out []protocol.Text
	for _, m := range o.chats {
		if !isAnnouncement(m) {
			out = append(out, m)
		}
	}
	return out
}

func (o outcome) announcements() []protocol.Text {
	var out []protocol.Text
	for _, m := range o.chats {
		if isAnnouncement(m) {
			out = append(out, m)
		}
	}
	return out
}

// expectFeedback checks the command answered with exactly one line of the
// given key and returns it; a refusal's line is the red wrapper's inner one.
func (o outcome) expectFeedback(t *testing.T, want string) protocol.Text {
	t.Helper()
	fb := o.feedback()
	if len(fb) != 1 || key(fb[0]) != want {
		t.Fatalf("feedback = %+v, want one %s", fb, want)
	}
	if len(fb[0].Extra) == 1 && fb[0].Translate == "" {
		return fb[0].Extra[0]
	}
	return fb[0]
}

func startOp(t *testing.T) (*server.Server, *servertest.Client) {
	t.Helper()
	srv, connect := servertest.Start(t, servertest.SurvivalTestConfig(), servertest.OpsListing(t, "Admin"))
	c, _ := join(t, connect, "Admin")
	return srv, c
}

func count(t protocol.Text) int64 {
	if t.Number == nil {
		return -1
	}
	return *t.Number
}

// TestGrantAndRevokeOnly grants the story root: the feedback names the
// advancement and the player, the tab arrives with the root's criterion
// obtained, and a completed advancement that announces is told in chat. A second grant has
// nothing to change and is refused; a revoke takes the tab away again.
// vanilla: AdvancementCommands.perform, PlayerAdvancements.award, revoke
func TestGrantAndRevokeOnly(t *testing.T) {
	t.Parallel()
	srv, c := startOp(t)

	o := run(t, srv, c, "advancement grant @s only minecraft:story/root")
	fb := o.expectFeedback(t, "commands.advancement.grant.one.to.one.success")
	if len(fb.With) != 2 || fb.With[1].Text != "Admin" {
		t.Errorf("grant feedback arguments = %+v, want the advancement and Admin", fb.With)
	}
	if len(o.updates) != 1 {
		t.Fatalf("got %d Update Advancements, want 1", len(o.updates))
	}
	expectStoryRoot(t, o.updates[0])
	// The root has announce_to_chat false.
	if ann := o.announcements(); len(ann) != 0 {
		t.Errorf("story/root was announced: %+v", ann)
	}
	o = run(t, srv, c, "advancement grant @s only minecraft:story/mine_stone")
	ann := o.announcements()
	if len(ann) != 1 || ann[0].Translate != "chat.type.advancement.task" || len(ann[0].With) != 2 || ann[0].With[0].Text != "Admin" {
		t.Errorf("announcements = %+v, want one chat.type.advancement.task for Admin", ann)
	}
	run(t, srv, c, "advancement revoke @s only minecraft:story/mine_stone")

	o = run(t, srv, c, "advancement grant @s only minecraft:story/root")
	o.expectFeedback(t, "commands.advancement.grant.one.to.one.failure")
	if len(o.updates) != 0 {
		t.Errorf("a refused grant sent %d updates", len(o.updates))
	}

	o = run(t, srv, c, "advancement revoke @s only minecraft:story/root")
	o.expectFeedback(t, "commands.advancement.revoke.one.to.one.success")
	if len(o.updates) != 1 || !slices.Contains(o.updates[0].removed, "minecraft:story/root") {
		t.Fatalf("revoke sent %+v, want story/root removed", o.updates)
	}
	if len(o.announcements()) != 0 {
		t.Error("a revoke announced something in chat")
	}
	o = run(t, srv, c, "advancement revoke @s only minecraft:story/root")
	o.expectFeedback(t, "commands.advancement.revoke.one.to.one.failure")
}

// TestAnnouncementFollowsTheGameRule turns show_advancement_messages off:
// the grant still shows the advancement but says nothing in chat.
// vanilla: PlayerAdvancements.award
func TestAnnouncementFollowsTheGameRule(t *testing.T) {
	t.Parallel()
	srv, c := startOp(t)
	run(t, srv, c, "gamerule show_advancement_messages false")
	o := run(t, srv, c, "advancement grant @s only minecraft:story/mine_stone")
	o.expectFeedback(t, "commands.advancement.grant.one.to.one.success")
	if len(o.updates) != 1 || len(o.announcements()) != 0 {
		t.Errorf("updates %d, announcements %+v; want one update and no announcement", len(o.updates), o.announcements())
	}
}

// TestModesAndCriterion covers the modes around an advancement and the
// one-criterion form.
// vanilla: AdvancementCommands.getAdvancements, performCriterion
func TestModesAndCriterion(t *testing.T) {
	t.Parallel()
	srv, c := startOp(t)

	o := run(t, srv, c, "advancement grant @s until minecraft:story/mine_stone")
	fb := o.expectFeedback(t, "commands.advancement.grant.many.to.one.success")
	if len(fb.With) != 2 || count(fb.With[0]) != 2 {
		t.Errorf("until arguments = %+v, want 2 advancements", fb.With)
	}

	o = run(t, srv, c, "advancement revoke @s only minecraft:story/root crafting_table")
	fb = o.expectFeedback(t, "commands.advancement.revoke.criterion.to.one.success")
	if len(fb.With) != 3 || fb.With[0].Text != "crafting_table" {
		t.Errorf("criterion arguments = %+v", fb.With)
	}
	o = run(t, srv, c, "advancement revoke @s only minecraft:story/root crafting_table")
	o.expectFeedback(t, "commands.advancement.revoke.criterion.to.one.failure")
	o = run(t, srv, c, "advancement grant @s only minecraft:story/root no_such_criterion")
	o.expectFeedback(t, "commands.advancement.criterionNotFound")
	o = run(t, srv, c, "advancement grant @s only minecraft:no_such/advancement")
	o.expectFeedback(t, "advancement.advancementNotFound")

	// Only mine_stone is still done, but feedback counts the whole selected subtree.
	o = run(t, srv, c, "advancement revoke @s from minecraft:story/root")
	fb = o.expectFeedback(t, "commands.advancement.revoke.many.to.one.success")
	selected := 0
	for _, n := range advancement.Vanilla().All() {
		if n.Root().ID == "minecraft:story/root" {
			selected++
		}
	}
	if count(fb.With[0]) != int64(selected) {
		t.Errorf("revoke from root selected %d advancements, want %d", count(fb.With[0]), selected)
	}
}

// vanilla: AdvancementCommands.perform
func TestRevokeEverythingReportsSelectionAndReturnsChanged(t *testing.T) {
	t.Parallel()
	srv, c := startOp(t)
	servertest.HoldTicks(t, srv)
	// Read feedback without advancing ticks, so automatic criteria cannot add progress.
	feedback := func(cmd string) outcome {
		t.Helper()
		c.ChatCommand(cmd)
		msg, ok := protocol.ReadText(protocol.NewReader(c.AwaitPlay(protocol.IDPlaySystemChat)))
		if !ok {
			t.Fatal("command feedback does not decode")
		}
		return outcome{chats: []protocol.Text{msg}}
	}
	feedback("advancement revoke @s everything")
	feedback("advancement grant @s only minecraft:story/root").expectFeedback(t, "commands.advancement.grant.one.to.one.success")

	fb := feedback("execute store result storage test:advancement changed int 1 run advancement revoke @s everything").expectFeedback(t, "commands.advancement.revoke.many.to.one.success")
	if want := int64(advancement.Vanilla().Len()); len(fb.With) != 2 || count(fb.With[0]) != want {
		t.Errorf("revoke arguments = %+v, want %d selected advancements", fb.With, want)
	}
	c.ChatCommand("data get storage test:advancement changed")
	c.ExpectCommandFeedback("commands.data.storage.query", "test:advancement", "1")
}

// TestEverythingGrantsWithoutToasts grants and revokes every advancement:
// the grant's update clears show advancements, the revoke's does not.
// vanilla: AdvancementCommands.perform's showAdvancements
func TestEverythingGrantsWithoutToasts(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.SurvivalTestConfig(), servertest.OpsListing(t, "Admin"))
	c, _ := join(t, connect, "Admin")
	total := int64(advancement.Vanilla().Len())

	o := run(t, srv, c, "advancement grant @s everything")
	fb := o.expectFeedback(t, "commands.advancement.grant.many.to.one.success")
	if count(fb.With[0]) != total {
		t.Errorf("grant everything selected %d, want %d", count(fb.With[0]), total)
	}
	if len(o.updates) != 1 || o.updates[0].show {
		t.Fatalf("grant everything sent %+v, want one update with show advancements clear", o.updates)
	}

	o = run(t, srv, c, "advancement revoke @s everything")
	fb = o.expectFeedback(t, "commands.advancement.revoke.many.to.one.success")
	if count(fb.With[0]) != total {
		t.Errorf("revoke everything selected %d, want %d", count(fb.With[0]), total)
	}
	if len(o.updates) != 1 || !o.updates[0].show {
		t.Fatalf("revoke everything sent %+v, want one update with show advancements set", o.updates)
	}
}

// TestAdvancementsSelectorOption matches a player by what they have done.
// vanilla: EntitySelectorOptions advancements option
func TestAdvancementsSelectorOption(t *testing.T) {
	t.Parallel()
	srv, c := startOp(t)
	probe := func(sel string) string {
		o := run(t, srv, c, "advancement grant "+sel+" only minecraft:story/root crafting_table")
		fb := o.feedback()
		if len(fb) != 1 {
			t.Fatalf("%s: feedback %+v", sel, fb)
		}
		return key(fb[0])
	}
	const none = "argument.entity.notfound.player"
	done, undone := "@a[advancements={minecraft:story/root=true}]", "@a[advancements={minecraft:story/root=false}]"
	if got := probe(done); got != none {
		t.Errorf("%s before a grant = %s, want nobody", done, got)
	}
	if got := probe(undone); got != "commands.advancement.grant.criterion.to.one.success" {
		t.Errorf("%s before a grant = %s, want the player", undone, got)
	}
	// The grant above made it done.
	if got := probe(done); got != "commands.advancement.grant.criterion.to.one.failure" {
		t.Errorf("%s after a grant = %s, want a match with nothing to change", done, got)
	}
	if got := probe("@a[advancements={minecraft:story/root={crafting_table=true}}]"); got != "commands.advancement.grant.criterion.to.one.failure" {
		t.Errorf("criterion=true after a grant = %s, want a match", got)
	}
	if got := probe("@a[advancements={minecraft:story/root={crafting_table=false}}]"); got != none {
		t.Errorf("criterion=false after a grant = %s, want nobody", got)
	}
	if got := probe("@a[advancements={minecraft:story/root={no_such=true}}]"); got != none {
		t.Errorf("unknown criterion = %s, want nobody", got)
	}
	if got := probe("@a[advancements={minecraft:no_such/advancement=false}]"); got != none {
		t.Errorf("unknown advancement = %s, want nobody", got)
	}
}
