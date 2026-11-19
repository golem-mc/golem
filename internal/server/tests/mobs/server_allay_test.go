package mobs

import (
	"fmt"
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

const allay = "minecraft:allay"

func allayType() int32 { id, _ := registry.EntityTypeID(allay); return int32(id) }

// allayWorld joins a survival player at (0.5, 64, 0.5) holding count of item.
func allayWorld(t *testing.T, item string, count int32) (*server.Server, *servertest.Client) {
	t.Helper()
	srv, c := servertest.BreedingPlayer(t, "survival", "Keeper", item, count)
	servertest.HoldTicks(t, srv)
	return srv, c
}

func TestAnAllaySpawnsAsItsOwnType(t *testing.T) {
	t.Parallel()
	srv, c := allayWorld(t, "", 0)
	id := srv.SpawnMob(allay, 3.5, 65, 0.5)
	m := c.ExpectMobSpawn(allayType())
	if m.EntityID != id || m.Health != 20 {
		t.Fatalf("spawn = %d, health %v; want %d, 20", m.EntityID, m.Health, id)
	}
}

func TestAnAllayBringsAMatchingItemToItsPlayer(t *testing.T) {
	t.Parallel()
	srv, c := allayWorld(t, "minecraft:diamond", 1)
	id := srv.SpawnMob(allay, 2.5, 65, 0.5)
	c.ExpectMobSpawn(allayType())
	c.UseOn(id)
	c.Settle()
	if got := srv.InventorySlot("Keeper", player.HotbarSlot).Count; got != 0 {
		t.Fatalf("diamonds left = %d, want 0", got)
	}
	if held := srv.MobEquipment(id, "mainhand"); held.ItemID != servertest.MustItemID(t, "minecraft:diamond") {
		t.Fatalf("allay holds %v, want a diamond", held)
	}
	srv.SpawnItemEntity(5.5, 64, 0.5, "minecraft:diamond", 1)
	near := false
	servertest.UntilTicks(t, srv, 400, "a diamond thrown near the player", func() bool {
		for _, it := range srv.ItemsInDimension("minecraft:overworld") {
			// The first diamond lies 5 blocks off; the delivered one lands by the player.
			if math.Abs(it.X-0.5) < 3 && math.Abs(it.Z-0.5) < 3 {
				near = true
			}
		}
		return near
	})
}

func TestAnEmptyHandTakesTheHeldItemBack(t *testing.T) {
	t.Parallel()
	srv, c := allayWorld(t, "minecraft:diamond", 1)
	id := srv.SpawnMob(allay, 2.5, 65, 0.5)
	c.ExpectMobSpawn(allayType())
	c.UseOn(id)
	c.Settle()
	c.UseOn(id)
	c.Settle()
	if got := srv.InventorySlot("Keeper", player.HotbarSlot); got.Count != 1 || got.ItemID != servertest.MustItemID(t, "minecraft:diamond") {
		t.Fatalf("hotbar = %v, want the diamond back", got)
	}
	if held := srv.MobEquipment(id, "mainhand"); !held.Empty() {
		t.Fatalf("allay still holds %v", held)
	}
}

func TestADancingAllayDuplicatesOnAnAmethystShard(t *testing.T) {
	t.Parallel()
	srv, c := allayWorld(t, "minecraft:music_disc_cat", 1)
	srv.SetBlock(3, 64, 0, registry.BlockStates["minecraft:jukebox"])
	id := srv.SpawnMob(allay, 2.5, 65, 0.5)
	c.ExpectMobSpawn(allayType())
	c.UseBlock(3, 64, 0)
	c.Settle()
	srv.TickMobs(2)
	if m := servertest.MetaOf(c.AwaitMetadataFor(id), protocol.MetadataIndexAllayDancing); m == nil || !m.FlagVal {
		t.Fatalf("dancing metadata = %v, want true", m)
	}
	srv.GiveSlot("Keeper", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:amethyst_shard"), Count: 2})
	c.UseOn(id)
	c.Settle()
	servertest.UntilTicks(t, srv, 5, "the duplicate", func() bool { return srv.MobCount() == 2 })
	if got := srv.InventorySlot("Keeper", player.HotbarSlot).Count; got != 1 {
		t.Fatalf("shards left = %d, want 1", got)
	}
	// On cooldown the allay takes the shard into its empty hand instead.
	c.UseOn(id)
	c.Settle()
	if got := srv.MobCount(); got != 2 {
		t.Fatalf("mobs = %d after the cooldown, want 2", got)
	}
}

func TestAnAllayArrivingAtAPlayingJukeboxDances(t *testing.T) {
	t.Parallel()
	srv, c := allayWorld(t, "minecraft:music_disc_cat", 1)
	srv.SetBlock(3, 64, 0, registry.BlockStates["minecraft:jukebox"])
	c.UseBlock(3, 64, 0)
	c.Settle()
	id := srv.SpawnMob(allay, 2.5, 65, 0.5)
	c.ExpectMobSpawn(allayType())
	// The song's own events have passed; the 20-tick repeat reaches the allay.
	servertest.TickBlockEntitiesByHand(srv, 21)
	srv.TickMobs(2)
	if m := servertest.MetaOf(c.AwaitMetadataFor(id), protocol.MetadataIndexAllayDancing); m == nil || !m.FlagVal {
		t.Fatalf("dancing metadata = %v, want true", m)
	}
}

// allayHolding spawns an allay by the player and puts stack in its hand.
func allayHolding(t *testing.T, srv *server.Server, c *servertest.Client, stack protocol.ItemStack) int32 {
	t.Helper()
	id := srv.SpawnMob(allay, 2.5, 65, 0.5)
	c.ExpectMobSpawn(allayType())
	srv.SetMobHand(id, stack)
	return id
}

func TestAnAllayCollectsItemsOfOtherComponents(t *testing.T) {
	t.Parallel()
	srv, c := allayWorld(t, "", 0)
	sword := protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:iron_sword"), Count: 1}
	id := allayHolding(t, srv, c, sword)
	worn := sword
	worn.Damage = 7
	srv.SpawnItemStack(3.5, 64, 0.5, worn)
	servertest.UntilTicks(t, srv, 200, "the damaged sword", func() bool { return len(srv.MobInventory(id)) == 1 })
}

func TestAnAllayRefusesOtherPotionContents(t *testing.T) {
	t.Parallel()
	srv, c := allayWorld(t, "", 0)
	id := allayHolding(t, srv, c, servertest.WaterBottle(t))
	// Right below the allay, inside its pickup reach; one pickup pass decides.
	srv.SpawnItemStack(2.5, 64, 0.5, servertest.SwiftnessPotion(t))
	servertest.RunTicks(t, srv, 1)
	if inv := srv.MobInventory(id); len(inv) != 0 {
		t.Fatalf("allay holding water took %v", inv)
	}
}

// The note block sits within the player's reach; the player then walks off so
// the note's top and the player's eyes are far apart.
const (
	allayNoteX, allayNoteZ = 3, 2
	awayX, awayZ           = -5.5, 0.5
)

// noteAllay gives an allay a diamond, plays a note block and moves the player away.
func noteAllay(t *testing.T) (*server.Server, *servertest.Client) {
	t.Helper()
	srv, c := allayWorld(t, "minecraft:diamond", 1)
	srv.SetBlock(allayNoteX, 64, allayNoteZ, registry.BlockStates["minecraft:note_block"])
	id := srv.SpawnMob(allay, 2.5, 65, 0.5)
	c.ExpectMobSpawn(allayType())
	c.UseOn(id)
	c.Settle()
	c.UseBlock(allayNoteX, 64, allayNoteZ)
	c.Settle()
	srv.TickPhysics(8)
	c.MoveToGround(awayX, 64, awayZ, true)
	c.Settle()
	return srv, c
}

// diamondLandsNear lays a diamond and waits for one to rest
// within 4 blocks of (x, z), the other being the player's or the note's.
func diamondLandsNear(t *testing.T, srv *server.Server, x, z float64) {
	t.Helper()
	srv.SpawnItemEntity(0.5, 64, -4.5, "minecraft:diamond", 1)
	servertest.UntilTicks(t, srv, 400, "a diamond delivered", func() bool {
		for _, it := range srv.ItemsInDimension("minecraft:overworld") {
			if math.Abs(it.X-x) < 4 && math.Abs(it.Z-z) < 4 {
				return true
			}
		}
		return false
	})
}

func TestAnAllayDeliversToAPlayedNoteBlock(t *testing.T) {
	t.Parallel()
	srv, _ := noteAllay(t)
	diamondLandsNear(t, srv, allayNoteX+0.5, allayNoteZ+0.5)
}

func TestAllayNoteWoolOcclusion(t *testing.T) {
	t.Parallel()
	for _, wall := range []string{"white_wool", "glass"} {
		for _, refresh := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/refresh=%t", wall, refresh), func(t *testing.T) {
				t.Parallel()
				srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Listener")
				c.MoveToGround(0.5, 64, 0.5, true)
				c.Settle()
				servertest.HoldTicks(t, srv)
				srv.SetBlock(0, 64, 0, registry.BlockNoteBlock.DefaultState())
				for y := int32(64); y < 70; y++ {
					for z := int32(-3); z <= 3; z++ {
						srv.SetBlock(2, y, z, registry.BlockStates["minecraft:"+wall])
					}
				}
				brain := ""
				if refresh {
					brain = `Brain:{memories:{"minecraft:liked_noteblock":{value:{dimension:"minecraft:overworld",pos:[I;0,64,0]}},"minecraft:liked_noteblock_cooldown_ticks":{value:123}}},`
				}
				srv.RunLine(`summon allay 4.5 65 0.5 {` + brain + `PersistenceRequired:1b}`)
				c.ExpectMobSpawn(allayType())
				c.UseBlock(0, 64, 0)
				c.Settle()
				servertest.RunTicks(t, srv, 1)

				query := func(key string) string {
					t.Helper()
					for _, text := range srv.RunLine(`data get entity @e[type=allay,limit=1] Brain.memories."minecraft:` + key + `".value`) {
						if text.Translate == "commands.data.entity.query" && len(text.With) == 2 {
							return text.With[1].Plain()
						}
					}
					return ""
				}
				note, cooldown := query("liked_noteblock"), query("liked_noteblock_cooldown_ticks")
				if wall == "white_wool" && !refresh {
					if note != "" || cooldown != "" {
						t.Fatalf("heard note through wool: note=%s, cooldown=%s", note, cooldown)
					}
					return
				}
				wantCooldown := "599"
				if wall == "white_wool" {
					wantCooldown = "122"
				}
				if want := `{pos: [I; 0, 64, 0], dimension: "minecraft:overworld"}`; note != want || cooldown != wantCooldown {
					t.Fatalf("note=%s, cooldown=%s; want %s, %s", note, cooldown, want, wantCooldown)
				}
			})
		}
	}
}

