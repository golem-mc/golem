package advancement

import (
	"encoding/json"
	"log/slog"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// node builds a displayed advancement with one criterion.
func node(id, parent string, hidden bool) registry.Advancement {
	return registry.Advancement{
		ID: id, Parent: parent,
		Display:      &registry.AdvancementDisplay{Title: id, Hidden: hidden},
		Criteria:     []registry.AdvancementCriterion{{Name: "c", Trigger: "minecraft:impossible"}},
		Requirements: [][]string{{"c"}},
	}
}

func TestLayoutCentresAParentOverItsChildren(t *testing.T) {
	tree := NewTree([]registry.Advancement{
		node("r", "", false),
		node("b", "r", false), node("b1", "b", false), node("b2", "b", false),
		node("a", "r", false),
	})
	want := map[string][2]float32{
		"r": {0, 0.5}, "a": {1, 0}, "b": {1, 1}, "b1": {2, 0.5}, "b2": {2, 1.5},
	}
	for id, xy := range want {
		if n := tree.Get(id); n.X != xy[0] || n.Y != xy[1] {
			t.Errorf("%s at (%v, %v), want (%v, %v)", id, n.X, n.Y, xy[0], xy[1])
		}
	}
}

func TestTheVanillaTreeLaysOutEveryTab(t *testing.T) {
	tree := Vanilla()
	if tree.Len() != len(registry.Advancements) {
		t.Fatalf("the tree holds %d advancements, want all %d", tree.Len(), len(registry.Advancements))
	}
	seen := map[[3]float32]string{}
	for _, n := range tree.nodes {
		if n.Root().Display == nil {
			continue // a tab never drawn (recipes/) has no layout
		}
		depth := 0
		for p := n.ParentNode; p != nil; p = p.ParentNode {
			depth++
		}
		if n.X != float32(depth) || n.Y < 0 {
			t.Errorf("%s at (%v, %v), want column %d", n.ID, n.X, n.Y, depth)
		}
		key := [3]float32{float32(n.Root().index), n.X, n.Y}
		if other, ok := seen[key]; ok {
			t.Errorf("%s and %s share (%v, %v)", n.ID, other, n.X, n.Y)
		}
		seen[key] = n.ID
	}
}

// shown flushes p and returns the IDs added.
func shown(t *testing.T, p *Progress) map[string]bool {
	t.Helper()
	u, _ := p.Flush()
	got := map[string]bool{}
	for _, n := range u.Added {
		got[n.ID] = true
	}
	return got
}

func TestADoneAdvancementShowsTwoLevelsBelowIt(t *testing.T) {
	tree := NewTree([]registry.Advancement{
		node("r", "", false), node("a", "r", false), node("b", "a", false),
		node("c", "b", false), node("h", "r", true),
	})
	p := NewProgress(tree)
	if u, ok := p.Flush(); ok || len(u.Added) != 0 {
		t.Fatalf("a fresh player's first flush sends %+v, want nothing", u)
	}
	p.Award(tree.Get("r"), "c", time.Unix(1, 0))
	got := shown(t, p)
	for id, want := range map[string]bool{"r": true, "a": true, "b": true, "c": false, "h": false} {
		if got[id] != want {
			t.Errorf("%s shown = %v, want %v", id, got[id], want)
		}
	}
	// A hidden one shows once done, and so do the ones above it.
	p.Award(tree.Get("h"), "c", time.Unix(2, 0))
	if got := shown(t, p); !got["h"] || len(got) != 1 {
		t.Errorf("after the hidden one is done, added %v, want h alone", got)
	}
}

// vanilla: InventoryChangeTrigger.TriggerInstance.matches
func TestInventoryChangedMatchesChangedStacks(t *testing.T) {
	for _, tc := range []struct {
		name     string
		items    [][]int32
		changed  []int32
		occupied int32
		want     bool
	}{
		{"unchanged", [][]int32{{1, 2}}, nil, 3, false},
		{"unrelated", [][]int32{{1, 2}}, []int32{3}, 3, false},
		{"matching", [][]int32{{1, 2}}, []int32{2}, 3, true},
		{"several changes", [][]int32{{1, 2}}, []int32{3, 2}, 3, true},
		{"slot bound", [][]int32{{1, 2}}, []int32{2}, 1, false},
		{"multiple predicates", [][]int32{{1, 2}, {3}}, []int32{4}, 3, true},
		{"no predicates on removal", nil, nil, 2, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tree := NewTree([]registry.Advancement{{
				ID: "r",
				Criteria: []registry.AdvancementCriterion{{
					Name: "items", Trigger: TriggerInventoryChanged, Items: tc.items,
					Occupied: registry.AdvancementRange{Min: 2, HasMin: true},
				}},
				Requirements: [][]string{{"items"}},
			}})
			p := NewProgress(tree)
			inv := Inventory{Held: []int32{2, 3, 4}, Changed: tc.changed, Occupied: tc.occupied}
			if got := p.InventoryChanged(inv, time.Now()); got != tc.want || p.Done(tree.Get("r")) != tc.want {
				t.Errorf("InventoryChanged(%+v) = %t, want %t", inv, got, tc.want)
			}
		})
	}
}

func TestInventoryChangedWantsEveryPredicate(t *testing.T) {
	tree := NewTree([]registry.Advancement{{
		ID:           "r",
		Display:      &registry.AdvancementDisplay{},
		Criteria:     []registry.AdvancementCriterion{{Name: "both", Trigger: TriggerInventoryChanged, Items: [][]int32{{1, 2}, {3}}}},
		Requirements: [][]string{{"both"}},
	}})
	p := NewProgress(tree)
	if p.InventoryChanged(Inventory{Held: []int32{2}}, time.Now()) {
		t.Error("one predicate of two passed the criterion")
	}
	if !p.InventoryChanged(Inventory{Held: []int32{2, 3}}, time.Now()) || !p.Done(tree.Get("r")) {
		t.Error("an item for each predicate did not pass the criterion")
	}
	if p.InventoryChanged(Inventory{Held: []int32{1, 3}}, time.Now()) {
		t.Error("an obtained criterion was awarded again")
	}
}

