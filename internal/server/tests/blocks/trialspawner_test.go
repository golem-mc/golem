package blocks

import (
	"math"
	"slices"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

const trialX, trialY, trialZ = 4, 64, 2

// trialScene joins a player beside a trial spawner at (4,64,2) with a stone
// floor under the room, the loop held for the test to step. The spawner
// spawns zombies out of a config that keeps the numbers small.
func trialScene(t *testing.T, config nbt.Compound) (*server.Server, *servertest.Client) {
	t.Helper()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Trialist",
		servertest.OpsListing(t, "Trialist"), func(s *server.Server) { s.SetKeepAlive(time.Hour, time.Hour) })
	servertest.HoldTicks(t, srv)
	c.MoveToGround(2.5, 64, 5.5, true)
	stone := servertest.MustState(t, "minecraft:stone", nil)
	air := servertest.MustState(t, "minecraft:air", nil)
	for x := int32(-4); x <= 12; x++ {
		for z := int32(-6); z <= 10; z++ {
			srv.SetBlock(x, 63, z, stone)
			for y := int32(64); y <= 67; y++ {
				srv.SetBlock(x, y, z, air)
			}
		}
	}
	srv.SetDifficulty(2)
	srv.SetBlock(trialX, trialY, trialZ, trialState(t, "inactive"))
	srv.SetBlockEntityTagAt(trialX, trialY, trialZ, "minecraft:trial_spawner", config)
	return srv, c
}

func trialState(t *testing.T, state string) uint16 {
	t.Helper()
	return servertest.MustState(t, "minecraft:trial_spawner", map[string]string{"trial_spawner_state": state})
}

// trialConfig is a normal_config spawning zombies.
func trialConfig(total, simultaneous int32, ticksBetween int32) nbt.Compound {
	return nbt.Compound{
		"target_cooldown_length": nbt.Int(100),
		"normal_config": nbt.Compound{
			"total_mobs":                         nbt.Float(float32(total)),
			"simultaneous_mobs":                  nbt.Float(float32(simultaneous)),
			"total_mobs_added_per_player":        nbt.Float(0),
			"simultaneous_mobs_added_per_player": nbt.Float(0),
			"ticks_between_spawn":                nbt.Int(ticksBetween),
			"spawn_potentials": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{nbt.Compound{
				"weight": nbt.Int(1),
				"data":   nbt.Compound{"entity": nbt.Compound{"id": nbt.String("minecraft:zombie")}},
			}}},
		},
	}
}

func trialStateOf(t *testing.T, srv *server.Server) string {
	t.Helper()
	name, props, _ := registry.BlockStateName(srv.BlockAt(trialX, trialY, trialZ))
	if name != "minecraft:trial_spawner" {
		t.Fatalf("the block at the spawner is %s", name)
	}
	return registry.ParseStateProps(props)["trial_spawner_state"]
}

func trialTag(t *testing.T, srv *server.Server) nbt.Compound {
	t.Helper()
	tag, ok := srv.BlockEntityTagAt(trialX, trialY, trialZ)
	if !ok {
		t.Fatal("the trial spawner carries no data")
	}
	return tag
}

func trialListLen(tag nbt.Compound, key string) int {
	list, _ := nbt.Get[nbt.List](tag, key)
	return len(list.Items)
}

