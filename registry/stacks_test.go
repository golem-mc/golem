package registry

import (
	"maps"
	"reflect"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
)

// TestPotionContentsSurviveTheStackCodec writes a potion carrying a colour, a
// named custom effect with a hidden twin and a custom name, then reads it
// back: the whole of what vanilla's PotionContents.CODEC names survives the
// file's NBT shape.
func TestPotionContentsSurviveTheStackCodec(t *testing.T) {
	color := int32(0x123456)
	name := "Brew"
	speed, jump := MustMobEffect("minecraft:speed"), MustMobEffect("minecraft:jump_boost")
	want := protocol.PotionContents{
		Potion:      MustPotion("minecraft:swiftness"),
		CustomColor: &color,
		CustomName:  &name,
		CustomEffects: []protocol.PotionEffectSpec{
			{
				ID: jump, Amplifier: 1, Duration: 200, ShowParticles: true, ShowIcon: true,
				Hidden: &protocol.PotionEffectSpec{
					ID: jump, Duration: 50, ShowParticles: true, ShowIcon: true,
				},
			},
			{ID: speed, Duration: 100, ShowParticles: true},
		},
	}
	stack := protocol.ItemStack{ItemID: MustItem("minecraft:potion"), Count: 1}
	stack.SetPotionContents(want)

	tag := StackTag(stack)
	components, ok := nbt.Get[nbt.Compound](tag.Entries(), "components")
	if !ok {
		t.Fatal("the stack was written without its components")
	}
	if _, ok := components["minecraft:potion_contents"]; !ok {
		t.Errorf("the file's components = %v, want the potion_contents named", components)
	}

	got, ok := StoredStack(tag)
	if !ok {
		t.Fatal("the stored reading refused the potion it wrote")
	}
	contents, ok := got.PotionContentsOf()
	if !ok {
		t.Fatal("the potion came back without its contents")
	}
	want.CustomEffects[0].Hidden = nil // the saved copy drops hidden effects
	if !reflect.DeepEqual(contents, want) {
		t.Errorf("contents =\n%+v\nwant\n%+v", contents, want)
	}
}

// potionEffectEntry digs the first custom effect compound out of a written
// potion stack.
func potionEffectEntry(t *testing.T, tag nbt.CompoundTag) nbt.Compound {
	t.Helper()
	components, ok := nbt.Get[nbt.Compound](tag.Entries(), "components")
	if !ok {
		t.Fatal("the stack was written without components")
	}
	contents, ok := nbt.Get[nbt.Compound](components, "minecraft:potion_contents")
	if !ok {
		t.Fatalf("components = %v, want potion_contents", components)
	}
	list, ok := nbt.Get[nbt.List](contents, "custom_effects")
	if !ok || len(list.Items) != 1 {
		t.Fatalf("contents = %v, want one custom effect", contents)
	}
	entry, ok := nbt.CompoundView(list.Items[0])
	if !ok {
		t.Fatalf("custom effect = %v, want a compound", list.Items[0])
	}
	return entry
}