// TestInventoryChangedWantsTheSlotCounts passes a criterion bounding the
// occupied slots only once enough are. vanilla: the chest recipe's
// has_lots_of_items
func TestInventoryChangedWantsTheSlotCounts(t *testing.T) {
	tree := NewTree([]registry.Advancement{{
		ID:           "r",
		Criteria:     []registry.AdvancementCriterion{{Name: "lots", Trigger: TriggerInventoryChanged, Occupied: registry.AdvancementRange{Min: 10, HasMin: true}}},
		Requirements: [][]string{{"lots"}},
	}})
	p := NewProgress(tree)
	if p.InventoryChanged(Inventory{Held: []int32{1}, Occupied: 9, Empty: 34}, time.Now()) {
		t.Error("nine occupied slots passed a criterion wanting ten")
	}
	if !p.InventoryChanged(Inventory{Held: []int32{1}, Occupied: 10, Empty: 33}, time.Now()) || !p.Done(tree.Get("r")) {
		t.Error("ten occupied slots did not pass a criterion wanting ten")
	}
}

func TestChangedDimensionMatchesFromAndTo(t *testing.T) {
	tree := NewTree([]registry.Advancement{{
		ID:      "r",
		Display: &registry.AdvancementDisplay{},
		Criteria: []registry.AdvancementCriterion{
			{Name: "to", Trigger: TriggerChangedDimension, To: "minecraft:the_nether"},
			{Name: "back", Trigger: TriggerChangedDimension, From: "minecraft:the_nether", To: "minecraft:overworld"},
		},
		Requirements: [][]string{{"to"}, {"back"}},
	}})
	p := NewProgress(tree)
	p.ChangedDimension("minecraft:overworld", "minecraft:the_end", time.Now())
	p.ChangedDimension("minecraft:the_end", "minecraft:overworld", time.Now())
	if p.obtained[0] != nil {
		t.Errorf("crossings that are not the criteria' obtained %v", p.obtained[0])
	}
	p.ChangedDimension("minecraft:the_end", "minecraft:the_nether", time.Now())
	p.ChangedDimension("minecraft:the_nether", "minecraft:overworld", time.Now())
	if !p.Done(tree.Get("r")) {
		t.Errorf("both crossings left %v", p.obtained[0])
	}
}

// one builds a tree whose only advancement has the given criteria, each its
// own requirement group.
func one(criteria ...registry.AdvancementCriterion) (*Tree, *Progress) {
	var reqs [][]string
	for _, c := range criteria {
		reqs = append(reqs, []string{c.Name})
	}
	tree := NewTree([]registry.Advancement{{
		ID: "r", Display: &registry.AdvancementDisplay{}, Criteria: criteria, Requirements: reqs,
	}})
	return tree, NewProgress(tree)
}

func TestPlayerKilledEntityMatchesTypeTagAndDimension(t *testing.T) {
	_, p := one(
		registry.AdvancementCriterion{Name: "cow", Trigger: TriggerPlayerKilledEntity, Entity: "minecraft:cow"},
		registry.AdvancementCriterion{Name: "raider", Trigger: TriggerPlayerKilledEntity, Entity: "#minecraft:raiders"},
		registry.AdvancementCriterion{Name: "ghast", Trigger: TriggerPlayerKilledEntity, Entity: "minecraft:ghast", Dimension: "minecraft:overworld"},
		registry.AdvancementCriterion{Name: "weird", Trigger: TriggerPlayerKilledEntity, Unmodelled: true},
	)
	now := time.Now()
	p.PlayerKilledEntity(Entity{Type: "minecraft:pig", Dimension: "minecraft:overworld"}, now)
	p.PlayerKilledEntity(Entity{Type: "minecraft:ghast", Dimension: "minecraft:the_nether"}, now)
	if p.obtained[0] != nil {
		t.Fatalf("kills that are not the criteria' obtained %v", p.obtained[0])
	}
	p.PlayerKilledEntity(Entity{Type: "minecraft:cow"}, now)
	p.PlayerKilledEntity(Entity{Type: "minecraft:pillager"}, now)
	p.PlayerKilledEntity(Entity{Type: "minecraft:ghast", Dimension: "minecraft:overworld"}, now)
	for _, name := range []string{"cow", "raider", "ghast"} {
		if _, ok := p.obtained[0][name]; !ok {
			t.Errorf("%s was not obtained: %v", name, p.obtained[0])
		}
	}
	if _, ok := p.obtained[0]["weird"]; ok {
		t.Error("a criterion with conditions the server does not model was awarded")
	}
}

func TestBredAnimalsMatchesParentsInEitherOrderAndTheChild(t *testing.T) {
	_, p := one(
		registry.AdvancementCriterion{Name: "turtle", Trigger: TriggerBredAnimals, Parent: "minecraft:turtle", Partner: "minecraft:turtle"},
		registry.AdvancementCriterion{Name: "mix", Trigger: TriggerBredAnimals, Parent: "minecraft:horse", Partner: "minecraft:donkey"},
		registry.AdvancementCriterion{Name: "wolf", Trigger: TriggerBredAnimals, Child: "minecraft:wolf"},
	)
	now := time.Now()
	// The wolf criterion wants a child, which a turtle's egg is not.
	p.BredAnimals("minecraft:turtle", "minecraft:turtle", "", now)
	if _, ok := p.obtained[0]["turtle"]; !ok {
		t.Error("two turtles with no child did not pass the turtle criterion")
	}
	if _, ok := p.obtained[0]["wolf"]; ok {
		t.Error("a breeding with no child passed a child condition")
	}
	p.BredAnimals("minecraft:donkey", "minecraft:horse", "minecraft:mule", now)
	if _, ok := p.obtained[0]["mix"]; !ok {
		t.Error("the parents swapped did not pass")
	}
	p.BredAnimals("minecraft:wolf", "minecraft:wolf", "minecraft:wolf", now)
	if _, ok := p.obtained[0]["wolf"]; !ok {
		t.Error("a wolf child did not pass the wolf criterion")
	}
}