// TestATrialSpawnerRunsItsNormalCycle: a spawner with a mob goes from inactive
// to waiting for players, to active once a survival player is in range, spawns
// up to its simultaneous limit, ejects after the last mob dies and cools down
// back to waiting with its data reset.
// vanilla: TrialSpawnerState.tickAndGetNext
func TestATrialSpawnerRunsItsNormalCycle(t *testing.T) {
	t.Parallel()
	srv, _ := trialScene(t, trialConfig(2, 2, 2))

	seen := []string{"inactive"}
	maxAlive := 0
	for range 700 {
		servertest.RunTicks(t, srv, 1)
		if state := trialStateOf(t, srv); state != seen[len(seen)-1] {
			seen = append(seen, state)
		}
		maxAlive = max(maxAlive, srv.MobCount())
		if seen[len(seen)-1] == "active" && srv.MobCount() == 2 && trialListLen(trialTag(t, srv), "current_mobs") == 2 {
			srv.RunLine("kill @e[type=minecraft:zombie]")
		}
		if len(seen) == 7 {
			break
		}
	}
	want := []string{"inactive", "waiting_for_players", "active", "waiting_for_reward_ejection",
		"ejecting_reward", "cooldown", "waiting_for_players"}
	if !slices.Equal(seen, want) {
		t.Fatalf("states = %v, want %v", seen, want)
	}
	if maxAlive != 2 {
		t.Errorf("at most %d mobs stood at once, want the simultaneous limit 2", maxAlive)
	}
	tag := trialTag(t, srv)
	if n := trialListLen(tag, "registered_players"); n != 0 {
		t.Errorf("registered_players = %d after the cooldown, want it reset", n)
	}
	if total, _ := nbt.Get[nbt.Int](tag, "total_mobs_spawned"); total != 0 {
		t.Errorf("total_mobs_spawned = %d after the cooldown, want 0", total)
	}
	if _, has := tag["spawn_data"]; has && trialListLen(tag, "current_mobs") != 0 {
		t.Errorf("current_mobs = %d after the cooldown, want none", trialListLen(tag, "current_mobs"))
	}
}

// TestATrialSpawnerStopsAtItsTotal: with a total of three mobs and one at a
// time, the spawner stands exactly three up, never two together, and the
// state data tracks them.
// vanilla: TrialSpawnerStateData.hasFinishedSpawningAllMobs, isReadyToSpawnNextMob
func TestATrialSpawnerStopsAtItsTotal(t *testing.T) {
	t.Parallel()
	srv, _ := trialScene(t, trialConfig(3, 1, 2))

	seenIDs := map[int32]bool{}
	for range 400 {
		servertest.RunTicks(t, srv, 1)
		if srv.MobCount() > 1 {
			t.Fatalf("%d mobs stand at once, want the simultaneous limit 1", srv.MobCount())
		}
		for _, m := range srv.Mobs() {
			if !seenIDs[m.ID] {
				seenIDs[m.ID] = true
				tag := trialTag(t, srv)
				if got := trialListLen(tag, "current_mobs"); got != 1 {
					t.Errorf("current_mobs = %d with a mob standing, want 1", got)
				}
				if got := trialListLen(tag, "registered_players"); got != 1 {
					t.Errorf("registered_players = %d, want the one player", got)
				}
				srv.RunLine("kill @e[type=minecraft:zombie]")
			}
		}
		if trialStateOf(t, srv) == "waiting_for_reward_ejection" {
			break
		}
	}
	if len(seenIDs) != 3 {
		t.Errorf("the spawner stood %d mobs up, want its total of 3", len(seenIDs))
	}
}

// setTrialistMode runs /gamemode and settles, so the mode applies before any
// tick runs.
func setTrialistMode(t *testing.T, srv *server.Server, c *servertest.Client, name string, mode int32) {
	t.Helper()
	c.ChatCommand("gamemode " + name)
	c.Settle()
	if got := srv.PlayerGameMode("Trialist"); got != mode {
		t.Fatalf("game mode = %d after /gamemode %s, want %d", got, name, mode)
	}
}

// TestATrialSpawnerIgnoresCreativePlayers: a creative player in range does not
// wake the spawner, a survival one does.
// vanilla: PlayerDetector.NO_CREATIVE_PLAYERS
func TestATrialSpawnerIgnoresCreativePlayers(t *testing.T) {
	t.Parallel()
	srv, c := trialScene(t, trialConfig(1, 1, 2))
	setTrialistMode(t, srv, c, "creative", 1)
	servertest.RunTicks(t, srv, 60)
	if got := trialStateOf(t, srv); got != "waiting_for_players" {
		t.Fatalf("state = %s with a creative player in range, want waiting_for_players", got)
	}
	setTrialistMode(t, srv, c, "survival", 0)
	servertest.UntilTicks(t, srv, 60, "the spawner waking", func() bool {
		return trialStateOf(t, srv) == "active"
	})
}

