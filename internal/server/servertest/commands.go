package servertest

import (
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// SendChangeGameMode sends the serverbound Change Game Mode packet the F3+F4
// switcher produces.
func (c *Client) SendChangeGameMode(mode int32) {
	c.T.Helper()
	c.Send(protocol.IDPlayChangeGameMode, func(b *protocol.Buffer) { b.PutVarInt(mode) })
}

// AwaitMetadataFuse reads until a metadata packet for the entity carries the
// given primed-TNT fuse, stepping over the burning fuse's own tick updates.
func (c *Client) AwaitMetadataFuse(entityID, want int32) {
	c.T.Helper()
	for packetID, data := range c.Packets(ExpectTimeout) {
		if packetID != protocol.IDPlayEntityMetadata {
			continue
		}
		r := protocol.NewReader(data)
		if r.ReadVarInt() != entityID {
			continue
		}
		entries, ok := ReadMetadata(r)
		if !ok {
			c.T.Fatalf("metadata for entity %d carries a field this reader does not know", entityID)
		}
		if m := MetaOf(entries, protocol.MetadataIndexTNTFuse); m != nil && m.VarintVal == want {
			return
		}
	}
	c.T.Fatalf("no fuse %d metadata for entity %d arrived within %s", want, entityID, ExpectTimeout)
}

// AdminFeedbackComponent is the line the broadcast carries: the runner's
// display name and the command's own feedback, the two the chat.type.admin
// layout spells as [Name: message].
func AdminFeedbackComponent(runner, feedback protocol.Text) protocol.Text {
	return protocol.Text{
		Translate: "chat.type.admin",
		With:      []protocol.Text{runner, feedback},
		Color:     protocol.ColorGray,
		Italic:    true,
	}
}

// SystemChatWire is a finished message as its System Chat packet carries it.
func SystemChatWire(text protocol.Text) []byte {
	var b protocol.Buffer
	b.PutText(text)
	b.PutBool(false) // in the chat window, not over the hotbar
	return b.Bytes()
}

// JoinFeedbackWorld logs one player in and has the already-joined players
// witness the join, so every connection's stream starts clean for the reads
// that follow.
func JoinFeedbackWorld(t *testing.T, connect func() *Client, name string, witnesses []*Client) *Client {
	t.Helper()
	c := connect().Join(name)
	for _, w := range witnesses {
		w.ExpectPlayerAdded(name)
		w.ExpectAnnouncement("multiplayer.player.joined", name)
	}
	return c
}

// AwaitAdminBroadcast reads c until the admin line built from the runner and
// the feedback arrives, failing when the window runs out. What it reads past
// is kept for whoever reads next.
func AwaitAdminBroadcast(t *testing.T, c *Client, runner, feedback protocol.Text) {
	t.Helper()
	want := SystemChatWire(AdminFeedbackComponent(runner, feedback))

	var skipped []PushedPacket
	defer func() { c.Unread(skipped...) }()

	for id, data := range c.Packets(10 * time.Second) {
		t.Helper()
		if id == protocol.IDPlaySystemChat && bytes.Equal(data, want) {
			return
		}
		skipped = append(skipped, PushedPacket{Id: id, Data: data})
	}
	t.Fatalf("no admin broadcast of %s arrived", feedback.Translate)
}

// AwaitNoSystemChat reads c for a while and fails if any System Chat message
// arrives, the check that a player reads nothing of a command that must not
// reach them. What it reads past is kept for whoever reads next.
func AwaitNoSystemChat(t *testing.T, c *Client) {
	t.Helper()
	var skipped []PushedPacket
	defer func() { c.Unread(skipped...) }()

	for id, data := range c.Packets(time.Second) {
		t.Helper()
		if id == protocol.IDPlaySystemChat {
			t.Fatalf("a System Chat message reached a player it must not: % X", data)
		}
		skipped = append(skipped, PushedPacket{Id: id, Data: data})
	}
}

// The translation keys /item answers with.
const (
	ItemBlockSetKey          = "commands.item.block.replace.success.known_item"
	ItemEntitySetSingleKey   = "commands.item.entity.replace.success.single.known_item"
	ItemEntitySetMultipleKey = "commands.item.entity.replace.success.multiple.known_item"
	ItemTargetNoContainerKey = "commands.item.target.not_a_container"
	ItemSourceNoContainerKey = "commands.item.source.not_a_container"
	ItemTargetNoSlotKey      = "commands.item.target.no_such_slot"
	ItemSourceNoSlotKey      = "commands.item.source.no_such_slot"
	ItemNoChangedKnownItem   = "commands.item.target.failed.known_item"
	ItemOverstackedKey       = "arguments.item.overstacked"
	SlotUnknownKey           = "slot.unknown"
	SlotOnlySingleKey        = "slot.only_single_allowed"
	ItemPosUnloadedKey       = "argument.pos.unloaded"
	ItemPosOutOfWorldKey     = "argument.pos.outofworld"
)

// ItemNumber is a number a /item feedback or failure carries a count or a
// coordinate as.
func ItemNumber(n int64) protocol.Text {
	return protocol.Text{Number: &n}
}

// ItemFeedbackText is the stack a /item run put somewhere, as the vanilla
// feedback names it: the display name in brackets, in the item's rarity
// colour, the hover previewing as many of the item as the stack carried.
func ItemFeedbackText(item, displayKey string, count int32) protocol.Text {
	return protocol.Text{
		Translate: "chat.square_brackets",
		Color:     "white",
		With:      []protocol.Text{{Extra: []protocol.Text{{Translate: displayKey}}}},
		HoverItem: &protocol.HoverItem{ID: item, Count: count},
	}
}

// ItemCreativeConfig is the config a /item test runs under: offline, so
// anyone may join, and creative, so the world the items sit in is quiet.
func ItemCreativeConfig() *config.Config {
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"
	return cfg
}

// ItemDisplayKey is the translation key the item's display name renders from,
// which the vanilla feedback names it by.
func ItemDisplayKey(t *testing.T, item string) string {
	t.Helper()
	id, ok := registry.ItemID(item)
	if !ok {
		t.Fatalf("the registry has no %s", item)
	}
	key, ok := registry.ItemTranslationKey(id)
	if !ok {
		t.Fatalf("the registry has no display name for %s", item)
	}
	return key
}

// AwaitCommandFailure reads packets until the bare red failure the key makes
// arrives, stepping over the success lines and block updates the commands
// before it left on the way.
func AwaitCommandFailure(t *testing.T, c *Client, key string) {
	t.Helper()
	want := CommandErrorMessageForTest(key)
	for packetID, data := range c.Packets(10 * time.Second) {
		if packetID == protocol.IDPlaySystemChat && bytes.Equal(data, want) {
			return
		}
	}
	t.Fatalf("no bare red failure for %s arrived", key)
}

// AwaitBlockUpdate reads packets until a Block Update for the given position
// and state arrives or the timeout runs out, answering keep-alives and
// stepping over the traffic the world makes around the player. A cell that
// changed twice before the reader reached it leaves the earlier update on
// the wire — a random tick grew a block the /setblock behind the wait
// replaces — and that one is passed over: the wanted state is the verdict.
func AwaitBlockUpdate(t *testing.T, c *Client, x, y, z int32, want uint16) {
	t.Helper()
	// A command's feedback goes out at once and its block update at the next
	// flush, so the feedback is kept for the reader that comes next.
	var chat []PushedPacket
	defer func() { c.Unread(chat...) }()
	for packetID, data := range c.Packets(10 * time.Second) {
		t.Helper()
		if packetID == protocol.IDPlaySystemChat {
			chat = append(chat, PushedPacket{Id: packetID, Data: data})
		}
		if _, ok := c.takeCell(packetID, data, func(ch BlockChange) bool {
			return ch.X == x && ch.Y == y && ch.Z == z && ch.State == want
		}); ok {
			return
		}
	}
	t.Fatalf("no block update for (%d, %d, %d) = %d arrived", x, y, z, want)
}

// DrainBlockUpdates reads packets for the given number of ticks, stepping
// over whatever passes, so that a test that just changed the world leaves
// nothing behind for the next reader to trip over.
func DrainBlockUpdates(t *testing.T, c *Client, ticks int) {
	t.Helper()
	for range c.Packets(time.Duration(ticks) * 50 * time.Millisecond) {
	}
}

// ExpectTeleport reads packets until a Synchronize Player Position arrives or
// the timeout runs out, answering keep-alives and stepping over the traffic
// the world makes around the player, and returns where it puts the player.
func ExpectTeleport(t *testing.T, c *Client) player.Position {
	t.Helper()
	if pos := c.pendingTeleport; pos != nil {
		c.pendingTeleport = nil
		return *pos
	}
	if _, ok := ReadUntil(t, c, ExpectTimeout, protocol.IDPlaySyncPosition); !ok {
		t.Fatal("no synchronize player position arrived")
	}
	return c.LastTeleport
}

// LookAtView is what a Player Look At carried.
type LookAtView struct {
	FromEyes bool
	X, Y, Z  float64
	Entity   int32 // 0 for the position form
	ToEyes   bool
}

// ExpectLookAt reads packets until a Player Look At arrives and returns it.
func ExpectLookAt(t *testing.T, c *Client) LookAtView {
	t.Helper()
	data, ok := ReadUntil(t, c, ExpectTimeout, protocol.IDPlayLookAt)
	if !ok {
		t.Fatal("no player look at arrived")
	}
	r := protocol.NewReader(data)
	v := LookAtView{FromEyes: r.ReadVarInt() == 1, X: r.ReadF64(), Y: r.ReadF64(), Z: r.ReadF64()}
	if r.ReadBool() {
		v.Entity = r.ReadVarInt()
		v.ToEyes = r.ReadVarInt() == 1
	}
	if err := r.Err(); err != nil {
		t.Fatalf("player look at: %v", err)
	}
	return v
}

// AwaitCommandFeedback reads packets for a while looking for the System Chat
// message the given translation key and arguments make, skipping everything
// else on the way. It is for the commands whose feedback shares the wire
// with what the command did - a death, a move, a cleared inventory - so the
// reading steps over the effect to reach the line.
func AwaitCommandFeedback(t *testing.T, c *Client, key string, args ...string) {
	t.Helper()
	AwaitCommandFeedbackText(t, c, key, FeedbackArgsForTest(key, args)...)
}

// AwaitCommandError is AwaitCommandFeedback for the red failure line a command
// sends through sendFailure, the shape a failure wears whoever is running.
func AwaitCommandError(t *testing.T, c *Client, key string, args ...string) {
	t.Helper()
	with := make([]protocol.Text, 0, len(args))
	for _, arg := range args {
		with = append(with, protocol.Text{Text: arg})
	}
	AwaitCommandErrorText(t, c, key, with...)
}

// AwaitCommandErrorText is AwaitCommandError with the arguments given as the
// components they travel as.
func AwaitCommandErrorText(t *testing.T, c *Client, key string, with ...protocol.Text) {
	t.Helper()
	var want protocol.Buffer
	want.PutText(protocol.Text{Color: protocol.ColorRed,
		Extra: []protocol.Text{{Translate: key, With: with}}})
	want.PutBool(false) // in the chat window, not over the hotbar

	var skipped []PushedPacket
	defer func() { c.Unread(skipped...) }()

	for packetID, data := range c.Packets(10 * time.Second) {
		switch packetID {
		case protocol.IDPlaySystemChat:
			if bytes.Equal(data, want.Bytes()) {
				return
			}
		case protocol.IDPlaySyncPosition:
			pos := c.LastTeleport
			c.pendingTeleport = &pos
			continue
		}
		skipped = append(skipped, PushedPacket{Id: packetID, Data: data})
	}
	t.Fatalf("no command error for %s arrived", key)
}

// AwaitCommandFeedbackText is AwaitCommandFeedback with the arguments given
// as the components they travel as, which is what a feedback that names an
// entity by its display name or counts by a number carries.
func AwaitCommandFeedbackText(t *testing.T, c *Client, key string, with ...protocol.Text) {
	t.Helper()
	AwaitCommandFeedbackTextWithin(t, c, 10*time.Second, key, with...)
}

// AwaitCommandFeedbackTextWithin is AwaitCommandFeedbackText with the wait
// bounded by the caller, for a command whose work is heavy enough that a
// runner sharing its cores can take longer than the default wait to answer.
func AwaitCommandFeedbackTextWithin(t *testing.T, c *Client, d time.Duration, key string, with ...protocol.Text) {
	t.Helper()
	var want protocol.Buffer
	want.PutText(protocol.Text{Translate: key, With: with})
	want.PutBool(false) // in the chat window, not over the hotbar

	// What the command did may overtake its feedback (see
	// Client.pushback), so everything read past is kept for whoever
	// reads next.
	var skipped []PushedPacket
	defer func() { c.Unread(skipped...) }()

	for packetID, data := range c.Packets(d) {
		switch packetID {
		case protocol.IDPlaySystemChat:
			if bytes.Equal(data, want.Bytes()) {
				return
			}
		case protocol.IDPlaySyncPosition:
			pos := c.LastTeleport
			c.pendingTeleport = &pos
			continue
		}
		skipped = append(skipped, PushedPacket{Id: packetID, Data: data})
	}
	t.Fatalf("no command feedback for %s arrived", key)
}

// SlotUpdate is one Set Container Slot packet, with the slot it set and what
// it now holds.
type SlotUpdate struct {
	Slot  int16
	Stack protocol.ItemStack
}

// AwaitSlotUpdates reads packets until the given number of Set Container Slot
// packets have passed, answering keep-alives and stepping over everything
// else, and returns what they said along with the System Chat messages that
// passed on the way - a command's feedback and its slot packets both ride the
// mailbox, but either may be the one that is still to come.
func AwaitSlotUpdates(t *testing.T, c *Client, want int) ([]SlotUpdate, [][]byte) {
	t.Helper()
	var updates []SlotUpdate
	var chats [][]byte
	for packetID, data := range c.Packets(10 * time.Second) {
		switch packetID {
		case protocol.IDPlayContainerSlot:
			r := protocol.NewReader(data)
			r.ReadVarInt() // Window ID, which is always the player's own here
			r.ReadVarInt() // State ID
			updates = append(updates, SlotUpdate{
				Slot:  r.ReadI16(),
				Stack: MustReadSlot(t, r),
			})
		case protocol.IDPlaySystemChat:
			chats = append(chats, data)
		}
		if len(updates) == want {
			return updates, chats
		}
	}
	t.Fatalf("only %d of %d slot updates arrived", len(updates), want)
	return nil, nil
}

// ExpectFeedbackSoon checks the command feedback arrived, whether it came
// before what the caller has read so far or behind it. The chats captured on
// the way are looked at first, and anything read past after that is held for
// whoever reads next.
func ExpectFeedbackSoon(t *testing.T, c *Client, chats [][]byte, key string, with ...protocol.Text) {
	t.Helper()
	var want protocol.Buffer
	want.PutText(protocol.Text{Translate: key, With: with})
	want.PutBool(false) // in the chat window, not over the hotbar
	for _, chat := range chats {
		if bytes.Equal(chat, want.Bytes()) {
			return
		}
	}

	var skipped []PushedPacket
	defer func() { c.Unread(skipped...) }()
	for gotID, data := range c.Packets(ExpectTimeout) {
		if gotID == protocol.IDPlaySystemChat && bytes.Equal(data, want.Bytes()) {
			return
		}
		skipped = append(skipped, PushedPacket{Id: gotID, Data: data})
	}
	t.Fatalf("no feedback for %s arrived", key)
}

// MustReadSlot reads one slot, failing the test if it does not read back.
func MustReadSlot(t *testing.T, r *protocol.Reader) protocol.ItemStack {
	t.Helper()
	stack, ok := r.ReadSlot()
	if !ok {
		t.Fatal("slot did not read back")
	}
	return stack
}

// ExpectDeathSequence reads the packets a death is made of: the empty bar,
// the damage and death events, the respawn screen and the chat message. They
// are taken in whatever order they arrive, since the bar and the respawn
// screen are written direct while the entity events ride the mailbox.
func ExpectDeathSequence(t *testing.T, c *Client, entityID int32, message protocol.Text) {
	t.Helper()
	var wantDeath protocol.Buffer
	wantDeath.PutVarInt(entityID)
	wantDeath.PutText(message)
	var wantChat protocol.Buffer
	wantChat.PutText(message)
	wantChat.PutBool(false)
	var wantEvent protocol.Buffer
	wantEvent.PutI32(entityID)
	wantEvent.PutU8(protocol.EntityStatusDeath)

	var damage []byte
	killType := registry.MustID("minecraft:damage_type", "minecraft:generic_kill")
	staleType := int32(-1)
	var haveBar, haveDeath, haveChat, haveEvent bool
	var combatDeath []byte
	// A living body's bar can still be on the wire ahead of the death's:
	// a respawn's or a heal's, sent by a tick that read the body before the
	// kill landed. The empty bar is the one looked for. The death message
	// is looked for among the chat lines rather than taken as the first: a
	// /kill's own feedback can land between the death and its message.
	for id, data := range c.Packets(ExpectTimeout) {
		switch id {
		case protocol.IDPlaySetHealth:
			if protocol.NewReader(data).ReadF32() == 0 {
				haveBar = true
			}
		case protocol.IDPlayDamageEvent:
			r := protocol.NewReader(data)
			if r.ReadVarInt() != entityID {
				break
			}
			// A damage event from before the kill (suffocation, a burn)
			// can still be on the wire ahead of the kill's own.
			if staleType = r.ReadVarInt(); staleType == killType {
				damage = data
			}
		case protocol.IDPlayCombatDeath:
			combatDeath, haveDeath = data, true
		case protocol.IDPlayEntityEvent:
			if bytes.Equal(data, wantEvent.Bytes()) {
				haveEvent = true
			}
		case protocol.IDPlaySystemChat:
			if bytes.Equal(data, wantChat.Bytes()) {
				haveChat = true
			}
		}
		if haveBar && damage != nil && haveDeath && haveChat && haveEvent {
			break
		}
	}
	if !haveBar {
		t.Fatal("no Set Health with health 0 arrived for the death")
	}
	if damage == nil {
		t.Fatalf("no generic kill damage event arrived for the death (last damage type seen %d, want %d)", staleType, killType)
	}
	if !haveDeath {
		t.Fatal("no combat death arrived")
	}
	if !bytes.Equal(combatDeath, wantDeath.Bytes()) {
		t.Errorf("combat death = % X, want % X", combatDeath, wantDeath.Bytes())
	}
	if !haveChat {
		t.Fatalf("no death message % X arrived in chat", wantChat.Bytes())
	}
	if !haveEvent {
		t.Fatalf("no death entity event arrived for entity %d", entityID)
	}
}

// ReadSpawnPosition decodes a Set Default Spawn Position packet into its block
// and the rotation it carries.
func ReadSpawnPosition(t *testing.T, data []byte) (x, y, z int32, yaw, pitch float32) {
	t.Helper()
	r := protocol.NewReader(data)
	r.ReadString(32767) // dimension name
	x, y, z = r.ReadPosition()
	yaw, pitch = r.ReadF32(), r.ReadF32()
	if err := r.Err(); err != nil {
		t.Fatalf("set default spawn position: %v", err)
	}
	return x, y, z, yaw, pitch
}

// ProgramCommandBlock sends the Program Command Block packet, what a client
// programs a command block with from its editor's Done button.
// vanilla: ServerboundSetCommandBlockPacket
func (c *Client) ProgramCommandBlock(x, y, z int32, command string, mode int32, trackOutput, conditional, automatic bool) {
	c.T.Helper()
	c.Send(protocol.IDPlaySetCommandBlockIn, func(b *protocol.Buffer) {
		b.PutPosition(x, y, z)
		b.PutString(command)
		b.PutVarInt(mode)
		flags := byte(0)
		if trackOutput {
			flags |= 0x01
		}
		if conditional {
			flags |= 0x02
		}
		if automatic {
			flags |= 0x04
		}
		b.PutU8(flags)
	})
}

// The Program Command Block packet's mode values, the ordinals of the wire
// enum: chain, repeating, impulse.
// vanilla: CommandBlockEntity.Mode
const (
	ProgramModeChain     = 0
	ProgramModeRepeating = 1
	ProgramModeImpulse   = 2
)

// SayPayload builds the Disguised Chat Message payload a /say broadcast
// carries: the message, the say_command chat type and the name the command's
// source goes by, which an unnamed block spells "@".
func SayPayload(sender protocol.Text, message string) []byte {
	var b protocol.Buffer
	b.PutText(protocol.Text{Text: message})
	b.PutVarInt(registry.MustID("minecraft:chat_type", "minecraft:say_command") + 1)
	b.PutText(sender)
	b.PutBool(false)
	return b.Bytes()
}

// ExpectSayLine waits for a /say broadcast and checks it says message under
// the sender the block is named by, stepping over the lever clicks, the game
// rule feedback and the ticking world noise that share the stream.
func (c *Client) ExpectSayLine(t *testing.T, sender, message string) {
	t.Helper()
	c.ExpectSayLineAs(t, protocol.Text{Text: sender}, message)
}

// ExpectListOutput reads the next system chat message and checks that it
// matches the /list command's output for the given profiles.
func (c *Client) ExpectListOutput(maxPlayers int, profiles []player.Profile, showUUIDs bool) {
	c.T.Helper()
	data := c.ExpectPlay(protocol.IDPlaySystemChat)
	want := ListCommandPayloadForTest(maxPlayers, profiles, showUUIDs)
	if !bytes.Equal(data, want) {
		c.T.Errorf("list output = % X, want % X", data, want)
	}
}

// ListCommandPayloadForTest builds the expected System Chat packet payload for
// the /list command's output, mirroring what the server sends.
func ListCommandPayloadForTest(maxPlayers int, profiles []player.Profile, showUUIDs bool) []byte {
	var b protocol.Buffer
	b.PutText(protocol.Text{
		Translate: "commands.list.players",
		With: []protocol.Text{
			{Text: strconv.Itoa(len(profiles))},
			{Text: strconv.Itoa(maxPlayers)},
			PlayerListTextForTest(profiles, showUUIDs),
		},
	})
	b.PutBool(false)
	return b.Bytes()
}

func PlayerListTextForTest(profiles []player.Profile, showUUIDs bool) protocol.Text {
	switch len(profiles) {
	case 0:
		return protocol.Text{Text: ""}
	case 1:
		return PlayerNameTextForTest(profiles[0], showUUIDs)
	}
	extra := make([]protocol.Text, 0, len(profiles)*2-1)
	for i, p := range profiles {
		if i > 0 {
			extra = append(extra, protocol.Text{Text: ", ", Color: protocol.ColorGray})
		}
		extra = append(extra, PlayerNameTextForTest(p, showUUIDs))
	}
	return protocol.Text{Extra: extra}
}

func PlayerNameTextForTest(p player.Profile, showUUIDs bool) protocol.Text {
	if showUUIDs {
		return protocol.Text{
			Translate: "commands.list.nameAndId",
			With: []protocol.Text{
				{Text: p.Name},
				{Text: p.UUID.String()},
			},
		}
	}
	return PlayerDisplayComponent(p.Name)
}

// ExpectCommandError reads the two System Chat packets a command parse error
// produces: the red error message, then the gray context line pointing at
// where the parser stopped.
func (c *Client) ExpectCommandError(first, second []byte) {
	c.T.Helper()
	data := c.ExpectPlay(protocol.IDPlaySystemChat)
	if !bytes.Equal(data, first) {
		c.T.Errorf("command error message = % X, want % X", data, first)
	}
	data = c.ExpectPlay(protocol.IDPlaySystemChat)
	if !bytes.Equal(data, second) {
		c.T.Errorf("command error context = % X, want % X", data, second)
	}
}

// ExpectCommandFailure reads the one System Chat packet a command failure
// that travels without a context line produces.
func (c *Client) ExpectCommandFailure(first []byte) {
	c.T.Helper()
	data := c.ExpectPlay(protocol.IDPlaySystemChat)
	if !bytes.Equal(data, first) {
		c.T.Errorf("command failure = % X, want % X", data, first)
	}
}

// CommandErrorMessageForTest builds the expected first System Chat payload for a
// command parse error: the translation key in red.
func CommandErrorMessageForTest(key string, with ...string) []byte {
	var b protocol.Buffer
	inner := protocol.Text{Translate: key}
	for _, value := range with {
		inner.With = append(inner.With, protocol.Text{Text: value})
	}
	b.PutText(protocol.Text{Color: protocol.ColorRed, Extra: []protocol.Text{inner}})
	b.PutBool(false)
	return b.Bytes()
}

// CommandErrorMessageTextForTest is CommandErrorMessageForTest with the
// arguments given as the components they travel as, which a refusal that
// carries a translated message among its arguments needs.
func CommandErrorMessageTextForTest(key string, with ...protocol.Text) []byte {
	var b protocol.Buffer
	inner := protocol.Text{Translate: key, With: with}
	b.PutText(protocol.Text{Color: protocol.ColorRed, Extra: []protocol.Text{inner}})
	b.PutBool(false)
	return b.Bytes()
}

// CommandErrorContextForTest builds the expected second System Chat payload for
// a command parse error, mirroring the vanilla server's context line: the input
// around the cursor in gray, the remaining input in red underlined, and the
// "<--[HERE]" marker in red italic.
func CommandErrorContextForTest(input string, cursor int) []byte {
	cursor = min(cursor, len(input))
	var context protocol.Text
	context.ClickCommand = "/" + input
	context.Color = protocol.ColorGray
	if cursor > 10 {
		context.Extra = append(context.Extra, protocol.Text{Text: "..."})
	}
	preStart := max(0, cursor-10)
	if preStart < cursor {
		context.Extra = append(context.Extra, protocol.Text{Text: input[preStart:cursor]})
	}
	if cursor < len(input) {
		context.Extra = append(context.Extra, protocol.Text{
			Text:      input[cursor:],
			Color:     protocol.ColorRed,
			Underline: true,
		})
	}
	context.Extra = append(context.Extra, protocol.Text{
		Translate: "command.context.here",
		Color:     protocol.ColorRed,
		Italic:    true,
	})
	// The context line travels through the empty red component every
	// failure wears, the way the vanilla server sends it.
	var b protocol.Buffer
	b.PutText(protocol.Text{Color: protocol.ColorRed, Extra: []protocol.Text{context}})
	b.PutBool(false)
	return b.Bytes()
}

// ExpectComponentFeedback reads the next System Chat message and checks it is
// the translation the given key carries with the given component arguments,
// which is the shape the feedback whose arguments are themselves translations
// - a difficulty's display name, say - takes.
func (c *Client) ExpectComponentFeedback(key string, with ...protocol.Text) {
	c.T.Helper()
	var want protocol.Buffer
	want.PutText(protocol.Text{Translate: key, With: with})
	want.PutBool(false) // in the chat window, not over the hotbar
	data := c.ExpectPlay(protocol.IDPlaySystemChat)
	if !bytes.Equal(data, want.Bytes()) {
		c.T.Errorf("command feedback = % X, want % X", data, want.Bytes())
	}
}

// ExpectComponentError reads the next System Chat message and checks it is
// the failure the given key makes with the component arguments its %s takes:
// the translation inside the empty red component every command failure wears.
func (c *Client) ExpectComponentError(key string, with ...protocol.Text) {
	c.T.Helper()
	var want protocol.Buffer
	want.PutText(protocol.Text{
		Color: protocol.ColorRed,
		Extra: []protocol.Text{{Translate: key, With: with}},
	})
	want.PutBool(false) // in the chat window, not over the hotbar
	data := c.ExpectPlay(protocol.IDPlaySystemChat)
	if !bytes.Equal(data, want.Bytes()) {
		c.T.Errorf("command error = % X, want % X", data, want.Bytes())
	}
}

// ExpectNoHealth fails if a Set Health arrives within ticks ticks, which is
// how the tests that expect a floor to hold prove nothing is moving. It
// answers keep-alives and steps over everything else on the way.
func ExpectNoHealth(t *testing.T, srv *server.Server, c *Client, ticks int) {
	t.Helper()
	ExpectNoPacket(t, srv, c, ticks, protocol.IDPlaySetHealth)
}

// ExpectNoPacket fails if a packet of the given ID arrives within ticks ticks.
func ExpectNoPacket(t *testing.T, srv *server.Server, c *Client, ticks int, unwanted int32) {
	t.Helper()
	c.ExpectNoneWithinTicks(srv, ticks, PacketIs(unwanted))
}

// ReadSavedFile reads the gzipped NBT at path and returns the compound under
// key: "Data" for a level.dat, "data" for a saved-data file.
func ReadSavedFile(t *testing.T, path, key string) nbt.Compound {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		t.Fatalf("decompress %s: %v", path, err)
	}
	defer gz.Close()
	data, err := io.ReadAll(gz)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	_, tag, err := nbt.Decode(data)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	root, ok := tag.(nbt.Compound)
	if !ok {
		t.Fatalf("%s root is a %s, want a compound", path, tag.Type())
	}
	dataTag, ok := nbt.Get[nbt.Compound](root, key)
	if !ok {
		t.Fatalf("%s has no %s compound", path, key)
	}
	return dataTag
}