func TestLocationConsumeAndEnchantedMatchTheirConditions(t *testing.T) {
	_, p := one(
		registry.AdvancementCriterion{Name: "plains", Trigger: TriggerLocation, Biome: "minecraft:plains"},
		registry.AdvancementCriterion{Name: "bread", Trigger: TriggerConsumeItem, Items: [][]int32{{7}}},
		registry.AdvancementCriterion{Name: "any", Trigger: TriggerConsumeItem},
		registry.AdvancementCriterion{Name: "enchant", Trigger: TriggerEnchantedItem},
		registry.AdvancementCriterion{Name: "anywhere", Trigger: TriggerLocation, Unmodelled: true},
	)
	now := time.Now()
	p.Location("minecraft:desert", nil, Standing{}, now)
	p.ConsumeItem(8, now)
	if _, ok := p.obtained[0]["bread"]; ok || len(p.obtained[0]) != 1 {
		t.Fatalf("after another biome and another food: %v", p.obtained[0])
	}
	p.Location("minecraft:plains", nil, Standing{}, now)
	p.ConsumeItem(7, now)
	p.EnchantedItem(now)
	for _, name := range []string{"plains", "bread", "any", "enchant"} {
		if _, ok := p.obtained[0][name]; !ok {
			t.Errorf("%s was not obtained: %v", name, p.obtained[0])
		}
	}
	if _, ok := p.obtained[0]["anywhere"]; ok {
		t.Error("a criterion with conditions the server does not model was awarded")
	}
}

func TestTheFileRoundTrips(t *testing.T) {
	tree := Vanilla()
	data := []byte(`{
  "minecraft:story/root": {"criteria": {"crafting_table": "2026-10-31 12:34:56 +0100", "gone": "2026-10-31 12:34:56 +0100"}, "done": true},
  "minecraft:story/mine_stone": {"criteria": {"get_stone": "not a time"}, "done": false},
  "example:removed/bread": {"criteria": {"has_wheat": "2020-01-01 00:00:00 +0000"}, "done": true},
  "DataVersion": 3700
}`)
	p, err := Parse(tree, data)
	if err != nil {
		t.Fatal(err)
	}
	root := tree.Get("minecraft:story/root")
	want := time.Date(2026, 10, 31, 11, 34, 56, 0, time.UTC)
	if at := p.obtained[root.index]["crafting_table"]; !at.Equal(want) {
		t.Errorf("crafting_table obtained %v, want %v", at, want)
	}
	if _, ok := p.obtained[root.index]["gone"]; ok {
		t.Error("a criterion story/root lacks was kept")
	}
	if p.obtained[tree.Get("minecraft:story/mine_stone").index] != nil {
		t.Error("a criterion whose time does not parse was kept")
	}
	if u, ok := p.Flush(); !ok || !u.Reset || len(u.Added) != 3 {
		t.Errorf("the first flush after a load = %+v, want a reset adding the story tab", u)
	}

	out, err := p.Encode(4903)
	if err != nil {
		t.Fatal(err)
	}
	var file map[string]json.RawMessage
	if err := json.Unmarshal(out, &file); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if !strings.Contains(string(file["example:removed/bread"]), "has_wheat") {
		t.Errorf("the advancement the tree lacks was not written back:\n%s", out)
	}
	if _, ok := file["minecraft:story/mine_stone"]; ok {
		t.Errorf("an advancement with nothing obtained was written:\n%s", out)
	}
	if string(file["DataVersion"]) != "4903" {
		t.Errorf("DataVersion = %s, want 4903", file["DataVersion"])
	}
	again, err := Parse(tree, out)
	if err != nil {
		t.Fatal(err)
	}
	if !again.Done(root) || !again.obtained[root.index]["crafting_table"].Equal(want) {
		t.Errorf("the written file read back as %v", again.obtained[root.index])
	}
}

// TestRevokingOneOfTwoAlternativesKeepsTheTabUpToDate revokes a criterion of
// an advancement that stays done, which needs no visibility pass.
// vanilla: PlayerAdvancements.revoke
func TestRevokingOneOfTwoAlternativesKeepsTheTabUpToDate(t *testing.T) {
	root := node("r", "", false)
	root.Criteria = append(root.Criteria, registry.AdvancementCriterion{Name: "d", Trigger: "minecraft:impossible"})
	root.Requirements = [][]string{{"c", "d"}}
	tree := NewTree([]registry.Advancement{root})
	p := NewProgress(tree)
	r := tree.Get("r")
	p.Award(r, "c", time.Unix(1, 0))
	p.Award(r, "d", time.Unix(2, 0))
	p.Flush()
	p.Revoke(r, "c")
	if len(p.rootsToUpdate) != 0 {
		t.Error("revoking one of two alternatives queued a visibility pass")
	}
	p.Revoke(r, "d")
	if len(p.rootsToUpdate) != 1 {
		t.Error("revoking the last criterion queued no visibility pass")
	}
}

// column is a Blocks reader over a few cells.
func column(cells map[[3]int32]uint16) Blocks {
	return func(dx, dy, dz int32) uint16 { return cells[[3]int32{dx, dy, dz}] }
}

func TestPlacedBlockReadsStateTagsAndNeighbours(t *testing.T) {
	tree := Vanilla()
	p := NewProgress(tree)
	now := time.Now()
	log := func(axis string) uint16 {
		return registry.MustState("minecraft:pale_oak_log", map[string]string{"axis": axis})
	}
	heart := func(axis string) uint16 {
		return registry.MustState("minecraft:creaking_heart", map[string]string{"axis": axis})
	}
	transplanter := tree.Get("minecraft:adventure/heart_transplanter")
	// A log with the heart one below and a log below that, all upright,
	// passes through the all_of inside any_of; a sideways middle does not.
	p.PlacedBlock(column(map[[3]int32]uint16{{0, 0, 0}: log("y"), {0, -1, 0}: heart("x"), {0, -2, 0}: log("y")}), int32(registry.ItemPaleOakLog), now)
	if p.Obtained(transplanter, "place_pale_oak_log") {
		t.Fatal("a heart on another axis passed")
	}
	p.PlacedBlock(column(map[[3]int32]uint16{{0, 0, 0}: log("y"), {0, -1, 0}: heart("y")}), int32(registry.ItemPaleOakLog), now)
	if p.Obtained(transplanter, "place_pale_oak_log") {
		t.Fatal("a log without the far end passed")
	}
	p.PlacedBlock(column(map[[3]int32]uint16{{0, 0, 0}: log("y"), {0, -1, 0}: heart("y"), {0, -2, 0}: log("y")}), int32(registry.ItemPaleOakLog), now)
	if !p.Obtained(transplanter, "place_pale_oak_log") {
		t.Error("log, heart, log in a column did not pass")
	}
	// A wheat crop placed passes its own criterion only.
	p.PlacedBlock(column(map[[3]int32]uint16{{0, 0, 0}: registry.MustState("minecraft:wheat", nil)}), int32(registry.ItemWheatSeeds), now)
	if !p.Obtained(tree.Get("minecraft:husbandry/plant_seed"), "wheat") ||
		p.Obtained(tree.Get("minecraft:husbandry/plant_seed"), "beetroots") {
		t.Error("a placed wheat crop did not obtain exactly the wheat criterion")
	}
	if p.Obtained(tree.Get("minecraft:husbandry/place_dried_ghast_in_water"), "place_dried_ghast_in_water") {
		t.Error("a dry crop obtained the waterlogged ghast")
	}
}

