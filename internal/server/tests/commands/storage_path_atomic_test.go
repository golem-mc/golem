package commands

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
)

// vanilla: DataCommands.manipulateData
func TestStoragePathRefusalsDiscardEdits(t *testing.T) {
	root := t.TempDir()
	srv, _ := startFunctionServerWith(t, t.TempDir(), nil, func(cfg *config.Config) {
		cfg.LevelName = root
	})
	servertest.HoldTicks(t, srv)
	wantSaved := map[string]nbt.Compound{}
	for _, tc := range []struct {
		name, initial, edit, refusal string
	}{
		{"partial_insert", `{rows:[[1,2],[1]]}`, `rows[] insert 2 value 9`, "commands.data.modify.invalid_index"},
		{"created_list", `{rows:[{v:[1,2]},{}]}`, `rows[].v insert 2 value 9`, "commands.data.modify.invalid_index"},
		{"partial_append", `{rows:[{v:[1]},{v:2}]}`, `rows[].v append value 9`, "commands.data.modify.expected_list"},
		{"partial_merge", `{rows:[{v:{a:1}},{v:2}]}`, `rows[].v merge value {b:2}`, "commands.data.modify.expected_object"},
		{"created_compound", `{rows:[{},{v:2}]}`, `rows[].v merge value {b:2}`, "commands.data.modify.expected_object"},
		{"created_root_key", `{guard:1}`, `missing.rows insert 2 value 9`, "commands.data.modify.invalid_index"},
		{"created_parent_no_change", `{guard:1}`, `missing.rows[1] set value 9`, "commands.data.merge.failed"},
		{"matching_root", `{guard:1,rows:[[1,2],[1]]}`, `{guard:1}.rows[] insert 2 value 9`, "commands.data.modify.invalid_index"},
		{"mismatching_root", `{guard:1,rows:[[1,2],[1]]}`, `{guard:2}.rows[] insert 2 value 9`, "arguments.nbtpath.nothing_found"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id := "t:" + tc.name
			initial, err := nbt.ParseSNBT(tc.initial)
			if err != nil {
				t.Fatal(err)
			}
			wantSaved[id] = initial
			if feedback := srv.RunRconLine("data merge storage " + id + " " + tc.initial); len(feedback) != 1 || feedback[0].Translate != "commands.data.storage.modified" {
				t.Fatalf("populate: %+v", feedback)
			}
			before := srv.RunRconLine("data get storage " + id)[0].With[1].Plain()
			if feedback := srv.RunRconLine("data modify storage " + id + " " + tc.edit); len(feedback) != 1 || feedback[0].Color != "red" || len(feedback[0].Extra) == 0 || feedback[0].Extra[0].Translate != tc.refusal {
				t.Fatalf("refusal: %+v, want %s", feedback, tc.refusal)
			}
			feedback := srv.RunRconLine("data get storage " + id)
			if len(feedback) != 1 || len(feedback[0].With) != 2 {
				t.Fatalf("query: %+v", feedback)
			}
			if got := feedback[0].With[1].Plain(); got != before {
				t.Errorf("storage after refusal = %s, want %s", got, before)
			}
		})
	}
	// Another successful write must not persist any of the refused edits.
	srv.RunRconLine(`data modify storage t:other v set value 1`)
	srv.Shutdown()
	stored := world.NewCommandStorages(root)
	for id, want := range wantSaved {
		stored.Read(id, func(got nbt.CompoundTag) {
			if !nbt.Equal(got, want) {
				t.Errorf("saved %s = %s, want %s", id, nbt.Stringify(got), nbt.Stringify(want))
			}
		})
	}
}

func TestStoragePathRootPredicatesCommitEdits(t *testing.T) {
	srv, _ := startFunctionServer(t, nil)
	servertest.HoldTicks(t, srv)
	for _, line := range []string{
		`data merge storage t:s {guard:1,rows:[1],obj:{v:1}}`,
		`data modify storage t:s {guard:1}.rows append value 2`,
		`data modify storage t:s {guard:1}.obj{v:1}.v set value 2`,
		`data modify storage t:s {guard:1}.created.v set value 3`,
		`data remove storage t:s {guard:1}.created`,
		`data modify storage t:s {guard:1} merge value {extra:3}`,
		`execute store result storage t:s {guard:1}.result int 1 run data get storage t:s extra`,
	} {
		if feedback := srv.RunRconLine(line); len(feedback) != 1 || feedback[0].Color == "red" {
			t.Fatalf("%s: %+v", line, feedback)
		}
	}
	for _, tc := range []struct{ path, want string }{
		{"guard", "1"}, {"rows", "[1, 2]"}, {"obj.v", "2"}, {"extra", "3"}, {"result", "3"},
	} {
		feedback := srv.RunRconLine("data get storage t:s " + tc.path)
		if len(feedback) != 1 || len(feedback[0].With) != 2 {
			t.Fatalf("%s query: %+v", tc.path, feedback)
		}
		if got := feedback[0].With[1].Plain(); got != tc.want {
			t.Errorf("%s = %s, want %s", tc.path, got, tc.want)
		}
	}
}
