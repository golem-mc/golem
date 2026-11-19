package world

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// TestAChestKeepsABundlesContents checks the block entity file keeps a
// bundle's contents: the component's value is wire bytes a file cannot name,
// so the codec writes the stacks it holds as the stacks a file names.
func TestAChestKeepsABundlesContents(t *testing.T) {
	bundle, ok := registry.ItemID("minecraft:bundle")
	if !ok {
		t.Fatal("the item registry has no minecraft:bundle")
	}
	stone, _ := registry.ItemID("minecraft:stone")

	stack := protocol.ItemStack{ItemID: bundle, Count: 1}
	stack.SetBundleContents([]protocol.ItemStack{{ItemID: stone, Count: 64}})

	be := newBlockEntity(KindChest, 4, 64, 0)
	be.Slots[0] = stack

	back, ok := decodeBlockEntity(be.EncodeNBT())
	if !ok {
		t.Fatal("the saved chest did not decode")
	}
	if got := back.Slots[0]; !got.Equal(stack) {
		t.Errorf("the bundle came back as %+v, want %+v", got, stack)
	}
}

// TestABeaconKeepsOnlyOfferedEffects checks the beacon's load filter and its
// NBT round trip: a side naming an effect the pyramid offers loads - a name
// without a namespace being the minecraft one - while any other effect is
// dropped, and what loaded is what the save writes back.
// vanilla: BeaconBlockEntity.loadEffect
func TestABeaconKeepsOnlyOfferedEffects(t *testing.T) {
	speed := registry.MustMobEffect("minecraft:speed")
	be, ok := decodeBlockEntity(nbt.Compound{
		"id":               nbt.String(KindBeacon.Name()),
		"x":                nbt.Int(4),
		"y":                nbt.Int(64),
		"z":                nbt.Int(0),
		"primary_effect":   nbt.String("speed"),
		"secondary_effect": nbt.String("minecraft:poison"),
	})
	if !ok {
		t.Fatal("the beacon entry did not decode")
	}
	if be.BeaconPrimary != speed {
		t.Errorf("the bare primary loaded as %d, want speed (%d)", be.BeaconPrimary, speed)
	}
	if be.BeaconSecondary != -1 {
		t.Errorf("the unoffered secondary loaded as %d, want it dropped", be.BeaconSecondary)
	}

	back, ok := decodeBlockEntity(be.EncodeNBT())
	if !ok {
		t.Fatal("the saved beacon did not decode")
	}
	if back.BeaconPrimary != speed {
		t.Errorf("after the round trip the primary = %d, want speed (%d)", back.BeaconPrimary, speed)
	}
	if back.BeaconSecondary != -1 {
		t.Errorf("after the round trip the secondary = %d, want none", back.BeaconSecondary)
	}
}

