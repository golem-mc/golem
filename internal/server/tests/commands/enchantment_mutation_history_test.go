package commands

import (
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
)

func TestEnchantCommandKeepsNativeCollisionHistory(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile("testdata/stack_enchantment_history.tsv")
	if err != nil {
		t.Fatal(err)
	}
	fixture := strings.Split(strings.Split(string(data), "\n")[1], "\t")
	input, err := nbt.ParseOrderedSNBT(fixture[3])
	if err != nil {
		t.Fatal(err)
	}
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Alice", servertest.OpsListing(t, "Alice"))
	servertest.HoldTicks(t, srv)
	c.ChatCommand(`item replace entity @s weapon.mainhand with diamond_boots[enchantments=` + nativeStackInput(input) + `]`)
	servertest.AwaitSystemChatSaying(t, c, "commands.item.entity.replace.success.single.known_item")
	c.ChatCommand(`enchant @s unbreaking 1`)
	servertest.AwaitSystemChatSaying(t, c, "commands.enchant.success.single")
	c.Settle()
	for range 3 {
		c.ChatCommand(`data get entity @s SelectedItem.components."minecraft:enchantments"`)
		line := servertest.FeedbackComponent(t, c)
		if len(line.With) < 2 {
			t.Fatal(line.Plain())
		}
		got, err := nbt.ParseOrderedSNBT(line.With[len(line.With)-1].Plain())
		if err != nil {
			t.Fatal(err)
		}
		keys := nbt.HashOrderedKeys(got)
		keys = slices.DeleteFunc(keys, func(key string) bool { return key == "minecraft:unbreaking" })
		if !slices.Equal(keys, nbt.HashOrderedKeys(input)) || got.Entries()["minecraft:unbreaking"] != nbt.Int(1) {
			t.Fatalf("mutation lost collision history: %s", line.Plain())
		}
	}
}