func TestItemUsedOnBlockWantsTheToolAndTheBlockState(t *testing.T) {
	tree := Vanilla()
	p := NewProgress(tree)
	now := time.Now()
	honeycomb, _ := registry.ItemID("minecraft:honeycomb")
	stick, _ := registry.ItemID("minecraft:stick")
	copper := column(map[[3]int32]uint16{{0, 0, 0}: registry.MustState("minecraft:copper_block", nil)})
	wax := tree.Get("minecraft:husbandry/wax_on")
	p.ItemUsedOnBlock(copper, stick, "", now)
	p.ItemUsedOnBlock(column(map[[3]int32]uint16{{0, 0, 0}: registry.MustState("minecraft:stone", nil)}), honeycomb, "", now)
	if p.Obtained(wax, "wax_on") {
		t.Fatal("the wrong tool or the wrong block passed")
	}
	p.ItemUsedOnBlock(copper, honeycomb, "", now)
	if !p.Obtained(wax, "wax_on") {
		t.Error("honeycomb on copper did not pass")
	}
	// A vault criterion reads the vault's state, not just its name.
	key, _ := registry.ItemID("minecraft:trial_key")
	ominous := column(map[[3]int32]uint16{{0, 0, 0}: registry.MustState("minecraft:vault", map[string]string{"ominous": "true"})})
	p.ItemUsedOnBlock(ominous, key, "", now)
	if p.Obtained(tree.Get("minecraft:adventure/under_lock_and_key"), "under_lock_and_key") {
		t.Error("an ominous vault passed the plain vault criterion")
	}
	// The jukebox criterion wants a disc and the meadow biome at the jukebox.
	disc, _ := registry.ItemID("minecraft:music_disc_cat")
	jukebox := column(map[[3]int32]uint16{{0, 0, 0}: registry.MustState("minecraft:jukebox", nil)})
	meadows := tree.Get("minecraft:adventure/play_jukebox_in_meadows")
	p.ItemUsedOnBlock(jukebox, disc, "minecraft:plains", now)
	p.ItemUsedOnBlock(jukebox, stick, "minecraft:meadow", now)
	if p.Obtained(meadows, "play_jukebox_in_meadows") {
		t.Fatal("a jukebox outside a meadow, or a stick, passed")
	}
	p.ItemUsedOnBlock(jukebox, disc, "minecraft:meadow", now)
	if !p.Obtained(meadows, "play_jukebox_in_meadows") {
		t.Error("a disc in a meadow jukebox did not pass")
	}
}

func TestTameFilledAndEffectsCriteria(t *testing.T) {
	_, p := one(
		registry.AdvancementCriterion{Name: "any", Trigger: TriggerTameAnimal},
		registry.AdvancementCriterion{Name: "wolf", Trigger: TriggerTameAnimal, Entity: "minecraft:wolf"},
		registry.AdvancementCriterion{Name: "variant", Trigger: TriggerTameAnimal, Unmodelled: true},
		registry.AdvancementCriterion{Name: "snow", Trigger: TriggerFilledBucket, Items: [][]int32{{9}}},
		registry.AdvancementCriterion{Name: "both", Trigger: TriggerEffectsChanged, Effects: []string{"minecraft:speed", "minecraft:haste"}},
	)
	now := time.Now()
	p.TameAnimal("minecraft:horse", "", now)
	p.FilledBucket(8, now)
	p.EffectsChanged([]string{"minecraft:speed"}, now)
	if len(p.obtained[0]) != 1 || p.obtained[0]["any"].IsZero() {
		t.Fatalf("after a horse, another bucket and one effect: %v", p.obtained[0])
	}
	p.FilledBucket(9, now)
	p.EffectsChanged([]string{"minecraft:haste", "minecraft:speed", "minecraft:glowing"}, now)
	for _, name := range []string{"snow", "both"} {
		if _, ok := p.obtained[0][name]; !ok {
			t.Errorf("%s was not obtained: %v", name, p.obtained[0])
		}
	}
	if _, ok := p.obtained[0]["wolf"]; ok {
		t.Error("a horse passed the wolf criterion")
	}
	if _, ok := p.obtained[0]["variant"]; ok {
		t.Error("a criterion with conditions the server does not model was awarded")
	}
}

func TestTradeBeaconBrewAndFishingCriteria(t *testing.T) {
	_, p := one(
		registry.AdvancementCriterion{Name: "trade", Trigger: TriggerVillagerTrade},
		registry.AdvancementCriterion{Name: "high", Trigger: TriggerVillagerTrade, MinY: 319, HasMinY: true},
		registry.AdvancementCriterion{Name: "lit", Trigger: TriggerConstructBeacon, LevelMin: 1},
		registry.AdvancementCriterion{Name: "full", Trigger: TriggerConstructBeacon, LevelMin: 4, LevelMax: 4},
		registry.AdvancementCriterion{Name: "brew", Trigger: TriggerBrewedPotion},
		registry.AdvancementCriterion{Name: "cod", Trigger: TriggerFishingRodHooked, Items: [][]int32{{7}}},
	)
	now := time.Now()
	p.VillagerTrade(318.9, now)
	p.ConstructBeacon(3, now)
	p.FishingRodHooked([]int32{8, 9}, now)
	if len(p.obtained[0]) != 2 || p.obtained[0]["trade"].IsZero() || p.obtained[0]["lit"].IsZero() {
		t.Fatalf("after a low trade, a level-3 beacon and a junk catch: %v", p.obtained[0])
	}
	p.VillagerTrade(319, now)
	p.ConstructBeacon(4, now)
	p.BrewedPotion(now)
	p.FishingRodHooked([]int32{8, 7}, now)
	for _, name := range []string{"high", "full", "brew", "cod"} {
		if _, ok := p.obtained[0][name]; !ok {
			t.Errorf("%s was not obtained: %v", name, p.obtained[0])
		}
	}
}

