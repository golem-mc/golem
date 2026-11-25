package commands

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// vanilla: MinecraftServer.reloadResources, ReloadableServerRegistries.reload
func TestReloadKeepsTagsLiveUntilValidated(t *testing.T) {
	// The pack tag and loot layers are process-wide.
	t.Cleanup(func() {
		registry.ApplyPackTags(nil)
		registry.ApplyPackLoot(nil, nil, nil)
		registry.ApplyPackSlotSources(nil)
	})
	cfg := packWorld(t)
	data := filepath.Join(cfg.LevelName, "datapacks", "a", "data", "x")
	tag := filepath.Join(data, "tags", "item", "t.json")
	writeFile(t, tag, `{"values":["minecraft:stone"]}`)
	writeFile(t, filepath.Join(data, "slot_source", "filtered.json"), `{"type":"minecraft:filtered",
		"slot_source":{"type":"minecraft:slot_range","slots":"weapon.mainhand"},"item_filter":{"items":"#x:t"}}`)
	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate", "Other"))
	other := servertest.DialServer(t, srv).Join("Other")
	servertest.HoldTicks(t, srv)
	other.ChatCommand("give @s minecraft:stone")
	other.Settle()
	c.Settle()

	expectTag := func(stage string, want []byte) {
		t.Helper()
		for _, args := range []string{"weapon.mainhand #x:t", "x:filtered *"} {
			other.ChatCommand("execute if items entity @s " + args)
			if got := other.ExpectWithinTicks(srv, 0, protocol.IDPlaySystemChat); !bytes.Equal(got, want) {
				t.Errorf("%s: tag check with %q answered % X, want % X", stage, args, got, want)
			}
		}
	}
	pass := conditionalPassCountForTest(1)
	fail := servertest.CommandErrorMessageForTest("commands.execute.conditional.fail")
	expectTag("before reload", pass)

	writeFile(t, tag, `{"values":["minecraft:dirt"]}`)
	loot := filepath.Join(data, "loot_table")
	broken := filepath.Join(loot, "z_broken.json")
	fifo := filepath.Join(loot, "a_wait.json")
	if err := os.MkdirAll(loot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(loot, "missing"), broken); err != nil {
		t.Fatal(err)
	}
	if err := unix.Mkfifo(fifo, 0o644); err != nil {
		t.Fatal(err)
	}

	c.ChatCommand("reload")
	readerReady := openReloadFIFO(t, fifo)
	other.Settle()
	expectTag("during validation", pass)
	if err := readerReady.Close(); err != nil {
		t.Fatal(err)
	}
	servertest.CheckCommandFeedback(t, c.ExpectWithinTicks(srv, 0, protocol.IDPlaySystemChat), "commands.reload.success")
	if got := c.ExpectWithinTicks(srv, 0, protocol.IDPlaySystemChat); !bytes.Equal(got, servertest.CommandErrorMessageForTest("commands.reload.failure")) {
		t.Errorf("failed reload answered % X, want the reload failure", got)
	}
	expectTag("after failure", pass)

	for _, path := range []string{fifo, broken} {
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
	}
	writeFile(t, filepath.Join(data, "tags", "item", "added.json"), `{"values":["#x:t"]}`)
	writeFile(t, filepath.Join(data, "tags", "block", "mineable.json"), `{"values":["minecraft:stone"]}`)
	writeFile(t, filepath.Join(loot, "pick.json"), `{"pools":[{"rolls":1,"entries":[{"type":"minecraft:tag","items":"#x:added","expand":true}]}]}`)
	writeFile(t, filepath.Join(data, "advancement", "icon.json"), `{
		"display":{"title":"Icon","description":"Icon with a new block tag","icon":{"id":"minecraft:stick",
			"components":{"minecraft:can_break":{"blocks":"#x:mineable"}}}},
		"criteria":{"grant":{"trigger":"minecraft:impossible"}}
	}`)
	c.ChatCommand("reload")
	servertest.CheckCommandFeedback(t, c.ExpectWithinTicks(srv, 0, protocol.IDPlaySystemChat), "commands.reload.success")
	c.ExpectWithinTicks(srv, 0, protocol.IDPlayUpdateTags)
	other.Settle()
	expectTag("after successful reload", fail)
	expectGave(t, other, "loot give @s loot x:pick", "minecraft:dirt", 1)
	c.Settle()
	c.ChatCommand("advancement grant @s only x:icon")
	if got := servertest.FeedbackComponent(t, c).Translate; got != "commands.advancement.grant.one.to.one.success" {
		t.Errorf("granting advancement with a tag in its icon answered %q", got)
	}
}

// The writer can open only once validation reaches the table. The deadline
// bounds a stalled reload and does not advance the held server clock.
func openReloadFIFO(t *testing.T, path string) *os.File {
	t.Helper()
	deadline := time.Now().Add(servertest.ExpectTimeout)
	for {
		fd, err := unix.Open(path, unix.O_WRONLY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
		if err == nil {
			file := os.NewFile(uintptr(fd), path)
			t.Cleanup(func() { _ = file.Close() })
			return file
		}
		if !errors.Is(err, unix.ENXIO) || time.Now().After(deadline) {
			t.Fatalf("waiting for reload to open %s: %v", path, err)
		}
		time.Sleep(time.Millisecond)
	}
}