// TestLodestoneTrackerSurvivesTheStackCodec writes a compass pointed at a
// nether lodestone and reads it back: the file's tracker is the vanilla
// codec's shape — the target's dimension named and its block an int array,
// tracked at its default left out — and what the stack carries comes back
// as it went in.
// vanilla: LodestoneTracker.CODEC, GlobalPos.MAP_CODEC, BlockPos.CODEC
func TestLodestoneTrackerSurvivesTheStackCodec(t *testing.T) {
	want := protocol.LodestoneTracker{
		Target:  &protocol.LodestoneTarget{Dimension: "minecraft:the_nether", X: -5, Y: 100, Z: 42},
		Tracked: true,
	}
	stack := protocol.ItemStack{ItemID: MustItem("minecraft:compass"), Count: 1}
	stack.SetLodestoneTracker(want)

	tag := StackTag(stack)
	components, ok := nbt.Get[nbt.Compound](tag.Entries(), "components")
	if !ok {
		t.Fatal("the stack was written without its components")
	}
	tracker, ok := nbt.Get[nbt.Compound](components, "minecraft:lodestone_tracker")
	if !ok {
		t.Fatalf("the file's components = %v, want the lodestone_tracker named", components)
	}
	if _, hasTracked := tracker["tracked"]; hasTracked {
		t.Errorf("the tracker = %v, want tracked left out at its default", tracker)
	}
	target, ok := nbt.Get[nbt.Compound](tracker, "target")
	if !ok {
		t.Fatalf("the tracker = %v, want a target", tracker)
	}
	if got, _ := nbt.Get[nbt.String](target, "dimension"); got != "minecraft:the_nether" {
		t.Errorf("dimension = %q, want minecraft:the_nether", got)
	}
	if got, _ := nbt.Get[nbt.IntArray](target, "pos"); len(got) != 3 ||
		got[0] != -5 || got[1] != 100 || got[2] != 42 {
		t.Errorf("pos = %v, want [-5 100 42]", got)
	}

	got, ok := StoredStack(tag)
	if !ok {
		t.Fatal("the stored reading refused the compass it wrote")
	}
	tracked, ok := got.LodestoneTrackerOf()
	if !ok {
		t.Fatal("the compass came back without its tracker")
	}
	if !reflect.DeepEqual(tracked, want) {
		t.Errorf("tracker =\n%+v\nwant\n%+v", tracked, want)
	}
}

// TestBlockStatePropertiesSurviveTheStackCodec writes a light carrying the
// level its block_state component names and reads it back: the properties
// survive the file's NBT shape both ways.
func TestBlockStatePropertiesSurviveTheStackCodec(t *testing.T) {
	stack := protocol.ItemStack{ItemID: MustItem("minecraft:light"), Count: 1}
	stack.SetBlockStateProperties(map[string]string{"level": "5"})

	tag := StackTag(stack)
	components, ok := nbt.Get[nbt.Compound](tag.Entries(), "components")
	if !ok {
		t.Fatal("the stack was written without its components")
	}
	blockState, ok := nbt.Get[nbt.Compound](components, "minecraft:block_state")
	if !ok {
		t.Errorf("the file's components = %v, want the block_state named", components)
	} else if level, ok := nbt.Get[nbt.String](blockState, "level"); !ok || string(level) != "5" {
		t.Errorf("the file's block_state = %v, want level 5", blockState)
	}

	got, ok := StoredStack(tag)
	if !ok {
		t.Fatal("the stored reading refused the light it wrote")
	}
	props, ok := got.BlockStatePropertiesOf()
	if !ok {
		t.Fatal("the light came back without its block_state")
	}
	if !reflect.DeepEqual(props, map[string]string{"level": "5"}) {
		t.Errorf("block_state properties = %v, want the level five alone", props)
	}
}

// TestMapIDSurvivesTheStackCodec writes a filled map naming the map's saved
// data and reads it back: the plain int the file's shape is survives both
// ways, so a map brought over from vanilla keeps pointing at its pixels and
// the save hands them back.
// vanilla: MapId.CODEC
func TestMapIDSurvivesTheStackCodec(t *testing.T) {
	stack := protocol.ItemStack{ItemID: MustItem("minecraft:filled_map"), Count: 1}
	stack.SetMapID(3)

	tag := StackTag(stack)
	components, ok := nbt.Get[nbt.Compound](tag.Entries(), "components")
	if !ok {
		t.Fatal("the stack was written without its components")
	}
	if id, ok := nbt.Get[nbt.Int](components, "minecraft:map_id"); !ok || id != 3 {
		t.Errorf("the file's components = %v, want the map_id 3 as a plain int", components)
	}

	got, ok := StoredStack(tag)
	if !ok {
		t.Fatal("the stored reading refused the map it wrote")
	}
	if id, ok := got.MapIDOf(); !ok || id != 3 {
		t.Errorf("the map came back with id %d, %v; want 3", id, ok)
	}
}