func TestArrowCriteriaWantTheShaftTheWeaponAndTheVictims(t *testing.T) {
	_, p := one(
		registry.AdvancementCriterion{Name: "arrow", Trigger: TriggerPlayerHurtEntity, Direct: "#minecraft:arrows"},
		registry.AdvancementCriterion{Name: "trident", Trigger: TriggerPlayerHurtEntity, Direct: "minecraft:trident"},
		registry.AdvancementCriterion{Name: "pair", Trigger: TriggerKilledByArrow, Items: [][]int32{{5}},
			Victims: []string{"minecraft:phantom", "minecraft:phantom"}},
		registry.AdvancementCriterion{Name: "five", Trigger: TriggerKilledByArrow, UniqueMin: 5, UniqueMax: 5},
		registry.AdvancementCriterion{Name: "mace", Trigger: TriggerPlayerHurtEntity, Unmodelled: true},
	)
	now := time.Now()
	p.PlayerHurtProjectile("minecraft:spectral_arrow", now)
	p.KilledByArrow([]string{"minecraft:phantom"}, 5, now)
	p.KilledByArrow([]string{"minecraft:phantom", "minecraft:phantom"}, 4, now)
	if len(p.obtained[0]) != 1 || p.obtained[0]["arrow"].IsZero() {
		t.Fatalf("after a spectral arrow, one phantom and a bow's pair: %v", p.obtained[0])
	}
	p.KilledByArrow([]string{"minecraft:phantom", "minecraft:cow", "minecraft:phantom"}, 5, now)
	p.KilledByArrow([]string{"a", "b", "c", "d", "e", "e"}, 4, now)
	for _, name := range []string{"pair", "five"} {
		if _, ok := p.obtained[0][name]; !ok {
			t.Errorf("%s was not obtained: %v", name, p.obtained[0])
		}
	}
	for _, name := range []string{"trident", "mace"} {
		if _, ok := p.obtained[0][name]; ok {
			t.Errorf("%s was obtained", name)
		}
	}
}

func TestFlushTickWaitsForAHold(t *testing.T) {
	tree := NewTree([]registry.Advancement{node("r", "", false), node("a", "r", false)})
	p := NewProgress(tree)
	p.Flush()
	release := p.Hold()
	p.Award(tree.Get("r"), "c", time.Unix(1, 0))
	if _, ok := p.FlushTick(); ok {
		t.Fatal("the tick flushed a held progress")
	}
	if u, ok := p.FlushShow(false); !ok || u.Show {
		t.Fatalf("held flush = %+v, %v, want the changes with show clear", u, ok)
	}
	p.Award(tree.Get("a"), "c", time.Unix(2, 0))
	release()
	if u, ok := p.FlushTick(); !ok || !u.Show {
		t.Fatalf("flush after the release = %+v, %v, want the pending changes", u, ok)
	}
}

func TestNewTreeLogsTheAdvancementsItDrops(t *testing.T) {
	var buf strings.Builder
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	defer slog.SetDefault(prev)
	tree := NewTree([]registry.Advancement{
		{ID: "t:a", Parent: "t:b", Criteria: []registry.AdvancementCriterion{{Name: "c"}}},
		{ID: "t:b", Parent: "t:a", Criteria: []registry.AdvancementCriterion{{Name: "c"}}},
		{ID: "t:orphan", Parent: "t:missing", Criteria: []registry.AdvancementCriterion{{Name: "c"}}},
		{ID: "t:root", Criteria: []registry.AdvancementCriterion{{Name: "c"}}},
	})
	if tree.Len() != 1 {
		t.Errorf("the tree has %d advancements, want only t:root", tree.Len())
	}
	for _, id := range []string{"t:a", "t:b", "t:orphan"} {
		if !strings.Contains(buf.String(), id) {
			t.Errorf("the log does not name the dropped %s: %s", id, buf.String())
		}
	}
}

// TestEntityInteractionCriteriaWantTheItemTheTypeAndTheBody: the held item,
// the entity type and the body slot each have to match, the shear trigger
// does not answer an interaction and an unmodelled criterion never passes.
func TestEntityInteractionCriteriaWantTheItemTheTypeAndTheBody(t *testing.T) {
	_, p := one(
		registry.AdvancementCriterion{Name: "mended", Trigger: TriggerInteractedEntity, Items: [][]int32{{7}}, Entity: "minecraft:wolf", Body: []int32{9}, BodyUndamaged: true},
		registry.AdvancementCriterion{Name: "sheared", Trigger: TriggerShearedEquipment, Items: [][]int32{{9}}, Entity: "minecraft:wolf"},
		registry.AdvancementCriterion{Name: "other", Trigger: TriggerInteractedEntity, Unmodelled: true},
	)
	now := time.Now()
	wolf := EntityInteraction{Type: "minecraft:wolf", Body: 9, BodyDamage: 8}
	p.InteractedWithEntity(wolf, 7, nil, now)
	p.InteractedWithEntity(EntityInteraction{Type: "minecraft:fox", Body: 9}, 7, nil, now)
	p.InteractedWithEntity(EntityInteraction{Type: "minecraft:wolf", Body: 4}, 7, nil, now)
	p.InteractedWithEntity(EntityInteraction{Type: "minecraft:wolf", Body: 9}, 8, nil, now)
	p.ShearedEquipment(EntityInteraction{Type: "minecraft:fox"}, 9, nil, now)
	p.ShearedEquipment(wolf, 8, nil, now)
	if len(p.obtained[0]) != 0 {
		t.Fatalf("a mismatch obtained %v", p.obtained[0])
	}
	p.InteractedWithEntity(EntityInteraction{Type: "minecraft:wolf", Body: 9}, 7, nil, now)
	p.ShearedEquipment(wolf, 9, nil, now)
	if _, ok := p.obtained[0]["mended"]; !ok {
		t.Error("an undamaged armor mended with the item did not pass")
	}
	if _, ok := p.obtained[0]["sheared"]; !ok {
		t.Error("shearing the armor off a wolf did not pass")
	}
	if _, ok := p.obtained[0]["other"]; ok {
		t.Error("an unmodelled criterion was awarded")
	}
}