// TestATrialSpawnerHoldsToItsSimultaneousLimit: with room for five mobs in
// all but two at a time, an unattended spawner keeps two standing and no more.
// vanilla: TrialSpawnerStateData.isReadyToSpawnNextMob
func TestATrialSpawnerHoldsToItsSimultaneousLimit(t *testing.T) {
	t.Parallel()
	srv, _ := trialScene(t, trialConfig(5, 2, 1))
	srv.RunLine("time set midnight")
	most := 0
	for range 100 {
		servertest.RunTicks(t, srv, 1)
		most = max(most, srv.MobCount())
		if got := trialListLen(trialTag(t, srv), "current_mobs"); got > 2 {
			t.Fatalf("%d mobs tracked, want at most the simultaneous limit 2", got)
		}
		if srv.MobCount() > 2 {
			t.Fatalf("%d mobs stand, want at most the simultaneous limit 2", srv.MobCount())
		}
	}
	if most != 2 {
		t.Errorf("at most %d mobs stood together, want the limit of 2 reached", most)
	}
}

// trialLootList is a loot_tables_to_eject list of equally weighted tables.
func trialLootList(tables ...string) nbt.List {
	items := make([]nbt.Tag, len(tables))
	for i, table := range tables {
		items[i] = nbt.Compound{"data": nbt.String(table), "weight": nbt.Int(1)}
	}
	return nbt.List{Elem: nbt.TypeCompound, Items: items}
}

// playTrialToCooldown kills each mob as it appears and steps until the
// spawner reaches the cooldown, reporting the world events the player heard.
func playTrialToCooldown(t *testing.T, srv *server.Server, c *servertest.Client) servertest.ToolHeard {
	t.Helper()
	servertest.UntilTicks(t, srv, 800, "the spawner reaching its cooldown", func() bool {
		if srv.MobCount() > 0 {
			srv.RunLine("kill @e[type=minecraft:zombie]")
		}
		return trialStateOf(t, srv) == "cooldown"
	})
	return servertest.HearTool(t, c)
}

func trialItems(srv *server.Server, name string) []server.TestItem {
	var out []server.TestItem
	for _, it := range srv.ItemsInDimension("minecraft:overworld") {
		if it.Item == name {
			out = append(out, it)
		}
	}
	return out
}

// TestATrialSpawnerEjectsItsRewardLoot: the ejecting state rolls one of the
// configured loot tables for the registered player, throws the items up out of
// the block and broadcasts level event 3014.
// vanilla: TrialSpawner.ejectReward
func TestATrialSpawnerEjectsItsRewardLoot(t *testing.T) {
	t.Parallel()
	config := trialConfig(1, 1, 2)
	config["normal_config"].(nbt.Compound)["loot_tables_to_eject"] = trialLootList("minecraft:spawners/trial_chamber/key")
	srv, c := trialScene(t, config)

	var thrown server.TestItem
	servertest.UntilTicks(t, srv, 800, "the key being ejected", func() bool {
		if srv.MobCount() > 0 {
			srv.RunLine("kill @e[type=minecraft:zombie]")
		}
		if keys := trialItems(srv, "minecraft:trial_key"); len(keys) > 0 {
			thrown = keys[0]
			return true
		}
		return false
	})
	if thrown.Y < trialY+1 || thrown.VY <= 0 || math.Abs(thrown.VX) > 0.1 || math.Abs(thrown.VZ) > 0.1 ||
		math.Abs(thrown.X-(trialX+0.5)) > 0.1 || math.Abs(thrown.Z-(trialZ+0.5)) > 0.1 {
		t.Errorf("key at (%v,%v,%v) moving (%v,%v,%v), want it thrown up from the top of the block",
			thrown.X, thrown.Y, thrown.Z, thrown.VX, thrown.VY, thrown.VZ)
	}
	if thrown.PickupDelay != 0 {
		t.Errorf("reward pickup delay = %d, want 0", thrown.PickupDelay)
	}
	heard := playTrialToCooldown(t, srv, c)
	if keys := trialItems(srv, "minecraft:trial_key"); len(keys) != 1 {
		t.Fatalf("%d trial keys ejected, want one for the one player", len(keys))
	}
	if got := len(heard.Events[3014]); got != 1 {
		t.Errorf("%d level events 3014, want 1", got)
	}
	if loot, _ := nbt.Get[nbt.String](trialTag(t, srv), "ejecting_loot_table"); loot != "" {
		t.Errorf("ejecting_loot_table = %q in the cooldown, want it cleared", loot)
	}
}

