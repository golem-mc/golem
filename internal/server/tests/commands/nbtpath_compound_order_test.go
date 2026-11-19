package commands

import (
	"fmt"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

func modifyOrderedStorage(t *testing.T, c *servertest.Client, id, operation string) {
	t.Helper()
	c.ChatCommand("data modify storage " + id + " " + operation)
	c.ExpectCommandFeedback("commands.data.storage.modified", "minecraft:"+id)
}

func expectOrderedStorage(t *testing.T, c *servertest.Client, id, path, want string) {
	t.Helper()
	c.ChatCommand("data get storage " + id + " " + path)
	line := servertest.FeedbackComponent(t, c)
	if line.Translate != "commands.data.storage.query" || len(line.With) != 2 {
		t.Fatalf("query = %+v", line)
	}
	if got := line.With[1].Plain(); got != want {
		t.Errorf("%s %s: data get = %q, want %q", id, path, got, want)
	}
	for _, plain := range []bool{false, true} {
		c.ChatCommand(fmt.Sprintf(`tellraw @s {nbt:%q,storage:%q,plain:%t}`, path, "minecraft:"+id, plain))
		if got := servertest.FeedbackComponent(t, c).Plain(); got != want {
			t.Errorf("%s %s: nbt component (plain=%t) = %q, want %q", id, path, plain, got, want)
		}
	}
}

// vanilla: NbtPathArgument.Node.createPreferredParentTag, MatchObjectNode.getOrCreateTag,
// MatchElementNode.getOrCreateTag, CompoundTag.copy
func TestNBTPathCreatedStorageCompoundOrder(t *testing.T) {
	t.Parallel()
	c := storageWorld(t, "Alice")
	for _, tc := range []struct{ name, create, path string }{
		{"child", `outer.inner.v set value 2`, "outer.inner"},
		{"merge", `outer.inner merge value {v:2,f:1,m:1,bo:1}`, "outer.inner"},
		{"list_child", `rows[].v set value 2`, "rows[0]"},
		{"list_merge", `rows[] merge value {v:2,f:1,m:1,bo:1}`, "rows[0]"},
		{"object_pattern", `outer.inner{v:1,f:1,m:1,bo:1}.v set value 2`, "outer.inner"},
		{"list_pattern", `rows[{v:1,f:1,m:1,bo:1}].v set value 2`, "rows[0]"},
		{"list_object_pattern", `rows[].inner{v:1,f:1,m:1,bo:1}.v set value 2`, "rows[0].inner"},
		{"nested_pattern", `outer{inner:{v:1,f:1,m:1,bo:1}}.inner.v set value 2`, "outer.inner"},
		{"nested_list_pattern", `outer{rows:[{v:1,f:1,m:1,bo:1}]}.rows[0].v set value 2`, "outer.rows[0]"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id := tc.name
			modifyOrderedStorage(t, c, id, tc.create)
			if tc.name == "child" || tc.name == "list_child" {
				for key := range strings.FieldsSeq("f m bo") {
					modifyOrderedStorage(t, c, id, tc.path+"."+key+" set value 1")
				}
			}
			expectOrderedStorage(t, c, id, tc.path, `{v: 2, f: 1, m: 1, bo: 1}`)
			modifyOrderedStorage(t, c, id, tc.path+".bo set value 4")
			c.ChatCommand("data remove storage " + id + " " + tc.path + ".v")
			c.ExpectCommandFeedback("commands.data.storage.modified", "minecraft:"+id)
			modifyOrderedStorage(t, c, id, tc.path+".v set value 3")
			expectOrderedStorage(t, c, id, tc.path, `{f: 1, v: 3, m: 1, bo: 4}`)
			for key := range strings.FieldsSeq("big s t w em k l n a") {
				modifyOrderedStorage(t, c, id, tc.path+"."+key+" set value 1")
			}
			c.ChatCommand("data remove storage " + id + " " + tc.path + ".a")
			c.ExpectCommandFeedback("commands.data.storage.modified", "minecraft:"+id)
			expectOrderedStorage(t, c, id, tc.path, `{big: 1, f: 1, em: 1, k: 1, l: 1, m: 1, bo: 4, n: 1, s: 1, t: 1, v: 3, w: 1}`)
		})
	}
}

// vanilla: NbtPathArgument.NbtPath.set/insert, CompoundTag.copy
func TestNBTPathStorageCopyRebuildsCompoundTable(t *testing.T) {
	t.Parallel()
	c := storageWorld(t, "Alice")
	modifyOrderedStorage(t, c, "copies", `source set value {big:1,s:1,t:1,v:1,f:1,w:1,em:1,k:1,l:1,m:1,bo:1,n:1,a:1}`)
	c.ChatCommand("data remove storage copies source.a")
	c.ExpectCommandFeedback("commands.data.storage.modified", "minecraft:copies")
	const retained = `{big: 1, f: 1, em: 1, k: 1, l: 1, m: 1, bo: 1, n: 1, s: 1, t: 1, v: 1, w: 1}`
	const copied = `{big: 1, s: 1, t: 1, f: 1, v: 1, w: 1, em: 1, k: 1, l: 1, m: 1, bo: 1, n: 1}`
	expectOrderedStorage(t, c, "copies", "source", retained)
	for _, tc := range []struct{ operation, path string }{
		{"dest set from storage copies source", "dest"},
		{"list append from storage copies source", "list[0]"},
		{"merged merge from storage copies source", "merged"},
	} {
		modifyOrderedStorage(t, c, "copies", tc.operation)
		expectOrderedStorage(t, c, "copies", tc.path, copied)
	}
	expectOrderedStorage(t, c, "copies", "source", retained)
}

