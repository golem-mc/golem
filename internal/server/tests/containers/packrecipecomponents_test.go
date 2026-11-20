package containers

import (
	"bytes"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// withComponents returns stack with the named components read onto it.
func withComponents(t *testing.T, stack protocol.ItemStack, components map[string]nbt.Tag) protocol.ItemStack {
	t.Helper()
	for name, value := range components {
		if !registry.ReadComponent(&stack, name, value) {
			t.Fatalf("component %s is refused", name)
		}
	}
	return stack
}

// vanilla: ItemStackTemplate.CODEC, ShapedRecipe.assemble
func TestAPackRecipeResultKeepsItsComponents(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackRecipes(registry.PackRecipes{}) })
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	servertest.WritePackFile(t, cfg, "p", "pk/recipe/wand.json", `{"type":"minecraft:crafting_shaped","pattern":["#"],
			"key":{"#":"minecraft:cobblestone"},
			"result":{"id":"minecraft:stick","components":{"minecraft:custom_name":"Wand","minecraft:enchantment_glint_override":true}}}`)
	_, w := servertest.OpenWindowed(t, cfg)
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:crafting_table", 1))
	w.PlaceBlock(4, 63, 0, protocol.FaceTop)
	w.ExpectBlockAck()
	w.UseBlock(4, 64, 0)
	w.ExpectContainerOpen(protocol.MenuCrafting, false, 0, 0, 0)
	w.SetCreativeSlot(36, servertest.ItemStack(t, "minecraft:cobblestone", 1))
	w.PutInGrid(37, 1)
	w.Click(0, 0, protocol.ClickModePickup)
	wand := withComponents(t, servertest.ItemStack(t, "minecraft:stick", 1), map[string]nbt.Tag{
		"minecraft:custom_name":                nbt.String("Wand"),
		"minecraft:enchantment_glint_override": nbt.Byte(1),
	})
	w.ExpectClickResult(map[int16]protocol.ItemStack{
		0: {}, 1: {}, 2: {}, 3: {}, 4: {}, 5: {}, 6: {}, 7: {}, 8: {}, 9: {},
	}, wand)
}

// vanilla: AbstractCookingRecipe.assemble, AbstractFurnaceBlockEntity.burn
func TestAPackSmeltingResultKeepsItsComponents(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackRecipes(registry.PackRecipes{}) })
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	servertest.WritePackFile(t, cfg, "p", "pk/recipe/tagged.json", `{"type":"minecraft:smelting","ingredient":"minecraft:feather","cookingtime":200,
			"result":{"id":"minecraft:stick","components":{"minecraft:custom_data":{"wand":1}}}}`)
	srv, _ := servertest.OpenWindowed(t, cfg)
	if !srv.PlaceContainer(4, 64, 0, "minecraft:furnace") {
		t.Fatal("could not place the furnace")
	}
	for slot, stack := range map[int]protocol.ItemStack{
		0: servertest.ItemStack(t, "minecraft:feather", 1),
		1: servertest.ItemStack(t, "minecraft:coal", 1),
	} {
		if !srv.SetInventoryAt(4, 64, 0, slot, stack) {
			t.Fatalf("could not fill furnace slot %d", slot)
		}
	}
	servertest.HoldTicks(t, srv)
	servertest.RunTicks(t, srv, 201)
	want := withComponents(t, servertest.ItemStack(t, "minecraft:stick", 1), map[string]nbt.Tag{
		"minecraft:custom_data": nbt.Compound{"wand": nbt.Int(1)},
	})
	if got := srv.InventoryAt(4, 64, 0); len(got) != 3 || !got[2].Equal(want) {
		t.Errorf("the furnace holds %+v, want the tagged stick in the output", got)
	}
}

// taggedStick is the stick the pack recipes below make.
func taggedStick(t *testing.T) protocol.ItemStack {
	t.Helper()
	return withComponents(t, servertest.ItemStack(t, "minecraft:stick", 1), map[string]nbt.Tag{
		"minecraft:custom_data": nbt.Compound{"wand": nbt.Int(1)},
	})
}

const taggedStickResult = `"result":{"id":"minecraft:stick","components":{"minecraft:custom_data":{"wand":1}}}`

// vanilla: SingleItemRecipe.assemble
func TestAPackStonecuttingResultKeepsItsComponents(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackRecipes(registry.PackRecipes{}) })
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	servertest.WritePackFile(t, cfg, "p", "pk/recipe/cut.json",
		`{"type":"minecraft:stonecutting","ingredient":"minecraft:feather",`+taggedStickResult+`}`)
	_, w := servertest.OpenWindowed(t, cfg)
	openStonecutterCreative(t, w)
	feather := servertest.ItemStack(t, "minecraft:feather", 1)
	w.SetCreativeSlot(36, feather)
	w.Click(stoneHotbar0, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{stoneHotbar0: {}}, feather)
	w.Click(stoneInputSlot, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{stoneInputSlot: feather}, protocol.ItemStack{})
	w.ClickButton(0)
	expectCorrectedSlots(w, map[int16]protocol.ItemStack{stoneResultSlot: taggedStick(t)})
}

