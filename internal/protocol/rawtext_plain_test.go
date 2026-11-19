package protocol_test

import (
	"bytes"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
)

func TestRawTranslatedTextPlainRoundTrip(t *testing.T) {
	t.Parallel()
	bold := protocol.Text{Raw: nbt.Compound{"text": nbt.String("suffix"), "bold": nbt.Byte(1)}}
	for _, tc := range []struct {
		name string
		text protocol.Text
		want string
	}{
		{"styled argument", protocol.Text{Translate: "chat.square_brackets", With: []protocol.Text{bold}}, "[suffix]"},
		{"nested translation and siblings", protocol.Text{
			Translate: "chat.square_brackets",
			With:      []protocol.Text{{Translate: "chat.square_brackets", With: []protocol.Text{bold}, Extra: []protocol.Text{{Text: "!"}}}},
			Extra:     []protocol.Text{{Text: " end"}},
		}, "[[suffix]!] end"},
		{"mixed numeric arguments", protocol.Text{
			Translate: "commands.setblock.success",
			With:      []protocol.Text{{Number: new(int64(7))}, {Float: new(float32(2.5))}, bold},
		}, "Changed the block at 7, 2.5, suffix"},
		{"unknown key", protocol.Text{Translate: "test.unknown.translation", With: []protocol.Text{bold}}, "test.unknown.translation"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var encoded protocol.Buffer
			encoded.PutText(tc.text)
			text, ok := protocol.ReadText(protocol.NewReader(encoded.Bytes()))
			if !ok || text.Raw == nil {
				t.Fatalf("ReadText = %+v, %v; want raw styled component", text, ok)
			}
			if got := text.Plain(); got != tc.want {
				t.Errorf("Plain = %q, want %q", got, tc.want)
			}
			var before, after protocol.Buffer
			before.PutText(text)
			text.Plain()
			after.PutText(text)
			if !bytes.Equal(before.Bytes(), after.Bytes()) {
				t.Fatal("plain rendering changed the wire component")
			}
			original, _, err := nbt.DecodeNetwork(encoded.Bytes())
			if err != nil || !nbt.Equal(original, text.Raw) {
				t.Fatalf("ReadText lost the original styling: %v", err)
			}
		})
	}
}

func TestPlainTextOfTranslationFallback(t *testing.T) {
	t.Parallel()
	for key, want := range map[string]string{
		"test.unknown.translation": "<suffix>",
		"chat.square_brackets":     "[suffix]",
	} {
		tag := nbt.Compound{
			"translate": nbt.String(key), "fallback": nbt.String("<%s>"),
			"with": nbt.NewList([]nbt.Tag{nbt.Compound{"text": nbt.String("suffix"), "bold": nbt.Byte(1)}}),
		}
		if got := protocol.PlainTextOf(tag); got != want {
			t.Errorf("PlainTextOf(%q) = %q, want %q", key, got, want)
		}
	}
}