// StartExecuteWorld stands a world up with the runner an operator, the way
// every /execute test needs it: the whole tree offered, a flat ground to
// stand on and to judge.
func StartExecuteWorld(t *testing.T, runner string) (*server.Server, *Client) {
	t.Helper()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, c := Join(t, cfg, runner, OpsListing(t, runner))
	return srv, c
}

// ExpectSystemChatPastMobs reads the next System Chat a chain answers with,
// stepping over the noise expectPlay steps over and the removal notices a
// body's leaving sends: a corpse is taken from the world twenty ticks after
// its death — vanilla's death animation — so its notice can still be in
// flight when a later chain's answers are read.
func (c *Client) ExpectSystemChatPastMobs() []byte {
	c.T.Helper()
	for gotID, data := range c.Packets(ExpectTimeout) {
		c.T.Helper()
		if gotID == protocol.IDPlaySystemChat {
			return data
		}
		switch gotID {
		case protocol.IDPlayBlockUpdate, protocol.IDPlaySectionBlocks,
			protocol.IDPlayUpdateLight, protocol.IDPlayUpdateTime,
			protocol.IDPlayWorldEvent, protocol.IDPlaySoundEffect,
			protocol.IDPlaySpawnEntity, protocol.IDPlayEntityMetadata,
			protocol.IDPlaySetHealth, protocol.IDPlayUpdateAttributes,
			protocol.IDPlayDamageEvent, protocol.IDPlaySetEntityMotion,
			protocol.IDPlayEntityAnimation, protocol.IDPlaySwingAnimation, protocol.IDPlayLevelParticles,
			protocol.IDPlayEntityPosition, protocol.IDPlayEntityPositionRot,
			protocol.IDPlayEntityRotation, protocol.IDPlayHeadRotation,
			protocol.IDPlayTeleportEntity, protocol.IDPlayRemoveEntities,
			protocol.IDPlayEntityEquipment, protocol.IDPlayEntityEvent:
			// expectPlay's noise, plus the removals the corpse's
			// leaving trails its death by and the equipment a mob's
			// worn piece broadcasts, which can land after settle.
		default:
			c.T.Fatalf("got packet 0x%02X, want 0x%02X", gotID, protocol.IDPlaySystemChat)
		}
	}
	c.T.Fatalf("packet 0x%02X did not arrive within %s", protocol.IDPlaySystemChat, ExpectTimeout)
	return nil
}

