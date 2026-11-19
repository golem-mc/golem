package commands

import (
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
