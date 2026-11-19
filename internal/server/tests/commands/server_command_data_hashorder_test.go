package commands

import (
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/server/servertest"
)

func storageWorld(t *testing.T, name string) *servertest.Client {
	t.Helper()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	_, c := servertest.Join(t, cfg, name, servertest.OpsListing(t, name))
	return c
}

// vanilla: TextComponentTagVisitor.visitCompound
func TestDataGetStorageHashOrder(t *testing.T) {
	t.Parallel()
	c := storageWorld(t, "Alice")
	c.ChatCommand(`data merge storage t {big:1,s:1,t:1,v:1,f:1,w:1,em:1,k:1,l:1,m:1,bo:1,n:1}`)
	c.ExpectCommandFeedback("commands.data.storage.modified", "minecraft:t")
	c.ChatCommand(`data get storage t`)
	line := servertest.FeedbackComponent(t, c)
	if line.Translate != "commands.data.storage.query" || len(line.With) != 2 {
		t.Fatalf("line = %+v, want the storage query", line)
	}
	const want = `{big: 1, s: 1, t: 1, v: 1, f: 1, w: 1, em: 1, k: 1, l: 1, m: 1, bo: 1, n: 1}`
	if got := line.With[1].Plain(); got != want {
		t.Fatalf("storage = %q, want %q", got, want)
	}
	for _, step := range []struct{ command, want string }{
		{`data modify storage t v set value 2`, `{big: 1, s: 1, t: 1, v: 2, f: 1, w: 1, em: 1, k: 1, l: 1, m: 1, bo: 1, n: 1}`},
		{`data remove storage t v`, `{big: 1, s: 1, t: 1, f: 1, w: 1, em: 1, k: 1, l: 1, m: 1, bo: 1, n: 1}`},
		{`data modify storage t v set value 3`, `{big: 1, s: 1, t: 1, f: 1, v: 3, w: 1, em: 1, k: 1, l: 1, m: 1, bo: 1, n: 1}`},
	} {
		c.ChatCommand(step.command)
		c.ExpectCommandFeedback("commands.data.storage.modified", "minecraft:t")
		c.ChatCommand(`data get storage t`)
		if got := servertest.FeedbackComponent(t, c).With[1].Plain(); got != step.want {
			t.Fatalf("%s: got %s, want %s", step.command, got, step.want)
		}
	}

}

func TestTellrawNbtCompoundHashOrder(t *testing.T) {
	t.Parallel()
	c := storageWorld(t, "Alice")
	c.ChatCommand(`data merge storage t {nested:{v:1,f:2,m:3,bo:4,a:1b,s:"x",l:[B;1b,2b]}}`)
	servertest.FeedbackComponent(t, c)
	for _, command := range []string{
		`tellraw @s {nbt:"nested",storage:"minecraft:t"}`,
		`tellraw @s {nbt:"nested",storage:"minecraft:t",plain:1b}`,
	} {
		c.ChatCommand(command)
		line := servertest.FeedbackComponent(t, c)
		if got, want := line.Plain(), `{a: 1b, s: "x", v: 1, f: 2, l: [B; 1b, 2b], m: 3, bo: 4}`; got != want {
			t.Fatalf("%s: text = %q, want %q", command, got, want)
		}
	}
}

