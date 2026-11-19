package chat

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
)

// TestSayCommand broadcasts to the whole world in the say_command chat type,
// the layout of which renders it the way a spoken line renders.
func TestSayCommand(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	// /say is a gamemaster's command, so the runner is listed as an
	// operator.
	_, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	c.ChatCommand("say hello world")
	servertest.ExpectDisguisedChat(t, c, "minecraft:say_command", "Testificate", "hello world", "")
	c.Disconnect()
}

// TestMeCommand acts out an action in the emote_command chat type, which is
// the layout that renders an action line.
func TestMeCommand(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	_, c := servertest.Join(t, cfg, "Testificate")

	c.ChatCommand("me waves hello")
	servertest.ExpectDisguisedChat(t, c, "minecraft:emote_command", "Testificate", "waves hello", "")
	c.Disconnect()
}

// TestSayCommandResolvesSelector expands a selector in the message into the
// display names of the players it names, the words around it kept, and an @
// that is no selector left as typed. vanilla: MessageArgument.Message.toComponent
func TestSayCommandResolvesSelector(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	// /say is a gamemaster's command, so the runner is listed as an
	// operator.
	_, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Alice"))

	alice := connect().Join("Alice")

	bob := connect().Join("Bob")
	alice.ExpectPlayerAdded("Bob")
	alice.ExpectAnnouncement("multiplayer.player.joined", "Bob")

	// @a names both players in join order, as a comma-separated list of
	// their display names.
	alice.ChatCommand("say @a")
	servertest.ExpectDisguisedChatComponent(t, alice, "minecraft:say_command", "Alice",
		protocol.Text{Extra: []protocol.Text{{Extra: []protocol.Text{
			servertest.PlayerDisplayComponent("Alice"),
			{Text: ", ", Color: protocol.ColorGray},
			servertest.PlayerDisplayComponent("Bob"),
		}}}}, "")

	// @s is the runner; @Cmd is no selector letter, so it stays as typed.
	alice.ChatCommand("say @s @Cmd hi")
	servertest.ExpectDisguisedChatComponent(t, alice, "minecraft:say_command", "Alice",
		protocol.Text{Extra: []protocol.Text{
			servertest.PlayerDisplayComponent("Alice"),
			{Text: " @Cmd hi"},
		}}, "")

	alice.Disconnect()
	bob.Disconnect()
}

// TestMsgCommandResolvesSelector expands a selector in the whispered message
// into the display names it names, for the target and the sender alike.
// vanilla: MessageArgument.Message.toComponent
func TestMsgCommandResolvesSelector(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	_, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Alice"))

	alice := connect().Join("Alice")

	bob := connect().Join("Bob")
	alice.ExpectPlayerAdded("Bob")
	alice.ExpectAnnouncement("multiplayer.player.joined", "Bob")

	// @s is Alice, so both ends read "Alice hi". The command arrives
	// unsigned, so the whisper is the Disguised form.
	alice.ChatCommand("msg Bob @s hi")
	message := protocol.Text{Extra: []protocol.Text{
		servertest.PlayerDisplayComponent("Alice"),
		{Text: " hi"},
	}}
	servertest.ExpectDisguisedChatComponent(t, bob, "minecraft:msg_command_incoming", "Alice", message, "")
	servertest.ExpectDisguisedChatComponent(t, alice, "minecraft:msg_command_outgoing", "Alice", message, "Bob")

	alice.Disconnect()
	bob.Disconnect()
}

// TestSayCommandRefusesOverlongMessage checks a message argument is capped at
// 256 characters, refused rather than broadcast whole.
// vanilla: MessageArgument.Message.parseText
func TestSayCommandRefusesOverlongMessage(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	message := strings.Repeat("a", 257)
	c.ChatCommand("say " + message)
	// The over-length refusal carries no input, so it travels as its message
	// alone, with no context line behind it.
	c.ExpectCommandFailure(servertest.CommandErrorMessageForTest("argument.message.too_long", "257", "256"))
	c.ExpectNoneWithinTicks(srv, 0, servertest.PacketIs(protocol.IDPlaySystemChat))
	c.Disconnect()
}

// TestMessageSelectorsNeedPermission checks resolving a message selector
// needs the gamemaster permission: below it the text is broadcast as typed,
// the @ included. vanilla: MessageArgument.Message.toComponent
func TestMessageSelectorsNeedPermission(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, connect := servertest.Start(t, cfg)

	alice := connect().Join("Alice")

	bob := connect().Join("Bob")
	alice.ExpectPlayerAdded("Bob")
	alice.ExpectAnnouncement("multiplayer.player.joined", "Bob")

	// /me needs no permission, but reading its selector does: @a stays.
	alice.ChatCommand("me @a")
	servertest.ExpectDisguisedChat(t, alice, "minecraft:emote_command", "Alice", "@a", "")
	servertest.ExpectDisguisedChat(t, bob, "minecraft:emote_command", "Alice", "@a", "")

	// The whisper keeps its message as typed for the same reason.
	alice.ChatCommand("msg Bob @a")
	servertest.ExpectDisguisedChat(t, bob, "minecraft:msg_command_incoming", "Alice", "@a", "")
	servertest.ExpectDisguisedChat(t, alice, "minecraft:msg_command_outgoing", "Alice", "@a", "Bob")

	// The cap is checked before the selectors are read, so a runner below
	// the permission is refused an over-long action all the same.
	alice.ChatCommand("me " + strings.Repeat("a", 257))
	alice.ExpectCommandFailure(servertest.CommandErrorMessageForTest("argument.message.too_long", "257", "256"))
	alice.ExpectNoneWithinTicks(srv, 0, servertest.PacketIs(protocol.IDPlaySystemChat))

	alice.Disconnect()
	bob.Disconnect()
}

