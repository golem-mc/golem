package commands

import (
	"bytes"
	"encoding/binary"
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
)

// titlePackets runs one command as the operator and returns the payloads of
// the title packets and the System Chat lines that came back, by packet ID.
func titlePackets(c *servertest.Client, command string) map[int32][][]byte {
	c.T.Helper()
	got := map[int32][][]byte{}
	c.ChatCommand(command)
	c.SettleAndCollect(func(id int32, data []byte) {
		switch id {
		case protocol.IDPlayTitleText, protocol.IDPlaySubtitleText, protocol.IDPlayActionBarText,
			protocol.IDPlayTitlesAnimation, protocol.IDPlayClearTitles, protocol.IDPlaySystemChat:
			got[id] = append(got[id], data)
		}
	})
	return got
}

func textPayload(text protocol.Text, overlay *bool) []byte {
	var b protocol.Buffer
	b.PutText(text)
	if overlay != nil {
		b.PutBool(*overlay)
	}
	return b.Bytes()
}

func TestTitleCommandSendsThePackets(t *testing.T) {
	t.Parallel()
	_, c, _ := servertest.JoinAs(t, servertest.SurvivalTestConfig(), "DJ", servertest.OpsListing(t, "DJ"))
	c.Settle()

	hi := textPayload(protocol.Text{Text: "Hi"}, nil)
	for _, tc := range []struct {
		command string
		id      int32
		key     string
	}{
		{`title @s title {"text":"Hi"}`, protocol.IDPlayTitleText, "commands.title.show.title.single"},
		{`title @s subtitle {"text":"Hi"}`, protocol.IDPlaySubtitleText, "commands.title.show.subtitle.single"},
		{`title @s actionbar {"text":"Hi"}`, protocol.IDPlayActionBarText, "commands.title.show.actionbar.single"},
	} {
		got := titlePackets(c, tc.command)
		if len(got[tc.id]) != 1 || string(got[tc.id][0]) != string(hi) {
			t.Errorf("%s: packet %#x = %x, want %x", tc.command, tc.id, got[tc.id], hi)
		}
		if len(got[protocol.IDPlaySystemChat]) != 1 {
			t.Errorf("%s: want one feedback line, got %d", tc.command, len(got[protocol.IDPlaySystemChat]))
		}
	}

	got := titlePackets(c, "title @s times 5 40 15")
	var want protocol.Buffer
	want.PutI32(5)
	want.PutI32(40)
	want.PutI32(15)
	if len(got[protocol.IDPlayTitlesAnimation]) != 1 || string(got[protocol.IDPlayTitlesAnimation][0]) != string(want.Bytes()) {
		t.Errorf("times packet = %x, want %x", got[protocol.IDPlayTitlesAnimation], want.Bytes())
	}

	for command, reset := range map[string]byte{"title @s clear": 0, "title @s reset": 1} {
		got := titlePackets(c, command)
		if len(got[protocol.IDPlayClearTitles]) != 1 || got[protocol.IDPlayClearTitles][0][0] != reset {
			t.Errorf("%s: clear titles = %x, want reset %d", command, got[protocol.IDPlayClearTitles], reset)
		}
	}
}

func TestTellrawSendsSystemChat(t *testing.T) {
	t.Parallel()
	_, c, _ := servertest.JoinAs(t, servertest.SurvivalTestConfig(), "DJ", servertest.OpsListing(t, "DJ"))
	c.Settle()

	overlay := false
	got := titlePackets(c, `tellraw @a {"text":"Hello","color":"red"}`)
	want := textPayload(protocol.Text{Text: "Hello", Color: "red"}, &overlay)
	if len(got[protocol.IDPlaySystemChat]) != 1 || string(got[protocol.IDPlaySystemChat][0]) != string(want) {
		t.Errorf("tellraw chat = %x, want %x", got[protocol.IDPlaySystemChat], want)
	}
}

func TestTitleAndTellrawNeedPermissionLevelTwo(t *testing.T) {
	t.Parallel()
	_, c, _ := servertest.JoinAs(t, servertest.SurvivalTestConfig(), "Pleb")
	c.Settle()

	for _, command := range []string{`title @s title {"text":"Hi"}`, `tellraw @s {"text":"Hi"}`} {
		got := titlePackets(c, command)
		if len(got[protocol.IDPlayTitleText]) != 0 {
			t.Errorf("%s sent a title to a non-operator", command)
		}
		for _, line := range got[protocol.IDPlaySystemChat] {
			if string(line) == string(textPayload(protocol.Text{Text: "Hi"}, new(bool))) {
				t.Errorf("%s delivered text to a non-operator", command)
			}
		}
	}
}