// vanilla: StorageDataAccessor.getData, NbtContents.resolve
func TestStorageSnapshotRetainsCompoundCapacity(t *testing.T) {
	t.Parallel()
	c := storageWorld(t, "Alice")
	c.ChatCommand(`data merge storage cap {big:1,s:1,t:1,v:1,f:1,w:1,em:1,k:1,l:1,m:1,bo:1,n:1,a:1}`)
	c.ExpectCommandFeedback("commands.data.storage.modified", "minecraft:cap")
	c.ChatCommand(`data remove storage cap a`)
	c.ExpectCommandFeedback("commands.data.storage.modified", "minecraft:cap")
	const want = `{big: 1, f: 1, em: 1, k: 1, l: 1, m: 1, bo: 1, n: 1, s: 1, t: 1, v: 1, w: 1}`
	c.ChatCommand(`data get storage cap`)
	line := servertest.FeedbackComponent(t, c)
	if line.Translate != "commands.data.storage.query" || len(line.With) != 2 {
		t.Fatalf("line = %+v, want the storage query", line)
	}
	if got := line.With[1].Plain(); got != want {
		t.Errorf("storage = %q, want %q", got, want)
	}
	for _, command := range []string{
		`tellraw @s {nbt:"{}",storage:"minecraft:cap"}`,
		`tellraw @s {nbt:"{}",storage:"minecraft:cap",plain:1b}`,
	} {
		c.ChatCommand(command)
		if got := servertest.FeedbackComponent(t, c).Plain(); got != want {
			t.Errorf("%s: text = %q, want %q", command, got, want)
		}
	}
}

// vanilla: StorageDataAccessor.getData, CompoundTag.remove
func TestStorageSnapshotRetainsEmptyNestedCompoundCapacity(t *testing.T) {
	t.Parallel()
	c := storageWorld(t, "Alice")
	c.ChatCommand(`data merge storage empty {nested:{big:1,s:1,t:1,v:1,f:1,w:1,em:1,k:1,l:1,m:1,bo:1,n:1,a:1}}`)
	c.ExpectCommandFeedback("commands.data.storage.modified", "minecraft:empty")
	for key := range strings.FieldsSeq("big s t v f w em k l m bo n a") {
		c.ChatCommand(`data remove storage empty nested.` + key)
		c.ExpectCommandFeedback("commands.data.storage.modified", "minecraft:empty")
	}
	for _, key := range []string{"v", "f"} {
		c.ChatCommand(`data modify storage empty nested.` + key + ` set value 2`)
		c.ExpectCommandFeedback("commands.data.storage.modified", "minecraft:empty")
	}
	const want = `{f: 2, v: 2}`
	c.ChatCommand(`data get storage empty nested`)
	line := servertest.FeedbackComponent(t, c)
	if line.Translate != "commands.data.storage.query" || len(line.With) != 2 {
		t.Fatalf("line = %+v, want the storage query", line)
	}
	if got := line.With[1].Plain(); got != want {
		t.Errorf("storage = %q, want %q", got, want)
	}
	for _, command := range []string{
		`tellraw @s {nbt:"nested",storage:"minecraft:empty"}`,
		`tellraw @s {nbt:"nested",storage:"minecraft:empty",plain:1b}`,
	} {
		c.ChatCommand(command)
		if got := servertest.FeedbackComponent(t, c).Plain(); got != want {
			t.Errorf("%s: text = %q, want %q", command, got, want)
		}
	}
}

func TestDataStorageOrderedCompoundBoundaries(t *testing.T) {
	t.Parallel()
	c := storageWorld(t, "Alice")
	c.ChatCommand(`data modify storage t outer.inner set value {v:1,f:2}`)
	c.ExpectCommandFeedback("commands.data.storage.modified", "minecraft:t")
	c.ChatCommand(`data modify storage t outer.inner merge value {m:3,bo:4}`)
	c.ExpectCommandFeedback("commands.data.storage.modified", "minecraft:t")
	c.ChatCommand(`data get storage t outer.inner`)
	line := servertest.FeedbackComponent(t, c)
	if got := line.With[1].Plain(); got != "{v: 1, f: 2, m: 3, bo: 4}" {
		t.Fatalf("nested compound = %q", got)
	}
	c.ChatCommand(`data modify storage t component set value {text:"hello",extra:[{text:" world"}]}`)
	c.ExpectCommandFeedback("commands.data.storage.modified", "minecraft:t")
	c.ChatCommand(`tellraw @s {nbt:"component",storage:"minecraft:t",interpret:1b}`)
	if got := servertest.FeedbackComponent(t, c).Plain(); got != "hello world" {
		t.Fatalf("interpreted component = %q", got)
	}
}
