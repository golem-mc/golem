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

// TestPotionContentsAreReadFromVanillaNBT reads the shape a vanilla server
// writes for a potion: the potion named alone, and a custom effect with a
// byte amplifier, an explicit show_icon and a hidden twin that carries no id
// of its own.
func TestPotionContentsAreReadFromVanillaNBT(t *testing.T) {
	entry := nbt.Compound{
		"id":    nbt.String("minecraft:potion"),
		"count": nbt.Int(1),
		"components": nbt.Compound{
			"minecraft:potion_contents": nbt.Compound{
				"potion": nbt.String("minecraft:healing"),
				"custom_effects": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
					nbt.Compound{
						"id":        nbt.String("minecraft:regeneration"),
						"amplifier": nbt.Byte(1),
						"duration":  nbt.Int(600),
						"show_icon": nbt.Byte(1),
						"hidden_effect": nbt.Compound{
							"duration":  nbt.Int(50),
							"show_icon": nbt.Byte(1),
						},
					},
				}},
			},
		},
	}
	stack, ok := StoredStack(entry)
	if !ok {
		t.Fatal("the stored reading refused a vanilla-written potion")
	}
	contents, ok := stack.PotionContentsOf()
	if !ok {
		t.Fatal("the potion came back without its contents")
	}
	if contents.Potion != MustPotion("minecraft:healing") {
		t.Errorf("potion = %d, want healing", contents.Potion)
	}
	if len(contents.CustomEffects) != 1 {
		t.Fatalf("custom effects = %+v, want one", contents.CustomEffects)
	}
	effect := contents.CustomEffects[0]
	if effect.ID != MustMobEffect("minecraft:regeneration") || effect.Duration != 600 ||
		effect.Amplifier != 1 || !effect.ShowParticles || !effect.ShowIcon {
		t.Errorf("effect = %+v, want regeneration II for 600 ticks, particles and icon", effect)
	}
	if effect.Hidden != nil {
		t.Errorf("hidden effect = %+v, want none: a stack stores the wire form, which drops it", effect.Hidden)
	}
}

// TestPotionEffectAmplifierReadsAsAnUnsignedByte reads the amplifier the
// codec writes as a byte: a byte carries its unsigned value, another numeric
// must land in 0..255, and past that the reading refuses the effect.
func TestPotionEffectAmplifierReadsAsAnUnsignedByte(t *testing.T) {
	potionWith := func(amplifier nbt.Tag) nbt.Compound {
		return nbt.Compound{
			"id":    nbt.String("minecraft:potion"),
			"count": nbt.Int(1),
			"components": nbt.Compound{
				"minecraft:potion_contents": nbt.Compound{
					"custom_effects": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
						nbt.Compound{"id": nbt.String("minecraft:speed"), "amplifier": amplifier},
					}},
				},
			},
		}
	}
	for _, c := range []struct {
		amplifier nbt.Tag
		want      int32
	}{
		{nbt.Byte(1), 1},
		{nbt.Byte(-1), 255},
		{nbt.Int(255), 255},
	} {
		stack, ok := StoredStack(potionWith(c.amplifier))
		if !ok {
			t.Fatalf("the stored reading refused amplifier %v", c.amplifier)
		}
		contents, ok := stack.PotionContentsOf()
		if !ok || len(contents.CustomEffects) != 1 {
			t.Fatalf("effects = %+v, %v; want one", contents.CustomEffects, ok)
		}
		if got := contents.CustomEffects[0].Amplifier; got != c.want {
			t.Errorf("amplifier %v = %d, want %d", c.amplifier, got, c.want)
		}
	}
	if _, ok := StoredStack(potionWith(nbt.Int(256))); ok {
		t.Error("the stored reading took an amplifier past 255")
	}
}

