package blocks

import (
	"maps"
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// ominousTrialConfig is trialConfig with an ominous_config spawning skeletons,
// the item table a plain item the test can find, and no item spawner dropping
// anything else.
func ominousTrialConfig(total, simultaneous, ticksBetween int32) nbt.Compound {
	config := trialConfig(total, simultaneous, ticksBetween)
	ominous := maps.Clone(config["normal_config"].(nbt.Compound))
	ominous["spawn_potentials"] = nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{nbt.Compound{
		"weight": nbt.Int(1),
		"data":   nbt.Compound{"entity": nbt.Compound{"id": nbt.String("minecraft:skeleton")}},
	}}}
	ominous["items_to_drop_when_ominous"] = nbt.String("minecraft:spawners/trial_chamber/key")
	config["ominous_config"] = ominous
	return config
}

func trialOminousOf(t *testing.T, srv *server.Server) bool {
	t.Helper()
	_, props, _ := registry.BlockStateName(srv.BlockAt(trialX, trialY, trialZ))
	return registry.ParseStateProps(props)["ominous"] == "true"
}

// heardAll collects every world event and sound name, whatever the category.
type heardAll struct {
	events map[int32][][4]int32
	sounds map[string]int
}

func hearAll(t *testing.T, c *servertest.Client) heardAll {
	t.Helper()
	h := heardAll{events: map[int32][][4]int32{}, sounds: map[string]int{}}
	c.SettleAndCollect(func(id int32, data []byte) {
		r := protocol.NewReader(data)
		switch id {
		case protocol.IDPlaySoundEffect:
			if sid := r.ReadVarInt(); sid > 0 {
				if name, ok := servertest.SoundEventName(sid - 1); ok {
					h.sounds[name]++
				}
			}
		case protocol.IDPlayWorldEvent:
			ev := r.ReadI32()
			x, y, z := r.ReadPosition()
			h.events[ev] = append(h.events[ev], [4]int32{x, y, z, r.ReadI32()})
		}
	})
	return h
}

// untilOminous steps until the spawner block is ominous.
func untilOminous(t *testing.T, srv *server.Server) {
	t.Helper()
	servertest.UntilTicks(t, srv, 400, "the spawner turning ominous", func() bool { return trialOminousOf(t, srv) })
}

// TestABadOmenTurnsATrialSpawnerOminous: a player with bad omen in sight of a
// spawner turns it ominous, and the omen becomes trial omen lasting 18000
// ticks a level.
// vanilla: TrialSpawnerStateData.tryDetectPlayers, transformBadOmenIntoTrialOmen
func TestABadOmenTurnsATrialSpawnerOminous(t *testing.T) {
	t.Parallel()
	srv, c := trialScene(t, ominousTrialConfig(2, 2, 2))
	srv.GiveEffect("Trialist", "minecraft:bad_omen", 100000, 1, false)
	untilOminous(t, srv)

	if _, _, ok := srv.PlayerEffect("Trialist", "minecraft:bad_omen"); ok {
		t.Errorf("the bad omen stayed after the spawner took it")
	}
	duration, amplifier, ok := srv.PlayerEffect("Trialist", "minecraft:trial_omen")
	if !ok || amplifier != 0 || duration < 2*18000-100 || duration > 2*18000 {
		t.Errorf("trial omen = %d ticks level %d (present %v), want 36000 at level 0", duration, amplifier, ok)
	}
	heard := hearAll(t, c)
	if got := heard.events[3020]; len(got) != 2 || got[0][3] != 0 || got[1][3] != 1 ||
		got[1][0] != trialX || got[1][1] != trialY || got[1][2] != trialZ {
		t.Errorf("level events 3020 = %v, want the player's with 0, then the spawner's with 1", got)
	}
	if got := len(heard.events[3013]); got != 0 {
		t.Errorf("%d detect events 3013 for the turn, want none", got)
	}
}