// TestALockedContainerAnswersOnlyKeysItsPredicateNames checks the predicate
// a lock is the way ItemPredicate reads it: the items it names - a bare name,
// a list of names, or an item tag as a bare string - its count as a number or
// a min/max compound, and an exact custom_name test. An empty predicate
// matches everything, a component test the model cannot run matches nothing,
// and a field the codec rejects is dropped on its own: a rejected count
// leaves the items standing, a list keeps only the entries that decoded.
// vanilla: LockCode.unlocksWith, ItemPredicate.CODEC, ItemPredicate.test
func TestALockedContainerAnswersOnlyKeysItsPredicateNames(t *testing.T) {
	hook, ok := registry.ItemID("minecraft:tripwire_hook")
	if !ok {
		t.Fatal("the item registry has no minecraft:tripwire_hook")
	}
	stone, _ := registry.ItemID("minecraft:stone")
	planks, ok := registry.ItemID("minecraft:oak_planks")
	if !ok {
		t.Fatal("the item registry has no minecraft:oak_planks")
	}
	be := newBlockEntity(KindChest, 4, 64, 0)
	oneHook := protocol.ItemStack{ItemID: hook, Count: 1}
	onePlank := protocol.ItemStack{ItemID: planks, Count: 1}

	if !be.UnlockedBy(oneHook) {
		t.Error("a chest nobody locked refused the hook, want it open to anyone")
	}

	be.Lock = nbt.Compound{"items": nbt.String("minecraft:tripwire_hook")}
	if !be.UnlockedBy(oneHook) {
		t.Error("the bare-name key refused the item it names")
	}
	if be.UnlockedBy(protocol.ItemStack{ItemID: stone, Count: 1}) {
		t.Error("the key let the stone it does not name in")
	}
	if be.UnlockedBy(protocol.ItemStack{}) {
		t.Error("the nameless empty hand opened the named-item lock")
	}

	be.Lock = nbt.Compound{"items": nbt.List{Elem: nbt.TypeString,
		Items: []nbt.Tag{nbt.String("minecraft:stone"), nbt.String("minecraft:tripwire_hook")}}}
	if !be.UnlockedBy(oneHook) || !be.UnlockedBy(protocol.ItemStack{ItemID: stone, Count: 1}) {
		t.Error("the list key refused one of the two items it names")
	}

	be.Lock = nbt.Compound{"items": nbt.String("#minecraft:planks")}
	if !be.UnlockedBy(onePlank) {
		t.Error("the item tag key refused a member of the tag it names")
	}
	if be.UnlockedBy(oneHook) {
		t.Error("the item tag key let the hook a member of another tag in")
	}

	// A list keeps only the entries that decoded, so a tag or an unknown
	// entry drops on its own: an empty result matches nothing, a mixed one
	// matches the good entries alone.
	be.Lock = nbt.Compound{"items": nbt.List{Elem: nbt.TypeString,
		Items: []nbt.Tag{nbt.String("#minecraft:planks")}}}
	if be.UnlockedBy(oneHook) || be.UnlockedBy(onePlank) {
		t.Error("a tag inside an items list left an empty holder set, want it to refuse everyone")
	}
	be.Lock = nbt.Compound{"items": nbt.List{Elem: nbt.TypeString,
		Items: []nbt.Tag{nbt.String("minecraft:tripwire_hook"), nbt.String("#minecraft:planks")}}}
	if !be.UnlockedBy(oneHook) || be.UnlockedBy(onePlank) {
		t.Error("a mixed items list kept the wrong entries")
	}

	// A bare item the registry does not name and a tag it does not define
	// drop the items field, while a count the codec rejects drops only the
	// count, leaving the rest of the predicate standing.
	be.Lock = nbt.Compound{"items": nbt.String("minecraft:not_an_item")}
	if !be.UnlockedBy(oneHook) {
		t.Error("an item the registry does not name dropped the items field, want the rest to open")
	}
	be.Lock = nbt.Compound{"items": nbt.String("#minecraft:no_such_tag")}
	if !be.UnlockedBy(oneHook) {
		t.Error("a tag the registry does not define dropped the items field, want the rest to open")
	}
	be.Lock = nbt.Compound{
		"items": nbt.List{Elem: nbt.TypeString, Items: []nbt.Tag{nbt.String("minecraft:tripwire_hook")}},
		"count": nbt.String("2"),
	}
	if !be.UnlockedBy(oneHook) || be.UnlockedBy(onePlank) {
		t.Error("a count written as a string took the items it was laid beside, want the items kept")
	}
	be.Lock = nbt.Compound{"count": nbt.String("2..")}
	if !be.UnlockedBy(oneHook) {
		t.Error("a count written as a string dropped the predicate, want the rest to open")
	}

	be.Lock = nbt.Compound{"count": nbt.Int(3)}
	if !be.UnlockedBy(protocol.ItemStack{ItemID: hook, Count: 3}) {
		t.Error("the exact count refused the stack that holds it")
	}
	if be.UnlockedBy(oneHook) {
		t.Error("the exact count took a stack holding another count")
	}

	be.Lock = nbt.Compound{"count": nbt.Compound{"min": nbt.Int(2)}, "items": nbt.String("minecraft:tripwire_hook")}
	if be.UnlockedBy(oneHook) || !be.UnlockedBy(protocol.ItemStack{ItemID: hook, Count: 2}) {
		t.Error("the compound range misread its min end")
	}
	be.Lock = nbt.Compound{"count": nbt.Compound{"min": nbt.Int(5), "max": nbt.Int(3)}}
	if !be.UnlockedBy(oneHook) {
		t.Error("the swapped bounds failed the codec, want the lock dropped")
	}

	key := protocol.Text{Text: "Skeleton Key"}
	be.Lock = nbt.Compound{
		"items":      nbt.String("minecraft:tripwire_hook"),
		"components": nbt.Compound{"minecraft:custom_name": nbt.String("Skeleton Key")},
	}
	if !be.UnlockedBy(protocol.ItemStack{ItemID: hook, Count: 1, CustomName: &key}) {
		t.Error("the custom_name component refused the named key")
	}
	if be.UnlockedBy(oneHook) {
		t.Error("the custom_name component took a stack carrying no name")
	}
	be.Lock = nbt.Compound{
		"items":      nbt.String("minecraft:tripwire_hook"),
		"components": nbt.Compound{"custom_name": nbt.String("Skeleton Key")},
	}
	if !be.UnlockedBy(protocol.ItemStack{ItemID: hook, Count: 1, CustomName: &key}) {
		t.Error("an unqualified custom_name key refused the named key")
	}
	be.Lock = nbt.Compound{
		"items":      nbt.String("minecraft:tripwire_hook"),
		"components": nbt.Compound{"minecraft:damage": nbt.Int(5)},
	}
	if be.UnlockedBy(oneHook) {
		t.Error("an exact component test this model cannot run unlocked the container, want no key at all")
	}
	be.Lock = nbt.Compound{
		"items":      nbt.String("minecraft:tripwire_hook"),
		"components": nbt.Compound{"minecraft:no_such_component": nbt.Int(1)},
	}
	if !be.UnlockedBy(oneHook) {
		t.Error("a component the registry does not name dropped its entry, want the rest to open")
	}
	be.Lock = nbt.Compound{
		"items":      nbt.String("minecraft:tripwire_hook"),
		"predicates": nbt.Compound{"minecraft:damage": nbt.Compound{"durability": nbt.Int(5)}},
	}
	if be.UnlockedBy(oneHook) {
		t.Error("a sub-predicate map this model cannot run unlocked the container, want no key at all")
	}
	be.Lock = nbt.Compound{
		"items":      nbt.String("minecraft:tripwire_hook"),
		"predicates": nbt.Compound{"minecraft:no_such_pred": nbt.Compound{}},
	}
	if !be.UnlockedBy(oneHook) {
		t.Error("a predicate type the registry does not name dropped its entry, want the rest to open")
	}

	be.Lock = nbt.Compound{}
	if !be.UnlockedBy(oneHook) || !be.UnlockedBy(protocol.ItemStack{}) {
		t.Error("the empty predicate refused what vanilla's own saves write for an all-any lock")
	}
}