// playerNameText is the display name a player's selector resolves to.
func playerNameText(name string) protocol.Text {
	id := protocol.OfflineUUID(name)
	var words [4]uint32
	for i := range words {
		words[i] = binary.BigEndian.Uint32(id[i*4:])
	}
	return protocol.Text{
		Text:         name,
		Insertion:    name,
		ClickCommand: "/tell " + name + " ",
		HoverEntity:  &protocol.HoverEntity{Name: protocol.Text{Text: name}, ID: "minecraft:player", UUID: words},
	}
}

func rawChat(tag nbt.Tag) []byte {
	overlay := false
	text, ok := protocol.RawTextOf(tag)
	if !ok {
		panic("component refused")
	}
	return textPayload(text, &overlay)
}

func chatLines(c *servertest.Client) [][]byte {
	c.T.Helper()
	var lines [][]byte
	c.SettleAndCollect(func(id int32, data []byte) {
		if id == protocol.IDPlaySystemChat {
			lines = append(lines, data)
		}
	})
	return lines
}

// TestTellrawResolvesSelectorsAgainstTheSender checks that @s in a component
// names the player who ran the command for every recipient.
// vanilla: ComponentArgument.getResolvedComponent, SelectorContents.resolve
func TestTellrawResolvesSelectorsAgainstTheSender(t *testing.T) {
	t.Parallel()
	_, r, g, _ := startExecuteWorldForTwo(t, "DJ", "Guest")
	r.Settle()
	g.Settle()

	r.ChatCommand(`tellraw @a {"text":"Hi ","extra":[{"selector":"@s"}]}`)
	want := rawChat(nbt.Compound{
		"text": nbt.String("Hi "),
		"extra": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{nbt.Compound{
			"text":  nbt.String(""),
			"extra": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{protocol.TextTag(playerNameText("DJ"))}},
		}}},
	})
	// DJ's ping answer follows his command; Guest's ping is on another
	// connection, so his line is read second.
	for _, who := range []struct {
		c    *servertest.Client
		name string
	}{{r, "DJ"}, {g, "Guest"}} {
		c, name := who.c, who.name
		lines := chatLines(c)
		if len(lines) != 1 || !bytes.Equal(lines[0], want) {
			t.Errorf("%s got %x, want %x", name, lines, want)
		}
	}
}

// TestTellrawResolvesScoreComponents checks that a score content shows the
// holder's score, `*` being each recipient.
// vanilla: ScoreContents.resolve
func TestTellrawResolvesScoreComponents(t *testing.T) {
	t.Parallel()
	_, r, g, _ := startExecuteWorldForTwo(t, "DJ", "Guest")
	r.Settle()
	g.Settle()
	for _, cmd := range []string{
		"scoreboard objectives add points dummy",
		"scoreboard players set DJ points 7",
		"scoreboard players set Guest points 3",
	} {
		r.ChatCommand(cmd)
	}
	r.Settle()
	g.Settle()
	chatLines(r)
	chatLines(g)

	shown := func(n string) []byte {
		items := []nbt.Tag{}
		if n != "" {
			items = append(items, nbt.Compound{"text": nbt.String(n)})
		}
		c := nbt.Compound{"text": nbt.String("")}
		if len(items) > 0 {
			c["extra"] = nbt.List{Elem: nbt.TypeCompound, Items: items}
		}
		return rawChat(c)
	}
	for _, tc := range []struct{ name, djWant, guestWant string }{
		{"DJ", "7", "7"},
		{"*", "7", "3"},
		{"Nobody", "", ""},
	} {
		r.ChatCommand(`tellraw @a {"score":{"name":"` + tc.name + `","objective":"points"}}`)
		for _, who := range []struct {
			c    *servertest.Client
			want []byte
		}{{r, shown(tc.djWant)}, {g, shown(tc.guestWant)}} {
			lines := chatLines(who.c)
			if len(lines) != 1 || !bytes.Equal(lines[0], who.want) {
				t.Errorf("name %q got %x, want %x", tc.name, lines, who.want)
			}
		}
	}
	// More than one holder refuses before anything is sent.
	r.ChatCommand(`tellraw @a {"score":{"name":"@a","objective":"points"}}`)
	for _, c := range []*servertest.Client{r, g} {
		lines := chatLines(c)
		if c == g && len(lines) != 0 {
			t.Errorf("guest got %x after a refused tellraw, want nothing", lines)
		}
		if c == r && (len(lines) != 1 || !bytes.Contains(lines[0], []byte("argument.entity.toomany"))) {
			t.Errorf("DJ got %x, want argument.entity.toomany", lines)
		}
	}

	// /title resolves `*` per recipient too.
	bar := func(n string) []byte {
		return textPayload(protocol.Text{Text: "", Extra: []protocol.Text{{Text: n}}}, nil)
	}
	r.ChatCommand(`title @a actionbar {"score":{"name":"*","objective":"points"}}`)
	for _, who := range []struct {
		c    *servertest.Client
		want []byte
	}{{r, bar("7")}, {g, bar("3")}} {
		var got [][]byte
		who.c.SettleAndCollect(func(id int32, data []byte) {
			if id == protocol.IDPlayActionBarText {
				got = append(got, data)
			}
		})
		if len(got) != 1 || !bytes.Equal(got[0], who.want) {
			t.Errorf("action bar = %x, want %x", got, who.want)
		}
	}
}