// TestATrialOmenKeepsItsDurationWhenTheSpawnerTurnsOminous: a player already
// under trial omen turns the spawner ominous without the effect changing.
// vanilla: TrialSpawnerStateData.findPlayerWithOminousEffect
func TestATrialOmenKeepsItsDurationWhenTheSpawnerTurnsOminous(t *testing.T) {
	t.Parallel()
	srv, _ := trialScene(t, ominousTrialConfig(2, 2, 2))
	srv.GiveEffect("Trialist", "minecraft:trial_omen", 500, 0, false)
	untilOminous(t, srv)
	if duration, _, ok := srv.PlayerEffect("Trialist", "minecraft:trial_omen"); !ok || duration > 500 {
		t.Errorf("trial omen = %d (present %v), want the 500 it had", duration, ok)
	}
}

// TestACreativePlayerWithOmenLeavesASpawnerNormal: the omen of a player the
// detector ignores does not turn the spawner.
// vanilla: PlayerDetector.NO_CREATIVE_PLAYERS
func TestACreativePlayerWithOmenLeavesASpawnerNormal(t *testing.T) {
	t.Parallel()
	srv, c := trialScene(t, ominousTrialConfig(2, 2, 2))
	setTrialistMode(t, srv, c, "creative", 1)
	srv.GiveEffect("Trialist", "minecraft:bad_omen", 100000, 0, false)
	servertest.RunTicks(t, srv, 100)
	if trialOminousOf(t, srv) {
		t.Fatal("the spawner turned ominous for a creative player")
	}
}

// TestAnOminousTrialSpawnerSpawnsFromItsOminousConfig: turning ominous
// discards the mobs of the normal mode, clears the spawn data and starts the
// ominous configuration's own mobs, flamed with the soul fire flag.
// vanilla: TrialSpawnerStateData.resetAfterBecomingOminous, TrialSpawner.spawnMob
func TestAnOminousTrialSpawnerSpawnsFromItsOminousConfig(t *testing.T) {
	t.Parallel()
	srv, c := trialScene(t, ominousTrialConfig(2, 2, 2))
	servertest.UntilTicks(t, srv, 400, "a normal zombie", func() bool { return srv.MobCount() > 0 })
	for _, m := range srv.Mobs() {
		if m.Kind != "minecraft:zombie" {
			t.Fatalf("a %s stood before the omen, want zombies", m.Kind)
		}
	}
	hearAll(t, c)
	srv.GiveEffect("Trialist", "minecraft:bad_omen", 100000, 0, false)
	untilOminous(t, srv)
	servertest.UntilTicks(t, srv, 400, "a skeleton", func() bool {
		for _, m := range srv.Mobs() {
			if m.Kind == "minecraft:skeleton" {
				return true
			}
		}
		return false
	})
	servertest.RunTicks(t, srv, 3)
	for _, m := range srv.Mobs() {
		if m.Kind == "minecraft:zombie" {
			t.Errorf("a zombie of the normal mode stood on after the spawner turned ominous")
		}
	}
	heard := hearAll(t, c)
	flamed := 0
	for _, e := range heard.events[3011] {
		if e[3] == 1 {
			flamed++
		}
	}
	if flamed == 0 {
		t.Errorf("no spawn event 3011 carried the ominous flame, got %v", heard.events[3011])
	}
}