// ReadSectionBlocks decodes an Update Section Blocks payload: the section's
// position long, then the entries, each a VarLong carrying the block state
// and the section-local cell it lands in.
func ReadSectionBlocks(t *testing.T, data []byte) (int64, []int64) {
	t.Helper()
	r := protocol.NewReader(data)
	pos := r.ReadI64()
	n := r.ReadVarInt()
	if n < 0 || n > 4096 {
		t.Fatalf("section blocks update names %d entries, want between 0 and 4096", n)
	}
	entries := make([]int64, n)
	for i := range entries {
		entries[i] = r.ReadVarLong()
	}
	if err := r.Err(); err != nil {
		t.Fatalf("section blocks update does not read back: %v", err)
	}
	return pos, entries
}

// AwaitBlockUpdateSoon is AwaitBlockUpdate for a reader that shares the
// connection with the ones around it: it reads through what they held back
// and holds back what it reads past, so the update a command's mailbox
// delivery and its feedback deliver in either order are both found.
func AwaitBlockUpdateSoon(t *testing.T, c *Client, x, y, z int32, want uint16) {
	t.Helper()
	var skipped []PushedPacket
	defer func() { c.Unread(skipped...) }()
	for packetID, data := range c.Packets(10 * time.Second) {
		t.Helper()
		// A section update can carry the cell's state from a change
		// before the one waited for; a lone Block Update is the verdict.
		if ch, ok := c.takeCell(packetID, data, func(ch BlockChange) bool {
			return ch.X == x && ch.Y == y && ch.Z == z && (ch.State == want || packetID == protocol.IDPlayBlockUpdate)
		}); ok {
			if ch.State != want {
				t.Errorf("block update at (%d, %d, %d) = %d, want %d", x, y, z, ch.State, want)
			}
			return
		}
		skipped = append(skipped, PushedPacket{Id: packetID, Data: data})
	}
	t.Fatalf("no block update for (%d, %d, %d) arrived", x, y, z)
}