// TestMsgCommand whispers to one player and no one else: the target reads
// the incoming form of the direct-message chat type, the sender the outgoing
// one, and the aliases reach the same command.
func TestMsgCommand(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	_, connect := servertest.Start(t, cfg)

	alice := connect().Join("Alice")

	bob := connect().Join("Bob")
	alice.ExpectPlayerAdded("Bob")
	alice.ExpectAnnouncement("multiplayer.player.joined", "Bob")

	alice.ChatCommand("msg Bob secret plans")
	servertest.ExpectDisguisedChat(t, bob, "minecraft:msg_command_incoming", "Alice", "secret plans", "")
	servertest.ExpectDisguisedChat(t, alice, "minecraft:msg_command_outgoing", "Alice", "secret plans", "Bob")

	// The aliases reach the same command: /w Alice thanks whispers to Alice,
	// who reads it in the incoming form with Bob as its sender.
	bob.ChatCommand("w Alice thanks")
	servertest.ExpectDisguisedChat(t, alice, "minecraft:msg_command_incoming", "Bob", "thanks", "")

	alice.Disconnect()
	bob.Disconnect()
}

// TestTeamMsgCommand sends a team message to the runner's team only: the
// runner reads the outgoing form and a teammate the incoming one, both naming
// the team, a player on no team is refused, and /tm reaches the same command.
// vanilla: TeamMsgCommand
func TestTeamMsgCommand(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	_, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Alice"))
	alice := connect().Join("Alice")
	bob := connect().Join("Bob")
	alice.ExpectPlayerAdded("Bob")
	alice.ExpectAnnouncement("multiplayer.player.joined", "Bob")
	carol := connect().Join("Carol")
	alice.ExpectPlayerAdded("Carol")
	alice.ExpectAnnouncement("multiplayer.player.joined", "Carol")

	alice.ChatCommand("team add red")
	alice.ChatCommand("team modify red color red")
	alice.ChatCommand("team join red Alice")
	alice.ChatCommand("team join red Bob")
	redAlice := servertest.TeamPlayerDisplayComponent("Alice", "", "red")
	redBob := servertest.TeamPlayerDisplayComponent("Bob", "", "red")
	red := protocol.Text{Translate: "chat.square_brackets", With: []protocol.Text{{Text: "red"}}, Color: "red",
		ClickCommand: "/teammsg ", HoverText: &protocol.Text{Translate: "chat.type.team.hover"}}

	alice.ChatCommand("teammsg hi team")
	servertest.ExpectDisguisedChatTo(t, alice, "minecraft:team_msg_command_outgoing", redAlice, protocol.Text{Text: "hi team"}, &red)
	servertest.ExpectDisguisedChatTo(t, bob, "minecraft:team_msg_command_incoming", redAlice, protocol.Text{Text: "hi team"}, &red)

	bob.ChatCommand("tm got it")
	servertest.ExpectDisguisedChatTo(t, bob, "minecraft:team_msg_command_outgoing", redBob, protocol.Text{Text: "got it"}, &red)
	servertest.ExpectDisguisedChatTo(t, alice, "minecraft:team_msg_command_incoming", redBob, protocol.Text{Text: "got it"}, &red)

	// Carol, on no team, saw neither: the next line she reads is a whisper.
	alice.ChatCommand("msg Carol only you")
	servertest.ExpectDisguisedChat(t, carol, "minecraft:msg_command_incoming", redAlice, "only you", "")
	carol.ChatCommand("teammsg anyone?")
	carol.ExpectCommandFailure(servertest.CommandErrorMessageForTest("commands.teammsg.failed.noteam"))

	alice.Disconnect()
	bob.Disconnect()
	carol.Disconnect()
}

// TestSeedCommand names the seed the world was generated from. The seed the
// test gives the world arrives in a level.dat written before the server
// starts, the way a world that has one carries it.
func TestSeedCommand(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()

	// A level.dat carrying the seed, which the world reads when it opens.
	data, err := nbt.Encode("", nbt.Compound{"Data": nbt.Compound{
		"WorldGenSettings": nbt.Compound{"seed": nbt.Long(12345)},
	}})
	if err != nil {
		t.Fatalf("encode level.dat: %v", err)
	}
	if err := servertest.WriteGzip(filepath.Join(cfg.LevelName, "level.dat"), data); err != nil {
		t.Fatalf("write level.dat: %v", err)
	}

	// /Seed is a gamemaster's command, so the runner is listed as an
	// operator.
	_, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	c.ChatCommand("seed")
	if seed := c.ExpectSeed(); seed != 12345 {
		t.Errorf("/seed named %d, want 12345", seed)
	}
	c.Disconnect()
}