// ejectingTrialConfig is a spawner already in the ejecting state with two
// registered players, the reward tables set to tables.
func ejectingTrialConfig(tables ...string) nbt.Compound {
	config := trialConfig(1, 1, 2)
	config["normal_config"].(nbt.Compound)["loot_tables_to_eject"] = trialLootList(tables...)
	config["cooldown_ends_at"] = nbt.Long(100)
	config["registered_players"] = nbt.List{Elem: nbt.TypeIntArray, Items: []nbt.Tag{
		nbt.IntArray{1, 2, 3, 4}, nbt.IntArray{5, 6, 7, 8}}}
	return config
}

// TestATrialSpawnerEjectsOncePerPlayerEveryThirtyTicks: each ejection rolls
// the kept table once for the first registered player and removes that
// player; the next one follows 30 ticks later, and the spawner cools down
// 30 ticks after the last. With no tables the players still leave on the same
// schedule but nothing drops.
// vanilla: TrialSpawnerState.EJECTING_REWARD, TrialSpawnerStateData.ejectResultItemsAndPlayerCount
func TestATrialSpawnerEjectsOncePerPlayerEveryThirtyTicks(t *testing.T) {
	t.Parallel()
	const key = "minecraft:spawners/trial_chamber/key"
	for _, tc := range []struct {
		name   string
		tables []string
		keys   [3]int
	}{
		{"with a table", []string{key}, [3]int{1, 2, 2}},
		{"without tables", nil, [3]int{0, 0, 0}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			srv, _ := trialScene(t, ejectingTrialConfig(tc.tables...))
			srv.SetBlock(trialX, trialY, trialZ, trialState(t, "ejecting_reward"))
			players := func() int { return trialListLen(trialTag(t, srv), "registered_players") }

			servertest.UntilTicks(t, srv, 30, "the first ejection", func() bool { return players() == 1 })
			// Two keys thrown from the same spot merge into one stack.
			keys := func() int {
				n := 0
				for _, it := range trialItems(srv, "minecraft:trial_key") {
					n += int(it.Count)
				}
				return n
			}
			if got := keys(); got != tc.keys[0] {
				t.Errorf("%d keys after the first ejection, want %d", got, tc.keys[0])
			}
			if loot, _ := nbt.Get[nbt.String](trialTag(t, srv), "ejecting_loot_table"); (loot != "") != (len(tc.tables) > 0) {
				t.Errorf("ejecting_loot_table = %q after the first roll, want it kept only with a table", loot)
			}
			servertest.RunTicks(t, srv, 29)
			if got := players(); got != 1 || keys() != tc.keys[0] {
				t.Errorf("%d players and %d keys 29 ticks after the first ejection, want 1 and %d", got, keys(), tc.keys[0])
			}
			servertest.RunTicks(t, srv, 1)
			if got := players(); got != 0 || keys() != tc.keys[1] {
				t.Errorf("%d players and %d keys 30 ticks after the first ejection, want 0 and %d", got, keys(), tc.keys[1])
			}
			servertest.RunTicks(t, srv, 29)
			if got := trialStateOf(t, srv); got != "ejecting_reward" {
				t.Errorf("state %s 29 ticks after the last ejection, want ejecting_reward", got)
			}
			servertest.RunTicks(t, srv, 1)
			if got := trialStateOf(t, srv); got != "cooldown" || keys() != tc.keys[2] {
				t.Errorf("state %s with %d keys 30 ticks after the last ejection, want cooldown and %d", got, keys(), tc.keys[2])
			}
		})
	}
}

