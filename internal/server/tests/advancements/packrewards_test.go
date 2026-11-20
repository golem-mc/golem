package advancements

import (
	"path/filepath"
	"testing"

	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// rewardAdvancement is packAdvancement("stick") with a rewards object.
func rewardAdvancement(rewards string) string {
	return `{"rewards":` + rewards + `,"criteria":{"got":{"trigger":"minecraft:inventory_changed","conditions":{"items":[{"items":"minecraft:stick"}]}}}}`
}

// TestPackFunctionRewardRunsAsThePlayerAndMayRevoke: the reward function
// runs as the player, and revoking its own advancement lets it fire again.
// vanilla: AdvancementRewards.grant
func TestPackFunctionRewardRunsAsThePlayerAndMayRevoke(t *testing.T) {
	cfg, data := packConfig(t)
	writePackFile(t, filepath.Join(data, "t", "advancement", "a.json"), rewardAdvancement(`{"function":"t:r"}`))
	writePackFile(t, filepath.Join(data, "t", "function", "r.mcfunction"),
		"scoreboard players add @s x 1\nadvancement revoke @s only t:a\n")
	srv, c := joinOp(t, cfg, "Packer")
	c.ChatCommand("scoreboard objectives add x dummy")
	c.Settle()

	for want := int32(1); want <= 2; want++ {
		give(srv, t, "Packer", "stick")
		c.ReadTicks(srv, 5, func(int32, []byte) bool { return false })
		if got, _ := srv.ScoreOf("Packer", "x"); got != want {
			t.Fatalf("score after trigger %d = %d, want %d", want, got, want)
		}
		give(srv, t, "Packer", "dirt")
		c.ReadTicks(srv, 2, func(int32, []byte) bool { return false })
	}
}

// TestPackLootRewardGivesTheTableAndDropsWhatDoesNotFit: a loot reward goes
// into the inventory; with it full the stack is dropped at the player.
// vanilla: AdvancementRewards.grant
func TestPackLootRewardGivesTheTableAndDropsWhatDoesNotFit(t *testing.T) {
	cfg, data := packConfig(t)
	writePackFile(t, filepath.Join(data, "t", "advancement", "a.json"), rewardAdvancement(`{"loot":["t:l"]}`))
	writePackFile(t, filepath.Join(data, "t", "advancement", "b.json"), rewardAdvancement(`{"loot":"t:l"}`))
	writePackFile(t, filepath.Join(data, "t", "loot_table", "l.json"),
		`{"type":"minecraft:advancement_reward","pools":[{"rolls":1,"entries":[{"type":"minecraft:item","name":"minecraft:diamond"}]}]}`)
	srv, c := joinOp(t, cfg, "Packer")

	c.ChatCommand("advancement grant @s only t:a")
	c.ReadTicks(srv, 5, func(int32, []byte) bool { return false })
	found := false
	for slot := player.MainSlot; slot < player.HotbarSlot+player.HotbarSlots; slot++ {
		if st := srv.InventorySlot("Packer", slot); st.Count == 1 && st.ItemID == servertest.ItemStack(t, "minecraft:diamond", 1).ItemID {
			found = true
		}
	}
	if !found {
		t.Fatal("the diamond is not in the inventory")
	}

	for slot := player.MainSlot; slot < player.HotbarSlot+player.HotbarSlots; slot++ {
		srv.GiveSlot("Packer", slot, servertest.ItemStack(t, "minecraft:dirt", 64))
	}
	c.ChatCommand("advancement grant @s only t:b")
	c.ReadTicks(srv, 5, func(int32, []byte) bool { return false })
	dropped := 0
	for _, it := range srv.ItemsInDimension("minecraft:overworld") {
		if it.Item == "minecraft:diamond" {
			dropped++
		}
	}
	if dropped != 1 {
		t.Errorf("dropped diamonds = %d, want 1", dropped)
	}
}