func TestBookContentSurvivesTheStackCodec(t *testing.T) {
	filtered := "f"
	writable := protocol.ItemStack{ItemID: MustItem("minecraft:writable_book"), Count: 1}
	wantWritable := protocol.WritableBook{Pages: []protocol.FilterablePage{{Raw: "one"}, {Raw: "two", Filtered: &filtered}}}
	if !writable.SetWritableBook(wantWritable) {
		t.Fatal("SetWritableBook refused")
	}
	written := protocol.ItemStack{ItemID: MustItem("minecraft:written_book"), Count: 1}
	wantWritten := protocol.WrittenBook{
		Title: "T", Author: "A", Generation: 2, Resolved: true,
		Pages: []protocol.WrittenPage{{Raw: protocol.Text{Text: "p1"}}, {Raw: protocol.Text{Text: "p2"}, Filtered: &protocol.Text{Text: "q"}}},
	}
	if !written.SetWrittenBook(wantWritten) {
		t.Fatal("SetWrittenBook refused")
	}
	gotW, ok := StoredStack(StackTag(writable))
	if !ok {
		t.Fatal("writable book refused")
	}
	if book, ok := gotW.WritableBookOf(); !ok || !reflect.DeepEqual(book, wantWritable) {
		t.Errorf("writable = %+v, %v, want %+v", book, ok, wantWritable)
	}
	gotS, ok := StoredStack(StackTag(written))
	if !ok {
		t.Fatal("written book refused")
	}
	if book, ok := gotS.WrittenBookOf(); !ok || !reflect.DeepEqual(book, wantWritten) {
		t.Errorf("written = %+v, %v, want %+v", book, ok, wantWritten)
	}
}

// vanilla: WrittenBookContent.CONTENT_CODEC, Filterable.codec
func TestDynamicBookPagesSurviveTheStackCodec(t *testing.T) {
	pages := []nbt.Tag{
		nbt.Compound{"nbt": nbt.String("v"), "storage": nbt.String("t:s")},
		nbt.Compound{"selector": nbt.String("@s")},
		nbt.Compound{"score": nbt.Compound{"name": nbt.String("*"), "objective": nbt.String("o")}},
	}
	pages = append(pages,
		nbt.Compound{"text": nbt.String("Hover"), "hover_event": nbt.Compound{"action": nbt.String("show_text"), "value": pages[0]}},
		nbt.Compound{"translate": nbt.String("%s"), "with": nbt.NewList([]nbt.Tag{pages[0]})},
		nbt.NewList([]nbt.Tag{nbt.Compound{"text": nbt.String("prefix")}, pages[0]}),
	)
	for _, page := range pages {
		t.Run(nbt.Stringify(page), func(t *testing.T) {
			stack, ok := StoredStack(writtenBookEntry(nbt.Compound{
				"pages": nbt.NewList([]nbt.Tag{page, nbt.Compound{"raw": page, "filtered": page}}),
			}))
			if !ok {
				t.Fatal("stored book refused")
			}
			for range 2 {
				book, ok := stack.WrittenBookOf()
				if !ok || book.Resolved || len(book.Pages) != 2 || book.Pages[1].Filtered == nil {
					t.Fatalf("book = %+v, %v; want two unresolved pages with a filtered twin", book, ok)
				}
				for _, got := range []protocol.Text{book.Pages[0].Raw, book.Pages[1].Raw, *book.Pages[1].Filtered} {
					if tag := protocol.TextTag(got); !nbt.Equal(tag, page) {
						t.Errorf("page = %v, want %v", tag, page)
					}
				}
				stack, ok = StoredStack(StackTag(stack))
				if !ok {
					t.Fatal("reloaded book refused")
				}
			}
		})
	}
}

// writtenBookEntry is a stored written book whose content compound carries
// extra on top of a title and an author.
func writtenBookEntry(extra nbt.Compound) nbt.Compound {
	content := nbt.Compound{"title": nbt.String("T"), "author": nbt.String("A")}
	maps.Copy(content, extra)
	return nbt.Compound{
		"id": nbt.String("minecraft:written_book"), "count": nbt.Int(1),
		"components": nbt.Compound{"minecraft:written_book_content": content},
	}
}

