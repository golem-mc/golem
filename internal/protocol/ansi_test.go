package protocol

import (
	"strings"
	"testing"
)

func TestANSI(t *testing.T) {
	tests := []struct {
		name string
		text Text
		want string
	}{
		{"no style draws no codes", Text{Text: "plain"}, "plain"},
		{
			"a translation's arguments take its colour",
			Text{Translate: "multiplayer.player.joined", With: []Text{{Text: "Steve"}}, Color: ColorYellow},
			"\x1b[0;93mSteve joined the game\x1b[0m",
		},
		{
			"a sibling inherits and adds, the next goes back",
			Text{Text: "plain ", Extra: []Text{
				{Text: "red", Color: ColorRed, Extra: []Text{{Text: " italic", Italic: true}}},
				{Text: " back"},
			}},
			"plain \x1b[0;91mred\x1b[0;91;3m italic\x1b[0m back",
		},
		{"underline", Text{Text: "here", Underline: true}, "\x1b[0;4mhere\x1b[0m"},
		{"a dark colour", Text{Text: "gold", Color: "gold"}, "\x1b[0;33mgold\x1b[0m"},
		{"a hex colour", Text{Text: "orange", Color: "#FF8000"}, "\x1b[0;38;2;255;128;0morange\x1b[0m"},
		{"a refused template keeps its colour", Text{Translate: "100%q", Color: ColorRed}, "\x1b[0;91m100%q\x1b[0m"},
		{"an escaped percent keeps its colour", Text{Translate: "%%", Color: ColorRed}, "\x1b[0;91m%\x1b[0m"},
		{"an indexed percent refuses the template", Text{Translate: "%1$%", Color: ColorRed}, "\x1b[0;91m%1$%\x1b[0m"},
		{
			"an indexed percent discards earlier substitutions and styles",
			Text{Translate: "%s %% %1$% %s", With: []Text{{Text: "first", Color: "blue"}, {Text: "second"}}, Color: ColorRed,
				Extra: []Text{{Text: " sibling"}}},
			"\x1b[0;91m%s %% %1$% %s sibling\x1b[0m",
		},
		{
			"the text's own escapes are escaped",
			Text{Text: "\x1b]0;pwned\x07", Color: ColorRed, Extra: []Text{{Text: "\x1b[2K\rFAKE"}}},
			"\x1b[0;91m\\x1b]0;pwned\\a\\x1b[2K\\rFAKE\x1b[0m",
		},
		{"a byte that is not UTF-8 is escaped", Text{Text: "\x9b31mC1", Color: ColorRed}, "\x1b[0;91m\\x9b31mC1\x1b[0m"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := test.text.ANSI(); got != test.want {
				t.Errorf("ANSI() = %q, want %q", got, test.want)
			}
			if plain := test.text.Plain(); strings.Contains(plain, "\x1b[0") {
				t.Errorf("Plain() = %q carries escape codes", plain)
			}
		})
	}
}