// SendClientCommand sends the serverbound Client Command packet with action,
// the shape a client's respawn button and game-rule screen produce.
func (c *Client) SendClientCommand(action int32) {
	c.T.Helper()
	c.Send(protocol.IDPlayClientCommandIn, func(b *protocol.Buffer) { b.PutVarInt(action) })
}

// SendSetGameRule sends the serverbound Set Game Rule packet, one entry per
// name/value pair.
func (c *Client) SendSetGameRule(entries ...[2]string) {
	c.T.Helper()
	c.Send(protocol.IDPlaySetGameRuleIn, func(b *protocol.Buffer) {
		b.PutVarInt(int32(len(entries)))
		for _, e := range entries {
			b.PutString(e[0])
			b.PutString(e[1])
		}
	})
}

// PlaySoundEffect is one Sound Effect packet as a client reads it: the sound
// by registry ID (or by its inline name when the registry does not carry it),
// the category, the effect position in eighths of a block, the volume and
// pitch, and whether the seed long rode behind them.
type PlaySoundEffect struct {
	RegistryID    int32
	Name          string
	HasFixedRange bool
	FixedRange    float32
	Category      int32
	X, Y, Z       int32
	Volume        float32
	Pitch         float32
	HasSeed       bool
	Seed          int64
}

// DecodePlaySoundEffect reads a Sound Effect packet, failing if the seed long
// is missing, which is what "the seed is present" means on the wire.
func DecodePlaySoundEffect(t *testing.T, data []byte) PlaySoundEffect {
	t.Helper()
	r := protocol.NewReader(data)
	var s PlaySoundEffect
	if id := r.ReadVarInt(); id == 0 {
		s.RegistryID = -1
		s.Name = r.ReadString(32767)
		s.HasFixedRange = r.ReadBool()
		if s.HasFixedRange {
			s.FixedRange = r.ReadF32()
		}
	} else {
		s.RegistryID = id - 1
	}
	s.Category = r.ReadVarInt()
	s.X, s.Y, s.Z = r.ReadI32(), r.ReadI32(), r.ReadI32()
	s.Volume, s.Pitch = r.ReadF32(), r.ReadF32()
	s.HasSeed = r.Remaining() == 8
	if s.HasSeed {
		s.Seed = r.ReadI64()
	}
	if err := r.Err(); err != nil || r.Remaining() != 0 {
		t.Fatalf("sound effect packet: %v, %d bytes left", err, r.Remaining())
	}
	return s
}