// TestALockedContainerRoundTripsItsLock checks the lock's place in the chunk
// format: what a chest's entry named loads and saves back whole, while the
// kinds vanilla does not base on BaseContainerBlockEntity - the
// jukebox - drop a lock tag they read.
// vanilla: LockCode.addToTag, LockCode.fromTag
func TestALockedContainerRoundTripsItsLock(t *testing.T) {
	lock := nbt.Compound{"items": nbt.String("minecraft:tripwire_hook")}
	be, ok := decodeBlockEntity(nbt.Compound{
		"id":    nbt.String(KindChest.Name()),
		"x":     nbt.Int(4),
		"y":     nbt.Int(64),
		"z":     nbt.Int(0),
		"lock":  lock,
		"Items": nbt.List{Elem: nbt.TypeEnd},
	})
	if !ok {
		t.Fatal("the locked chest entry did not decode")
	}
	if len(be.Lock.Entries()) != 1 || be.Lock.Entries()["items"] != nbt.String("minecraft:tripwire_hook") {
		t.Errorf("the loaded lock = %v, want what the entry named", be.Lock)
	}
	back, ok := decodeBlockEntity(be.EncodeNBT())
	if !ok {
		t.Fatal("the saved locked chest did not decode")
	}
	if back.Lock == nil || back.Lock.Entries()["items"] != nbt.String("minecraft:tripwire_hook") {
		t.Errorf("after the round trip the lock = %v, want it back whole", back.Lock)
	}

	free, ok := decodeBlockEntity(nbt.Compound{
		"id":   nbt.String(KindJukebox.Name()),
		"x":    nbt.Int(4),
		"y":    nbt.Int(64),
		"z":    nbt.Int(0),
		"lock": lock,
	})
	if !ok {
		t.Fatal("the jukebox entry did not decode")
	}
	if free.Lock != nil {
		t.Errorf("the jukebox kept a lock at %v, want it dropped: no vanilla BaseContainer keeps one", free.Lock)
	}
	if _, has := free.EncodeNBT()["lock"]; has {
		t.Error("the jukebox's save wrote a lock, want none")
	}
}