func TestAnAllayReturnsToThePlayerWhenTheNoteBlockIsGone(t *testing.T) {
	t.Parallel()
	srv, _ := noteAllay(t)
	srv.SetBlock(allayNoteX, 64, allayNoteZ, registry.BlockStates["minecraft:air"])
	diamondLandsNear(t, srv, awayX, awayZ)
}

func TestAnAllayForgetsAPlayedNoteBlockAfterSixHundredTicks(t *testing.T) {
	t.Parallel()
	srv, _ := noteAllay(t)
	servertest.RunTicks(t, srv, 601)
	diamondLandsNear(t, srv, awayX, awayZ)
}

func TestAnEmptyHandThrowsOutWhatTheAllayCarries(t *testing.T) {
	t.Parallel()
	srv, c := allayWorld(t, "minecraft:diamond", 1)
	id := srv.SpawnMob(allay, 2.5, 65, 0.5)
	c.ExpectMobSpawn(allayType())
	c.UseOn(id)
	c.Settle()
	srv.SpawnItemEntity(3.5, 64, 0.5, "minecraft:diamond", 1)
	servertest.UntilTicks(t, srv, 200, "a carried diamond", func() bool { return len(srv.MobInventory(id)) == 1 })
	c.UseOn(id)
	c.Settle()
	if inv := srv.MobInventory(id); len(inv) != 0 {
		t.Fatalf("allay still carries %v", inv)
	}
	found := false
	for _, it := range srv.ItemsInDimension("minecraft:overworld") {
		found = found || it.Item == "minecraft:diamond"
	}
	if !found {
		t.Fatal("the carried diamond was not thrown out")
	}
}