// DriveTargetTicks advances the stopped loop by n ticks: the physics pass that
// runs a hold's scheduled visit, and the entity pass that flies an arrow.
func DriveTargetTicks(srv *server.Server, n int) {
	for range n {
		srv.TickPhysics(1)
		srv.TickEntities(1)
	}
}

// TickingStateWire builds the Set Ticking State packet's payload.
func TickingStateWire(rate float32, frozen bool) []byte {
	var b protocol.Buffer
	b.PutF32(rate)
	b.PutBool(frozen)
	return b.Bytes()
}

// ExpectTickingState reads the next Set Ticking State packet and checks it
// names the rate and the frozen state.
func (c *Client) ExpectTickingState(rate float32, frozen bool) {
	c.T.Helper()
	data := c.ExpectPlay(protocol.IDPlayTickingState)
	if !bytes.Equal(data, TickingStateWire(rate, frozen)) {
		r := protocol.NewReader(data)
		gotRate, gotFrozen := r.ReadF32(), r.ReadBool()
		c.T.Errorf("ticking state = rate %.1f frozen %v, want rate %.1f frozen %v",
			gotRate, gotFrozen, rate, frozen)
	}
}

// ExpectTickingStep reads the next Step Tick packet and checks the steps it
// carries.
func (c *Client) ExpectTickingStep(steps int32) {
	c.T.Helper()
	data := c.ExpectPlay(protocol.IDPlayTickingStep)
	if got := protocol.NewReader(data).ReadVarInt(); got != steps {
		c.T.Errorf("tick steps = %d, want %d", got, steps)
	}
}