// TestALecternKeepsItsBookAndPage loads a lectern saved with a three-page
// book and a page past its end: the page is clamped, the signal follows it,
// and the save writes the book back whole.
func TestALecternKeepsItsBookAndPage(t *testing.T) {
	entry := nbt.Compound{
		"id": nbt.String(KindLectern.Name()), "x": nbt.Int(1), "y": nbt.Int(64), "z": nbt.Int(2),
		"Book": nbt.Compound{
			"id": nbt.String("minecraft:writable_book"), "count": nbt.Int(1),
			"components": nbt.Compound{"minecraft:writable_book_content": nbt.Compound{
				"pages": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
					nbt.Compound{"raw": nbt.String("a")},
					nbt.Compound{"raw": nbt.String("b")},
					nbt.Compound{"raw": nbt.String("c")},
				}},
			}},
		},
		"Page": nbt.Int(9),
	}
	be, ok := decodeBlockEntity(entry)
	if !ok {
		t.Fatal("the lectern did not decode")
	}
	if !be.HasBook() || be.BookPages() != 3 || be.Page != 2 {
		t.Fatalf("lectern = book %v, %d pages, page %d, want a book of 3 pages at page 2", be.HasBook(), be.BookPages(), be.Page)
	}
	if got := be.LecternSignal(); got != 15 {
		t.Errorf("signal on the last page = %d, want 15", got)
	}
	if !be.SetLecternPage(0) || be.LecternSignal() != 1 {
		t.Errorf("signal on the first page = %d, want 1", be.LecternSignal())
	}
	saved := be.EncodeNBT()
	if saved["Page"] != nbt.Int(0) {
		t.Errorf("saved Page = %v, want 0", saved["Page"])
	}
	if _, ok := nbt.Get[nbt.Compound](saved, "Book"); !ok {
		t.Error("the saved lectern lost its book")
	}
	if be.SetLecternPage(-5) {
		t.Error("a page before the first moved the book")
	}
}

func TestACampfireKeepsItsCookingThroughASave(t *testing.T) {
	t.Parallel()
	be := newBlockEntity(KindCampfire, 1, 2, 3)
	beef := registry.MustItem("minecraft:beef")
	if !be.PlaceFood(protocol.ItemStack{ItemID: beef, Count: 3}) {
		t.Fatal("the campfire refused raw beef")
	}
	be.CampfireProgress[0] = 41
	saved := be.EncodeNBT()
	if times, _ := nbt.Get[nbt.IntArray](saved, "CookingTimes"); len(times) != 4 || times[0] != 41 {
		t.Errorf("CookingTimes = %v, want four ticks starting at 41", times)
	}
	back := newBlockEntity(KindCampfire, 1, 2, 3)
	back.loadFrom(saved)
	if back.CampfireProgress[0] != 41 || back.CampfireTotals[0] != 600 || back.Slots[0].Count != 1 {
		t.Errorf("reloaded: progress %v totals %v slot %+v", back.CampfireProgress, back.CampfireTotals, back.Slots[0])
	}
	if tag := be.UpdateTag(); tag["CookingTimes"] != nil || tag["Items"] == nil {
		t.Errorf("update tag = %v, want the items alone", tag)
	}
}