// vanilla: NbtPathArgument.NbtPath.getOrCreateParents
func TestNBTPathStorageCreatesSeparateParents(t *testing.T) {
	t.Parallel()
	c := storageWorld(t, "Alice")
	for _, operation := range []string{"set", "merge"} {
		modifyOrderedStorage(t, c, operation, "rows set value [{},{}]")
		path, value := "rows[].inner.v", "2"
		if operation == "merge" {
			path, value = "rows[].inner.nested", "{v:2,f:1,m:1,bo:1}"
		}
		c.ChatCommand("execute store result storage counts " + operation + " int 1 run data modify storage " + operation + " " + path + " " + operation + " value " + value)
		c.ExpectCommandFeedback("commands.data.storage.modified", "minecraft:"+operation)
		c.ChatCommand("data get storage counts " + operation)
		c.ExpectCommandFeedback("commands.data.storage.query", "minecraft:counts", "2")
	}
}

// vanilla: DataCommands.manipulateData, NbtPathArgument.NbtPath.set/insert, CompoundTag.merge
func TestNBTPathStorageCopiesMapSourceCompounds(t *testing.T) {
	t.Parallel()
	c := storageWorld(t, "Alice")
	c.ChatCommand(`setblock 1 64 1 chest{Items:[{Slot:0b,id:"minecraft:stone",count:1,components:{"minecraft:custom_data":{a:1}}}]}`)
	c.ExpectCommandFeedback("commands.setblock.success", "1", "64", "1")
	for _, tc := range []struct{ name, operation, path string }{
		{"set", `dest set from block 1 64 1 Items`, `dest[0].components."minecraft:custom_data"`},
		{"append", `dest append from block 1 64 1 Items[]`, `dest[0].components."minecraft:custom_data"`},
		{"merge", `dest merge from block 1 64 1 Items[0]`, `dest.components."minecraft:custom_data"`},
	} {
		modifyOrderedStorage(t, c, tc.name, tc.operation)
		for key := range strings.FieldsSeq("v f m bo") {
			modifyOrderedStorage(t, c, tc.name, tc.path+"."+key+" set value 2")
		}
		expectOrderedStorage(t, c, tc.name, tc.path, `{a: 1, v: 2, f: 2, m: 2, bo: 2}`)
	}
}

// vanilla: CopyCustomDataFunction.MergeStrategy
func TestItemCopyCustomDataMergesCreatedCompounds(t *testing.T) {
	t.Parallel()
	c := storageWorld(t, "Alice")
	c.ChatCommand(`setblock 1 64 1 chest{Items:[{Slot:0b,id:"minecraft:stick",count:1}]}`)
	c.ExpectCommandFeedback("commands.setblock.success", "1", "64", "1")
	c.ChatCommand(`data merge storage source {x:5,object:{v:1,f:2,m:3,bo:4}}`)
	c.ExpectCommandFeedback("commands.data.storage.modified", "minecraft:source")
	c.ChatCommand(`item modify block 1 64 1 container.0 {type:"minecraft:copy_custom_data",source:{type:"minecraft:storage",source:"source"},ops:[{source:"x",target:"a.b",op:"replace"},{source:"object",target:"a",op:"merge"},{source:"object",target:"rows[].inner",op:"merge"},{source:"x",target:"rows[{inner:{v:1}}].inner.f",op:"replace"}]}`)
	servertest.FeedbackComponent(t, c)
	for _, tc := range []struct{ path, want string }{
		{"a.b", "5"},
		{"a.v", "1"},
		{"rows[0].inner.f", "5"},
	} {
		c.ChatCommand(`data get block 1 64 1 Items[0].components."minecraft:custom_data".` + tc.path)
		c.ExpectCommandFeedback("commands.data.block.query", "1", "64", "1", tc.want)
	}
}

// vanilla: SetCustomDataFunction.run, CompoundTag.merge
func TestItemSetCustomDataMergesNestedCompounds(t *testing.T) {
	t.Parallel()
	c := storageWorld(t, "Alice")
	c.ChatCommand(`setblock 1 64 1 chest{Items:[{Slot:0b,id:"minecraft:stick",count:1,components:{"minecraft:custom_data":{nested:{v:1,rows:[{kept:3}]}}}}]}`)
	c.ExpectCommandFeedback("commands.setblock.success", "1", "64", "1")
	c.ChatCommand(`item modify block 1 64 1 container.0 {type:"minecraft:set_custom_data",tag:"{nested:{f:2}}"}`)
	servertest.AwaitSystemChatSaying(t, c, "commands.item.block.modify.success")
	for _, tc := range []struct{ path, want string }{
		{"nested.v", "1"}, {"nested.f", "2"}, {"nested.rows[0].kept", "3"},
	} {
		c.ChatCommand(`data get block 1 64 1 Items[0].components."minecraft:custom_data".` + tc.path)
		c.ExpectCommandFeedback("commands.data.block.query", "1", "64", "1", tc.want)
	}
}
