package advancement

import (
	"encoding/json"
	"github.com/golem-mc/golem/internal/protocol"
	"slices"
	"strings"
	"testing"
	"time"

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
	p.BredAnimals("minecraft:turtle", "minecraft:turtle", "", nil, now)
	if _, ok := p.obtained[0]["turtle"]; !ok {
		t.Error("two turtles with no child did not pass the turtle criterion")
	}
	if _, ok := p.obtained[0]["wolf"]; ok {
		t.Error("a breeding with no child passed a child condition")
	}
	p.BredAnimals("minecraft:donkey", "minecraft:horse", "minecraft:mule", nil, now)
	if _, ok := p.obtained[0]["mix"]; !ok {
		t.Error("the parents swapped did not pass")
	}
	p.BredAnimals("minecraft:wolf", "minecraft:wolf", "minecraft:wolf", nil, now)
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
	p.ConsumeItem(protocol.ItemStack{ItemID: 8, Count: 1}, now)
	if _, ok := p.obtained[0]["bread"]; ok || len(p.obtained[0]) != 1 {
		t.Fatalf("after another biome and another food: %v", p.obtained[0])
	}
	p.Location("minecraft:plains", nil, Standing{}, now)
	p.ConsumeItem(protocol.ItemStack{ItemID: 7, Count: 1}, now)
	p.EnchantedItem(protocol.ItemStack{ItemID: 7, Count: 1}, 1, now)
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

// column is a Blocks reader over a few cells.
func column(cells map[[3]int32]uint16) Blocks {
	return Blocks{At: func(dx, dy, dz int32) uint16 { return cells[[3]int32{dx, dy, dz}] }}
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
	p.PlacedBlock(column(map[[3]int32]uint16{{0, 0, 0}: log("y"), {0, -1, 0}: heart("x"), {0, -2, 0}: log("y")}), stack(int32(registry.ItemPaleOakLog)), now)
	if p.Obtained(transplanter, "place_pale_oak_log") {
		t.Fatal("a heart on another axis passed")
	}
	p.PlacedBlock(column(map[[3]int32]uint16{{0, 0, 0}: log("y"), {0, -1, 0}: heart("y")}), stack(int32(registry.ItemPaleOakLog)), now)
	if p.Obtained(transplanter, "place_pale_oak_log") {
		t.Fatal("a log without the far end passed")
	}
	p.PlacedBlock(column(map[[3]int32]uint16{{0, 0, 0}: log("y"), {0, -1, 0}: heart("y"), {0, -2, 0}: log("y")}), stack(int32(registry.ItemPaleOakLog)), now)
	if !p.Obtained(transplanter, "place_pale_oak_log") {
		t.Error("log, heart, log in a column did not pass")
	}
	// A wheat crop placed passes its own criterion only.
	p.PlacedBlock(column(map[[3]int32]uint16{{0, 0, 0}: registry.MustState("minecraft:wheat", nil)}), stack(int32(registry.ItemWheatSeeds)), now)
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
	p.ItemUsedOnBlock(copper, stack(stick), "", now)
	p.ItemUsedOnBlock(column(map[[3]int32]uint16{{0, 0, 0}: registry.MustState("minecraft:stone", nil)}), stack(honeycomb), "", now)
	if p.Obtained(wax, "wax_on") {
		t.Fatal("the wrong tool or the wrong block passed")
	}
	p.ItemUsedOnBlock(copper, stack(honeycomb), "", now)
	if !p.Obtained(wax, "wax_on") {
		t.Error("honeycomb on copper did not pass")
	}
	// A vault criterion reads the vault's state, not just its name.
	key, _ := registry.ItemID("minecraft:trial_key")
	ominous := column(map[[3]int32]uint16{{0, 0, 0}: registry.MustState("minecraft:vault", map[string]string{"ominous": "true"})})
	p.ItemUsedOnBlock(ominous, stack(key), "", now)
	if p.Obtained(tree.Get("minecraft:adventure/under_lock_and_key"), "under_lock_and_key") {
		t.Error("an ominous vault passed the plain vault criterion")
	}
	// The jukebox criterion wants a disc and the meadow biome at the jukebox.
	disc, _ := registry.ItemID("minecraft:music_disc_cat")
	jukebox := column(map[[3]int32]uint16{{0, 0, 0}: registry.MustState("minecraft:jukebox", nil)})
	meadows := tree.Get("minecraft:adventure/play_jukebox_in_meadows")
	p.ItemUsedOnBlock(jukebox, stack(disc), "minecraft:plains", now)
	p.ItemUsedOnBlock(jukebox, stack(stick), "minecraft:meadow", now)
	if p.Obtained(meadows, "play_jukebox_in_meadows") {
		t.Fatal("a jukebox outside a meadow, or a stick, passed")
	}
	p.ItemUsedOnBlock(jukebox, stack(disc), "minecraft:meadow", now)
	if !p.Obtained(meadows, "play_jukebox_in_meadows") {
		t.Error("a disc in a meadow jukebox did not pass")
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
	p.KilledByArrow([]string{"minecraft:phantom"}, stack(5), nil, now)
	p.KilledByArrow([]string{"minecraft:phantom", "minecraft:phantom"}, stack(4), nil, now)
	if len(p.obtained[0]) != 1 || p.obtained[0]["arrow"].IsZero() {
		t.Fatalf("after a spectral arrow, one phantom and a bow's pair: %v", p.obtained[0])
	}
	p.KilledByArrow([]string{"minecraft:phantom", "minecraft:cow", "minecraft:phantom"}, stack(5), nil, now)
	p.KilledByArrow([]string{"a", "b", "c", "d", "e", "e"}, stack(4), nil, now)
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
	p.InteractedWithEntity(wolf, stack(7), nil, now)
	p.InteractedWithEntity(EntityInteraction{Type: "minecraft:fox", Body: 9}, stack(7), nil, now)
	p.InteractedWithEntity(EntityInteraction{Type: "minecraft:wolf", Body: 4}, stack(7), nil, now)
	p.InteractedWithEntity(EntityInteraction{Type: "minecraft:wolf", Body: 9}, stack(8), nil, now)
	p.ShearedEquipment(EntityInteraction{Type: "minecraft:fox"}, stack(9), nil, now)
	p.ShearedEquipment(wolf, stack(8), nil, now)
	if len(p.obtained[0]) != 0 {
		t.Fatalf("a mismatch obtained %v", p.obtained[0])
	}
	p.InteractedWithEntity(EntityInteraction{Type: "minecraft:wolf", Body: 9}, stack(7), nil, now)
	p.ShearedEquipment(wolf, stack(9), nil, now)
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
	if p.RecipeCrafted("other", stacks(1, 2), time.Now()) {
		t.Error("another recipe passed the criterion")
	}
	if p.RecipeCrafted("pot", stacks(1, 3), time.Now()) {
		t.Error("one sherd for two predicates passed the criterion")
	}
	if !p.RecipeCrafted("pot", stacks(1, 2, 9), time.Now()) {
		t.Error("two sherds did not pass the criterion")
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

func stack(id int32) protocol.ItemStack { return protocol.ItemStack{ItemID: id, Count: 1} }

func stacks(ids ...int32) []protocol.ItemStack {
	out := make([]protocol.ItemStack, len(ids))
	for i, id := range ids {
		out[i] = stack(id)
	}
	return out
}