// TestThrownItemCriteriaWantTheItemTheEntityAndNoArmor: the picked-up
// item, the other entity's type and age and the player's armor each have
// to match, and each pickup trigger answers only its own criteria.
func TestThrownItemCriteriaWantTheItemTheEntityAndNoArmor(t *testing.T) {
	_, p := one(
		registry.AdvancementCriterion{Name: "distract", Trigger: TriggerThrownPickedUpByEntity, Items: [][]int32{{7}}, Entity: "minecraft:piglin", Adult: true, NotWearing: []int32{9}},
		registry.AdvancementCriterion{Name: "deliver", Trigger: TriggerThrownPickedUpByPlayer, Entity: "minecraft:allay"},
	)
	now := time.Now()
	piglin := Entity{Type: "minecraft:piglin"}
	p.ThrownItemPickedUp(true, 8, piglin, nil, now)
	p.ThrownItemPickedUp(true, 7, Entity{Type: "minecraft:piglin", Baby: true}, nil, now)
	p.ThrownItemPickedUp(true, 7, piglin, []int32{3, 9}, now)
	p.ThrownItemPickedUp(false, 7, piglin, nil, now)
	p.ThrownItemPickedUp(true, 7, Entity{Type: "minecraft:allay"}, nil, now)
	if len(p.obtained[0]) != 0 {
		t.Fatalf("a mismatch obtained %v", p.obtained[0])
	}
	p.ThrownItemPickedUp(true, 7, piglin, []int32{3}, now)
	p.ThrownItemPickedUp(false, 1, Entity{Type: "minecraft:allay"}, nil, now)
	for _, name := range []string{"distract", "deliver"} {
		if _, ok := p.obtained[0][name]; !ok {
			t.Errorf("%s did not pass", name)
		}
	}
}

func TestDistanceCriteriaMeasureFromTheStart(t *testing.T) {
	at := func(min float64) registry.AdvancementRange { return registry.AdvancementRange{Min: min, HasMin: true} }
	_, p := one(
		registry.AdvancementCriterion{Name: "far", Trigger: TriggerNetherTravel, Distance: registry.AdvancementDistance{Horizontal: at(7000)}},
		registry.AdvancementCriterion{Name: "cliffs", Trigger: TriggerFallFromHeight, Distance: registry.AdvancementDistance{Y: at(379)},
			StartY: at(319), PlayerY: registry.AdvancementRange{Max: -59, HasMax: true}},
		registry.AdvancementCriterion{Name: "up", Trigger: TriggerLevitation, Distance: registry.AdvancementDistance{Y: at(50)}},
	)
	now := time.Now()
	p.Moved(TriggerNetherTravel, [3]float64{0, 64, 0}, [3]float64{4950, 300, 4949}, now)
	p.Moved(TriggerFallFromHeight, [3]float64{0, 318, 0}, [3]float64{0, -62, 0}, now)
	p.Moved(TriggerFallFromHeight, [3]float64{0, 330, 0}, [3]float64{0, -58, 0}, now)
	p.Moved(TriggerLevitation, [3]float64{0, 64, 0}, [3]float64{0, 14, 0}, now)
	if len(p.obtained[0]) != 1 || p.obtained[0]["up"].IsZero() {
		t.Fatalf("after a 6999-block return, falls from too low and to too high, and a 50-block levitation: %v", p.obtained[0])
	}
	p.Moved(TriggerNetherTravel, [3]float64{0, 64, 0}, [3]float64{4950, 64, 4950}, now)
	p.Moved(TriggerFallFromHeight, [3]float64{0, 330, 0}, [3]float64{0, -60, 0}, now)
	for _, name := range []string{"far", "cliffs"} {
		if _, ok := p.obtained[0][name]; !ok {
			t.Errorf("%s was not obtained: %v", name, p.obtained[0])
		}
	}
}

func TestRiderConditionsReadTheVehicleAndItsPassengers(t *testing.T) {
	_, p := one(
		registry.AdvancementCriterion{Name: "goat", Trigger: TriggerStartedRiding, Vehicle: "#minecraft:boat", Passenger: "minecraft:goat"},
		registry.AdvancementCriterion{Name: "home", Trigger: TriggerRideInLava, Dimension: "minecraft:overworld", Vehicle: "minecraft:strider",
			Distance: registry.AdvancementDistance{Horizontal: registry.AdvancementRange{Min: 50, HasMin: true}}},
	)
	now := time.Now()
	boat := Rider{Dimension: "minecraft:overworld", Vehicle: "minecraft:oak_boat", Passengers: []string{"minecraft:player"}}
	p.StartedRiding(boat, now)
	p.StartedRiding(Rider{Dimension: "minecraft:overworld", Passengers: []string{"minecraft:goat"}}, now)
	strider := Rider{Dimension: "minecraft:the_nether", Vehicle: "minecraft:strider", Passengers: []string{"minecraft:player"}}
	p.RodeInLava(strider, [3]float64{0, 64, 0}, [3]float64{60, 64, 0}, now)
	strider.Dimension = "minecraft:overworld"
	p.RodeInLava(strider, [3]float64{0, 64, 0}, [3]float64{30, 64, 30}, now)
	if len(p.obtained[0]) != 0 {
		t.Fatalf("an empty boat, a goat on foot, a nether ride and a 42-block ride obtained %v", p.obtained[0])
	}
	boat.Passengers = append(boat.Passengers, "minecraft:goat")
	p.StartedRiding(boat, now)
	p.RodeInLava(strider, [3]float64{0, 64, 0}, [3]float64{40, 64, 40}, now)
	for _, name := range []string{"goat", "home"} {
		if _, ok := p.obtained[0][name]; !ok {
			t.Errorf("%s was not obtained: %v", name, p.obtained[0])
		}
	}
}