// TestBannerLayersProfileAndBaseColorSurviveTheStackCodec writes a shield
// carrying an inline pattern layer beside a registry one and a base colour,
// and a head carrying a full profile with a signed property, then reads
// both back whole.
func TestBannerLayersProfileAndBaseColorSurviveTheStackCodec(t *testing.T) {
	shieldID, _ := ItemID("minecraft:shield")
	headID, _ := ItemID("minecraft:player_head")
	reg, _ := Get("minecraft:banner_pattern")
	shield := protocol.ItemStack{ItemID: shieldID, Count: 1}
	if !shield.SetBannerPatterns([]protocol.BannerPatternLayer{
		{Pattern: reg.ID("minecraft:stripe_top"), Dye: 14},
		{Pattern: -1, Dye: 3, AssetID: "example:wave", TranslationKey: "block.example.wave"},
	}) {
		t.Fatal("the layers were refused")
	}
	if !shield.SetBaseColor(11) {
		t.Fatal("the base colour was refused")
	}
	head := protocol.ItemStack{ItemID: headID, Count: 1}
	if !head.SetProfile(protocol.Profile{
		Name: "Notch", HasName: true,
		UUID: [16]byte{0x06, 0x9a, 0x79, 0xf4}, HasUUID: true,
		Properties: []protocol.ProfileProperty{{Name: "textures", Value: "e30=", Signature: "c2ln", Signed: true}},
	}) {
		t.Fatal("the profile was refused")
	}
	for _, stack := range []protocol.ItemStack{shield, head} {
		got, ok := StoredStack(StackTag(stack))
		if !ok {
			t.Fatalf("StoredStack refused %v", StackTag(stack))
		}
		if !got.Equal(stack) {
			t.Errorf("read back %+v, want %+v", got, stack)
		}
	}
}

// TestStoredStacksQualifyIDAndDefaultCount covers stored stack partial counts.
// vanilla: ItemStack.MAP_CODEC
func TestStoredStacksQualifyIDAndDefaultCount(t *testing.T) {
	stone, _ := ItemID("minecraft:stone")
	for _, tc := range []struct {
		name  string
		entry nbt.Compound
		want  int32 // 0 means the entry is rejected
	}{
		{"short id", nbt.Compound{"id": nbt.String("stone"), "count": nbt.Int(3)}, 3},
		{"missing count", nbt.Compound{"id": nbt.String("minecraft:stone")}, 1},
		{"byte count", nbt.Compound{"id": nbt.String("stone"), "count": nbt.Byte(5)}, 5},
		{"maximum count", nbt.Compound{"id": nbt.String("stone"), "count": nbt.Int(99)}, 99},
		{"fractional count", nbt.Compound{"id": nbt.String("stone"), "count": nbt.Double(5.9)}, 5},
		{"negative count", nbt.Compound{"id": nbt.String("stone"), "count": nbt.Int(-1)}, 1},
		{"zero count", nbt.Compound{"id": nbt.String("stone"), "count": nbt.Int(0)}, 1},
		{"count over 99", nbt.Compound{"id": nbt.String("minecraft:stone"), "count": nbt.Int(200)}, 1},
		{"missing id", nbt.Compound{"count": nbt.Int(1)}, 0},
		{"non-numeric count", nbt.Compound{"id": nbt.String("stone"), "count": nbt.String("2")}, 0},
		{"unknown id", nbt.Compound{"id": nbt.String("nonsense")}, 0},
		{"legacy Count key", nbt.Compound{"id": nbt.String("minecraft:stone"), "Count": nbt.Byte(3)}, 0},
	} {
		for _, reader := range []struct {
			name string
			read func(nbt.Compound) (protocol.ItemStack, bool)
		}{
			{"base", StoredStackBase},
			{"stack", func(entry nbt.Compound) (protocol.ItemStack, bool) { return StoredStack(entry) }},
			{"container", func(entry nbt.Compound) (protocol.ItemStack, bool) { return StoredContainerStack(entry) }},
		} {
			t.Run(reader.name+"/"+tc.name, func(t *testing.T) {
				got, ok := reader.read(tc.entry)
				if tc.want == 0 {
					if ok {
						t.Errorf("read %v, want rejected", got)
					}
					return
				}
				if !ok || got.ItemID != stone || got.Count != tc.want {
					t.Errorf("read %v, %v, want stone x%d", got, ok, tc.want)
				}
			})
		}
	}
}