func TestAnUnlitCampfireCoolsANegativeTotalSlotToItsTotal(t *testing.T) {
	t.Parallel()
	be := newBlockEntity(KindCampfire, 1, 2, 3)
	be.CampfireProgress[0], be.CampfireTotals[0] = 5, -7
	be.CampfireProgress[1], be.CampfireTotals[1] = 1, 600
	be.CampfireProgress[2], be.CampfireTotals[2] = 50, 600
	if _, changed := be.CampfireTick(false); !changed {
		t.Error("cooling reported no change")
	}
	if got, want := be.CampfireProgress[:3], []int32{-7, 0, 48}; !slices.Equal(got, want) {
		t.Errorf("progress after cooling = %v, want %v", got, want)
	}
	saved := be.EncodeNBT()
	if times, _ := nbt.Get[nbt.IntArray](saved, "CookingTimes"); len(times) != 4 || times[0] != -7 {
		t.Errorf("CookingTimes = %v, want -7 first", times)
	}
}

// TestAFuelsSpeedIsSavedWithItsGauges checks the keys a cooking block and a
// brewing stand keep their fuel's speed and their gauges' full marks under,
// and the values a file without them loads as.
// vanilla: AbstractFurnaceBlockEntity.loadAdditional, BrewingStandBlockEntity.loadAdditional
func TestAFuelsSpeedIsSavedWithItsGauges(t *testing.T) {
	smoker := newBlockEntity(KindSmoker, 1, 64, 1)
	if got := smoker.EncodeNBT()["speed_multiplier"]; got != nbt.Float(1) {
		t.Errorf("a fresh smoker saves speed_multiplier %#v, want Float 1", got)
	}
	smoker.SpeedMultiplier = 2
	back, ok := decodeBlockEntity(smoker.EncodeNBT())
	if !ok || back.SpeedMultiplier != 2 {
		t.Errorf("reloaded smoker speed = %v, want 2", back.SpeedMultiplier)
	}
	if got := back.CookTotalOf(&registry.CookingRecipe{CookTime: 201}); got != 101 {
		t.Errorf("201 ticks at twice the speed = %d, want 101 (rounded up)", got)
	}

	stand := newBlockEntity(KindBrewingStand, 1, 64, 1)
	stand.BrewTotal, stand.FuelTotal = 400, 20
	saved := stand.EncodeNBT()
	if saved["total_brew_time"] != nbt.Int(400) || saved["total_fuel"] != nbt.Int(20) || saved["speed_multiplier"] != nbt.Float(1) {
		t.Errorf("saved stand = %v, want total_brew_time 400, total_fuel 20, speed_multiplier 1f", saved)
	}
	delete(saved, "total_brew_time")
	delete(saved, "total_fuel")
	delete(saved, "speed_multiplier")
	old, ok := decodeBlockEntity(saved)
	if !ok || old.BrewTotal != 400 || old.FuelTotal != 20 || old.SpeedMultiplier != 1 {
		t.Errorf("a stand without the keys loads (%d, %d, %v), want (400, 20, 1)", old.BrewTotal, old.FuelTotal, old.SpeedMultiplier)
	}
}

