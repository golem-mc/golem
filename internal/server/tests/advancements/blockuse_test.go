package advancements

import (
	"path/filepath"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// The triggers of recipes unlocked, eyes of ender thrown, raid omens absorbed
// and blocks used.

// TestRecipeGiveObtainsTheRecipeAdvancement: a recipe added to the book fires
// recipe_unlocked, which its recipes/ advancement waits on; granting such an
// advancement unlocks its reward recipe.
// vanilla: ServerRecipeBook.addRecipes, RecipeUnlockedTrigger, AdvancementRewards.grant
func TestRecipeGiveObtainsTheRecipeAdvancement(t *testing.T) {
	t.Parallel()
	srv, c := servertest.AttachActor(t)
	const stoneAxe = "minecraft:recipes/tools/stone_axe"
	if srv.AdvancementObtained("Actor", stoneAxe, "has_the_recipe") {
		t.Fatal("stone_axe's recipe advancement was obtained before the recipe")
	}
	c.ChatCommand("recipe give @s minecraft:stone_axe")
	servertest.UntilTicks(t, srv, 20, stoneAxe+" obtained", func() bool {
		return srv.AdvancementObtained("Actor", stoneAxe, "has_the_recipe")
	})

	c.ChatCommand("advancement grant @s only minecraft:recipes/tools/iron_axe")
	c.ChatCommand("recipe give @s minecraft:iron_axe")
	servertest.AwaitCommandError(t, c, "commands.recipe.give.failed")
}

// fixedEyeTarget is a stronghold at one position.
type fixedEyeTarget struct{ x, y, z int32 }

func (f fixedEyeTarget) NearestEyeTarget(_, _, _ int32, _ int) (int32, int32, int32, bool) {
	return f.x, f.y, f.z, true
}

// TestPackBlockUseAndEnderEyeAdvancements: a data pack's advancements on
// default_block_use (opening a chest), any_block_use and used_ender_eye are
// granted, an eye's distance condition counting horizontally to the
// structure; a click on stone, which uses nothing, fires neither block use,
// and one on redstone ore fires any_block_use and item_used_on_block located
// at the ore but not default_block_use.
// vanilla: ServerPlayerGameMode.useItemOn, ServerGamePacketListenerImpl.handleUseItemOn,
// EnderEyeItem.use
func TestPackBlockUseAndEnderEyeAdvancements(t *testing.T) {
	cfg, data := packConfig(t)
	dir := filepath.Join(data, "pk", "advancement")
	writePackFile(t, filepath.Join(dir, "chest.json"), `{"criteria":{"used":{"trigger":"minecraft:default_block_use",`+
		`"conditions":{"location":[{"condition":"minecraft:block_state_property","block":"minecraft:chest"}]}}}}`)
	writePackFile(t, filepath.Join(dir, "any.json"), `{"criteria":{"used":{"trigger":"minecraft:any_block_use"}}}`)
	writePackFile(t, filepath.Join(dir, "default.json"), `{"criteria":{"used":{"trigger":"minecraft:default_block_use"}}}`)
	writePackFile(t, filepath.Join(dir, "used_on.json"), `{"criteria":{"used":{"trigger":"minecraft:item_used_on_block",`+
		`"conditions":{"location":[{"condition":"minecraft:block_state_property","block":"minecraft:redstone_ore"}]}}}}`)
	writePackFile(t, filepath.Join(dir, "used_on_stone.json"), `{"criteria":{"used":{"trigger":"minecraft:item_used_on_block",`+
		`"conditions":{"location":[{"condition":"minecraft:block_state_property","block":"minecraft:stone"}]}}}}`)
	writePackFile(t, filepath.Join(dir, "eye_near.json"),
		`{"criteria":{"used":{"trigger":"minecraft:used_ender_eye","conditions":{"distance":{"max":100}}}}}`)
	writePackFile(t, filepath.Join(dir, "eye_far.json"),
		`{"criteria":{"used":{"trigger":"minecraft:used_ender_eye","conditions":{"distance":{"min":1000}}}}}`)
	srv, c := joinOp(t, cfg, "Packer")
	obtained := func(id string) bool { return srv.AdvancementObtained("Packer", "pk:"+id, "used") }

	pos := srv.PlayerPosition("Packer")
	x, y, z := rules.BlockOf(pos.X)+2, rules.BlockOf(pos.Y), rules.BlockOf(pos.Z)
	servertest.Setblock(t, c, x, y, z, "minecraft:stone")
	c.UseBlock(x, y, z)
	c.Settle()
	if obtained("chest") || obtained("any") || obtained("default") {
		t.Fatal("a bare-handed click on stone fired a block use")
	}
	// Redstone ore's click is its useItemOn, not useWithoutItem.
	// vanilla: RedStoneOreBlock.useItemOn, ServerPlayerGameMode.useItemOn
	servertest.Setblock(t, c, x, y, z, "minecraft:redstone_ore")
	c.UseBlock(x, y, z)
	servertest.UntilTicks(t, srv, 20, "pk:any and pk:used_on obtained", func() bool {
		return obtained("any") && obtained("used_on")
	})
	if obtained("default") {
		t.Fatal("a bare-handed click on redstone ore fired default_block_use")
	}
	if obtained("used_on_stone") {
		t.Fatal("item_used_on_block on redstone ore passed a location of stone")
	}
	servertest.Setblock(t, c, x, y, z, "minecraft:chest")
	c.UseBlock(x, y, z)
	servertest.UntilTicks(t, srv, 20, "pk:chest obtained", func() bool { return obtained("chest") })

	srv.SetEyeTargets(fixedEyeTarget{int32(pos.X) + 40, 30, int32(pos.Z)})
	srv.GiveSlot("Packer", player.HotbarSlot, protocol.ItemStack{ItemID: int32(registry.ItemEnderEye), Count: 1})
	c.Settle()
	servertest.UseHeld(c, -90, 0)
	servertest.UntilTicks(t, srv, 20, "pk:eye_near obtained", func() bool { return obtained("eye_near") })
	if obtained("eye_far") {
		t.Error("an eye thrown 40 blocks from its structure passed a distance of at least 1000")
	}
}

// TestPackVoluntaryExileIsGrantedWhenARaidAbsorbsTheOmen: a data pack's
// advancement on voluntary_exile is granted when the raid omen a player
// carried into a village starts the raid.
// vanilla: Raid.absorbRaidOmen, CriteriaTriggers.RAID_OMEN
func TestPackVoluntaryExileIsGrantedWhenARaidAbsorbsTheOmen(t *testing.T) {
	cfg, data := packConfig(t)
	writePackFile(t, filepath.Join(data, "pk", "advancement", "exile.json"),
		`{"criteria":{"exiled":{"trigger":"minecraft:voluntary_exile"}}}`)
	srv, _ := joinOp(t, cfg, "Packer")
	srv.SetDifficulty(rules.DifficultyNormal)
	servertest.HoldTicks(t, srv)
	pos := srv.PlayerPosition("Packer")
	bell := [3]int32{int32(pos.X) + 3, int32(pos.Y), int32(pos.Z)}
	if !srv.SetBlock(bell[0], bell[1], bell[2], registry.MustState("minecraft:bell", nil)) {
		t.Fatal("the bell was not placed")
	}
	srv.SpawnMobInDimension("minecraft:villager", "minecraft:overworld", float64(bell[0])+0.5, float64(bell[1]), float64(bell[2])+1.5)
	servertest.UntilTicks(t, srv, 400, "the villager claims the bell", func() bool {
		return srv.VillageNear("minecraft:overworld", bell[0], bell[1], bell[2])
	})

	srv.GiveEffect("Packer", "minecraft:bad_omen", 100000, 0, true)
	servertest.RunTicks(t, srv, 2)
	if _, _, ok := srv.PlayerEffect("Packer", "minecraft:raid_omen"); !ok {
		t.Fatal("the bad omen did not become a raid omen")
	}
	if srv.AdvancementObtained("Packer", "pk:exile", "exiled") {
		t.Fatal("voluntary_exile fired before a raid absorbed the omen")
	}
	// The raid omen runs 600 ticks before the raid absorbs it.
	// vanilla: Raid.absorbRaidOmen, MobEffects.RAID_OMEN
	servertest.UntilTicks(t, srv, 620, "pk:exile obtained", func() bool {
		return srv.AdvancementObtained("Packer", "pk:exile", "exiled")
	})
}
