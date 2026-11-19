package commands

import (
	"reflect"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestDataGetHighlightsTheTagLikeVanilla checks /data get sends the tag as
// syntax-highlighted pieces.
// vanilla: TextComponentTagVisitor.RichStyling
func TestDataGetHighlightsTheTagLikeVanilla(t *testing.T) {
	t.Parallel()
	c := dataWorld(t, "Alice")
	c.ChatCommand(`data merge storage t:p {a:1b,s:"x",l:[B;1b,2b]}`)
	servertest.FeedbackComponent(t, c)
	c.ChatCommand(`data get storage t:p`)
	line := servertest.FeedbackComponent(t, c)
	if len(line.With) != 2 {
		t.Fatalf("line = %+v, want two arguments", line)
	}
	tag := line.With[1]
	if got, want := tag.Plain(), `{a: 1b, s: "x", l: [B; 1b, 2b]}`; got != want {
		t.Fatalf("plain = %q, want %q", got, want)
	}
	colours := map[string]string{}
	var order []string
	for _, e := range tag.Extra {
		if e.Color != "" {
			colours[e.Text] = e.Color
			order = append(order, e.Text)
		}
	}
	for text, want := range map[string]string{"a": protocol.ColorAqua, "1": protocol.ColorGold, "b": protocol.ColorRed, "x": protocol.ColorGreen} {
		if colours[text] != want {
			t.Errorf("colour of %q = %q, want %q (coloured: %v)", text, colours[text], want, order)
		}
	}
}

// TestDataGetHealthIsGoldWithARedSuffix checks a player's Health query
// sends the pieces a vanilla 26.3 server sends: the digits gold, the f red.
// vanilla: TextComponentTagVisitor.visitFloat
func TestDataGetHealthIsGoldWithARedSuffix(t *testing.T) {
	t.Parallel()
	c := dataWorld(t, "Alice")
	c.ChatCommand("data get entity @s Health")
	line := servertest.FeedbackComponent(t, c)
	if line.Translate != "commands.data.entity.query" || len(line.With) != 2 {
		t.Fatalf("line = %+v, want the entity query", line)
	}
	want := protocol.Text{Extra: []protocol.Text{
		{Text: "20.0", Color: protocol.ColorGold},
		{Text: "f", Color: protocol.ColorRed},
	}}
	if got := line.With[1]; !reflect.DeepEqual(got, want) {
		t.Fatalf("health = %+v, want %+v", got, want)
	}
}

// TestTellrawNbtHighlightsUnlessPlain checks an nbt component sends coloured
// pieces, and the same text uncoloured with plain:1b.
// vanilla: NbtContents.resolve
func TestTellrawNbtHighlightsUnlessPlain(t *testing.T) {
	t.Parallel()
	c := dataWorld(t, "Alice")
	c.ChatCommand(`data merge storage t:p {a:1b}`)
	servertest.FeedbackComponent(t, c)

	c.ChatCommand(`tellraw @s {nbt:"a",storage:"t:p"}`)
	rich := servertest.FeedbackComponent(t, c)
	c.ChatCommand(`tellraw @s {nbt:"a",storage:"t:p",plain:1b}`)
	plain := servertest.FeedbackComponent(t, c)

	if rich.Plain() != "1b" || plain.Plain() != rich.Plain() {
		t.Fatalf("plain texts = %q and %q, want both 1b", rich.Plain(), plain.Plain())
	}
	coloured := func(text protocol.Text) (n int) {
		var walk func(protocol.Text)
		walk = func(e protocol.Text) {
			if e.Color != "" {
				n++
			}
			for _, child := range e.Extra {
				walk(child)
			}
		}
		for _, child := range text.Extra {
			walk(child)
		}
		return n
	}
	if coloured(rich) == 0 {
		t.Errorf("highlighted nbt = %+v, want coloured children", rich)
	}
	if coloured(plain) != 0 {
		t.Errorf("plain nbt = %+v, want no colour on any child", plain)
	}
}