func TestASavedAllayStillDeliversToItsPlayer(t *testing.T) {
	t.Parallel()
	srv, c := allayWorld(t, "", 0)
	w := servertest.OfflineUUIDWords("Keeper")
	srv.RunLine(fmt.Sprintf("summon minecraft:allay 2.5 65 0.5 {equipment:{mainhand:{id:\"minecraft:diamond\",count:1}},Brain:{memories:{\"minecraft:liked_player\":{value:[I;%d,%d,%d,%d]}}}}",
		int32(w[0]), int32(w[1]), int32(w[2]), int32(w[3])))
	c.ExpectMobSpawn(allayType())
	srv.SpawnItemEntity(5.5, 64, 0.5, "minecraft:diamond", 1)
	near := false
	servertest.UntilTicks(t, srv, 400, "a diamond thrown near the player", func() bool {
		for _, it := range srv.ItemsInDimension("minecraft:overworld") {
			if math.Abs(it.X-0.5) < 3 && math.Abs(it.Z-0.5) < 3 {
				near = true
			}
		}
		return near
	})
}

func TestAnAllayReportsAnEmptyInventoryAfterDelivery(t *testing.T) {
	t.Parallel()
	srv, c := servertest.BreedingPlayer(t, "survival", "Keeper", "", 0, servertest.OpsListing(t, "Keeper"))
	servertest.HoldTicks(t, srv)
	w := servertest.OfflineUUIDWords("Keeper")
	c.ChatCommand(fmt.Sprintf(`summon minecraft:allay 2.5 65 0.5 {Inventory:[{Slot:0b,id:"minecraft:diamond",count:1}],equipment:{mainhand:{id:"minecraft:diamond",count:1}},Brain:{memories:{"minecraft:liked_player":{value:[I;%d,%d,%d,%d]}}}}`,
		int32(w[0]), int32(w[1]), int32(w[2]), int32(w[3])))
	id := c.ExpectMobSpawn(allayType()).EntityID
	if inv := srv.MobInventory(id); len(inv) != 1 || inv[0].ItemID != int32(registry.ItemDiamond) || inv[0].Count != 1 {
		t.Fatalf("carried inventory = %v, want one diamond", inv)
	}
	servertest.UntilTicks(t, srv, 400, "the delivered diamond in the player's inventory", func() bool {
		stack := srv.InventorySlot("Keeper", player.HotbarSlot)
		return stack.ItemID == int32(registry.ItemDiamond) && stack.Count == 1
	})
	if inv := srv.MobInventory(id); len(inv) != 0 {
		t.Fatalf("carried inventory after delivery = %v, want empty", inv)
	}
	if items := srv.ItemsInDimension("minecraft:overworld"); len(items) != 0 {
		t.Fatalf("dropped items after delivery = %v, want none", items)
	}
	if got := entityData(t, c, "@e[type=minecraft:allay,limit=1] Inventory"); got != "[]" {
		t.Fatalf("Inventory after delivery = %s, want []", got)
	}
}