// TestALockSurvivesTheStackCodec saves a stack carrying the lock component
// and reads it back.
func TestALockSurvivesTheStackCodec(t *testing.T) {
	id, _ := ItemID("minecraft:chest")
	lock := nbt.Compound{"items": nbt.List{Elem: nbt.TypeString, Items: []nbt.Tag{nbt.String("minecraft:tripwire_hook"), nbt.String("minecraft:chest")}}}
	got, ok := StoredStack(StackTag(protocol.ItemStack{ItemID: id, Count: 1, Lock: lock}))
	if !ok || !nbt.Equal(got.Lock, lock) {
		t.Errorf("stored stack lock = %v (ok %v), want %v", got.Lock, ok, lock)
	}
	// A one-element list is written as the element.
	single := nbt.Compound{"items": nbt.List{Elem: nbt.TypeString, Items: []nbt.Tag{nbt.String("diamond")}}}
	got, ok = StoredStack(StackTag(protocol.ItemStack{ItemID: id, Count: 1, Lock: single}))
	if want := (nbt.Compound{"items": nbt.String("minecraft:diamond")}); !ok || !nbt.Equal(got.Lock, want) {
		t.Errorf("stored singleton lock = %v (ok %v), want %v", got.Lock, ok, want)
	}
}

// TestDebugStickStateSurvivesTheStackCodec saves a stick's selection and reads
// it back, and refuses a property the block does not have.
func TestDebugStickStateSurvivesTheStackCodec(t *testing.T) {
	stack := protocol.ItemStack{ItemID: MustItem("minecraft:debug_stick"), Count: 1,
		DebugStick: map[string]string{"minecraft:oak_stairs": "half"}}
	tag := StackTag(stack)
	got, ok := StoredStack(tag)
	if !ok || !reflect.DeepEqual(got.DebugStick, stack.DebugStick) {
		t.Errorf("the stick read back = %v, %v, want %v", got.DebugStick, ok, stack.DebugStick)
	}
	components, _ := nbt.Get[nbt.Compound](tag.Entries(), "components")
	components["minecraft:debug_stick_state"] = nbt.Compound{"minecraft:stone": nbt.String("facing")}
	if _, ok := StoredStack(tag); ok {
		t.Error("a selection of a property stone lacks was read")
	}
}

// TestKnowledgeBookRecipesSurviveTheStackCodec saves a book's recipes and
// reads them back namespaced, and refuses a key that is no identifier.
func TestKnowledgeBookRecipesSurviveTheStackCodec(t *testing.T) {
	stack := protocol.ItemStack{ItemID: MustItem("minecraft:knowledge_book"), Count: 1,
		Recipes: []string{"minecraft:oak_boat", "minecraft:stick"}}
	tag := StackTag(stack)
	got, ok := StoredStack(tag)
	if !ok || !reflect.DeepEqual(got.Recipes, stack.Recipes) {
		t.Errorf("the recipes read back = %v, %v, want %v", got.Recipes, ok, stack.Recipes)
	}
	components, _ := nbt.Get[nbt.Compound](tag.Entries(), "components")
	components["minecraft:recipes"] = nbt.List{Elem: nbt.TypeString, Items: []nbt.Tag{nbt.String("oak_boat")}}
	if got, ok := StoredStack(tag); !ok || !reflect.DeepEqual(got.Recipes, []string{"minecraft:oak_boat"}) {
		t.Errorf("an unnamespaced key read back = %v, %v", got.Recipes, ok)
	}
	components["minecraft:recipes"] = nbt.List{Elem: nbt.TypeString, Items: []nbt.Tag{nbt.String("Oak Boat")}}
	if _, ok := StoredStack(tag); ok {
		t.Error("a key that is no identifier was read")
	}
}

