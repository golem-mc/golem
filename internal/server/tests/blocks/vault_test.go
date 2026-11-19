package blocks

import (
	"math"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

const vaultX, vaultY, vaultZ = 4, 64, 2

// vaultScene joins a player beside a vault at (4,64,2) over a stone floor,
// the loop held for the test to step.
func vaultScene(t *testing.T, state string, tag nbt.Compound) (*server.Server, *servertest.Client) {
	t.Helper()
	return vaultSceneIn(t, servertest.MobTestConfig(), state, tag)
}

// vaultSceneIn is vaultScene under cfg.
func vaultSceneIn(t *testing.T, cfg *config.Config, state string, tag nbt.Compound) (*server.Server, *servertest.Client) {
	t.Helper()
	srv, c := servertest.Join(t, cfg, "Vaulter",
		servertest.OpsListing(t, "Vaulter"), func(s *server.Server) { s.SetKeepAlive(time.Hour, time.Hour) })
	servertest.HoldTicks(t, srv)
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()
	stone := servertest.MustState(t, "minecraft:stone", nil)
	air := servertest.MustState(t, "minecraft:air", nil)
	for x := int32(-4); x <= 14; x++ {
		for z := int32(-6); z <= 10; z++ {
			srv.SetBlock(x, 63, z, stone)
			for y := int32(64); y <= 67; y++ {
				srv.SetBlock(x, y, z, air)
			}
		}
	}
	srv.SetBlock(vaultX, vaultY, vaultZ, vaultState(t, state, false))
	srv.SetBlockEntityTagAt(vaultX, vaultY, vaultZ, "minecraft:vault", tag)
	return srv, c
}

func vaultState(t *testing.T, state string, ominous bool) uint16 {
	t.Helper()
	o := "false"
	if ominous {
		o = "true"
	}
	return servertest.MustState(t, "minecraft:vault", map[string]string{"vault_state": state, "ominous": o, "facing": "north"})
}

func vaultStateOf(t *testing.T, srv *server.Server) string {
	t.Helper()
	name, props, _ := registry.BlockStateName(srv.BlockAt(vaultX, vaultY, vaultZ))
	if name != "minecraft:vault" {
		t.Fatalf("the block at the vault is %s", name)
	}
	return registry.ParseStateProps(props)["vault_state"]
}

func vaultTag(t *testing.T, srv *server.Server) nbt.Compound {
	t.Helper()
	tag, ok := srv.BlockEntityTagAt(vaultX, vaultY, vaultZ)
	if !ok {
		t.Fatal("the vault carries no data")
	}
	return tag
}

// keyConfig is a vault config that rolls one trial key out of the spawner key
// table and takes the item given as its key.
func keyConfig(key string) nbt.Compound {
	return nbt.Compound{"config": nbt.Compound{
		"loot_table": nbt.String("minecraft:spawners/trial_chamber/key"),
		"key_item":   nbt.Compound{"id": nbt.String(key), "count": nbt.Int(1)},
	}}
}

// pastFailBuffer steps the game clock beyond the first 15 ticks, in which the
// insert-fail sound is still held back by its zero timestamp.
func pastFailBuffer(t *testing.T, srv *server.Server) {
	t.Helper()
	servertest.RunTicks(t, srv, 16)
}

func giveVaultKey(t *testing.T, srv *server.Server, name string, count int32) {
	t.Helper()
	srv.GiveSlot("Vaulter", 36, protocol.ItemStack{ItemID: registry.MustItem(name), Count: count})
}

func vaultHotbar(srv *server.Server) protocol.ItemStack { return srv.InventorySlot("Vaulter", 36) }

// TestAVaultWakesForAPlayerAndSleepsWhenTheyLeave: a player within 4 blocks
// turns an inactive vault active with event 3015, and past 4.5 it goes
// inactive again with event 3016.
// vanilla: VaultState.tickAndGetNext
func TestAVaultWakesForAPlayerAndSleepsWhenTheyLeave(t *testing.T) {
	t.Parallel()
	srv, c := vaultScene(t, "inactive", nbt.Compound{})
	servertest.RunTicks(t, srv, 1)
	if got := vaultStateOf(t, srv); got != "active" {
		t.Fatalf("state = %s with a player in range, want active", got)
	}
	heard := servertest.HearTool(t, c)
	if got := len(heard.Events[3015]); got != 1 {
		t.Errorf("%d level events 3015, want 1", got)
	}
	shared, _ := nbt.Get[nbt.Compound](vaultTag(t, srv), "shared_data")
	if players, _ := nbt.Get[nbt.List](shared, "connected_players"); len(players.Items) != 1 {
		t.Errorf("%d connected players, want 1", len(players.Items))
	}

	srv.RunLine("tp Vaulter 11.5 64 2.5")
	c.Settle()
	servertest.UntilTicks(t, srv, 40, "the vault going inactive", func() bool { return vaultStateOf(t, srv) == "inactive" })
	if got := len(servertest.HearTool(t, c).Events[3016]); got != 1 {
		t.Errorf("%d level events 3016, want 1", got)
	}
}

// TestAVaultTakesItsKeyAndEjectsItsLoot: the configured key goes in, the vault
// unlocks for 14 ticks, opens its shutter and throws the rolled item up, and
// the player is remembered.
// vanilla: VaultBlockEntity.Server.tryInsertKey, VaultState.tickAndGetNext
func TestAVaultTakesItsKeyAndEjectsItsLoot(t *testing.T) {
	t.Parallel()
	srv, c := vaultScene(t, "inactive", keyConfig("minecraft:trial_key"))
	servertest.RunTicks(t, srv, 1)
	giveVaultKey(t, srv, "minecraft:trial_key", 2)
	servertest.HearTool(t, c)

	c.UseBlockWith(protocol.HandMain, vaultX, vaultY, vaultZ)
	heard := servertest.HearTool(t, c)
	if got := vaultHotbar(srv).Count; got != 1 {
		t.Errorf("%d keys left, want 1", got)
	}
	if got := vaultStateOf(t, srv); got != "unlocking" {
		t.Fatalf("state = %s after the key, want unlocking", got)
	}
	if heard.Sounds["minecraft:block.vault.insert_item"] != 1 {
		t.Errorf("insert sounds = %d, want 1", heard.Sounds["minecraft:block.vault.insert_item"])
	}
	if used := srv.StatValue("Vaulter", registry.StatUsed, "minecraft:trial_key"); used != 1 {
		t.Errorf("trial key used stat = %d, want 1", used)
	}

	// Unlocking lasts 14 ticks, then the shutter opens and 20 ticks later the
	// first item flies.
	servertest.RunTicks(t, srv, 13)
	if got := vaultStateOf(t, srv); got != "unlocking" {
		t.Fatalf("state = %s 13 ticks in, want unlocking", got)
	}
	servertest.RunTicks(t, srv, 1)
	if got := vaultStateOf(t, srv); got != "ejecting" {
		t.Fatalf("state = %s 14 ticks in, want ejecting", got)
	}
	if len(trialItems(srv, "minecraft:trial_key")) != 0 {
		t.Fatal("an item flew before the shutter delay")
	}
	servertest.RunTicks(t, srv, 19)
	if len(trialItems(srv, "minecraft:trial_key")) != 0 {
		t.Fatal("an item flew one tick early")
	}
	servertest.RunTicks(t, srv, 1)
	keys := trialItems(srv, "minecraft:trial_key")
	if len(keys) != 1 {
		t.Fatalf("%d keys ejected, want 1", len(keys))
	}
	k := keys[0]
	if k.VY <= 0 || math.Abs(k.X-(vaultX+0.5)) > 0.1 || math.Abs(k.Z-(vaultZ+0.5)) > 0.1 || k.Y < vaultY+1 {
		t.Errorf("key at (%v,%v,%v) moving (%v,%v,%v), want it thrown up from the top of the block", k.X, k.Y, k.Z, k.VX, k.VY, k.VZ)
	}
	heard = servertest.HearTool(t, c)
	if len(heard.Events[3017]) != 1 || heard.Sounds["minecraft:block.vault.eject_item"] != 1 {
		t.Errorf("eject event/sound = %d/%d, want 1/1", len(heard.Events[3017]), heard.Sounds["minecraft:block.vault.eject_item"])
	}

	servertest.RunTicks(t, srv, 20)
	servertest.UntilTicks(t, srv, 60, "the vault closing", func() bool { return vaultStateOf(t, srv) != "ejecting" })
	if got := vaultStateOf(t, srv); got != "inactive" {
		t.Errorf("state = %s after the ejection, want inactive since the only player was rewarded", got)
	}
	server, _ := nbt.Get[nbt.Compound](vaultTag(t, srv), "server_data")
	if rewarded, _ := nbt.Get[nbt.List](server, "rewarded_players"); len(rewarded.Items) != 1 {
		t.Errorf("%d rewarded players, want 1", len(rewarded.Items))
	}
	if heard := servertest.HearTool(t, c); heard.Sounds["minecraft:block.vault.close_shutter"] != 1 {
		t.Errorf("close sounds = %d, want 1", heard.Sounds["minecraft:block.vault.close_shutter"])
	}
}

// TestAVaultRefusesAWrongKeyAndKeepsIt: another item, or the other kind of
// key, plays the fail sound and is not spent; the sound repeats only after 15
// ticks.
// vanilla: VaultBlockEntity.Server.isValidToInsert, playInsertFailSound
func TestAVaultRefusesAWrongKeyAndKeepsIt(t *testing.T) {
	t.Parallel()
	srv, c := vaultScene(t, "inactive", keyConfig("minecraft:ominous_trial_key"))
	pastFailBuffer(t, srv)
	giveVaultKey(t, srv, "minecraft:trial_key", 1)
	servertest.HearTool(t, c)

	c.UseBlockWith(protocol.HandMain, vaultX, vaultY, vaultZ)
	c.UseBlockWith(protocol.HandMain, vaultX, vaultY, vaultZ)
	heard := servertest.HearTool(t, c)
	if got := heard.Sounds["minecraft:block.vault.insert_item_fail"]; got != 1 {
		t.Errorf("%d fail sounds for two clicks in a tick, want 1", got)
	}
	if got := vaultHotbar(srv).Count; got != 1 {
		t.Errorf("%d keys left, want it kept", got)
	}
	if got := vaultStateOf(t, srv); got != "active" {
		t.Errorf("state = %s, want active", got)
	}
	servertest.RunTicks(t, srv, 14)
	c.UseBlockWith(protocol.HandMain, vaultX, vaultY, vaultZ)
	if got := servertest.HearTool(t, c).Sounds["minecraft:block.vault.insert_item_fail"]; got != 0 {
		t.Errorf("%d fail sounds 14 ticks after the last, want 0", got)
	}
	servertest.RunTicks(t, srv, 1)
	c.UseBlockWith(protocol.HandMain, vaultX, vaultY, vaultZ)
	if got := servertest.HearTool(t, c).Sounds["minecraft:block.vault.insert_item_fail"]; got != 1 {
		t.Errorf("%d fail sounds 15 ticks after the last, want 1", got)
	}

	giveVaultKey(t, srv, "minecraft:ominous_trial_key", 1)
	c.UseBlockWith(protocol.HandMain, vaultX, vaultY, vaultZ)
	c.Settle()
	if got := vaultStateOf(t, srv); got != "unlocking" {
		t.Errorf("state = %s after the right key, want unlocking", got)
	}
}

// TestAVaultTurnsARewardedPlayerAway: a player in the rewarded set keeps the
// key and hears the reject sound.
// vanilla: VaultServerData.hasRewardedPlayer
func TestAVaultTurnsARewardedPlayerAway(t *testing.T) {
	t.Parallel()
	tag := keyConfig("minecraft:trial_key")
	tag["server_data"] = nbt.Compound{"rewarded_players": nbt.List{Elem: nbt.TypeIntArray, Items: []nbt.Tag{
		nbt.UUIDToIntArray(protocol.OfflineUUID("Vaulter"))}}}
	srv, c := vaultScene(t, "active", tag)
	pastFailBuffer(t, srv)
	srv.SetBlock(vaultX, vaultY, vaultZ, vaultState(t, "active", false))
	giveVaultKey(t, srv, "minecraft:trial_key", 1)
	servertest.HearTool(t, c)

	c.UseBlockWith(protocol.HandMain, vaultX, vaultY, vaultZ)
	heard := servertest.HearTool(t, c)
	if heard.Sounds["minecraft:block.vault.reject_rewarded_player"] != 1 {
		t.Errorf("reject sounds = %d, want 1", heard.Sounds["minecraft:block.vault.reject_rewarded_player"])
	}
	if got := vaultHotbar(srv).Count; got != 1 {
		t.Errorf("%d keys left, want it kept", got)
	}
	if got := vaultStateOf(t, srv); got != "active" {
		t.Errorf("state = %s, want active", got)
	}
	servertest.RunTicks(t, srv, 25)
	if got := vaultStateOf(t, srv); got != "inactive" {
		t.Errorf("state = %s with only a rewarded player near, want inactive", got)
	}
}