// TestAnOminousTrialSpawnerDropsItsItemsThroughAnItemSpawner: once the item
// period has passed, an ominous item spawner appears above a player or a mob,
// warns, and lets the loot item fall as an item entity.
// vanilla: TrialSpawnerState.spawnOminousOminousItemSpawner, OminousItemSpawner.tickServer
func TestAnOminousTrialSpawnerDropsItsItemsThroughAnItemSpawner(t *testing.T) {
	t.Parallel()
	config := ominousTrialConfig(100, 1, 1000)
	config["normal_config"].(nbt.Compound)["ticks_between_spawn"] = nbt.Int(1000)
	srv, c := trialScene(t, config)
	srv.GiveEffect("Trialist", "minecraft:trial_omen", 100000, 0, false)
	untilOminous(t, srv)
	hearAll(t, c)

	// The first item spawner waits out the 160 ticks the turn started.
	var tag nbt.Compound
	servertest.UntilTicks(t, srv, 400, "an ominous item spawner", func() bool {
		tag = srv.FixtureSavedTag()
		return tag != nil
	})
	if id, _ := nbt.Get[nbt.String](tag, "id"); id != "minecraft:ominous_item_spawner" {
		t.Fatalf("the fixture is %q, want an ominous item spawner", id)
	}
	after, _ := nbt.LongValue(tag["spawn_item_after_ticks"])
	if after < 60 || after > 120 {
		t.Errorf("spawn_item_after_ticks = %d, want 60..120", after)
	}
	item, _ := nbt.Get[nbt.Compound](tag, "item")
	if id, _ := nbt.Get[nbt.String](item, "id"); id != "minecraft:trial_key" {
		t.Errorf("the spawner holds %q, want the trial key of the item table", id)
	}
	servertest.UntilTicks(t, srv, 200, "the key dropping", func() bool { return len(trialItems(srv, "minecraft:trial_key")) > 0 })
	heard := hearAll(t, c)
	if heard.sounds["minecraft:block.trial_spawner.about_to_spawn_item"] != 1 {
		t.Errorf("warning sounds = %d, want 1", heard.sounds["minecraft:block.trial_spawner.about_to_spawn_item"])
	}
	if heard.sounds["minecraft:block.trial_spawner.spawn_item_begin"] != 1 {
		t.Errorf("begin sounds = %d, want 1", heard.sounds["minecraft:block.trial_spawner.spawn_item_begin"])
	}
	if len(heard.events[3021]) != 1 {
		t.Errorf("%d level events 3021, want 1", len(heard.events[3021]))
	}
	if srv.FixtureCount() != 0 {
		t.Errorf("%d fixtures stand after the drop, want the spawner gone", srv.FixtureCount())
	}
}

// TestAnOminousTrialSpawnerWaitsOutTheItemPeriod: no item spawner appears in
// the 160 ticks after the turn.
// vanilla: TrialSpawnerStateData.resetAfterBecomingOminous
func TestAnOminousTrialSpawnerWaitsOutTheItemPeriod(t *testing.T) {
	t.Parallel()
	srv, _ := trialScene(t, ominousTrialConfig(100, 1, 1000))
	srv.GiveEffect("Trialist", "minecraft:trial_omen", 100000, 0, false)
	untilOminous(t, srv)
	servertest.RunTicks(t, srv, 150)
	if n := srv.FixtureCount(); n != 0 {
		t.Fatalf("%d item spawners stood within the first 150 ticks, want none", n)
	}
}

// TestAnOminousItemSpawnerShootsAProjectileItemDown: an arrow in the spawner
// is shot, not dropped.
// vanilla: OminousItemSpawner.spawnProjectile
func TestAnOminousItemSpawnerShootsAProjectileItemDown(t *testing.T) {
	t.Parallel()
	srv, _ := trialScene(t, trialConfig(2, 2, 2))
	srv.RunLine("summon minecraft:ominous_item_spawner 4.5 66 4.5 {item:{id:\"minecraft:arrow\",count:1},spawn_item_after_ticks:3L}")
	if srv.FixtureCount() != 1 {
		t.Fatalf("%d fixtures after /summon, want the item spawner", srv.FixtureCount())
	}
	srv.TickOminousItemSpawners(2)
	if srv.FixtureCount() != 1 || srv.ArrowCount() != 0 {
		t.Fatalf("the spawner dropped early: %d fixtures, %d arrows", srv.FixtureCount(), srv.ArrowCount())
	}
	srv.TickOminousItemSpawners(1)
	if srv.FixtureCount() != 0 || srv.ArrowCount() != 1 {
		t.Fatalf("after its ticks: %d fixtures, %d arrows, want the arrow shot", srv.FixtureCount(), srv.ArrowCount())
	}
}