// ExpectFailure reads the single red System Chat line a command's refusal
// sends, without the context line a parse error carries behind it.
func (c *Client) ExpectFailure(key string) {
	c.T.Helper()
	data := c.ExpectPlay(protocol.IDPlaySystemChat)
	if got, want := data, CommandErrorMessageForTest(key); !bytes.Equal(got, want) {
		c.T.Errorf("failure = % X, want % X", got, want)
	}
}

// ExpectFeedbackLine reads the next command feedback line and checks its
// translation key alone, for the lines whose numbers depend on how the
// machine ran.
func (c *Client) ExpectFeedbackLine(key string) {
	c.T.Helper()
	data := c.ExpectPlay(protocol.IDPlaySystemChat)
	got, ok := protocol.ReadText(protocol.NewReader(data))
	if !ok || got.Translate != key {
		c.T.Errorf("feedback key = %q, want %q", got.Translate, key)
	}
}

// AwaitSprintReport reads the [Server: Sprint completed ...] line a
// finished sprint hands the operators, checking its shape but not its
// numbers: how many ticks a second the sprint managed is the machine's
// own. What it reads past is kept for whoever reads next.
func (c *Client) AwaitSprintReport() {
	c.T.Helper()
	var skipped []PushedPacket
	defer func() { c.Unread(skipped...) }()
	for id, data := range c.Packets(30 * time.Second) {
		if id != protocol.IDPlaySystemChat {
			skipped = append(skipped, PushedPacket{Id: id, Data: data})
			continue
		}
		echo, ok := protocol.ReadText(protocol.NewReader(data))
		if !ok || echo.Translate != "chat.type.admin" || len(echo.With) != 2 ||
			echo.With[0].Text != "Server" ||
			echo.With[1].Translate != "commands.tick.sprint.report" ||
			len(echo.With[1].With) != 2 ||
			echo.With[1].With[0].Number == nil || echo.With[1].With[1].Text == "" {
			skipped = append(skipped, PushedPacket{Id: id, Data: data})
			continue
		}
		return
	}
	c.T.Fatal("the sprint's report never arrived")
}