func TestOminousBottleAmplifierSurvivesTheStackCodec(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name                   string
		item                   Item
		amplifier              int32
		patch, remove, present bool
	}{
		{"default bottle", ItemOminousBottle, 0, false, false, true},
		{"patched bottle", ItemOminousBottle, 3, true, false, true},
		{"plain apple", ItemApple, 0, false, false, false},
		{"patched apple", ItemApple, 2, true, false, true},
		{"zero apple", ItemApple, 0, true, false, true},
		{"removed default", ItemOminousBottle, 0, false, true, false},
		{"removed patch", ItemApple, 0, true, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stack := protocol.ItemStack{ItemID: int32(tc.item), Count: 1}
			if tc.patch {
				stack.SetOminousBottleAmplifier(tc.amplifier)
			}
			if tc.remove && !stack.Remove(protocol.DataComponentOminousBottleAmplifier) {
				t.Fatal("could not remove amplifier")
			}
			tag := StackTag(stack)
			components, _ := nbt.Get[nbt.Compound](tag.Entries(), "components")
			if _, patched := components["minecraft:ominous_bottle_amplifier"]; patched != (tc.patch && !tc.remove) {
				t.Errorf("stored amplifier patch = %v, want %v", patched, tc.patch && !tc.remove)
			}
			stored, ok := StoredStack(tag)
			if !ok {
				t.Fatal("stored stack did not decode")
			}
			var wire protocol.Buffer
			wire.PutSlot(stack)
			slot, ok := protocol.NewReader(wire.Bytes()).ReadSlot()
			if !ok {
				t.Fatal("slot did not decode")
			}
			for _, got := range []protocol.ItemStack{stack, stored, slot} {
				amplifier, present := got.OminousBottleAmplifier()
				if amplifier != tc.amplifier || present != tc.present || got.Removes(protocol.DataComponentOminousBottleAmplifier) != tc.remove {
					t.Errorf("amplifier = %d, %v, removed = %v; want %d, %v, %v", amplifier, present,
						got.Removes(protocol.DataComponentOminousBottleAmplifier), tc.amplifier, tc.present, tc.remove)
				}
			}
			stack.SetOminousBottleAmplifier(4)
			if amplifier, present := stack.OminousBottleAmplifier(); amplifier != 4 || !present {
				t.Errorf("replacement amplifier = %d, %v, want 4, true", amplifier, present)
			}
		})
	}
}

// TestChargedProjectilesSurviveTheStackCodec writes a crossbow loaded with
// three arrows and reads it back, and reads vanilla's shape for one rocket.
func TestChargedProjectilesSurviveTheStackCodec(t *testing.T) {
	arrow := protocol.ItemStack{ItemID: MustItem("minecraft:arrow"), Count: 1}
	crossbow := protocol.ItemStack{ItemID: MustItem("minecraft:crossbow"), Count: 1}
	crossbow.SetChargedProjectiles([]protocol.ItemStack{arrow, arrow, arrow})

	got, ok := StoredStack(StackTag(crossbow))
	if !ok {
		t.Fatal("the stored reading refused the loaded crossbow it wrote")
	}
	if loaded := got.ChargedProjectiles(); len(loaded) != 3 || loaded[0].ItemID != arrow.ItemID {
		t.Errorf("loaded = %+v, want three arrows", loaded)
	}

	unloaded := protocol.ItemStack{ItemID: crossbow.ItemID, Count: 1}
	if _, ok := nbt.Get[nbt.Compound](StackTag(unloaded).Entries(), "components"); ok {
		t.Error("an unloaded crossbow was written with components")
	}

	vanilla := nbt.Compound{
		"id": nbt.String("minecraft:crossbow"), "count": nbt.Int(1),
		"components": nbt.Compound{"minecraft:charged_projectiles": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
			nbt.Compound{"id": nbt.String("minecraft:firework_rocket")},
		}}},
	}
	got, ok = StoredStack(vanilla)
	if !ok {
		t.Fatal("the stored reading refused vanilla's loaded crossbow")
	}
	if loaded := got.ChargedProjectiles(); len(loaded) != 1 || loaded[0].ItemID != MustItem("minecraft:firework_rocket") || loaded[0].Count != 1 {
		t.Errorf("loaded = %+v, want one rocket", loaded)
	}
}