// vanilla: SmithingTransformRecipe.assemble, TransmuteRecipe.createWithOriginalComponents
func TestAPackSmithingResultLaysItsComponentsOverTheBases(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackRecipes(registry.PackRecipes{}) })
	cfg := servertest.SurvivalTestConfig()
	cfg.LevelName = t.TempDir()
	servertest.WritePackFile(t, cfg, "p", "pk/recipe/upgrade.json",
		`{"type":"minecraft:smithing_transform","template":"minecraft:feather","base":"minecraft:diamond_pickaxe",
		"addition":"minecraft:cobblestone","result":{"id":"minecraft:netherite_pickaxe","components":{"minecraft:custom_name":"Wand"}}}`)
	srv, w := servertest.OpenWindowed(t, cfg)
	w.OpenSmithing(srv)
	w.GiveAndLay(srv, servertest.ItemStack(t, "minecraft:feather", 1), smithTemplateSlot)
	pickaxe := servertest.ItemStack(t, "minecraft:diamond_pickaxe", 1)
	pickaxe.Damage = 7
	pickaxe.Enchantments = map[int32]int32{servertest.SharpnessID(t): 5}
	w.GiveAndLay(srv, pickaxe, smithBaseSlot)
	stone := servertest.ItemStack(t, "minecraft:cobblestone", 1)
	srv.GiveSlot("Testificate", player.HotbarSlot, stone)
	w.Drain()
	w.Click(smithHotbar0, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{smithHotbar0: {}}, stone)
	w.Click(smithAdditionSlot, 0, protocol.ClickModePickup)
	want := withComponents(t, servertest.ItemStack(t, "minecraft:netherite_pickaxe", 1), map[string]nbt.Tag{
		"minecraft:custom_name": nbt.String("Wand"),
	})
	want.Damage = pickaxe.Damage
	want.Enchantments = pickaxe.Enchantments
	w.ExpectClickResult(map[int16]protocol.ItemStack{
		smithAdditionSlot: stone,
		smithResultSlot:   want,
	}, protocol.ItemStack{})
}

// vanilla: CampfireBlockEntity.cookTick
func TestAPackCampfireResultKeepsItsComponents(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackRecipes(registry.PackRecipes{}) })
	cfg := servertest.SurvivalTestConfig()
	cfg.LevelName = t.TempDir()
	servertest.WritePackFile(t, cfg, "p", "pk/recipe/fire.json",
		`{"type":"minecraft:campfire_cooking","ingredient":"minecraft:feather","cookingtime":100,`+taggedStickResult+`}`)
	srv, w := servertest.OpenWindowed(t, cfg)
	srv.StopTick()
	lit, ok := registry.FindState("minecraft:campfire", map[string]string{"lit": "true"})
	if !ok {
		t.Fatal("no lit campfire state")
	}
	srv.SetBlock(3, 64, 0, lit)
	srv.GiveSlot("Testificate", player.HotbarSlot, servertest.ItemStack(t, "minecraft:feather", 1))
	w.Settle()
	w.UseBlock(3, 64, 0)
	w.Settle()
	servertest.TickBlockEntitiesByHand(srv, 100)
	var got []protocol.ItemStack
	for _, it := range srv.ItemsInDimension("minecraft:overworld") {
		got = append(got, it.Stack)
	}
	if want := taggedStick(t); len(got) != 1 || !got[0].Equal(want) {
		t.Errorf("the campfire dropped %+v, want the tagged stick", got)
	}
}

// vanilla: ServerPlayer.recipeBook, RecipeDisplay.result
func TestARecipeBookDisplaysAPackResultWithItsComponents(t *testing.T) {
	t.Cleanup(func() { registry.ApplyPackRecipes(registry.PackRecipes{}) })
	cfg := servertest.SurvivalTestConfig()
	cfg.LevelName = t.TempDir()
	servertest.WritePackFile(t, cfg, "p", "pk/recipe/cut.json",
		`{"type":"minecraft:stonecutting","ingredient":"minecraft:feather",`+taggedStickResult+`}`)
	srv, c := servertest.Dial(t, cfg, servertest.OpsListing(t, "DJ"))
	c.Login("DJ")
	c.WatchBook = true
	c.Configure()
	c.EnterWorldAs("DJ")
	c.Settle()
	c.ChatCommand("recipe give @s pk:cut")
	data := c.ExpectWithinTicks(srv, 5, protocol.IDPlayRecipeBookAdd)
	var slot protocol.Buffer
	slot.PutVarInt(5) // slot display: item stack
	slot.PutItemTemplate(taggedStick(t))
	if !bytes.Contains(data, slot.Bytes()) {
		t.Errorf("the book entry %x does not display the tagged stick %x", data, slot.Bytes())
	}
}