// The registry IDs of the two clocks the minecraft:world_clock registry
// numbers, what an Update Time entry names its clock by.
var (
	OverworldClockID = registry.MustID("minecraft:world_clock", "minecraft:overworld")
	EndClockID       = registry.MustID("minecraft:world_clock", "minecraft:the_end")
)

// ExpectTimeFeedback reads packets until a System Chat message arrives, skipping
// the Update Time broadcast that /time commands also send, and checks that it
// carries the given translation key and arguments. If an Update Time packet
// arrives first it is captured for ExpectUpdateTime to use, since the broadcast
// (async) and the feedback (sync) can arrive in either order.
func (c *Client) ExpectTimeFeedback(key string, args ...protocol.Text) {
	c.T.Helper()
	for gotID, data := range c.Packets(ExpectTimeout) {
		c.T.Helper()
		switch gotID {
		case protocol.IDPlayUpdateTime:
			c.lastUpdateTime = slices.Clone(data)
			continue
		case protocol.IDPlaySystemChat:
			var want protocol.Buffer
			want.PutText(protocol.Text{Translate: key, With: args})
			want.PutBool(false)
			if !bytes.Equal(data, want.Bytes()) {
				c.T.Errorf("time feedback = % X, want % X", data, want.Bytes())
			}
			return
		}
	}
	c.T.Fatalf("read time feedback: nothing came within %s", ExpectTimeout)
}

// ReadUpdateTime returns the data of an Update Time packet, using one captured
// by ExpectTimeFeedback if available, or reading from the connection otherwise.
func (c *Client) ReadUpdateTime() []byte {
	c.T.Helper()
	if c.lastUpdateTime != nil {
		data := c.lastUpdateTime
		c.lastUpdateTime = nil
		return data
	}
	for gotID, data := range c.Packets(ExpectTimeout) {
		switch gotID {
		case protocol.IDPlaySystemChat:
			continue
		case protocol.IDPlayUpdateTime:
			return data
		}
	}
	c.T.Fatalf("read update time: nothing came within %s", ExpectTimeout)
	return nil
}

// ExpectUpdateTime reads packets until an Update Time arrives, skipping any
// System Chat messages, and checks that it carries the overworld's clock
// alone, at a day time of at least wantDayTime: the tick loop may have
// advanced it by a tick or two between the command setting it and the packet
// being built.
func (c *Client) ExpectUpdateTime(wantDayTime int64) {
	c.T.Helper()
	data := c.ReadUpdateTime()
	r := protocol.NewReader(data)
	r.ReadI64() // world age
	if got := r.ReadVarInt(); got != 1 {
		c.T.Fatalf("clocks = %d, want 1", got)
	}
	if got := r.ReadVarInt(); got != OverworldClockID {
		c.T.Errorf("clock ID = %d, want %d (minecraft:overworld)", got, OverworldClockID)
	}
	if got := r.ReadVarLong(); got < wantDayTime {
		c.T.Errorf("day time = %d, want at least %d", got, wantDayTime)
	}
}

