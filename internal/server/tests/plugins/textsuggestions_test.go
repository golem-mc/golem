package plugins_test

import (
	"bytes"
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/plugin"
)

// Expectations from ComponentArgument's SnbtGrammar.parseForSuggestions.
func TestPluginTextCommandSuggestions(t *testing.T) {
	t.Parallel()
	srv := servertest.NewTestServerWithPlugins(t, servertest.BlockTestConfig(), []plugin.Registration{{Name: "text", Plugin: &testPlugin{enable: func(s plugin.Server) error {
		return s.Commands().Register(plugin.Command{Name: "text", Aliases: []string{"txt"}, Children: []plugin.Node{{Arg: plugin.TextArg("value")}}})
	}}}})
	c := servertest.DialServer(t, srv)
	c.ViewAsk = config.MinDistance
	c.Login("Tester")
	c.Configure()
	var argument protocol.Buffer
	argument.PutU8(2 | 16) // argument with server suggestions
	argument.PutVarInt(0)
	argument.PutString("value")
	argument.PutVarInt(command.ParserComponent)
	argument.PutString("minecraft:ask_server")
	found := false
	c.ReadJoin(func(id int32, data []byte) {
		if id == protocol.IDPlayCommands {
			found = bytes.Contains(data, argument.Bytes())
		}
	})
	c.MarkLoaded()
	if !found {
		t.Fatal("text argument does not request server suggestions in the client tree")
	}
	for _, tc := range []struct {
		partial string
		offset  int
		matches []string
	}{
		{"", 0, nil},
		{"{", 1, []string{`"`, "'", "}"}},
		{"{text", 5, []string{":"}},
		{"{text:", 0, nil},
		{"[", 1, []string{"]", "B", "I", "L"}},
		{"[I;1", 4, []string{",", "]", "b", "B", "i", "I", "l", "L", "s", "S", "u", "U"}},
		{"1", 1, []string{".", "b", "B", "d", "D", "e", "E", "f", "F", "i", "I", "l", "L", "s", "S", "u", "U"}},
		{`"hello\`, 7, []string{`"`, "'", `\`, "b", "f", "n", "N", "r", "s", "t", "u", "U", "x"}},
		{"bool(", 5, []string{")"}},
		{`"\U00110000`, 0, nil},
	} {
		for _, name := range []string{"text", "txt"} {
			prefix := "/" + name + " "
			input := prefix + tc.partial
			c.CommandSuggestion(33, input)
			id, start, length, matches := registrationSuggestions(t, srv, c)
			wantStart, wantLength := int32(0), int32(0)
			if len(tc.matches) > 0 {
				wantStart = int32(len(prefix) + tc.offset)
				wantLength = int32(len(input)) - wantStart
			}
			slices.Sort(matches)
			wantMatches := slices.Clone(tc.matches)
			slices.Sort(wantMatches)
			if id != 33 || start != wantStart || length != wantLength || !slices.Equal(matches, wantMatches) {
				t.Errorf("suggest %q = %d %d %d %v; want start=%d length=%d %v", input, id, start, length, matches, wantStart, wantLength, tc.matches)
			}
		}
	}
}