func TestRecipeCraftedWantsTheRecipeAndADistinctItemPerPredicate(t *testing.T) {
	tree := NewTree([]registry.Advancement{{
		ID:      "r",
		Display: &registry.AdvancementDisplay{},
		Criteria: []registry.AdvancementCriterion{
			{Name: "pot", Trigger: TriggerRecipeCrafted, RecipeID: "pot", Items: [][]int32{{1, 2}, {1, 2}}},
		},
		Requirements: [][]string{{"pot"}},
	}})
	p := NewProgress(tree)
	if p.RecipeCrafted("other", []int32{1, 2}, time.Now()) {
		t.Error("another recipe passed the criterion")
	}
	if p.RecipeCrafted("pot", []int32{1, 3}, time.Now()) {
		t.Error("one sherd for two predicates passed the criterion")
	}
	if !p.RecipeCrafted("pot", []int32{1, 2, 9}, time.Now()) {
		t.Error("two sherds did not pass the criterion")
	}
}

func TestUsingItemAsksWhatTheLookMeetsOnlyForACriterionThatWantsIt(t *testing.T) {
	tree := NewTree([]registry.Advancement{{
		ID:      "r",
		Display: &registry.AdvancementDisplay{},
		Criteria: []registry.AdvancementCriterion{
			{Name: "ghast", Trigger: TriggerUsingItem, Items: [][]int32{{7}}, LookingAt: "minecraft:ghast"},
		},
		Requirements: [][]string{{"ghast"}},
	}})
	p := NewProgress(tree)
	asked := 0
	look := func(name string) func() string {
		return func() string { asked++; return name }
	}
	if p.UsingItem(8, look("minecraft:ghast"), time.Now()) || asked != 0 {
		t.Errorf("another item passed or asked the look (asked %d)", asked)
	}
	if p.UsingItem(7, look("minecraft:cow"), time.Now()) {
		t.Error("looking at a cow passed")
	}
	if !p.UsingItem(7, look("minecraft:ghast"), time.Now()) {
		t.Error("looking at a ghast with the item did not pass")
	}
}

func TestHeroOfTheVillageAwardsItsCriterion(t *testing.T) {
	_, p := one(
		registry.AdvancementCriterion{Name: "hero", Trigger: TriggerHeroOfTheVillage},
		registry.AdvancementCriterion{Name: "other", Trigger: TriggerUsedTotem},
	)
	p.HeroOfTheVillage(time.Now())
	if _, ok := p.obtained[0]["hero"]; !ok {
		t.Error("hero was not obtained")
	}
	if _, ok := p.obtained[0]["other"]; ok {
		t.Error("a criterion of another trigger was obtained")
	}
}

func TestBeeNestDestroyedNeedsBlockSilkTouchAndBees(t *testing.T) {
	_, p := one(registry.AdvancementCriterion{Name: "nest", Trigger: TriggerBeeNestDestroyed,
		Block: "minecraft:bee_nest", Enchantment: "minecraft:silk_touch", Bees: 3})
	id, _ := registry.EnchantmentID("minecraft:silk_touch")
	silk := map[int32]int32{id: 1}
	now := time.Now()
	for _, tc := range []struct {
		block string
		tool  map[int32]int32
		bees  int
	}{
		{"minecraft:beehive", silk, 3},
		{"minecraft:bee_nest", nil, 3},
		{"minecraft:bee_nest", silk, 2},
	} {
		if p.BeeNestDestroyed(tc.block, tc.tool, tc.bees, now) {
			t.Errorf("%+v passed", tc)
		}
	}
	if !p.BeeNestDestroyed("minecraft:bee_nest", silk, 3, now) {
		t.Error("a silk-touch nest with three bees did not pass")
	}
}

func TestAvoidVibrationAwardsItsCriterion(t *testing.T) {
	_, p := one(
		registry.AdvancementCriterion{Name: "avoid", Trigger: TriggerAvoidVibration},
		registry.AdvancementCriterion{Name: "other", Trigger: TriggerUsedTotem},
	)
	p.AvoidVibration(time.Now())
	if _, ok := p.obtained[0]["avoid"]; !ok {
		t.Error("avoid was not obtained")
	}
	if _, ok := p.obtained[0]["other"]; ok {
		t.Error("a criterion of another trigger was obtained")
	}
}

func TestFallAfterExplosionReadsTheCauseAndTheDrop(t *testing.T) {
	_, p := one(registry.AdvancementCriterion{Name: "rockets", Trigger: TriggerFallAfterExplosion, Entity: "minecraft:wind_charge",
		Distance: registry.AdvancementDistance{Y: registry.AdvancementRange{Min: 7, HasMin: true}}})
	now := time.Now()
	impact := [3]float64{0, 64, 0}
	p.FallAfterExplosion(impact, [3]float64{0, 80, 0}, "minecraft:creeper", now)
	p.FallAfterExplosion(impact, [3]float64{0, 80, 0}, "", now)
	p.FallAfterExplosion(impact, [3]float64{0, 70, 0}, "minecraft:wind_charge", now)
	if len(p.obtained[0]) != 0 {
		t.Fatalf("a creeper's blast, no cause and a 6-block rise passed: %v", p.obtained[0])
	}
	p.FallAfterExplosion(impact, [3]float64{0, 71, 0}, "minecraft:wind_charge", now)
	if _, ok := p.obtained[0]["rockets"]; !ok {
		t.Error("a wind charge's blast and a 7-block rise did not pass")
	}
}

func TestSlideDownBlockAwardsItsCriterion(t *testing.T) {
	_, p := one(
		registry.AdvancementCriterion{Name: "slide", Trigger: TriggerSlideDownBlock},
		registry.AdvancementCriterion{Name: "other", Trigger: TriggerUsedTotem},
	)
	p.SlideDownBlock(time.Now())
	if _, ok := p.obtained[0]["slide"]; !ok {
		t.Error("slide was not obtained")
	}
	if _, ok := p.obtained[0]["other"]; ok {
		t.Error("a criterion of another trigger was obtained")
	}
}