// TestPotionEffectShowIconDefaultsToShowParticles reads the codec's default:
// show_icon absent takes show_particles, so particles off turns the icon off.
func TestPotionEffectShowIconDefaultsToShowParticles(t *testing.T) {
	entry := nbt.Compound{
		"id":    nbt.String("minecraft:potion"),
		"count": nbt.Int(1),
		"components": nbt.Compound{
			"minecraft:potion_contents": nbt.Compound{
				"custom_effects": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
					nbt.Compound{
						"id":             nbt.String("minecraft:speed"),
						"show_particles": nbt.Byte(0),
					},
				}},
			},
		},
	}
	stack, ok := StoredStack(entry)
	if !ok {
		t.Fatal("the stored reading refused the potion")
	}
	contents, ok := stack.PotionContentsOf()
	if !ok || len(contents.CustomEffects) != 1 {
		t.Fatalf("effects = %+v, %v; want one", contents.CustomEffects, ok)
	}
	if effect := contents.CustomEffects[0]; effect.ShowParticles || effect.ShowIcon {
		t.Errorf("effect = %+v, want no particles and no icon", effect)
	}
}

// TestPotionEffectWritesShowIconAlways writes an effect whose particles are
// off and whose icon is on: show_icon is written even though it is true, so
// the codec does not read the icon's absence as the particles' off.
func TestPotionEffectWritesShowIconAlways(t *testing.T) {
	stack := protocol.ItemStack{ItemID: MustItem("minecraft:potion"), Count: 1}
	stack.SetPotionContents(protocol.PotionContents{
		Potion: MustPotion("minecraft:swiftness"),
		CustomEffects: []protocol.PotionEffectSpec{
			{ID: MustMobEffect("minecraft:speed"), ShowParticles: false, ShowIcon: true},
		},
	})
	effect := potionEffectEntry(t, StackTag(stack))
	if effect["show_icon"] != nbt.Byte(1) || effect["show_particles"] != nbt.Byte(0) {
		t.Errorf("effect = %v, want show_icon 1 and show_particles 0", effect)
	}
}