func TestAnAllayKeepsItsLikedNoteBlockAcrossASave(t *testing.T) {
	t.Parallel()
	srv, c := allayWorld(t, "", 0)
	srv.SetBlock(allayNoteX, 64, allayNoteZ, registry.BlockStates["minecraft:note_block"])
	w := servertest.OfflineUUIDWords("Keeper")
	srv.RunLine(fmt.Sprintf("summon minecraft:allay 2.5 65 0.5 {equipment:{mainhand:{id:\"minecraft:diamond\",count:1}},Brain:{memories:{"+
		"\"minecraft:liked_player\":{value:[I;%d,%d,%d,%d]},"+
		"\"minecraft:liked_noteblock\":{value:{dimension:\"minecraft:overworld\",pos:[I;%d,64,%d]}},"+
		"\"minecraft:liked_noteblock_cooldown_ticks\":{value:600}}}}",
		int32(w[0]), int32(w[1]), int32(w[2]), int32(w[3]), allayNoteX, allayNoteZ))
	c.ExpectMobSpawn(allayType())
	query := ""
	for _, text := range srv.RunLine(`data get entity @e[type=minecraft:allay,limit=1] Brain.memories."minecraft:liked_noteblock".value.pos`) {
		if text.Translate == "commands.data.entity.query" && len(text.With) == 2 {
			query = text.With[1].Plain()
		}
	}
	if want := fmt.Sprintf("[I; %d, 64, %d]", allayNoteX, allayNoteZ); query != want {
		t.Fatalf("liked_noteblock pos = %q, want %q", query, want)
	}
	c.MoveToGround(awayX, 64, awayZ, true)
	c.Settle()
	diamondLandsNear(t, srv, allayNoteX+0.5, allayNoteZ+0.5)
}