// TestAnOminousItemSpawnerFiresChargesDown: a fire charge and a wind charge
// leave the spawner as projectiles, not items.
// vanilla: OminousItemSpawner.spawnItem, FireChargeItem.asProjectile, WindChargeItem.asProjectile
func TestAnOminousItemSpawnerFiresChargesDown(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		item, kind string
		event      int32
	}{
		{"minecraft:fire_charge", "minecraft:small_fireball", 1018},
		{"minecraft:wind_charge", "minecraft:wind_charge", 1051},
	} {
		t.Run(tc.item, func(t *testing.T) {
			t.Parallel()
			srv, c := trialScene(t, trialConfig(2, 2, 2))
			hearAll(t, c)
			srv.RunLine("summon minecraft:ominous_item_spawner 4.5 66 4.5 {item:{id:\"" + tc.item + "\",count:1},spawn_item_after_ticks:1L}")
			srv.TickOminousItemSpawners(1)
			heard := hearAll(t, c)
			if got := heard.events[tc.event]; len(got) != 1 || got[0] != [4]int32{4, 66, 4, 0} {
				t.Errorf("dispense events %d = %v, want one at the spawner's block with data 0", tc.event, got)
			}
			if len(heard.events[3021]) != 1 {
				t.Errorf("%d level events 3021, want 1", len(heard.events[3021]))
			}
			var kinds []string
			for _, th := range srv.ThrownInDimension("minecraft:overworld") {
				kinds = append(kinds, th.Kind)
				if th.Y != 66 {
					t.Errorf("the %s starts at y=%v, want the spawner's 66", tc.kind, th.Y)
				}
			}
			if !slices.Equal(kinds, []string{tc.kind}) {
				t.Errorf("projectiles = %v, want one %s", kinds, tc.kind)
			}
			if items := srv.ItemsInDimension("minecraft:overworld"); len(items) != 0 {
				t.Errorf("%d item entities, want the charge fired", len(items))
			}
		})
	}
}

// TestAnOminousItemSpawnerDropsAnItemWithNoPickupDelay: a plain item is
// created with the bare ItemEntity constructor.
// vanilla: OminousItemSpawner.spawnItem, ItemEntity.<init>
func TestAnOminousItemSpawnerDropsAnItemWithNoPickupDelay(t *testing.T) {
	t.Parallel()
	srv, _ := trialScene(t, trialConfig(2, 2, 2))
	srv.RunLine("summon minecraft:ominous_item_spawner 4.5 66 4.5 {item:{id:\"minecraft:trial_key\",count:1},spawn_item_after_ticks:1L}")
	srv.TickOminousItemSpawners(1)
	items := trialItems(srv, "minecraft:trial_key")
	if len(items) != 1 {
		t.Fatalf("%d keys dropped, want 1", len(items))
	}
	if items[0].PickupDelay != 0 {
		t.Errorf("pickup delay = %d, want 0", items[0].PickupDelay)
	}
}

// TestAnOminousTrialSpawnerRollsItsLootOnce: the item table is rolled when
// the spawner first needs it, not again on every tick it stays ominous.
// vanilla: TrialSpawnerStateData.getDispensingItems
func TestAnOminousTrialSpawnerRollsItsLootOnce(t *testing.T) {
	t.Parallel()
	srv, _ := trialScene(t, ominousTrialConfig(100, 1, 1000))
	srv.GiveEffect("Trialist", "minecraft:trial_omen", 100000, 0, false)
	untilOminous(t, srv)
	servertest.RunTicks(t, srv, 50)
	if n := srv.OminousLootRolls(); n != 1 {
		t.Errorf("%d loot rolls over 50 ominous ticks, want 1", n)
	}
}

// TestAnOminousTrialSpawnerEndsItsOmenAfterTheCooldown: the ominous property
// goes when the cooldown ends.
// vanilla: TrialSpawnerState.COOLDOWN, TrialSpawner.removeOminous
func TestAnOminousTrialSpawnerEndsItsOmenAfterTheCooldown(t *testing.T) {
	t.Parallel()
	config := ominousTrialConfig(2, 2, 2)
	config["cooldown_ends_at"] = nbt.Long(5)
	srv, c := trialScene(t, config)
	setTrialistMode(t, srv, c, "creative", 1)
	ominousCooldown := servertest.MustState(t, "minecraft:trial_spawner",
		map[string]string{"trial_spawner_state": "cooldown", "ominous": "true"})
	srv.SetBlock(trialX, trialY, trialZ, ominousCooldown)
	servertest.RunTicks(t, srv, 5)
	if trialOminousOf(t, srv) || trialStateOf(t, srv) != "waiting_for_players" {
		t.Fatalf("after the cooldown: state %s, ominous %v, want waiting_for_players and normal",
			trialStateOf(t, srv), trialOminousOf(t, srv))
	}
}