// TestABrewingStandSavesBrewTimeAndFuelAsInt checks the saved tag types, and
// that a file with the older short and byte tags loads.
func TestABrewingStandSavesBrewTimeAndFuelAsInt(t *testing.T) {
	be := newBlockEntity(KindBrewingStand, 1, 64, 1)
	be.BrewTime, be.Fuel = 300, 20
	saved := be.EncodeNBT()
	if got, ok := saved["BrewTime"].(nbt.Int); !ok || got != 300 {
		t.Fatalf("BrewTime = %#v, want Int 300", saved["BrewTime"])
	}
	if got, ok := saved["Fuel"].(nbt.Int); !ok || got != 20 {
		t.Fatalf("Fuel = %#v, want Int 20", saved["Fuel"])
	}
	old := be.EncodeNBT()
	old["BrewTime"], old["Fuel"] = nbt.Short(300), nbt.Byte(20)
	for _, tags := range []nbt.Compound{saved, old} {
		back, ok := decodeBlockEntity(tags)
		if !ok || back.BrewTime != 300 || back.Fuel != 20 {
			t.Fatalf("loaded %v: ok=%v brew=%d fuel=%d", tags, ok, back.BrewTime, back.Fuel)
		}
	}
}

// TestAnEmptyChestSavesAsVanillaDoes checks the chunk-format compound of an
// empty chest: an empty components compound and an empty Items list typed
// End, while the update and spawner tags carry no components.
// vanilla: BlockEntity.saveWithoutMetadata, BlockEntity.saveCustomOnly
func TestAnEmptyChestSavesAsVanillaDoes(t *testing.T) {
	chest := newBlockEntity(KindChest, 0, 64, 0)
	saved := chest.EncodeNBT()
	if components, ok := nbt.Get[nbt.Compound](saved, "components"); !ok || len(components) != 0 {
		t.Errorf("components = %v, want an empty compound", saved["components"])
	}
	if items, ok := nbt.Get[nbt.List](saved, "Items"); !ok || items.Elem != nbt.TypeEnd || len(items.Items) != 0 {
		t.Errorf("Items = %#v, want an empty list typed End", saved["Items"])
	}
	if _, ok := chest.UpdateTag()["components"]; ok {
		t.Error("the update tag carries components")
	}
	if _, ok := NewSpawnerState().SpawnerTag()["components"]; ok {
		t.Error("the spawner tag carries components")
	}
}

// TestCrafterLoadSkipsADisabledSlotThatHoldsAnItem checks a saved crafter
// disables only the empty slots its disabled_slots lists.
// vanilla: CrafterBlockEntity.loadAdditional
func TestCrafterLoadSkipsADisabledSlotThatHoldsAnItem(t *testing.T) {
	stone, _ := registry.ItemID("minecraft:stone")
	be := newBlockEntity(KindCrafter, 4, 64, 0)
	be.Slots[0] = protocol.ItemStack{ItemID: stone, Count: 1}
	tag := be.EncodeNBT()
	tag["disabled_slots"] = nbt.IntArray{0, 1}

	back, ok := decodeBlockEntity(tag)
	if !ok {
		t.Fatal("the saved crafter did not decode")
	}
	if back.DisabledSlots[0] || !back.DisabledSlots[1] {
		t.Errorf("disabled slots = %v, want only slot 1", back.DisabledSlots)
	}

	// An in-place load clears the states first, so slot 0, disabled before
	// it was given an item, ends up enabled.
	inPlace := newBlockEntity(KindCrafter, 4, 64, 0)
	inPlace.DisabledSlots[0] = true
	inPlace.loadFrom(tag)
	if inPlace.Slots[0].Count != 1 || inPlace.DisabledSlots[0] || !inPlace.DisabledSlots[1] {
		t.Errorf("in place: slot 0 count %d, disabled slots = %v, want slot 0 holding stone and only slot 1 disabled",
			inPlace.Slots[0].Count, inPlace.DisabledSlots)
	}
}

// TestANamedCommandBlockKeepsNoCustomNameLeftover checks the name a command
// block reads itself is not also kept as a leftover component.
func TestANamedCommandBlockKeepsNoCustomNameLeftover(t *testing.T) {
	id, ok := registry.ItemID("minecraft:command_block")
	if !ok {
		t.Fatal("the item registry has no minecraft:command_block")
	}
	stack := protocol.ItemStack{ItemID: id, Count: 1}
	name := protocol.Text{Text: "Named"}
	stack.CustomName = &name
	if left := leftoverComponents(KindCommandBlock, stack); len(left) != 0 {
		t.Fatalf("leftover components = %v, want none", left)
	}
}
