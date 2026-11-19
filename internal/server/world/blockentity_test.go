package world

import (
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
