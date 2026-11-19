package admin

import (
	"encoding/json"
	"github.com/golem-mc/golem/internal/server/servertest"
	"maps"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// statKey is one counter of Award Statistics, by the IDs it travels as.
type statKey struct{ typeID, keyID int32 }

// customStat is the wire key of a minecraft:custom statistic.
func customStat(t *testing.T, name string) statKey {
	t.Helper()
	typeID, keyID, ok := registry.StatID(registry.StatCustom, name)
	if !ok {
		t.Fatalf("no custom statistic %s", name)
	}
	return statKey{typeID, keyID}
}

// requestStats sends the Client Command the statistics screen sends on
// opening and reads the Award Statistics that answers it.
func requestStats(c *servertest.Client) map[statKey]int32 {
	c.T.Helper()
	c.Send(protocol.IDPlayClientCommandIn, func(b *protocol.Buffer) {
		b.PutVarInt(protocol.ClientCommandRequestStats)
	})
	r := protocol.NewReader(c.AwaitPlay(protocol.IDPlayAwardStats))
	n := r.ReadVarInt()
	got := make(map[statKey]int32, n)
	for range n {
		key := statKey{r.ReadVarInt(), r.ReadVarInt()}
		got[key] = r.ReadVarInt()
	}
	if err := r.Err(); err != nil {
		c.T.Fatalf("award statistics: %v", err)
	}
	if r.Remaining() != 0 {
		c.T.Fatalf("award statistics has %d bytes left over", r.Remaining())
	}
	return got
}

// statsFileOf reads a player's players/stats/<uuid>.json.
func statsFileOf(t *testing.T, level, name string) (stats map[string]map[string]int32, dataVersion int32) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(level, "players", "stats", protocol.OfflineUUID(name).String()+".json"))
	if err != nil {
		t.Fatalf("read the stats file: %v", err)
	}
	var file struct {
		Stats       map[string]map[string]int32 `json:"stats"`
		DataVersion int32                       `json:"DataVersion"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatalf("parse the stats file: %v\n%s", err, data)
	}
	return file.Stats, file.DataVersion
}

// writeStatsFile puts a stats file in place for a player before they join.
func writeStatsFile(t *testing.T, level, name, body string) {
	t.Helper()
	dir := filepath.Join(level, "players", "stats")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, protocol.OfflineUUID(name).String()+".json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestAMinedStoneIsCountedAcrossARejoin breaks a stone with a pickaxe that
// earns it at the finishing click, leaves, and finds mined/minecraft:stone in
// the stats file vanilla writes, with the world version, and in the Award
// Statistics the rejoined player's Request Stats is answered with.
// vanilla: Block.playerDestroy, ServerStatsCounter.save, sendStats
func TestAMinedStoneIsCountedAcrossARejoin(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalConfig()
	cfg.LevelName = t.TempDir()
	srv, connect := servertest.Start(t, cfg)
	c := connect().Join("Miner")
	c.MoveToGround(0.5, 64, 0.5, true)
	// Netherite and efficiency V take 35 / 1.5 / 30 = 0.78 of a stone a
	// tick, past the 0.7 the finishing click asks.
	srv.GiveSlot("Miner", player.HotbarSlot,
		servertest.EnchantedStack(t, "minecraft:netherite_pickaxe", "minecraft:efficiency", 5))
	// A tick puts Efficiency's mining_efficiency on the player.
	servertest.RunTicks(t, srv, 1)
	if !srv.SetBlock(servertest.DigTarget[0], servertest.DigTarget[1], servertest.DigTarget[2], registry.MustState("minecraft:stone", nil)) {
		t.Fatal("could not stand the stone")
	}
	c.Settle()
	c.BreakBlock(servertest.DigTarget[0], servertest.DigTarget[1], servertest.DigTarget[2])
	c.Settle()
	servertest.ExpectAir(t, srv, servertest.DigTarget)
	c.Disconnect()

	stats, version := statsFileOf(t, cfg.LevelName, "Miner")
	if got := stats["minecraft:mined"]["minecraft:stone"]; got != 1 {
		t.Errorf("the stats file's mined stone = %d, want 1 (stats %v)", got, stats)
	}
	if version != 5023 {
		t.Errorf("the stats file's DataVersion = %d, want 26.3's 5023", version)
	}
	if got := stats["minecraft:custom"]["minecraft:play_time"]; got <= 0 {
		t.Errorf("the stats file's play_time = %d, want the ticks played", got)
	}

	again := connect().Join("Miner")
	got := requestStats(again)
	typeID, stoneID, _ := registry.StatID(registry.StatMined, "minecraft:stone")
	if got[statKey{typeID, stoneID}] != 1 {
		t.Errorf("Award Statistics' mined stone = %d, want 1 (got %v)", got[statKey{typeID, stoneID}], got)
	}
}

// TestAStoneBrokenByHandIsNotMined: stone wants a pickaxe to drop, and a
// break that drops nothing is not counted.
// vanilla: ServerPlayerGameMode.destroyBlock reaches playerDestroy only with
// hasCorrectToolForDrops
func TestAStoneBrokenByHandIsNotMined(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalConfig(), "Puncher")
	c.MoveToGround(0.5, 64, 0.5, true)
	if !srv.SetBlock(servertest.DigTarget[0], servertest.DigTarget[1], servertest.DigTarget[2], registry.MustState("minecraft:stone", nil)) {
		t.Fatal("could not stand the stone")
	}
	c.Settle()
	// By hand stone takes 1.5 * 100 = 150 ticks; the early click leaves
	// the rest to the ticks.
	c.BreakBlock(servertest.DigTarget[0], servertest.DigTarget[1], servertest.DigTarget[2])
	servertest.PollUntil(t, 20*time.Second, func() bool {
		return srv.BlockAt(servertest.DigTarget[0], servertest.DigTarget[1], servertest.DigTarget[2]) == 0
	})
	got := requestStats(c)
	typeID, stoneID, _ := registry.StatID(registry.StatMined, "minecraft:stone")
	if n, ok := got[statKey{typeID, stoneID}]; ok {
		t.Errorf("Award Statistics' mined stone = %d after a break by hand, want none", n)
	}
}

// TestStepsJumpsAndDeathsAreCounted walks a block along the ground, jumps,
// and dies, and reads walk_one_cm, jump, deaths and the reset
// time_since_death off Request Stats; a second request carries only what
// changed since the first.
// vanilla: ServerPlayer.checkMovementStatistics, jumpFromGround, die
func TestStepsJumpsAndDeathsAreCounted(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.SurvivalConfig(), "Walker")
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()
	first := requestStats(c)
	if first[customStat(t, "minecraft:play_time")] <= 0 {
		t.Errorf("play_time = %d, want the ticks played", first[customStat(t, "minecraft:play_time")])
	}

	c.MoveToGround(1.5, 64, 0.5, true)
	c.MoveToGround(1.5, 64.42, 0.5, false)
	c.Settle()
	got := requestStats(c)
	if n := got[customStat(t, "minecraft:walk_one_cm")]; n != 100 {
		t.Errorf("walk_one_cm = %d, want the block walked, 100", n)
	}
	if n := got[customStat(t, "minecraft:jump")]; n != 1 {
		t.Errorf("jump = %d, want 1", n)
	}
	if _, ok := got[customStat(t, "minecraft:deaths")]; ok {
		t.Error("the second request carried deaths, which nothing changed")
	}

	c.MoveToGround(1.5, 64, 0.5, true)
	if !srv.Hurt("Walker", 1000) {
		t.Fatal("the killing blow did not land")
	}
	c.Settle()
	got = requestStats(c)
	if n := got[customStat(t, "minecraft:deaths")]; n != 1 {
		t.Errorf("deaths = %d, want 1", n)
	}
	if n, ok := got[customStat(t, "minecraft:time_since_death")]; !ok || n != 0 {
		t.Errorf("time_since_death = %d (sent %t), want it reset to 0", n, ok)
	}
}

// TestAMobKillIsCounted kills a cow with one blow and finds mob_kills and
// killed/minecraft:cow. vanilla: ServerPlayer.awardKillScore,
// Player.killedEntity
func TestAMobKillIsCounted(t *testing.T) {
	t.Parallel()
	cfg, _ := servertest.ExperienceSetup(t)
	srv, c, _ := servertest.JoinAs(t, cfg, "Butcher")
	srv.GiveSlot("Butcher", player.HotbarSlot,
		servertest.EnchantedStack(t, "minecraft:netherite_sword", "minecraft:sharpness", 3))
	srv.SpawnMob("minecraft:cow", 1.5, 64, 0.5)
	cow := c.ExpectMobSpawn(protocol.EntityTypeCow)
	servertest.AwaitFullSwing(t, srv, "Butcher")
	c.Attack(cow.EntityID)

	typeID, cowID, _ := registry.StatID(registry.StatKilled, "minecraft:cow")
	seen := map[statKey]int32{}
	servertest.PollUntil(t, 10*time.Second, func() bool {
		maps.Copy(seen, requestStats(c))
		return seen[customStat(t, "minecraft:mob_kills")] > 0
	})
	if n := seen[customStat(t, "minecraft:mob_kills")]; n != 1 {
		t.Errorf("mob_kills = %d, want 1", n)
	}
	if n := seen[statKey{typeID, cowID}]; n != 1 {
		t.Errorf("killed cow = %d, want 1", n)
	}
}

// TestAKillCommandAfterAPunchCountsNoKilledStat punches a cow and then
// kills it with /kill: the player is still the last to have hurt it, so
// mob_kills counts, but the killing blow was nobody's, so killed/cow stays
// zero. vanilla: LivingEntity.die, ServerPlayer.awardKillScore,
// Player.killedEntity
func TestAKillCommandAfterAPunchCountsNoKilledStat(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.SurvivalConfig(), "Puncher")
	srv.SpawnMob("minecraft:cow", 1.5, 64, 0.5)
	cow := c.ExpectMobSpawn(protocol.EntityTypeCow)
	servertest.AwaitFullSwing(t, srv, "Puncher")
	c.Attack(cow.EntityID)
	c.Settle()
	if !srv.KillMob(cow.EntityID) {
		t.Fatal("the cow did not answer /kill")
	}

	typeID, cowID, _ := registry.StatID(registry.StatKilled, "minecraft:cow")
	seen := map[statKey]int32{}
	servertest.PollUntil(t, 10*time.Second, func() bool {
		maps.Copy(seen, requestStats(c))
		return seen[customStat(t, "minecraft:mob_kills")] > 0
	})
	if n := seen[customStat(t, "minecraft:mob_kills")]; n != 1 {
		t.Errorf("mob_kills = %d, want 1", n)
	}
	if n := seen[statKey{typeID, cowID}]; n != 0 {
		t.Errorf("killed cow = %d, want 0", n)
	}
}

// TestAVanillaStatsFileIsReadAndKept joins over a stats file a vanilla
// server wrote: its counters come back on Request Stats, one under a name
// this version does not have is dropped, a file older than the
// play_one_minute rename has it read as play_time, and the save writes the
// rest back.
// vanilla: ServerStatsCounter.parse, StatsRenameFix
func TestAVanillaStatsFileIsReadAndKept(t *testing.T) {
	t.Parallel()
	cfg := servertest.SurvivalConfig()
	cfg.LevelName = t.TempDir()
	writeStatsFile(t, cfg.LevelName, "Veteran", `{
  "stats": {
    "minecraft:killed": {"minecraft:zombie": 3},
    "minecraft:custom": {"minecraft:jump": 7, "minecraft:play_one_minute": 5000},
    "minecraft:mined": {"minecraft:no_such_block": 9}
  },
  "DataVersion": 2584
}`)
	_, c := servertest.Join(t, cfg, "Veteran")
	got := requestStats(c)
	typeID, zombieID, _ := registry.StatID(registry.StatKilled, "minecraft:zombie")
	if n := got[statKey{typeID, zombieID}]; n != 3 {
		t.Errorf("killed zombie = %d, want the file's 3", n)
	}
	if n := got[customStat(t, "minecraft:jump")]; n != 7 {
		t.Errorf("jump = %d, want the file's 7", n)
	}
	if n := got[customStat(t, "minecraft:play_time")]; n < 5000 {
		t.Errorf("play_time = %d, want at least the file's play_one_minute, 5000", n)
	}
	c.Disconnect()

	stats, version := statsFileOf(t, cfg.LevelName, "Veteran")
	if n := stats["minecraft:killed"]["minecraft:zombie"]; n != 3 {
		t.Errorf("saved killed zombie = %d, want 3", n)
	}
	if _, ok := stats["minecraft:mined"]["minecraft:no_such_block"]; ok {
		t.Error("the save kept a counter for a block this version does not have")
	}
	if _, ok := stats["minecraft:custom"]["minecraft:play_one_minute"]; ok {
		t.Error("the save kept play_one_minute under its old name")
	}
	if version != 5023 {
		t.Errorf("saved DataVersion = %d, want 5023", version)
	}
}

// craftedPlanks runs act on a player-inventory window whose grid holds
// logs oak logs in the first cell, then reads crafted/oak_planks.
func craftedPlanks(t *testing.T, logs byte, act func(w *servertest.WindowedClient)) int32 {
	t.Helper()
	_, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:oak_log", int32(logs)))
	w.Click(36, 0, protocol.ClickModePickup)
	w.Click(1, 0, protocol.ClickModePickup)
	w.SettleTrackingState()
	act(w)
	w.SettleTrackingState()
	got := requestStats(w.Client)
	typeID, keyID, _ := registry.StatID(registry.StatCrafted, "minecraft:oak_planks")
	return got[statKey{typeID, keyID}]
}

// TestCraftedPlanksAreCountedPerPath asserts each way of taking a crafting
// result on its own.
// vanilla: ResultSlot.checkTakeAchievements, ItemStack.onCraftedBy
func TestCraftedPlanksAreCountedPerPath(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		logs byte
		act  func(w *servertest.WindowedClient)
		want int32
	}{
		{"click", 1, func(w *servertest.WindowedClient) { w.Click(0, 0, protocol.ClickModePickup) }, 4},
		// vanilla: AbstractContainerMenu.doClick asks for (count+1)/2.
		{"right click", 1, func(w *servertest.WindowedClient) { w.Click(0, 1, protocol.ClickModePickup) }, 2},
		{"right click holding the same item", 1, func(w *servertest.WindowedClient) {
			w.SetCreativeSlot(37, servertest.ItemStack(t, "minecraft:oak_planks", 4))
			w.Click(37, 0, protocol.ClickModePickup)
			w.Click(0, 1, protocol.ClickModePickup)
		}, 4},
		{"number key", 1, func(w *servertest.WindowedClient) { w.Click(0, 3, protocol.ClickModeSwap) }, 4},
		{"shift click", 2, func(w *servertest.WindowedClient) { w.Click(0, 0, protocol.ClickModeQuickMove) }, 8},
		{"Q", 2, func(w *servertest.WindowedClient) { w.Click(0, 0, protocol.ClickModeThrow) }, 1},
		{"Ctrl+Q", 2, func(w *servertest.WindowedClient) { w.Click(0, 1, protocol.ClickModeThrow) }, 8},
		{"shift click that half fits", 1, func(w *servertest.WindowedClient) {
			for slot := int16(9); slot <= 44; slot++ {
				stack := servertest.ItemStack(t, "minecraft:cobblestone", 64)
				if slot == 44 {
					stack = servertest.ItemStack(t, "minecraft:oak_planks", 62)
				}
				w.SetCreativeSlot(slot, stack)
			}
			w.SettleTrackingState()
			w.Click(0, 0, protocol.ClickModeQuickMove)
		}, 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := craftedPlanks(t, tt.logs, tt.act); got != tt.want {
				t.Errorf("crafted oak planks = %d, want %d", got, tt.want)
			}
		})
	}
}

// statOf is the count of one stat in an Award Statistics, zero when absent.
func statOf(t *testing.T, got map[statKey]int32, statType, key string) int32 {
	t.Helper()
	typeID, keyID, ok := registry.StatID(statType, key)
	if !ok {
		t.Fatalf("no stat %s / %s", statType, key)
	}
	return got[statKey{typeID, keyID}]
}

// TestSmeltedIngotsTakenFromAFurnaceAreCrafted takes a furnace's output and
// finds crafted/iron_ingot.
// vanilla: FurnaceResultSlot.checkTakeAchievements, ItemStack.onCraftedBy
func TestSmeltedIngotsTakenFromAFurnaceAreCrafted(t *testing.T) {
	t.Parallel()
	srv, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	servertest.StopTheLoop(t, srv)
	servertest.LoadFurnace(t, srv, w, "minecraft:raw_iron", 1, 1)
	for range 202 {
		srv.TickBlockEntities()
	}
	w.UseBlock(4, 64, 0)
	w.ExpectContainerOpen(protocol.MenuFurnace, true, 4, 64, 0)
	w.Click(2, 0, protocol.ClickModeQuickMove)
	w.SettleKeeping()
	srv.TickSurvival()
	if n := statOf(t, requestStats(w.Client), registry.StatCrafted, "minecraft:iron_ingot"); n != 1 {
		t.Errorf("crafted iron_ingot = %d, want 1", n)
	}
}

// TestAStonecutterTakeIsCrafted takes one cut and finds it counted.
// vanilla: StonecutterMenu result slot onTake, ItemStack.onCraftedBy
func TestAStonecutterTakeIsCrafted(t *testing.T) {
	t.Parallel()
	_, w := servertest.OpenWindowed(t, servertest.BlockTestConfig())
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:stonecutter", 1))
	w.PlaceBlock(4, 63, 0, protocol.FaceTop)
	w.ExpectBlockAck()
	w.UseBlock(4, 64, 0)
	w.ExpectContainerOpen(protocol.MenuStonecutter, false, 4, 64, 0)
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:stone", 3))
	w.Click(2+27, 0, protocol.ClickModePickup)
	w.Click(0, 0, protocol.ClickModePickup)
	w.ClickButton(0)
	w.SettleTrackingState()
	w.Click(1, 0, protocol.ClickModePickup)
	w.SettleTrackingState()
	if n := statOf(t, requestStats(w.Client), registry.StatCrafted, "minecraft:chiseled_stone_bricks"); n != 1 {
		t.Errorf("crafted chiseled_stone_bricks = %d, want 1", n)
	}
}

// TestAZombieKillIsCountedAsKilledBy lets a zombie land the fatal bite.
// vanilla: ServerPlayer.die
func TestAZombieKillIsCountedAsKilledBy(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.HostileTestConfig())
	srv.SetDifficulty(3)
	srv.SetDayTime(18000)
	c := connect()
	c.JoinAs("Prey")
	c.Settle()
	srv.SpawnMob("minecraft:zombie", 1.5, 64, 0.5)
	srv.SetHealth("Prey", 1)
	seen := false
	c.ReadTicksSkipping(srv, servertest.HostileTicks, nil, func(id int32, _ []byte) bool {
		seen = id == protocol.IDPlayCombatDeath
		return seen
	})
	if !seen {
		t.Fatal("the player never died")
	}
	if n := statOf(t, requestStats(c), registry.StatKilledBy, "minecraft:zombie"); n != 1 {
		t.Errorf("killed_by zombie = %d, want 1", n)
	}
}