func TestTradeComponentsSurviveTheStackCodec(t *testing.T) {
	stack := protocol.ItemStack{ItemID: MustItem("minecraft:suspicious_stew"), Count: 1}
	stack.SetDyedColor(0xF9801D)
	effects := []protocol.StewEffect{{Effect: MustMobEffect("minecraft:night_vision"), Duration: 100}, {Effect: MustMobEffect("minecraft:saturation"), Duration: 7}}
	stack.SetSuspiciousStewEffects(effects)
	tag := StackTag(stack)
	components, _ := nbt.Get[nbt.Compound](tag.Entries(), "components")
	if components["minecraft:dyed_color"] != nbt.Int(0xF9801D) {
		t.Fatalf("saved colour %v", components)
	}
	list, _ := nbt.Get[nbt.List](components, "minecraft:suspicious_stew_effects")
	if len(list.Items) != 2 || list.Items[0].(nbt.CompoundTag).Entries()["id"] != nbt.String("minecraft:night_vision") {
		t.Fatalf("saved effects %v", list)
	}
	got, ok := StoredStack(tag)
	if !ok {
		t.Fatal("could not load the saved components")
	}
	if rgb, _ := got.DyedColorOf(); rgb != 0xF9801D || !reflect.DeepEqual(got.SuspiciousStewEffectsOf(), effects) {
		t.Fatalf("loaded stack %+v", got)
	}
}

// TestLoreCustomDataAndAttributeModifiersSurviveTheStackCodec saves a stack
// carrying the three raw-held components and reads it back equal.
func TestLoreCustomDataAndAttributeModifiersSurviveTheStackCodec(t *testing.T) {
	attr, _ := AttributeByName("minecraft:attack_damage")
	var stack protocol.ItemStack
	stack.ItemID = int32(ItemDiamondSword)
	stack.Count = 1
	stack.SetLore([]protocol.Text{{Text: "first"}, {Text: "second", Color: "red"}})
	stack.SetCustomData(nbt.Compound{"k": nbt.Int(3)})
	hand, _ := SlotGroupID("mainhand")
	stack.SetAttributeModifiers([]protocol.AttributeModifierEntry{
		{Attribute: attr.ID, ID: "minecraft:test", Amount: 2.5, Operation: 1, Slot: hand},
		{Attribute: attr.ID, ID: "minecraft:other", Amount: -1, Display: 1},
	})
	data, err := nbt.Encode("", StackTag(stack))
	if err != nil {
		t.Fatal(err)
	}
	_, decoded, err := nbt.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := StoredStack(decoded.(nbt.Compound))
	if !ok {
		t.Fatal("stored stack not read back")
	}
	if !reflect.DeepEqual(got.LoreOf(), stack.LoreOf()) {
		t.Errorf("lore = %v, want %v", got.LoreOf(), stack.LoreOf())
	}
	if !reflect.DeepEqual(got.CustomData(), stack.CustomData()) {
		t.Errorf("custom_data = %v", got.CustomData())
	}
	want, _ := stack.AttributeModifiersOf()
	have, _ := got.AttributeModifiersOf()
	if !reflect.DeepEqual(have, want) {
		t.Errorf("attribute modifiers = %v, want %v", have, want)
	}
}

