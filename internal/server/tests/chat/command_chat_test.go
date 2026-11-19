package chat

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
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

// TestMeCommandResolvesSelector expands a selector in the action into the
// display names it names. vanilla: MessageArgument.Message.toComponent
func TestMeCommandResolvesSelector(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	// Selecting entities needs the gamemaster permission even though /me
	// itself does not.
	_, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	c.ChatCommand("me waves at @p")
	servertest.ExpectDisguisedChatComponent(t, c, "minecraft:emote_command", "Testificate",
		protocol.Text{Text: "waves at ", Extra: []protocol.Text{
			servertest.PlayerDisplayComponent("Testificate"),
		}}, "")
	c.Disconnect()
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

// TestSayCommandRefusesMalformedSelector checks a selector a message
// argument cannot read fails the command at the parser rather than being
// broadcast whole: only an unknown or missing selector letter is stepped
// over. vanilla: MessageArgument.Message.parseText
func TestSayCommandRefusesMalformedSelector(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Alice"))

	alice := connect().Join("Alice")

	bob := connect().Join("Bob")
	alice.ExpectPlayerAdded("Bob")
	alice.ExpectAnnouncement("multiplayer.player.joined", "Bob")

	alice.ChatCommand("say hi @a[")
	alice.ExpectCommandError(
		servertest.CommandErrorMessageForTest("argument.entity.options.unterminated"),
		servertest.CommandErrorContextForTest("say hi @a[", len("say hi @a[")))

	// The refusal ends the command, so nothing is broadcast to the world:
	// once Alice's command is done with, nothing is queued to Bob.
	alice.SettleKeeping()
	bob.ExpectNoneWithinTicks(srv, 0, servertest.PacketIs(protocol.IDPlayDisguisedChat))

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

// TestSayCommandLengthCountsCharacters checks the message cap counts UTF-16
// code units, not bytes: 256 Cyrillic characters sit at the cap and go out,
// and one more is refused.
// vanilla: MessageArgument.Message.parseText
func TestSayCommandLengthCountsCharacters(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	atCap := strings.Repeat("ф", 256)
	c.ChatCommand("say " + atCap)
	servertest.ExpectDisguisedChat(t, c, "minecraft:say_command", "Testificate", atCap, "")

	over := strings.Repeat("ф", 257)
	c.ChatCommand("say " + over)
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

// TestMsgCommandToTwoTargets whispers to two players at once: each target
// reads the message, and the sender reads one outgoing message per target.
// vanilla: MsgCommand.sendMessage
func TestMsgCommandToTwoTargets(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	// A player needs to be an operator to use a selector.
	_, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Alice"))
	alice := connect().Join("Alice")
	targets := map[string]*servertest.Client{}
	for _, name := range []string{"Bob", "Carol"} {
		c := connect().Join(name)
		alice.ExpectPlayerAdded(name)
		alice.ExpectAnnouncement("multiplayer.player.joined", name)
		targets[name] = c
	}
	// The command arrives unsigned, so the sender reads one outgoing
	// Disguised form per target, in target order, and each target the
	// incoming form with no target.
	// incoming form named by the sender.
	alice.ChatCommand("msg @a[name=!Alice] hi both")
	servertest.ExpectDisguisedChat(t, alice, "minecraft:msg_command_outgoing", "Alice", "hi both", "Bob")
	servertest.ExpectDisguisedChat(t, alice, "minecraft:msg_command_outgoing", "Alice", "hi both", "Carol")
	for _, c := range targets {
		servertest.ExpectDisguisedChat(t, c, "minecraft:msg_command_incoming", "Alice", "hi both", "")
		c.Disconnect()
	}
	alice.Disconnect()
}

// TestMsgSelectorWithSpacesInItsBrackets checks the targets argument read
// whole and the message found behind it: the whisper reaches the player the
// selector names, and the message is the text after the selector rather than
// one of its own options.
func TestMsgSelectorWithSpacesInItsBrackets(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	// A player needs to be an operator to use a selector.
	_, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Alice"))

	alice := connect().Join("Alice")

	bob := connect().Join("Bob")
	alice.ExpectPlayerAdded("Bob")
	alice.ExpectAnnouncement("multiplayer.player.joined", "Bob")

	alice.ChatCommand("msg @a[ name=Bob ] secret plans")
	servertest.ExpectDisguisedChat(t, bob, "minecraft:msg_command_incoming", "Alice", "secret plans", "")
	servertest.ExpectDisguisedChat(t, alice, "minecraft:msg_command_outgoing", "Alice", "secret plans", "Bob")

	alice.Disconnect()
	bob.Disconnect()
}

// TestSelectorTargetsResolveInJoinOrder checks the order a selector's targets
// resolve in: the player list, the order players joined in, not order by
// name. vanilla: PlayerList.getPlayers
func TestSelectorTargetsResolveInJoinOrder(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, connect := servertest.Start(t, cfg)

	bob := connect().Join("Bob")

	alice := connect().Join("Alice")
	bob.ExpectPlayerAdded("Alice")
	bob.ExpectAnnouncement("multiplayer.player.joined", "Alice")

	// Bob joined first, so @a names him first even though Alice's name sorts
	// first; the console reads one outgoing whisper per target.
	reply := srv.RunLine("msg @a both")
	if len(reply) != 2 || reply[0].Plain() != "You whisper to Bob: both" ||
		reply[1].Plain() != "You whisper to Alice: both" {
		t.Fatalf("msg @a answered %v, want Bob then Alice", reply)
	}

	// The limit cuts from the same join order, so it keeps Bob.
	reply = srv.RunLine("msg @a[limit=1] alone")
	if len(reply) != 1 || reply[0].Plain() != "You whisper to Bob: alone" {
		t.Fatalf("msg @a[limit=1] answered %v, want Bob alone", reply)
	}

	// Both stand at the spawn, so @p is a tie, which breaks towards the same
	// join order.
	reply = srv.RunLine("msg @p nearest")
	if len(reply) != 1 || reply[0].Plain() != "You whisper to Bob: nearest" {
		t.Fatalf("msg @p answered %v, want Bob", reply)
	}

	// /list names them in the same order, not sorted by name.
	list := srv.RunLine("list")
	if len(list) != 1 || list[0].Translate != "commands.list.players" {
		t.Fatalf("list answered %v, want the players translation", list)
	}
	var names []string
	for _, part := range list[0].With[2].Extra {
		if part.Text != ", " {
			names = append(names, part.Text)
		}
	}
	if len(names) != 2 || names[0] != "Bob" || names[1] != "Alice" {
		t.Fatalf("list names = %v, want [Bob Alice]", names)
	}

	// Each target still reads the whispers in turn.
	servertest.ExpectDisguisedChat(t, bob, "minecraft:msg_command_incoming", "Server", "both", "")
	servertest.ExpectDisguisedChat(t, alice, "minecraft:msg_command_incoming", "Server", "both", "")
	servertest.ExpectDisguisedChat(t, bob, "minecraft:msg_command_incoming", "Server", "alone", "")
	servertest.ExpectDisguisedChat(t, bob, "minecraft:msg_command_incoming", "Server", "nearest", "")

	bob.Disconnect()
	alice.Disconnect()
}

// TestSelectorTargetsFollowARespawn checks that a respawn re-adds the player to
// the end of the player list, so a selector names them last.
// vanilla: PlayerList.respawn
func TestSelectorTargetsFollowARespawn(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, connect := servertest.Start(t, cfg)

	bob := connect().Join("Bob")

	alice := connect().Join("Alice")
	bob.ExpectPlayerAdded("Alice")
	bob.ExpectAnnouncement("multiplayer.player.joined", "Alice")

	// Bob joined first, so he is at the head; the respawn puts him at the
	// end, and @a names Alice first.
	srv.RunLine("kill Bob")
	bob.Send(protocol.IDPlayClientCommandIn, func(b *protocol.Buffer) {
		b.PutVarInt(protocol.ClientCommandPerformRespawn)
	})
	bob.AwaitPacket(protocol.IDPlayRespawn)

	reply := srv.RunLine("msg @a back")
	if len(reply) != 2 || reply[0].Plain() != "You whisper to Alice: back" ||
		reply[1].Plain() != "You whisper to Bob: back" {
		t.Fatalf("msg @a answered %v, want Alice then Bob", reply)
	}

	bob.Disconnect()
	alice.Disconnect()
}

// TestWorldLimitedSelectorFollowsDimensionOrder checks that a selector limited
// to the runner's dimension iterates that dimension's own player list, where a
// player who crossed in is at the end, rather than the global join order.
// vanilla: ServerLevel.players
func TestWorldLimitedSelectorFollowsDimensionOrder(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Carol"))

	alice := connect().Join("Alice")

	bob := connect().Join("Bob")
	alice.ExpectPlayerAdded("Bob")
	alice.ExpectAnnouncement("multiplayer.player.joined", "Bob")

	carol := connect().Join("Carol")
	alice.ExpectPlayerAdded("Carol")
	alice.ExpectAnnouncement("multiplayer.player.joined", "Carol")
	bob.ExpectPlayerAdded("Carol")
	bob.ExpectAnnouncement("multiplayer.player.joined", "Carol")

	// Into the nether in an order that differs from the join order: Bob
	// crossed first, so the nether's own list is Bob, Alice, Carol.
	srv.MovePlayerToDimension("Bob", world.NetherName, 8, 40, 8)
	srv.MovePlayerToDimension("Alice", world.NetherName, 8, 40, 8)
	srv.MovePlayerToDimension("Carol", world.NetherName, 8, 40, 8)

	// Carol runs it and is excluded, leaving Bob and Alice: the nether's
	// order is Bob then Alice, the join order the reverse.
	carol.ChatCommand("msg @a[name=!Carol,distance=..64] here")
	servertest.ExpectDisguisedChat(t, carol, "minecraft:msg_command_outgoing", "Carol", "here", "Bob")
	servertest.ExpectDisguisedChat(t, carol, "minecraft:msg_command_outgoing", "Carol", "here", "Alice")

	for _, c := range []*servertest.Client{alice, bob, carol} {
		c.Disconnect()
	}
}

// TestWorldLimitedSelectorFollowsARespawn checks that a same-dimension respawn
// re-adds the player to the end of the dimension's own player list, not only
// the global one, so a world-limited selector names them last.
// vanilla: ServerLevel.addRespawnedPlayer
func TestWorldLimitedSelectorFollowsARespawn(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Carol"))

	alice := connect().Join("Alice")

	bob := connect().Join("Bob")
	alice.ExpectPlayerAdded("Bob")
	alice.ExpectAnnouncement("multiplayer.player.joined", "Bob")

	carol := connect().Join("Carol")
	alice.ExpectPlayerAdded("Carol")
	alice.ExpectAnnouncement("multiplayer.player.joined", "Carol")
	bob.ExpectPlayerAdded("Carol")
	bob.ExpectAnnouncement("multiplayer.player.joined", "Carol")

	// Alice joined first, but her respawn moves her to the end of the
	// overworld's own list: Bob then Alice.
	srv.RunLine("kill Alice")
	alice.Send(protocol.IDPlayClientCommandIn, func(b *protocol.Buffer) {
		b.PutVarInt(protocol.ClientCommandPerformRespawn)
	})
	alice.AwaitPacket(protocol.IDPlayRespawn)

	// Carol runs it and is excluded, leaving Bob and Alice in the overworld's
	// order after the respawn: Bob then Alice.
	carol.ChatCommand("msg @a[name=!Carol,distance=..64] here")
	servertest.ExpectDisguisedChat(t, carol, "minecraft:msg_command_outgoing", "Carol", "here", "Bob")
	servertest.ExpectDisguisedChat(t, carol, "minecraft:msg_command_outgoing", "Carol", "here", "Alice")

	for _, c := range []*servertest.Client{alice, bob, carol} {
		c.Disconnect()
	}
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