// equipmentTrialConfig spawns up to eight zombies at once, each dressed from
// the equipment table with the given slot drop chances.
func equipmentTrialConfig(table string, chances nbt.Tag) nbt.Compound {
	config := trialConfig(8, 8, 2)
	potential := config["normal_config"].(nbt.Compound)["spawn_potentials"].(nbt.List).Items[0].(nbt.Compound)
	potential["data"].(nbt.Compound)["equipment"] = nbt.Compound{
		"loot_table": nbt.String(table), "slot_drop_chances": chances}
	return config
}

// trialGear spawns the zombies of a config and returns the gear of each.
func trialGear(t *testing.T, config nbt.Compound) (gear [][6]protocol.ItemStack, chances [][6]float32) {
	t.Helper()
	srv, _ := trialScene(t, config)
	srv.RunLine("time set midnight")
	servertest.UntilTicks(t, srv, 400, "eight zombies", func() bool { return srv.MobCount() >= 8 })
	for _, m := range srv.Mobs() {
		g, c := srv.MobGear(m.ID)
		gear, chances = append(gear, g), append(chances, c)
	}
	return gear, chances
}

// TestATrialSpawnerDressesItsMobsFromTheEquipmentTable: the melee table gives
// every zombie an iron or diamond sword, the armour that rolled is trimmed
// and enchanted, and the slot drop chances land on the slots that were filled.
// vanilla: TrialSpawner.spawnMob, EquipmentUser.equip
func TestATrialSpawnerDressesItsMobsFromTheEquipmentTable(t *testing.T) {
	t.Parallel()
	gear, chances := trialGear(t, equipmentTrialConfig("minecraft:equipment/trial_chamber_melee",
		nbt.Compound{"mainhand": nbt.Float(0.25), "head": nbt.Float(0.5)}))
	swords := []int32{servertest.MustItemID(t, "minecraft:iron_sword"), servertest.MustItemID(t, "minecraft:diamond_sword")}
	armoured := 0
	for i, g := range gear {
		if !slices.Contains(swords, g[0].ItemID) || g[0].Count != 1 {
			t.Errorf("mob %d holds item %d x%d, want an iron or diamond sword", i, g[0].ItemID, g[0].Count)
		}
		if chances[i][0] != 0.25 || chances[i][1] != 0.085 {
			t.Errorf("mob %d mainhand/offhand drop chances %v/%v, want 0.25/0.085", i, chances[i][0], chances[i][1])
		}
		for slot := 2; slot < 6; slot++ {
			piece := g[slot]
			if piece.Empty() {
				continue
			}
			armoured++
			if _, _, ok := piece.TrimOf(); !ok || piece.Count != 1 || len(piece.Enchantments) != 3 {
				t.Errorf("mob %d slot %d: trim %v, count %d, %d enchantments, want a trimmed single piece with three enchantments",
					i, slot, ok, piece.Count, len(piece.Enchantments))
			}
			if want := map[int]float32{2: 0.085, 3: 0.085, 4: 0.085, 5: 0.5}[slot]; chances[i][slot] != want {
				t.Errorf("mob %d slot %d drop chance %v, want %v", i, slot, chances[i][slot], want)
			}
		}
	}
	if armoured == 0 {
		t.Error("no zombie wore any armour, want some of the 50% pieces to roll")
	}
}