// TestAConsumableComponentSurvivesTheStoredForm: own consumable and food,
// with every consume effect kind and a hidden effect, are written to a file and read back.
func TestAConsumableComponentSurvivesTheStoredForm(t *testing.T) {
	t.Parallel()
	item, _ := ItemID("minecraft:stick")
	stack := protocol.ItemStack{ItemID: item, Count: 1}
	speed := MustMobEffect("minecraft:speed")
	stack.SetConsumable(protocol.ConsumableComponent{
		Seconds: 0.5, Animation: protocol.ItemUseAnimationDrink, HasParticles: false,
		Sound: protocol.SoundHolder{Name: "test:custom"},
		Effects: []protocol.ConsumeEffectComponent{
			{Type: protocol.ConsumeEffectApplyEffects, Probability: 0.5,
				Effects: []protocol.PotionEffectSpec{{ID: speed, Duration: 200, ShowParticles: true,
					Hidden: &protocol.PotionEffectSpec{Duration: 400, Amplifier: 2, ShowParticles: true}}}},
			{Type: protocol.ConsumeEffectRemoveEffects, Remove: []int32{speed}},
			{Type: protocol.ConsumeEffectClearAllEffects},
			{Type: protocol.ConsumeEffectTeleportRandomly, Diameter: 8},
			{Type: protocol.ConsumeEffectPlaySound, Sound: protocol.SoundHolder{Name: "test:other"}},
		},
	})
	stack.SetFood(protocol.FoodComponent{Nutrition: 4, Saturation: 2.4, CanAlwaysEat: true})

	back, ok := StoredStack(StackTag(stack))
	if !ok {
		t.Fatal("the stored stack was refused")
	}
	own, _ := back.ConsumableOf()
	want, _ := stack.ConsumableOf()
	if !reflect.DeepEqual(own, want) {
		t.Errorf("consumable = %+v, want %+v", own, want)
	}
	if food, _ := back.FoodOf(); food.Nutrition != 4 || !food.CanAlwaysEat {
		t.Errorf("food = %+v", food)
	}
	c, ok := StackConsumable(back)
	if !ok || !c.Drink || c.Seconds != 0.5 || len(c.Effects) != 5 || c.Effects[4].Sound != (protocol.SoundHolder{Name: "test:other"}) {
		t.Errorf("StackConsumable = %+v, %v", c, ok)
	}

	// An inline sound named after the default is not the default reference
	// holder, so it is kept (vanilla: Holder.Direct never equals a Holder.Reference).
	inline := protocol.ItemStack{ItemID: item, Count: 1}
	inline.SetConsumable(protocol.ConsumableComponent{
		Seconds: 1.6, Animation: 1, HasParticles: true,
		Sound: protocol.SoundHolder{Name: "minecraft:entity.generic.eat"},
	})
	back, ok = StoredStack(StackTag(inline))
	if !ok {
		t.Fatal("the stored stack with an inline default sound was refused")
	}
	if own, _ := back.ConsumableOf(); own.Sound != (protocol.SoundHolder{Name: "minecraft:entity.generic.eat"}) {
		t.Errorf("inline default sound reloaded as %+v", own.Sound)
	}
}

func TestExplicitEmptyBundleContentsSurvivesTheStackCodec(t *testing.T) {
	stone := protocol.ItemStack{ItemID: MustItem("minecraft:stone"), Count: 1}
	if !ReadComponent(&stone, "minecraft:bundle_contents", nbt.List{Elem: nbt.TypeEnd}) {
		t.Fatal("ReadComponent refused empty SNBT list for minecraft:bundle_contents")
	}
	if contents, ok := stone.BundleContentsOf(); !ok || len(contents) != 0 {
		t.Fatalf("stone.BundleContentsOf() = (%v, %v), want empty explicit component", contents, ok)
	}
	tag := StackTag(stone)
	back, ok := StoredStack(tag)
	if !ok {
		t.Fatal("StoredStack refused stone with empty bundle_contents")
	}
	if contents, ok := back.BundleContentsOf(); !ok || len(contents) != 0 {
		t.Errorf("reloaded stone.BundleContentsOf() = (%v, %v), want empty explicit component", contents, ok)
	}

	// Empty bundle_contents on a bundle equals its default and is omitted.
	bundle := protocol.ItemStack{ItemID: MustItem("minecraft:bundle"), Count: 1}
	bundle.Remove(protocol.DataComponentBundleContents)
	if !ReadComponent(&bundle, "minecraft:bundle_contents", nbt.List{Elem: nbt.TypeEnd}) {
		t.Fatal("ReadComponent refused empty list on bundle")
	}
	if _, ok := bundle.BundleContentsOf(); ok || bundle.Removes(protocol.DataComponentBundleContents) {
		t.Errorf("bundle after empty bundle_contents = %+v, want unpatched default", bundle)
	}
}