// ExpectUpdateTimeWithRate reads packets until an Update Time arrives and
// checks that it carries the overworld's clock alone, advancing at
// wantRate. The day time is not checked, since the tick loop may have
// advanced it before the command takes effect.
func (c *Client) ExpectUpdateTimeWithRate(wantDayTime int64, wantRate float32) {
	c.T.Helper()
	_ = wantDayTime // documented in the signature; not checked (see comment above)
	data := c.ReadUpdateTime()
	r := protocol.NewReader(data)
	r.ReadI64() // world age
	if got := r.ReadVarInt(); got != 1 {
		c.T.Fatalf("clocks = %d, want 1", got)
	}
	if got := r.ReadVarInt(); got != OverworldClockID {
		c.T.Errorf("clock ID = %d, want %d (minecraft:overworld)", got, OverworldClockID)
	}
	r.ReadVarLong() // day time (not checked)
	r.ReadF32()     // fractional time
	if got := r.ReadF32(); got != wantRate {
		c.T.Errorf("rate = %g, want %g", got, wantRate)
	}
}

// UpdateClock is one clock's Update Time entry as the tests assert it: the
// clock's registry ID, its total ticks and its rate.
type UpdateClock struct {
	Id    int32
	Total int64
	Rate  float32
}

// ExpectUpdateClocks reads packets until an Update Time arrives, skipping
// any System Chat messages, and checks the whole clock list it carries: one
// entry per clock given, in the order given, each with its registry ID,
// exact total, a fractional part of zero and its rate. The tests that use it
// hold every clock still or stop the tick loop, so the totals are exact;
// the whole-number rates they set never leave a fraction behind.
func (c *Client) ExpectUpdateClocks(want ...UpdateClock) {
	c.T.Helper()
	data := c.ReadUpdateTime()
	r := protocol.NewReader(data)
	r.ReadI64() // world age
	if got := r.ReadVarInt(); got != int32(len(want)) {
		c.T.Fatalf("clocks = %d, want %d", got, len(want))
	}
	for i, w := range want {
		if got := r.ReadVarInt(); got != w.Id {
			c.T.Errorf("clock %d ID = %d, want %d", i, got, w.Id)
		}
		if got := r.ReadVarLong(); got != w.Total {
			c.T.Errorf("clock %d total = %d, want %d", i, got, w.Total)
		}
		if got := r.ReadF32(); got != 0 {
			c.T.Errorf("clock %d fractional time = %g, want 0", i, got)
		}
		if got := r.ReadF32(); got != w.Rate {
			c.T.Errorf("clock %d rate = %g, want %g", i, got, w.Rate)
		}
	}
	if err := r.Err(); err != nil {
		c.T.Fatalf("update time: %v", err)
	}
}

// DataWorld joins one operator, the level /data runs at.
func DataWorld(t *testing.T, name string) *Client {
	t.Helper()
	cfg := config.Default()
	cfg.OnlineMode = false
	_, c := Join(t, cfg, name, OpsListing(t, name))
	return c
}

// ExpectBlockEntityData reads the packet the server broadcasts a block
// entity's data with, stepping over the noise a command's run and a ticking
// world fill the stream with, and fails when none arrives within ten seconds.
func ExpectBlockEntityData(t *testing.T, c *Client) []byte {
	t.Helper()
	for id, data := range c.Packets(10 * time.Second) {
		switch id {
		case protocol.IDPlayBlockEntityData:
			return data
		case protocol.IDPlayEntityPosition, protocol.IDPlayEntityPositionRot,
			protocol.IDPlayEntityRotation, protocol.IDPlayHeadRotation,
			protocol.IDPlayTeleportEntity, protocol.IDPlayWorldEvent,
			protocol.IDPlaySoundEffect, protocol.IDPlayUpdateLight,
			protocol.IDPlayUpdateTime, protocol.IDPlayEntityMetadata,
			protocol.IDPlaySystemChat, protocol.IDPlayAckBlockChange,
			protocol.IDPlaySetHealth, protocol.IDPlayKeepAlive,
			protocol.IDPlayBlockUpdate, protocol.IDPlaySectionBlocks:
		default:
			t.Fatalf("unexpected packet 0x%02X while waiting for block entity data", id)
		}
	}
	t.Fatal("block entity data did not arrive within 10s")
	return nil
}

// ExpectNoSystemChat fails the test when a system chat arrives before the
// ping the helper sends answers: a fork whose every context dropped must
// leave the runner nothing to read, and this is how the test hears that.
func ExpectNoSystemChat(t *testing.T, c *Client) {
	t.Helper()
	c.Send(protocol.IDPlayPingRequest, func(b *protocol.Buffer) { b.PutI64(1) })
	for id, data := range c.Packets(ExpectTimeout) {
		t.Helper()
		switch id {
		case protocol.IDPlayPingResponse:
			return
		case protocol.IDPlaySystemChat:
			t.Fatalf("a system chat arrived where silence was wanted: % X", data)
		}
	}
	t.Fatalf("read packet: nothing came within %s", ExpectTimeout)
}

// ConditionalPassCount is the System Chat a passing count-form execute
// condition answers with.
func ConditionalPassCount(count int64) []byte {
	var b protocol.Buffer
	b.PutText(protocol.Text{
		Translate: "commands.execute.conditional.pass_count",
		With:      []protocol.Text{{Number: &count}},
	})
	b.PutBool(false)
	return b.Bytes()
}

// ItemFailureText is a bare red failure message as its System Chat packet
// carries it.
func ItemFailureText(key string, with ...protocol.Text) []byte {
	var b protocol.Buffer
	inner := protocol.Text{Translate: key, With: with}
	b.PutText(protocol.Text{Color: protocol.ColorRed, Extra: []protocol.Text{inner}})
	b.PutBool(false)
	return b.Bytes()
}

// AwaitItemFailure reads until the bare red failure for key arrives.
func AwaitItemFailure(t *testing.T, c *Client, key string, with ...protocol.Text) {
	t.Helper()
	want := ItemFailureText(key, with...)
	for packetID, data := range c.Packets(10 * time.Second) {
		if packetID == protocol.IDPlaySystemChat && bytes.Equal(data, want) {
			return
		}
	}
	t.Fatalf("no bare red failure for %s arrived", key)
}

// PlainFeedbackTextsForTest builds the expected first System Chat payload for
// a command's own success feedback, whose arguments are the plain texts the
// vanilla server formats the set and filled coordinates as.
func PlainFeedbackTextsForTest(key string, with ...string) []byte {
	var b protocol.Buffer
	b.PutText(protocol.Text{Translate: key, With: FeedbackArgsForTest(key, with)})
	b.PutBool(false)
	return b.Bytes()
}
