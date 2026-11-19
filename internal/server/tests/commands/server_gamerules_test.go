package commands

import (
	"bytes"
	"github.com/golem-mc/golem/internal/server/servertest"
	"strconv"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/world"
)

// readGameRuleValues decodes a Game Rule Values packet into a name to value
// map and reports the entry count it claimed.
func readGameRuleValues(t *testing.T, data []byte) (int32, map[string]string) {
	t.Helper()
	r := protocol.NewReader(data)
	count := r.ReadVarInt()
	values := make(map[string]string, count)
	for range count {
		values[r.ReadString(32767)] = r.ReadString(32767)
	}
	if err := r.Err(); err != nil {
		t.Fatalf("game rule values: %v", err)
	}
	if r.Remaining() != 0 {
		t.Errorf("game rule values has %d bytes left over", r.Remaining())
	}
	return count, values
}

// TestGameRuleValuesAsOperator has an operator ask for the game rule values,
// the request the Edit Game Rules screen sends when it opens: every offered
// rule comes back under its identifier with its current value as text.
func TestGameRuleValuesAsOperator(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, c := servertest.Join(t, cfg, "Alice", servertest.OpsListing(t, "Alice"))

	c.SendClientCommand(protocol.ClientCommandRequestGameRuleValues)
	count, values := readGameRuleValues(t, c.ExpectPlay(protocol.IDPlayGameRuleValues))

	if int(count) != len(values) {
		t.Errorf("game rule values = %d entries but %d distinct names", count, len(values))
	}
	offered := 0
	for _, rule := range world.GameRuleTable {
		if rule.FeatureGated {
			continue
		}
		offered++
		name := "minecraft:" + rule.ID
		got, ok := values[name]
		if !ok {
			t.Errorf("game rule values is missing %s", name)
			continue
		}
		var want string
		if rule.Kind == world.RuleBool {
			want = strconv.FormatBool(srv.GameRule(rule.ID))
		} else {
			want = strconv.FormatInt(int64(srv.GameRuleValue(rule.ID)), 10)
		}
		if got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
	if int(count) != offered {
		t.Errorf("game rule values = %d entries, want %d offered rules", count, offered)
	}
}

// TestGameRuleValuesWithoutPermissionIsIgnored has a player who is not an
// operator ask for the game rule values: vanilla refuses it, so no values
// packet goes out.
func TestGameRuleValuesWithoutPermissionIsIgnored(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	_, c := servertest.Join(t, cfg, "Visitor")

	c.SendClientCommand(protocol.ClientCommandRequestGameRuleValues)
	c.SettleAndCollect(func(id int32, _ []byte) {
		if id == protocol.IDPlayGameRuleValues {
			t.Errorf("a non-operator's request for game rule values produced the values packet")
		}
	})
}

// TestSetGameRuleAsOperator has an operator send the Set Game Rule packet:
// the rule is changed and the change carries the same broadcast /gamerule
// sends, the Game Event an immediate_respawn set owes the players.
func TestSetGameRuleAsOperator(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, c := servertest.Join(t, cfg, "Alice", servertest.OpsListing(t, "Alice"))

	c.SendSetGameRule([2]string{"minecraft:immediate_respawn", "true"})
	r := protocol.NewReader(c.ExpectPlay(protocol.IDPlayGameEvent))
	if got := r.ReadU8(); got != protocol.GameEventImmediateRespawn {
		t.Errorf("game event = %d, want %d (immediate respawn)", got, protocol.GameEventImmediateRespawn)
	}
	if got := r.ReadF32(); got != 1 {
		t.Errorf("game event value = %v, want 1", got)
	}
	if err := r.Err(); err != nil {
		t.Fatalf("game event: %v", err)
	}
	if !srv.GameRule("immediate_respawn") {
		t.Error("immediate_respawn is off after an operator set it true")
	}

	// An integer rule is set through the same path, the value clamped by the
	// world rather than the screen.
	c.SendSetGameRule([2]string{"minecraft:random_tick_speed", "5"})
	c.Settle()
	if got := srv.GameRuleValue("random_tick_speed"); got != 5 {
		t.Errorf("random_tick_speed = %d, want 5", got)
	}
}

// TestSetGameRuleWithoutPermissionIsIgnored has a player who is not an
// operator send the Set Game Rule packet: vanilla refuses it, so the rule is
// left alone and nothing about it goes out.
func TestSetGameRuleWithoutPermissionIsIgnored(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, c := servertest.Join(t, cfg, "Visitor")

	c.SendSetGameRule([2]string{"minecraft:immediate_respawn", "true"})
	c.SettleAndCollect(func(id int32, _ []byte) {
		if id == protocol.IDPlayGameEvent {
			t.Errorf("a non-operator's Set Game Rule produced the changed rule's broadcast")
		}
	})
	if srv.GameRule("immediate_respawn") {
		t.Error("a non-operator's Set Game Rule changed the rule")
	}
}

// expectGameruleSetChat reads packets until the commands.gamerule.set system
// chat arrives, skipping the Update Time broadcasts a rule change can carry,
// and checks the rule and value it names.
func expectGameruleSetChat(t *testing.T, c *servertest.Client, rule, value string) {
	t.Helper()
	for gotID, data := range c.Stream() {
		t.Helper()
		switch gotID {
		case protocol.IDPlayUpdateTime:
			continue
		case protocol.IDPlaySystemChat:
			var want protocol.Buffer
			want.PutText(protocol.Text{Translate: "commands.gamerule.set",
				With: []protocol.Text{{Text: rule}, {Text: value}}})
			want.PutBool(false)
			if !bytes.Equal(data, want.Bytes()) {
				t.Errorf("gamerule set chat = % X, want % X", data, want.Bytes())
			}
			return
		}
	}
	t.Fatal("gamerule set chat did not arrive")
}

// TestSetGameRuleSendsOperatorChat has an operator set a boolean rule from the
// packet: the sender is answered the same commands.gamerule.set system chat
// /gamerule sends.
func TestSetGameRuleSendsOperatorChat(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, c := servertest.Join(t, cfg, "Alice", servertest.OpsListing(t, "Alice"))

	c.SendSetGameRule([2]string{"minecraft:keep_inventory", "true"})
	expectGameruleSetChat(t, c, "keep_inventory", "true")
	if !srv.GameRule("keep_inventory") {
		t.Error("keep_inventory is off after an operator set it true")
	}
}

// TestSetGameRuleChatNotSentToNonOperator has an operator set a rule while a
// non-operator is online: the operator hears the change, the non-operator
// hears nothing about it.
func TestSetGameRuleChatNotSentToNonOperator(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	_, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Alice"))
	op := connect().Join("Alice")

	visitor := connect().Join("Visitor")

	// The visitor's arrival is still queued on the operator's connection;
	// settle it so the chat read next is the rule change's.
	op.Settle()

	op.SendSetGameRule([2]string{"minecraft:keep_inventory", "true"})
	expectGameruleSetChat(t, op, "keep_inventory", "true")

	visitor.SettleAndCollect(func(id int32, data []byte) {
		if id == protocol.IDPlaySystemChat {
			t.Errorf("a non-operator received a rule change chat: % X", data)
		}
	})
}

// TestSetGameRuleBroadcastReachesOtherOperator has two operators online: a
// rule set from one reaches the other as the plain commands.gamerule.set
// message, not the grey admin echo /gamerule sends.
func TestSetGameRuleBroadcastReachesOtherOperator(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	_, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Alice", "Bob"))
	alice := connect().Join("Alice")

	bob := connect().Join("Bob")

	// Bob's arrival is still queued on Alice's connection; settle both so the
	// chat read next is the rule change's.
	alice.Settle()
	bob.Settle()

	alice.SendSetGameRule([2]string{"minecraft:keep_inventory", "true"})
	expectGameruleSetChat(t, alice, "keep_inventory", "true")
	expectGameruleSetChat(t, bob, "keep_inventory", "true")
}

// TestSetGameRuleBroadcastIgnoresFeedbackRule sends one packet that turns
// send_command_feedback off and then sets another rule: both operators still
// read a plain message per entry, which vanilla's packet path sends without
// the feedback gate.
func TestSetGameRuleBroadcastIgnoresFeedbackRule(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Alice", "Bob"))
	alice := connect().Join("Alice")

	bob := connect().Join("Bob")

	alice.Settle()
	bob.Settle()

	alice.SendSetGameRule(
		[2]string{"minecraft:send_command_feedback", "false"},
		[2]string{"minecraft:keep_inventory", "true"},
	)
	expectGameruleSetChat(t, alice, "send_command_feedback", "false")
	expectGameruleSetChat(t, alice, "keep_inventory", "true")
	expectGameruleSetChat(t, bob, "send_command_feedback", "false")
	expectGameruleSetChat(t, bob, "keep_inventory", "true")
	if srv.GameRule("send_command_feedback") {
		t.Error("send_command_feedback is on after the packet turned it off")
	}
}

// TestSetGameRuleOutOfRangeIntIsRefused sets an integer rule below its floor
// from the packet: the value is refused and the rule keeps its old value,
// where the /gamerule tree would have refused the argument already.
func TestSetGameRuleOutOfRangeIntIsRefused(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, c := servertest.Join(t, cfg, "Alice", servertest.OpsListing(t, "Alice"))

	c.SendSetGameRule([2]string{"minecraft:random_tick_speed", "-5"})
	c.Settle()
	if got := srv.GameRuleValue("random_tick_speed"); got != 3 {
		t.Errorf("random_tick_speed = %d, want the default 3 left unchanged", got)
	}
}

// TestSetGameRuleLeadingPlusIsRefused sets an integer rule with a leading
// plus, which the vanilla integer reader refuses though strconv accepts it.
func TestSetGameRuleLeadingPlusIsRefused(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, c := servertest.Join(t, cfg, "Alice", servertest.OpsListing(t, "Alice"))

	c.SendSetGameRule([2]string{"minecraft:max_entity_cramming", "+7"})
	c.Settle()
	if got := srv.GameRuleValue("max_entity_cramming"); got != 24 {
		t.Errorf("max_entity_cramming = %d, want the default 24 left unchanged", got)
	}
}

// TestSetGameRuleUnparseableValueIsRefused sets a boolean rule to a word that
// is no boolean: the rule is left unchanged.
func TestSetGameRuleUnparseableValueIsRefused(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, c := servertest.Join(t, cfg, "Alice", servertest.OpsListing(t, "Alice"))

	c.SendSetGameRule([2]string{"minecraft:keep_inventory", "maybe"})
	c.Settle()
	if srv.GameRule("keep_inventory") {
		t.Error("a non-boolean value turned keep_inventory on")
	}
}

// TestSetGameRuleTrailingValueIsRefused sets a boolean rule with a tail
// behind the value, which the vanilla reader leaves unread and refuses.
func TestSetGameRuleTrailingValueIsRefused(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, c := servertest.Join(t, cfg, "Alice", servertest.OpsListing(t, "Alice"))

	c.SendSetGameRule([2]string{"minecraft:keep_inventory", "true false"})
	c.Settle()
	if srv.GameRule("keep_inventory") {
		t.Error("a value with a tail turned keep_inventory on")
	}
}

// TestSetGameRuleUnknownKeyIsIgnored sends an entry naming no rule of the
// table: it changes nothing and owes no broadcast, while the other entries
// of the same packet are still applied.
func TestSetGameRuleUnknownKeyIsIgnored(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, c := servertest.Join(t, cfg, "Alice", servertest.OpsListing(t, "Alice"))

	c.SendSetGameRule(
		[2]string{"minecraft:not_a_rule", "true"},
		[2]string{"minecraft:keep_inventory", "true"},
	)
	c.Settle()
	if !srv.GameRule("keep_inventory") {
		t.Error("the known entry beside an unknown key was not applied")
	}
}