// TestHiddenEffectIsNotWritten saves an effect with a hidden twin: the
// codec reads the copying accessor, so the twin is dropped.
func TestHiddenEffectIsNotWritten(t *testing.T) {
	stack := protocol.ItemStack{ItemID: MustItem("minecraft:potion"), Count: 1}
	stack.SetPotionContents(protocol.PotionContents{
		Potion: MustPotion("minecraft:swiftness"),
		CustomEffects: []protocol.PotionEffectSpec{
			{ID: MustMobEffect("minecraft:speed"), Duration: 100,
				Hidden: &protocol.PotionEffectSpec{ID: MustMobEffect("minecraft:speed"), Duration: 50}},
		},
	})
	effect := potionEffectEntry(t, StackTag(stack))
	if _, ok := effect["hidden_effect"]; ok {
		t.Errorf("effect = %v, want no hidden_effect", effect)
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

// TestPotionContentsReadFromThePotionAlone reads the codec's alternative to
// the full compound: a potion's name on its own carries that potion and
// nothing else.
func TestPotionContentsReadFromThePotionAlone(t *testing.T) {
	entry := nbt.Compound{
		"id":    nbt.String("minecraft:potion"),
		"count": nbt.Int(1),
		"components": nbt.Compound{
			"minecraft:potion_contents": nbt.String("minecraft:healing"),
		},
	}
	stack, ok := StoredStack(entry)
	if !ok {
		t.Fatal("the stored reading refused a potion named alone")
	}
	contents, ok := stack.PotionContentsOf()
	if !ok || contents.Potion != MustPotion("minecraft:healing") {
		t.Fatalf("contents = %+v, %v; want healing", contents, ok)
	}
	if contents.CustomColor != nil || len(contents.CustomEffects) != 0 || contents.CustomName != nil {
		t.Errorf("contents = %+v, want the potion alone", contents)
	}
}

// TestStoredPotionContentsSkipAnUnknownField checks that a potion_contents
// field this server has no word for is skipped, as the codec skips it.
// vanilla: PotionContents.CODEC
func TestStoredPotionContentsSkipAnUnknownField(t *testing.T) {
	entry := nbt.Compound{
		"id":    nbt.String("minecraft:potion"),
		"count": nbt.Int(1),
		"components": nbt.Compound{
			"minecraft:potion_contents": nbt.Compound{
				"potion": nbt.String("minecraft:healing"),
				"future": nbt.String("something"),
			},
		},
	}
	if _, ok := StoredStack(entry); !ok {
		t.Error("the stored reading refused a potion_contents field it does not know")
	}
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

// TestLodestoneTrackerIsReadFromVanillaNBT reads the shapes the codec takes
// from files it did not write: the block's ints as a list of numbers the
// way NbtOps also hands the codec one, an explicit tracked flag, and the
// targetless tracker whose two absent flags are their own defaults.
// vanilla: LodestoneTracker.CODEC, NbtOps.getIntStream
func TestLodestoneTrackerIsReadFromVanillaNBT(t *testing.T) {
	entry := nbt.Compound{
		"id":    nbt.String("minecraft:compass"),
		"count": nbt.Int(1),
		"components": nbt.Compound{
			"minecraft:lodestone_tracker": nbt.Compound{
				"target": nbt.Compound{
					"dimension": nbt.String("minecraft:the_end"),
					"pos": nbt.List{Elem: nbt.TypeInt, Items: []nbt.Tag{
						nbt.Int(3), nbt.Int(64), nbt.Int(-7),
					}},
				},
				"tracked": nbt.Byte(1),
			},
		},
	}
	stack, ok := StoredStack(entry)
	if !ok {
		t.Fatal("the stored reading refused a vanilla-written tracker")
	}
	tracker, ok := stack.LodestoneTrackerOf()
	if !ok {
		t.Fatal("the compass came back without its tracker")
	}
	if tracker.Target == nil || tracker.Target.Dimension != "minecraft:the_end" ||
		tracker.Target.X != 3 || tracker.Target.Y != 64 || tracker.Target.Z != -7 {
		t.Errorf("tracker = %+v, want the end's (3, 64, -7)", tracker)
	}
	if !tracker.Tracked {
		t.Error("tracker = untracked, want the explicit flag read back")
	}

	// A tracker without its target reads as the spinning one: the flags
	// carry the target's absence, and tracked stays at its default.
	entry["components"] = nbt.Compound{"minecraft:lodestone_tracker": nbt.Compound{}}
	stack, ok = StoredStack(entry)
	if !ok {
		t.Fatal("the stored reading refused a targetless tracker")
	}
	tracker, ok = stack.LodestoneTrackerOf()
	if !ok || tracker.Target != nil || !tracker.Tracked {
		t.Errorf("tracker = %+v, %v; want no target and tracked by default", tracker, ok)
	}
}

// TestLodestoneTrackerReadsAnyNumberAsTracked checks the tracked flag's
// codec: a boolean reads from any number, nonzero as true, so a file that
// wrote it as an int is read the way the game's own codec reads it.
// vanilla: Codec.BOOL
func TestLodestoneTrackerReadsAnyNumberAsTracked(t *testing.T) {
	entry := nbt.Compound{
		"id":    nbt.String("minecraft:compass"),
		"count": nbt.Int(1),
		"components": nbt.Compound{
			"minecraft:lodestone_tracker": nbt.Compound{"tracked": nbt.Int(5)},
		},
	}
	stack, ok := StoredStack(entry)
	if !ok {
		t.Fatal("the stored reading refused a tracked the codec reads as true")
	}
	if tracker, ok := stack.LodestoneTrackerOf(); !ok || !tracker.Tracked {
		t.Errorf("tracker = %+v, %v; want tracked read as true", tracker, ok)
	}

	entry["components"] = nbt.Compound{
		"minecraft:lodestone_tracker": nbt.Compound{"tracked": nbt.Long(0)},
	}
	stack, ok = StoredStack(entry)
	if !ok {
		t.Fatal("the stored reading refused a tracked the codec reads as false")
	}
	if tracker, ok := stack.LodestoneTrackerOf(); !ok || tracker.Tracked {
		t.Errorf("tracker = %+v, %v; want tracked read as false", tracker, ok)
	}
}

// TestStoredLodestoneTrackerRefusesWhatTheCodecDoesNotWrite checks the
// reading's refusals: a tracker naming a field the codec has no word for, a
// dimension the game's own parser would not read, a block of another number
// of ints, and a target missing one of its fields each leave the whole
// stack to the file rather than being saved back half-read.
func TestStoredLodestoneTrackerRefusesWhatTheCodecDoesNotWrite(t *testing.T) {
	trackerWith := func(tracker nbt.Tag) nbt.Compound {
		return nbt.Compound{
			"id":    nbt.String("minecraft:compass"),
			"count": nbt.Int(1),
			"components": nbt.Compound{
				"minecraft:lodestone_tracker": tracker,
			},
		}
	}
	pos := nbt.IntArray{2, 63, 1}
	target := nbt.Compound{"dimension": nbt.String("minecraft:overworld"), "pos": pos}
	for name, tracker := range map[string]nbt.Tag{
		"a field the codec has no word for": nbt.Compound{"target": target, "future": nbt.Byte(1)},
		"a dimension no parser would read": nbt.Compound{"target": nbt.Compound{
			"dimension": nbt.String("Overworld"), "pos": pos}},
		"a block of two ints": nbt.Compound{"target": nbt.Compound{
			"dimension": nbt.String("minecraft:overworld"), "pos": nbt.IntArray{2, 63}}},
		"a target missing its dimension": nbt.Compound{"target": nbt.Compound{"pos": pos}},
		"a target missing its block":     nbt.Compound{"target": nbt.Compound{"dimension": nbt.String("minecraft:overworld")}},
		"a tracked of another shape":     nbt.Compound{"target": target, "tracked": nbt.String("true")},
	} {
		if _, ok := StoredStack(trackerWith(tracker)); ok {
			t.Errorf("the stored reading took %s", name)
		}
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

// TestBlockStatePropertiesAreReadFromVanillaNBT reads the shape a vanilla
// server writes: the properties as plain strings, checked against nothing -
// which of them a block answers to is its placement's to decide. A value
// that is not a string refuses the whole stack.
func TestBlockStatePropertiesAreReadFromVanillaNBT(t *testing.T) {
	entry := nbt.Compound{
		"id":    nbt.String("minecraft:light"),
		"count": nbt.Int(1),
		"components": nbt.Compound{
			"minecraft:block_state": nbt.Compound{
				"level":       nbt.String("7"),
				"waterlogged": nbt.String("false"),
			},
		},
	}
	stack, ok := StoredStack(entry)
	if !ok {
		t.Fatal("the stored reading refused a vanilla-written light")
	}
	props, ok := stack.BlockStatePropertiesOf()
	if !ok {
		t.Fatal("the light came back without its block_state")
	}
	want := map[string]string{"level": "7", "waterlogged": "false"}
	if !reflect.DeepEqual(props, want) {
		t.Errorf("block_state properties = %v, want %v", props, want)
	}

	entry["components"] = nbt.Compound{"minecraft:block_state": nbt.Compound{
		"level": nbt.Int(7),
	}}
	if _, ok := StoredStack(entry); ok {
		t.Error("the stored reading took a block_state whose value is not a string")
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

// TestMapIDIsReadFromVanillaNBT reads the shape a vanilla server writes: the
// id as the plain int its codec takes, another number's tag narrow enough to
// count as one. A value that is no number is a stack no file holds.
// vanilla: MapId.CODEC, Codec.INT
func TestMapIDIsReadFromVanillaNBT(t *testing.T) {
	entry := func(id nbt.Tag) nbt.Compound {
		return nbt.Compound{
			"id":         nbt.String("minecraft:filled_map"),
			"count":      nbt.Int(1),
			"components": nbt.Compound{"minecraft:map_id": id},
		}
	}
	for _, id := range []nbt.Tag{nbt.Int(0), nbt.Byte(3), nbt.Long(9)} {
		stack, ok := StoredStack(entry(id))
		if !ok {
			t.Fatalf("the stored reading refused a vanilla-written map with map_id %v", id)
		}
		if _, ok := stack.MapIDOf(); !ok {
			t.Errorf("the map came back without its map_id %v", id)
		}
	}
	if _, ok := StoredStack(entry(nbt.String("0"))); ok {
		t.Error("the stored reading took a map_id whose value is a string")
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

func TestBookContentReadsBarePages(t *testing.T) {
	entry := nbt.Compound{
		"id": nbt.String("minecraft:writable_book"), "count": nbt.Int(1),
		"components": nbt.Compound{"minecraft:writable_book_content": nbt.Compound{
			"pages": nbt.List{Elem: nbt.TypeString, Items: []nbt.Tag{nbt.String("hi")}},
		}},
	}
	stack, ok := StoredStack(entry)
	if !ok {
		t.Fatal("refused")
	}
	if book, _ := stack.WritableBookOf(); len(book.Pages) != 1 || book.Pages[0].Raw != "hi" {
		t.Errorf("book = %+v", book)
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

// TestBookResolvedReadsAnyNumber: resolved is a Codec.BOOL, which over NBT
// takes any number and is true when it is not zero.
func TestBookResolvedReadsAnyNumber(t *testing.T) {
	for _, tc := range []struct {
		tag  nbt.Tag
		want bool
	}{
		{nbt.Byte(1), true},
		{nbt.Byte(2), true},
		{nbt.Byte(-1), true},
		{nbt.Int(1), true},
		{nbt.Double(0.5), true},
		{nbt.Byte(0), false},
		{nbt.Long(0), false},
	} {
		stack, ok := StoredStack(writtenBookEntry(nbt.Compound{"resolved": tc.tag}))
		if !ok {
			t.Errorf("resolved %v: stack refused", tc.tag)
			continue
		}
		book, ok := stack.WrittenBookOf()
		if !ok || book.Resolved != tc.want {
			t.Errorf("resolved %v: book %+v, %v, want resolved %v", tc.tag, book, ok, tc.want)
		}
	}
	if _, ok := StoredStack(writtenBookEntry(nbt.Compound{"resolved": nbt.String("true")})); ok {
		t.Error("took a resolved that is a string")
	}
}

// TestBookFilterableIgnoresExtraKeys: the full {raw, filtered} form is
// tried first and ignores keys it does not name.
func TestBookFilterableIgnoresExtraKeys(t *testing.T) {
	entry := nbt.Compound{
		"id": nbt.String("minecraft:writable_book"), "count": nbt.Int(1),
		"components": nbt.Compound{"minecraft:writable_book_content": nbt.Compound{
			"pages": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
				nbt.Compound{"raw": nbt.String("x"), "z": nbt.Int(1)},
			}},
		}},
	}
	stack, ok := StoredStack(entry)
	if !ok {
		t.Fatal("writable page with an extra key refused")
	}
	if book, _ := stack.WritableBookOf(); len(book.Pages) != 1 || book.Pages[0].Raw != "x" {
		t.Errorf("writable book = %+v", book)
	}
	stack, ok = StoredStack(writtenBookEntry(nbt.Compound{
		"title": nbt.Compound{"raw": nbt.String("T"), "z": nbt.Int(1)},
	}))
	if !ok {
		t.Fatal("title with an extra key refused")
	}
	if book, _ := stack.WrittenBookOf(); book.Title != "T" {
		t.Errorf("written book = %+v", book)
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

// TestBannerPatternNameWithoutNamespaceReadsAsMinecraft checks that a bare
// pattern name reads in the minecraft namespace.
func TestBannerPatternNameWithoutNamespaceReadsAsMinecraft(t *testing.T) {
	reg, _ := Get("minecraft:banner_pattern")
	layers, ok := BannerPatternsOf(nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{nbt.Compound{
		"pattern": nbt.String("stripe_top"),
		"color":   nbt.String("red"),
	}}})
	if !ok || len(layers) != 1 || layers[0].Pattern != reg.ID("minecraft:stripe_top") {
		t.Errorf("BannerPatternsOf(stripe_top) = %v, %v; want the minecraft:stripe_top layer", layers, ok)
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
	lock := nbt.Compound{"items": nbt.List{Elem: nbt.TypeString, Items: []nbt.Tag{nbt.String("minecraft:tripwire_hook")}}}
	got, ok := StoredStack(StackTag(protocol.ItemStack{ItemID: id, Count: 1, Lock: lock}))
	if !ok || !nbt.Equal(got.Lock, lock) {
		t.Errorf("stored stack lock = %v (ok %v), want %v", got.Lock, ok, lock)
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

func TestTradeComponentsReadVanillaCodecForms(t *testing.T) {
	var stack protocol.ItemStack
	if !ReadComponent(&stack, "minecraft:dyed_color", nbt.List{Elem: nbt.TypeFloat, Items: []nbt.Tag{nbt.Float(1), nbt.Float(0.5), nbt.Float(0)}}) {
		t.Fatal("could not read RGB channels")
	}
	if rgb, _ := stack.DyedColorOf(); rgb != -33024 {
		t.Fatalf("RGB channels = %x, want ffff7f00", rgb)
	}
	for _, duration := range []nbt.Tag{nil, nbt.String("bad"), nbt.Double(7.9), nbt.Int(-1)} {
		entry := nbt.Compound{"id": nbt.String("night_vision")}
		if duration != nil {
			entry["duration"] = duration
		}
		if !ReadComponent(&stack, "minecraft:suspicious_stew_effects", nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{entry}}) {
			t.Fatal("could not read effect")
		}
		want := int32(160)
		if duration == nbt.Double(7.9) {
			want = 7
		} else if duration == nbt.Int(-1) {
			want = -1
		}
		if got := stack.SuspiciousStewEffectsOf(); len(got) != 1 || got[0].Duration != want {
			t.Fatalf("effects %v", got)
		}
	}
	for _, tc := range []struct {
		name  string
		value nbt.Tag
	}{
		{"minecraft:dyed_color", nbt.String("bad")},
		{"minecraft:dyed_color", nbt.List{Elem: nbt.TypeFloat}},
		{"minecraft:suspicious_stew_effects", nbt.Compound{}},
		{"minecraft:suspicious_stew_effects", nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{nbt.Compound{"id": nbt.String("minecraft:unknown")}}}},
	} {
		if !ComponentRefused(tc.name, tc.value) {
			t.Fatalf("accepted %s = %v", tc.name, tc.value)
		}
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

// TestLoreReadsVanillaWrappedList reads a lore list whose plain line is
// wrapped under the empty name, as vanilla writes a mixed list.
func TestLoreReadsVanillaWrappedList(t *testing.T) {
	lore := nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
		nbt.Compound{"": nbt.String("a")},
		nbt.Compound{"text": nbt.String("b"), "color": nbt.String("red")},
	}}
	tag := nbt.Compound{
		"id":         nbt.String("minecraft:diamond_sword"),
		"count":      nbt.Int(1),
		"components": nbt.Compound{"minecraft:lore": lore},
	}
	data, err := nbt.Encode("", tag)
	if err != nil {
		t.Fatal(err)
	}
	_, decoded, err := nbt.Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := StoredStack(decoded.(nbt.Compound))
	if !ok {
		t.Fatal("stored stack not read")
	}
	want := []protocol.Text{{Text: "a"}, {Text: "b", Color: "red"}}
	if !reflect.DeepEqual(got.LoreOf(), want) {
		t.Fatalf("lore = %v, want %v", got.LoreOf(), want)
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

func TestContainerBookPartialsKeepStrictValidation(t *testing.T) {
	for _, page := range []string{`{nbt:"v"}`, `{text:"T",bold:"yes"}`, `{raw:"T",filtered:{nbt:"v"}}`} {
		t.Run(page, func(t *testing.T) {
			tag, err := nbt.ParseSNBT(`{title:"Invalid",author:"Tester",pages:[` + page + `]}`)
			if err != nil {
				t.Fatal(err)
			}
			entry := writtenBookEntry(tag)
			if _, ok := StoredStack(entry); ok {
				t.Fatal("strict stored stack reader accepted malformed content")
			}
			var scratch protocol.ItemStack
			if ReadComponent(&scratch, "minecraft:written_book_content", tag) || !ComponentRefused("minecraft:written_book_content", tag) {
				t.Fatal("strict component validation accepted malformed content")
			}
			slots := make([]protocol.ItemStack, 1)
			LoadSlottedItems(nbt.Compound{"Items": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{entry}}}, slots)
			if slots[0].Empty() {
				t.Fatal("container did not retain a partial book")
			}
			if saved, ok := StoredStack(StackTag(slots[0])); !ok || !saved.Equal(slots[0]) {
				t.Fatal("partial result did not round-trip through strict stack decoding")
			}
		})
	}
}