func TestAnAllayReturnsToItsPlayerWhenItsLikedNoteBlockIsTooFarAway(t *testing.T) {
	t.Parallel()
	srv, c := allayWorld(t, "", 0)
	// The allay's block X is 2, so this note is 1025 blocks away.
	t.Cleanup(srv.HoldChunk(1027, 0))
	if !srv.SetBlock(1027, 64, 0, registry.BlockStates["minecraft:note_block"]) {
		t.Fatal("could not place the distant note block")
	}
	w := servertest.OfflineUUIDWords("Keeper")
	srv.RunLine(fmt.Sprintf(`summon minecraft:allay 2.5 65 0.5 {equipment:{mainhand:{id:"minecraft:diamond",count:1}},Inventory:[{id:"minecraft:diamond",count:1}],Brain:{memories:{"minecraft:liked_player":{value:[I;%d,%d,%d,%d]},"minecraft:liked_noteblock":{value:{dimension:"minecraft:overworld",pos:[I;1027,64,0]}},"minecraft:liked_noteblock_cooldown_ticks":{value:600}}}}`,
		int32(w[0]), int32(w[1]), int32(w[2]), int32(w[3])))
	c.ExpectMobSpawn(allayType())
	servertest.RunTicks(t, srv, 5)
	items := srv.ItemsInDimension("minecraft:overworld")
	if len(items) != 1 || items[0].Item != "minecraft:diamond" || math.Abs(items[0].X-0.5) >= 3 || math.Abs(items[0].Z-0.5) >= 3 {
		t.Fatalf("items delivered near the liked player = %v, want one diamond", items)
	}
	for _, text := range srv.RunLine(`data get entity @e[type=minecraft:allay,limit=1] Brain.memories."minecraft:liked_noteblock"`) {
		if text.Translate == "commands.data.entity.query" {
			t.Fatalf("the allay still remembers its distant note block: %v", text)
		}
	}
}