func TestCuringAZombieVillagerAwardsOnlyModelledCureCriteria(t *testing.T) {
	_, p := one(
		registry.AdvancementCriterion{Name: "cure", Trigger: TriggerCuredZombieVillager},
		registry.AdvancementCriterion{Name: "filtered", Trigger: TriggerCuredZombieVillager, Unmodelled: true},
		registry.AdvancementCriterion{Name: "trade", Trigger: TriggerVillagerTrade},
	)
	if !p.CuredZombieVillager(time.Now()) {
		t.Fatal("cure did not award a criterion")
	}
	if len(p.obtained[0]) != 1 {
		t.Fatalf("awarded %v, want only cure", p.obtained[0])
	}
}

func TestLocationRequiresBiomeAndAnyStructurePiece(t *testing.T) {
	_, p := one(
		registry.AdvancementCriterion{Name: "both", Trigger: TriggerLocation, Biome: "minecraft:plains", Structures: []string{"minecraft:fortress", "minecraft:bastion_remnant"}},
		registry.AdvancementCriterion{Name: "empty", Trigger: TriggerLocation, Structures: []string{}},
	)
	now := time.Now()
	inside := func(ids []string) bool { return slices.Contains(ids, "minecraft:bastion_remnant") }
	p.Location("minecraft:desert", inside, Standing{}, now)
	p.Location("minecraft:plains", nil, Standing{}, now)
	p.Location("minecraft:plains", func([]string) bool { return false }, Standing{}, now)
	if len(p.obtained[0]) != 0 {
		t.Fatalf("a missing biome or piece awarded %v", p.obtained[0])
	}
	p.Location("minecraft:plains", inside, Standing{}, now)
	if _, ok := p.obtained[0]["both"]; !ok {
		t.Error("matching biome and the second structure did not award both")
	}
	if _, ok := p.obtained[0]["empty"]; ok {
		t.Error("empty structure set matched")
	}
}

func TestLocationWantsTheFeetItemAndTheBlockSteppedOn(t *testing.T) {
	tree := Vanilla()
	n := tree.Get("minecraft:adventure/walk_on_powder_snow_with_leather_boots")
	p := NewProgress(tree)
	now := time.Now()
	boots, _ := registry.ItemID("minecraft:leather_boots")
	iron, _ := registry.ItemID("minecraft:iron_boots")
	snow := registry.MustState("minecraft:powder_snow", nil)
	stone := registry.MustState("minecraft:stone", nil)
	for _, s := range []Standing{
		{Feet: iron, OnGround: true, On: snow},
		{Feet: boots, OnGround: true, On: stone},
		{Feet: boots, OnGround: false, On: snow},
		{OnGround: true, On: snow},
	} {
		p.Location("minecraft:plains", nil, s, now)
	}
	if p.Obtained(n, "walk_on_powder_snow_with_leather_boots") {
		t.Fatal("wrong boots, wrong block, airborne or bare feet passed")
	}
	p.Location("minecraft:plains", nil, Standing{Feet: boots, OnGround: true, On: snow}, now)
	if !p.Obtained(n, "walk_on_powder_snow_with_leather_boots") {
		t.Error("leather boots on powder snow did not pass")
	}
}

func TestAxolotlSupportNeedsAnEffectSource(t *testing.T) {
	tree := NewTree(registry.Advancements)
	n := tree.Get("minecraft:husbandry/kill_axolotl_target")
	p := NewProgress(tree)
	now := time.Now()
	p.EffectsChanged([]string{"minecraft:regeneration"}, now)
	p.EffectsChangedFrom([]string{"minecraft:regeneration"}, "minecraft:witch", now)
	if p.Done(n) {
		t.Fatal("regeneration without an axolotl source obtained the advancement")
	}
	p.EffectsChangedFrom([]string{"minecraft:regeneration"}, "minecraft:axolotl", now)
	if !p.Done(n) {
		t.Fatal("the axolotl's support did not obtain the advancement")
	}
}

// One struck villager serves both of a channeled_lightning criterion's
// villager predicates; spear_mobs wants Count stabbed bodies, any without one.
// vanilla: ChanneledLightningTrigger.TriggerInstance.matches, SpearMobsTrigger.TriggerInstance.matches
func TestChanneledLightningSharesAVictimAndSpearMobsCounts(t *testing.T) {
	_, p := one(
		registry.AdvancementCriterion{Name: "two", Trigger: TriggerChanneledLightning,
			Victims: []string{"minecraft:villager", "minecraft:villager"}},
		registry.AdvancementCriterion{Name: "cow", Trigger: TriggerChanneledLightning, Victims: []string{"minecraft:cow"}},
		registry.AdvancementCriterion{Name: "five", Trigger: TriggerSpearMobs, Count: 5},
		registry.AdvancementCriterion{Name: "any", Trigger: TriggerSpearMobs},
	)
	now := time.Now()
	p.ChanneledLightning([]string{"minecraft:player", "minecraft:villager"}, now)
	p.SpearMobs(4, now)
	if len(p.obtained[0]) != 2 || p.obtained[0]["two"].IsZero() || p.obtained[0]["any"].IsZero() {
		t.Fatalf("after a struck villager and four stabs: %v", p.obtained[0])
	}
	p.SpearMobs(5, now)
	if p.obtained[0]["five"].IsZero() {
		t.Errorf("five stabs did not obtain five: %v", p.obtained[0])
	}
}

// TestInventoryOfCountsTheSlots checks an item held twice is listed once, a
// stack at its item's limit counts as full, and the slots a player never
// fills count as empty.
func TestInventoryOfCountsTheSlots(t *testing.T) {
	log, ok := registry.ItemID("minecraft:oak_log")
	if !ok {
		t.Fatal("no oak log")
	}
	slots := make([]protocol.ItemStack, 41)
	slots[0] = protocol.ItemStack{ItemID: log, Count: 64}
	slots[5] = protocol.ItemStack{ItemID: log, Count: 1}
	got := InventoryOf(slots, nil)
	if !slices.Equal(got.Held, []int32{log}) || got.Occupied != 2 || got.Full != 1 || got.Empty != 41 {
		t.Errorf("InventoryOf = %+v, want one log held, two occupied, one full, 41 empty", got)
	}
}
