package commands

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// vanilla: DataCommands.resolveSourcePath, NbtPathArgument.NbtPath.insert,
// FunctionCommand.getArgumentTag
func TestStoragePathEditsDetachSources(t *testing.T) {
	srv, _ := startFunctionServer(t, map[string]string{
		"t/function/edit.mcfunction": "$data modify storage t:s args.v set value $(v)0\n$return $(v)\n",
	})
	servertest.HoldTicks(t, srv)
	for _, line := range []string{
		`data merge storage t:s {rows:[{v:1},{v:2}],args:{v:7},text:"abcd"}`,
		`data modify storage t:s rows append from storage t:s rows[]`,
		`data modify storage t:s rows[0].v set value 9`,
		`data modify storage t:s copy set from storage t:s rows`,
		`data modify storage t:s rows prepend from storage t:s rows[]`,
		`data modify storage t:s part set string storage t:s text 1 3`,
		`execute store result storage t:s count int 1 run data remove storage t:s rows[{v:2}]`,
		`execute store result storage t:s result int 1 run function t:edit with storage t:s args`,
	} {
		feedback := srv.RunRconLine(line)
		if len(feedback) == 0 || feedback[0].Color != "" {
			t.Fatalf("%s: %+v", line, feedback)
		}
	}
	for _, tc := range []struct{ path, want string }{
		{"copy", `[{v: 9}, {v: 2}, {v: 1}, {v: 2}]`},
		{"rows", `[{v: 9}, {v: 1}, {v: 9}, {v: 1}]`},
		{"part", `"bc"`},
		{"count", "4"},
		{"args.v", "70"},
		{"result", "7"},
	} {
		feedback := srv.RunRconLine("data get storage t:s " + tc.path)
		if len(feedback) != 1 || feedback[0].Translate != "commands.data.storage.query" || len(feedback[0].With) != 2 {
			t.Fatalf("%s query: %+v", tc.path, feedback)
		}
		if got := feedback[0].With[1].Plain(); got != tc.want {
			t.Errorf("%s = %s, want %s", tc.path, got, tc.want)
		}
	}
}