// TestAnOminousTrialSpawnerKeepsItsOmenThroughTheCooldownScan: a player in
// range does not restart an ominous spawner that is cooling down.
// vanilla: TrialSpawnerStateData.tryDetectPlayers
func TestAnOminousTrialSpawnerKeepsItsOmenThroughTheCooldownScan(t *testing.T) {
	t.Parallel()
	config := ominousTrialConfig(2, 2, 2)
	config["cooldown_ends_at"] = nbt.Long(100000)
	srv, _ := trialScene(t, config)
	srv.SetBlock(trialX, trialY, trialZ, servertest.MustState(t, "minecraft:trial_spawner",
		map[string]string{"trial_spawner_state": "cooldown", "ominous": "true"}))
	servertest.RunTicks(t, srv, 60)
	if got := trialStateOf(t, srv); got != "cooldown" || !trialOminousOf(t, srv) {
		t.Fatalf("state %s, ominous %v, want the ominous cooldown held", got, trialOminousOf(t, srv))
	}
}

// TestAnOminousItemSpawnerIsSavedWithItsItemAndDelay: the saved compound keeps
// what vanilla writes, and the spawner it was read back from drops its item.
// vanilla: OminousItemSpawner.addAdditionalSaveData, readAdditionalSaveData
func TestAnOminousItemSpawnerIsSavedWithItsItemAndDelay(t *testing.T) {
	t.Parallel()
	srv, _ := trialScene(t, trialConfig(2, 2, 2))
	srv.RunLine("summon minecraft:ominous_item_spawner 4.5 66 4.5 {item:{id:\"minecraft:trial_key\",count:1},spawn_item_after_ticks:4L}")
	srv.TickOminousItemSpawners(2)
	tag := srv.FixtureSavedTag()
	if after, _ := nbt.LongValue(tag["spawn_item_after_ticks"]); after != 4 {
		t.Errorf("spawn_item_after_ticks = %d, want 4", after)
	}
	item, _ := nbt.Get[nbt.Compound](tag, "item")
	if id, _ := nbt.Get[nbt.String](item, "id"); id != "minecraft:trial_key" {
		t.Errorf("item id = %q, want the trial key", id)
	}
	srv.TickOminousItemSpawners(2)
	if keys := trialItems(srv, "minecraft:trial_key"); len(keys) != 1 {
		t.Fatalf("%d keys after the delay, want 1", len(keys))
	}
}

// TestAnOminousTrialSpawnerSavesItsOminousData: the carried compound keeps the
// ominous configuration and the item period's end.
// vanilla: TrialSpawner.store
func TestAnOminousTrialSpawnerSavesItsOminousData(t *testing.T) {
	t.Parallel()
	srv, _ := trialScene(t, ominousTrialConfig(2, 2, 2))
	srv.GiveEffect("Trialist", "minecraft:trial_omen", 100000, 0, false)
	untilOminous(t, srv)
	tag := trialTag(t, srv)
	if cfg, ok := nbt.Get[nbt.Compound](tag, "ominous_config"); !ok || cfg["items_to_drop_when_ominous"] != nbt.String("minecraft:spawners/trial_chamber/key") {
		t.Errorf("ominous_config = %v, want the configured one kept", cfg)
	}
	if ends, _ := nbt.LongValue(tag["cooldown_ends_at"]); ends <= 0 {
		t.Errorf("cooldown_ends_at = %d, want the end of the item period", ends)
	}
	if !slices.Contains([]string{"waiting_for_players", "active"}, trialStateOf(t, srv)) {
		t.Errorf("state = %s", trialStateOf(t, srv))
	}
}