// TestTellrawKeepsListsStylesAndEvents checks that the components vanilla
// reads but the Text model has no field for are sent as typed.
// vanilla: ComponentArgument.EXAMPLES, Style
func TestTellrawKeepsListsStylesAndEvents(t *testing.T) {
	t.Parallel()
	_, c, _ := servertest.JoinAs(t, servertest.SurvivalTestConfig(), "DJ", servertest.OpsListing(t, "DJ"))
	c.Settle()

	c.ChatCommand(`tellraw @s ["list ",{"text":"bold","bold":true,"strikethrough":true,"obfuscated":true,"font":"minecraft:uniform","click_event":{"action":"run_command","command":"/say hi"},"hover_event":{"action":"show_text","value":"tip"}}]`)
	want := rawChat(nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
		nbt.Compound{"": nbt.String("list ")},
		nbt.Compound{
			"text":          nbt.String("bold"),
			"bold":          nbt.Byte(1),
			"strikethrough": nbt.Byte(1),
			"obfuscated":    nbt.Byte(1),
			"font":          nbt.String("minecraft:uniform"),
			"click_event":   nbt.Compound{"action": nbt.String("run_command"), "command": nbt.String("/say hi")},
			"hover_event":   nbt.Compound{"action": nbt.String("show_text"), "value": nbt.String("tip")},
		},
	}})
	lines := chatLines(c)
	if len(lines) != 1 || !bytes.Equal(lines[0], want) {
		t.Errorf("got %x, want %x", lines, want)
	}
}

// TestTellrawRefusesBadSyntaxAndBothNbtFlags checks the SNBT parser's own
// error for unreadable text and the refusal of interpret with plain.
// vanilla: NbtContents.CODEC, SnbtGrammar
func TestTellrawRefusesBadSyntaxAndBothNbtFlags(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Start(t, config.Default())

	reply := srv.RunLine(`tellraw @a {text:`)
	if len(reply) == 0 || len(reply[0].Extra) != 1 || reply[0].Extra[0].Translate != "snbt.parser.expected_unquoted_string" {
		t.Errorf("reply = %v, want the SNBT parser's expected_unquoted_string", reply)
	}
	reply = srv.RunLine(`tellraw @a {nbt:"a",storage:"minecraft:x",interpret:1b,plain:1b}`)
	if len(reply) == 0 || len(reply[0].Extra) != 1 || reply[0].Extra[0].Translate != "argument.component.invalid" {
		t.Errorf("reply = %v, want argument.component.invalid", reply)
	}
}

// TestTitleFeedbackCountsTheTargets checks the multiple form of the feedback.
func TestTitleFeedbackCountsTheTargets(t *testing.T) {
	t.Parallel()
	_, r, g, _ := startExecuteWorldForTwo(t, "DJ", "Guest")
	r.Settle()
	g.Settle()

	r.ChatCommand(`title @a title ["x"]`)
	var feedback [][]byte
	titles := 0
	r.SettleAndCollect(func(id int32, data []byte) {
		switch id {
		case protocol.IDPlaySystemChat:
			feedback = append(feedback, data)
		case protocol.IDPlayTitleText:
			titles++
		}
	})
	if titles != 1 || len(feedback) != 1 || !bytes.Contains(feedback[0], []byte("commands.title.show.title.multiple")) {
		t.Errorf("titles = %d, feedback = %q", titles, feedback)
	}
}
