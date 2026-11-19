package protocol

import "testing"

// num is a whole number as an argument carries it.
func num(n int64) Text { return Text{Number: &n} }

func TestPlain(t *testing.T) {
	tests := []struct {
		name  string
		text  Text
		check func(t *testing.T, got string)
	}{
		{
			name: "a literal",
			text: Text{Text: "hi there"},
			check: func(t *testing.T, got string) {
				if got != "hi there" {
					t.Errorf("rendered %q, want hi there", got)
				}
			},
		},
		{
			name: "siblings in order",
			text: Text{Extra: []Text{
				{Text: "a"}, {Text: ", "}, {Text: "b"},
			}},
			check: func(t *testing.T, got string) {
				if got != "a, b" {
					t.Errorf("rendered %q, want a, b", got)
				}
			},
		},
		{
			name: "a joined player",
			text: Text{
				Translate: "multiplayer.player.joined",
				With:      []Text{{Text: "Steve"}},
			},
			check: func(t *testing.T, got string) {
				if got != "Steve joined the game" {
					t.Errorf("rendered %q, want Steve joined the game", got)
				}
			},
		},
		{
			name: "a count",
			text: Text{
				Translate: "commands.setblock.success",
				With:      []Text{num(1), num(65), num(1)},
			},
			check: func(t *testing.T, got string) {
				if got != "Changed the block at 1, 65, 1" {
					t.Errorf("rendered %q, want Changed the block at 1, 65, 1", got)
				}
			},
		},
		{
			name: "a wide count",
			text: Text{
				Translate: "commands.setblock.success",
				With:      []Text{num(1), {Long: new(int64(1) << 40)}, num(1)},
			},
			check: func(t *testing.T, got string) {
				if got != "Changed the block at 1, 1099511627776, 1" {
					t.Errorf("rendered %q", got)
				}
			},
		},
		{
			name: "a nested component as an argument",
			text: Text{
				Translate: "multiplayer.player.joined",
				With: []Text{{
					Text:  "Steve",
					Extra: []Text{{Text: " of Minecraft"}},
				}},
			},
			check: func(t *testing.T, got string) {
				if got != "Steve of Minecraft joined the game" {
					t.Errorf("rendered %q", got)
				}
			},
		},
		{
			name: "an unknown key is the key itself",
			text: Text{
				Translate: "console.no.such.key",
				With:      []Text{{Text: "ignored"}},
			},
			check: func(t *testing.T, got string) {
				if got != "console.no.such.key" {
					t.Errorf("rendered %q, want the raw key", got)
				}
			},
		},
		{
			name: "a format the reader refuses shows the raw template",
			text: Text{Translate: "console.test.percent.d"},
			check: func(t *testing.T, got string) {
				if got != "%d apples" {
					t.Errorf("rendered %q, want the raw template", got)
				}
			},
		},
		{
			name: "a stray percent shows the raw template",
			text: Text{Translate: "console.test.stray"},
			check: func(t *testing.T, got string) {
				if got != "50%" {
					t.Errorf("rendered %q, want the raw template", got)
				}
			},
		},
		{
			name: "a placeholder past the arguments shows the raw template",
			text: Text{Translate: "console.test.too.few", With: []Text{{Text: "one"}}},
			check: func(t *testing.T, got string) {
				if got != "%s to %s" {
					t.Errorf("rendered %q, want the raw template", got)
				}
			},
		},
		{
			name: "a percent escapes itself",
			text: Text{Translate: "console.test.escaped", With: []Text{{Text: "half"}}},
			check: func(t *testing.T, got string) {
				if got != "half% of it" {
					t.Errorf("rendered %q, want half%% of it", got)
				}
			},
		},
		{
			name: "a numbered placeholder picks its argument",
			text: Text{Translate: "console.test.swapped", With: []Text{{Text: "b"}, {Text: "a"}}},
			check: func(t *testing.T, got string) {
				if got != "a then b" {
					t.Errorf("rendered %q, want a then b", got)
				}
			},
		},
		{
			name: "a skipped argument is still wanted by the later ones",
			text: Text{Translate: "console.test.swapped", With: []Text{{Text: "b"}}},
			check: func(t *testing.T, got string) {
				// Vanilla catches the format error and shows the
				// template it could not decompose as it stands.
				if got != "%2$s then %1$s" {
					t.Errorf("rendered %q, want the raw template", got)
				}
			},
		},
		{
			name: "styles render as nothing",
			text: Text{
				Text:      "shouted",
				Color:     ColorRed,
				Underline: true,
				Extra:     []Text{{Translate: "command.context.here", Italic: true}},
			},
			check: func(t *testing.T, got string) {
				if got != "shouted<--[HERE]" {
					t.Errorf("rendered %q, want shouted<--[HERE]", got)
				}
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.check(t, test.text.Plain())
		})
	}
}

func TestPlainTemplateArgs(t *testing.T) {
	args := []Text{{Text: "first"}, {Text: "second"}}
	for _, test := range []struct {
		template, want string
	}{
		{"hello %s", "hello first"},
		{"%1$s", "first"},
		{"%%", "%"},
		{"trailing %", "trailing %"},
		{"%d", "%d"},
		{"%5", "%5"},
		{"%1$", "%1$"},
		{"no placeholder", "no placeholder"},
		{"%2$s %s", "second first"},
		{"%2$s %2$s %s %s", "second second first second"},
		{"%s %1$s %s", "first first second"},
		{"%2$s %% %s", "second % first"},
		{"%s %3$s", "%s %3$s"},
		{"%s %0$s", "%s %0$s"},
		{"%s %q", "%s %q"},
		{"%5 then %s", "%5 then %s"},
		{"%s %5", "%s %5"},
		{"%1$%", "%1$%"},
		{"%0$%", "%0$%"},
		{"%01$%", "%01$%"},
		{"%s %% %1$% %s", "%s %% %1$% %s"},
		{"%1$% %% %s", "%1$% %% %s"},
		{"%01$s", "first"},
		{"%é %s", "%é %s"},
		{"%１２$s", "%１２$s"},
	} {
		t.Run(test.template, func(t *testing.T) {
			if got := (Text{Translate: test.template, With: args}).Plain(); got != test.want {
				t.Errorf("Plain() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestPlainFloatsJavaStyle(t *testing.T) {
	tests := []struct {
		f    float32
		want string
	}{
		{f: 30, want: "30.0"},
		{f: 1.5, want: "1.5"},
		{f: -179.5, want: "-179.5"},
		{f: 0.25, want: "0.25"},
	}
	for _, test := range tests {
		if got := javaFloat(test.f); got != test.want {
			t.Errorf("javaFloat(%v) = %q, want %q", test.f, got, test.want)
		}
	}
	angle := float32(30)
	if got := (Text{Float: &angle}).Plain(); got != "30.0" {
		t.Errorf("float component rendered %q, want 30.0", got)
	}
}

func init() {
	// The templates the render tests try the reader's edges with, which
	// the vanilla language does not carry.
	vanillaLanguage["console.test.percent.d"] = "%d apples"
	vanillaLanguage["console.test.stray"] = "50%"
	vanillaLanguage["console.test.too.few"] = "%s to %s"
	vanillaLanguage["console.test.escaped"] = "%s%% of it"
	vanillaLanguage["console.test.swapped"] = "%2$s then %1$s"
}
