package protocol

import (
	"testing"

	"github.com/golem-mc/golem/nbt"
)

// TestTextOfReadsTheServerClicks checks that a run_command, open_url and
// change_page click is read, kept through the tag and wire forms, and that a
// click the codec refuses still fails the component.
// vanilla: ClickEvent.CODEC
func TestTextOfReadsTheServerClicks(t *testing.T) {
	t.Parallel()
	for _, text := range []Text{
		{Text: "Bob", ClickRun: "/say hi"},
		{Text: "Bob", ClickURL: "https://example.com/a?b=c"},
		{Text: "Bob", ClickPage: 3},
	} {
		got, ok := TextOf(TextTag(text))
		if !ok || !TextEqual(got, text) {
			t.Errorf("TextOf(TextTag(%+v)) = %+v, %v", text, got, ok)
		}
		var b Buffer
		b.PutText(text)
		tag, _, err := nbt.DecodeNetwork(b.Bytes())
		if err != nil {
			t.Fatalf("decode %+v: %v", text, err)
		}
		if got, ok := TextOf(tag); !ok || !TextEqual(got, text) {
			t.Errorf("wire form of %+v reads back as %+v, %v", text, got, ok)
		}
	}
	for name, click := range map[string]nbt.Compound{
		"run_command with a section sign": {"action": nbt.String("run_command"), "command": nbt.String("/say §a")},
		"run_command without a command":   {"action": nbt.String("run_command")},
		"open_url of a file":              {"action": nbt.String("open_url"), "url": nbt.String("file:///etc/passwd")},
		"open_url with no scheme":         {"action": nbt.String("open_url"), "url": nbt.String("example.com")},
		"open_url with a space":           {"action": nbt.String("open_url"), "url": nbt.String("https://a b")},
		"change_page to page 0":           {"action": nbt.String("change_page"), "page": nbt.Int(0)},
		"open_file":                       {"action": nbt.String("open_file"), "path": nbt.String("x")},
	} {
		if _, ok := TextOf(nbt.Compound{"text": nbt.String("Bob"), "click_event": click}); ok {
			t.Errorf("%s: read, want refused", name)
		}
	}
}

// TestWithoutClickDropsEveryClick checks that a name loses its click and its
// siblings' clicks, and keeps the rest.
// vanilla: Entity.removeAction
func TestWithoutClickDropsEveryClick(t *testing.T) {
	t.Parallel()
	name := Text{Text: "Bob", Color: "red", ClickRun: "/x", Extra: []Text{{Text: "!", ClickURL: "https://a"}}}
	want := Text{Text: "Bob", Color: "red", Extra: []Text{{Text: "!"}}}
	if got := name.WithoutClick(); !TextEqual(got, want) {
		t.Errorf("WithoutClick = %+v, want %+v", got, want)
	}
	if name.Extra[0].ClickURL == "" {
		t.Error("WithoutClick changed the sibling it was given")
	}
}
