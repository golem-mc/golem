package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// The strider's boost, its jockeys, its walk to lava and where a rider steps
// off.
// vanilla: Strider, FoodOnAStickItem

// striderBoostTime follows the boost length (metadata 18) of one entity; it
// stays zero until the server tells one.
func striderBoostTime(c *servertest.Client, id int32, boost *int32) func(int32, []byte) {
	return func(packet int32, data []byte) {
		if packet != protocol.IDPlayEntityMetadata {
			return
		}
		r := protocol.NewReader(data)
		if r.ReadVarInt() != id {
			return
		}
		for r.Err() == nil {
			index := r.ReadU8()
			if index == protocol.MetadataEnd {
				return
			}
			switch r.ReadVarInt() {
			case protocol.MetadataByte:
				r.ReadU8()
			case protocol.MetadataVarInt:
				if v := r.ReadVarInt(); index == protocol.MetadataIndexStriderBoost {
					*boost = v
				}
			case protocol.MetadataFloat:
				r.ReadF32()
			case protocol.MetadataBoolean:
				r.ReadBool()
			default:
				return
			}
		}
	}
}

// TestTheFungusStickBoostsTheStriderOnceAtATime: using the stick on a steered
// strider tells a boost of 140 to 980 ticks and wears the stick by one; a
// second use while it runs does neither.
func TestTheFungusStickBoostsTheStriderOnceAtATime(t *testing.T) {
	t.Parallel()
	c, srv, id := striderSteer(t, "minecraft:warped_fungus_on_a_stick")
	var boost int32
	record := striderBoostTime(c, id, &boost)
	c.UseItemRightClicks(protocol.HandMain)
	c.Settle()
	srv.TickMobs(1)
	c.SettleAndCollect(record)
	if boost < 140 || boost > 980 {
		t.Fatalf("boost length %d, want 140 to 980", boost)
	}
	if got := srv.InventorySlot("Rider", player.HotbarSlot).Damage; got != 1 {
		t.Fatalf("stick wear %d after one use, want 1", got)
	}
	c.UseItemRightClicks(protocol.HandMain)
	c.Settle()
	if got := srv.InventorySlot("Rider", player.HotbarSlot).Damage; got != 1 {
		t.Errorf("stick wear %d after a use during the boost, want 1", got)
	}
}

// TestTheFungusStickDoesNothingWithoutASteeredStrider: the stick used with no
// strider under the player is not worn.
func TestTheFungusStickDoesNothingWithoutASteeredStrider(t *testing.T) {
	t.Parallel()
	srv, c := striderWorld(t)
	stick, _ := registry.ItemID("minecraft:warped_fungus_on_a_stick")
	srv.GiveSlot("Rider", player.HotbarSlot, protocol.ItemStack{ItemID: stick, Count: 1})
	c.Settle()
	c.UseItemRightClicks(protocol.HandMain)
	c.Settle()
	if got := srv.InventorySlot("Rider", player.HotbarSlot).Damage; got != 0 {
		t.Errorf("stick wear %d with no strider under the rider, want 0", got)
	}
}

// TestTheFungusStickCountsAsUsedOnlyWhenItBoostsNothing: a use with no strider
// under the player counts; the use that boosts one does not.
// vanilla: FoodOnAStickItem.use
func TestTheFungusStickCountsAsUsedOnlyWhenItBoostsNothing(t *testing.T) {
	t.Parallel()
	const stick = "minecraft:warped_fungus_on_a_stick"
	srv, c := striderWorld(t)
	srv.GiveSlot("Rider", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItemID(t, stick), Count: 1})
	c.Settle()
	c.UseItemRightClicks(protocol.HandMain)
	c.Settle()
	if got := srv.StatValue("Rider", registry.StatUsed, stick); got != 1 {
		t.Errorf("used with no strider = %d, want 1", got)
	}

	c2, srv2, _ := striderSteer(t, stick)
	c2.UseItemRightClicks(protocol.HandMain)
	c2.Settle()
	srv2.TickMobs(1)
	if got := srv2.StatValue("Rider", registry.StatUsed, stick); got != 0 {
		t.Errorf("used by a boosting use = %d, want 0", got)
	}
}

// TestABrokenFungusStickBecomesAFishingRod: the use that wears the stick out
// leaves a fishing rod in the hand.
func TestABrokenFungusStickBecomesAFishingRod(t *testing.T) {
	t.Parallel()
	srv, c := striderWorld(t)
	stick, _ := registry.ItemID("minecraft:warped_fungus_on_a_stick")
	srv.GiveSlot("Rider", player.HotbarSlot, protocol.ItemStack{ItemID: stick, Count: 1, Damage: 99, CustomName: &protocol.Text{Text: "Goad"}})
	c.Settle()
	c.ChatCommand("summon minecraft:strider 5.5 64 5.5 " + striderSaddleTag)
	spawn := c.ExpectMobSpawn(striderTypeID(t))
	c.UseOn(spawn.EntityID)
	c.Settle()
	c.UseItemRightClicks(protocol.HandMain)
	c.Settle()
	rod, _ := registry.ItemID("minecraft:fishing_rod")
	if got := srv.InventorySlot("Rider", player.HotbarSlot); got.ItemID != rod || got.Count != 1 {
		t.Errorf("hand holds item %d x%d, want one fishing rod %d", got.ItemID, got.Count, rod)
	}
	if got := srv.InventorySlot("Rider", player.HotbarSlot); got.CustomName == nil || got.CustomName.Text != "Goad" {
		t.Errorf("the rod lost the stick's custom name: %v", got.CustomName)
	}
}

// TestStridersRollJockeysWhenTheySpawn: among striders stood up fresh, some
// carry a zombified piglin holding the stick and some a baby strider, and a
// baby strider never carries one.
// vanilla: Strider.finalizeSpawn
func TestStridersRollJockeysWhenTheySpawn(t *testing.T) {
	t.Parallel()
	srv, _ := striderWorld(t)
	for seed := int64(1); seed <= 150; seed++ {
		srv.SpawnMobSeeded("minecraft:strider", 20.5, 64, 20.5, seed)
	}
	stick, _ := registry.ItemID("minecraft:warped_fungus_on_a_stick")
	kind := map[int32]string{}
	baby := map[int32]bool{}
	mobs := srv.Mobs()
	for _, m := range mobs {
		kind[m.ID], baby[m.ID] = m.Kind, m.Baby
	}
	piglins, babies := 0, 0
	for _, m := range mobs {
		if m.Vehicle == 0 {
			continue
		}
		if kind[m.Vehicle] != "minecraft:strider" || baby[m.Vehicle] {
			t.Fatalf("%s %d rides %s (baby %v), want a grown strider", m.Kind, m.ID, kind[m.Vehicle], baby[m.Vehicle])
		}
		switch m.Kind {
		case "minecraft:zombified_piglin":
			piglins++
			if got := srv.MobHand(m.ID); got.ItemID != stick {
				t.Errorf("jockey piglin holds item %d, want the stick %d", got.ItemID, stick)
			}
		case "minecraft:strider":
			babies++
			if !m.Baby {
				t.Errorf("strider jockey %d is grown", m.ID)
			}
		default:
			t.Errorf("%s rides a strider", m.Kind)
		}
	}
	if piglins == 0 || babies == 0 {
		t.Errorf("%d piglin and %d baby strider jockeys among 150 striders, want some of each", piglins, babies)
	}
}

// TestStriderJockeysRespectMonsterSpawningRules checks summons and natural
// arrivals, including a blocked piglin roll that falls through to a baby.
// vanilla: Strider.finalizeSpawn, ServerLevel.isSpawningMonsters
func TestStriderJockeysRespectMonsterSpawningRules(t *testing.T) {
	t.Parallel()
	for _, rule := range []string{world.RuleSpawnMobs.String(), world.RuleSpawnMonsters.String()} {
		for _, reason := range []string{"summon", "natural"} {
			t.Run(rule+"/"+reason, func(t *testing.T) {
				srv, _ := striderWorld(t)
				srv.SetGameRule(rule, false)
				var fallbackParent int32
				for seed := int64(1); seed <= 150; seed++ {
					id := srv.SpawnMobSeeded("minecraft:strider", 20.5, 64, 20.5, seed, reason == "natural")
					// Seed 28 rolls zero for both the piglin and baby checks.
					if seed == 28 {
						fallbackParent = id
					}
				}
				piglins, babies, fallback := 0, 0, false
				for _, m := range srv.Mobs() {
					if m.Kind == "minecraft:zombified_piglin" {
						piglins++
					}
					if m.Kind == "minecraft:strider" && m.Vehicle != 0 {
						if !m.Baby {
							t.Errorf("grown strider %d rides %d", m.ID, m.Vehicle)
						}
						babies++
						fallback = fallback || m.Vehicle == fallbackParent
					}
				}
				if piglins != 0 || babies == 0 {
					t.Errorf("%d piglin and %d baby jockeys with %s off, want no piglins and some babies", piglins, babies, rule)
				}
				if !fallback {
					t.Error("the blocked piglin roll did not fall through to a baby strider")
				}
			})
		}
	}
}

// TestAStriderWalksToNearbyLava: a strider on dry ground within eight blocks
// of lava ends up at its edge.
// vanilla: Strider.StriderGoToLavaGoal
func TestAStriderWalksToNearbyLava(t *testing.T) {
	t.Parallel()
	srv, _ := striderWorld(t)
	layLava(t, srv, 11, 10, 14, 14)
	id := srv.SpawnMob("minecraft:strider", 5.5, 64, 12.5)
	for range 250 {
		srv.TickMobs(1)
	}
	x, _, _, ok := srv.MobPos(id)
	if !ok {
		t.Fatal("the strider is gone")
	}
	if x < 11 {
		t.Errorf("strider at x=%v after 250 ticks, want it at the lava's edge from x=11", x)
	}
}

// TestARiderStepsOffAStriderOntoDryGroundNotIntoLava: a strider on lava puts
// its rider on the solid cells beside it rather than in the lava.
// vanilla: Strider.getDismountLocationForPassenger
func TestARiderStepsOffAStriderOntoDryGroundNotIntoLava(t *testing.T) {
	t.Parallel()
	srv, c := striderWorld(t)
	layLava(t, srv, 4, 4, 6, 6)
	// The join leaves the rider facing east, so the stone is on that side.
	for z := int32(4); z <= 6; z++ {
		srv.SetBlock(6, 63, z, servertest.BlockState(t, "minecraft:stone"))
	}
	c.ChatCommand("summon minecraft:strider 5.5 64 5.5 " + striderSaddleTag)
	spawn := c.ExpectMobSpawn(striderTypeID(t))
	srv.TickMobs(10)
	c.UseOn(spawn.EntityID)
	c.Settle()
	c.SendSneak(true)
	c.Settle()
	pos := srv.PlayerPosition("Rider")
	if pos.Y < 63.95 || pos.Y > 64.05 || pos.X < 6 || pos.X >= 7 {
		t.Errorf("rider stands at (%v, %v, %v), want on the stone at y=64 over x=6", pos.X, pos.Y, pos.Z)
	}
}

// TestARiderIsNotPutInLavaWhenNoDryCellIsBeside: with lava all around, the
// rider is set on top of the strider.
func TestARiderIsNotPutInLavaWhenNoDryCellIsBeside(t *testing.T) {
	t.Parallel()
	srv, c := striderWorld(t)
	layLava(t, srv, 4, 4, 6, 6)
	c.ChatCommand("summon minecraft:strider 5.5 64 5.5 " + striderSaddleTag)
	spawn := c.ExpectMobSpawn(striderTypeID(t))
	srv.TickMobs(10)
	c.UseOn(spawn.EntityID)
	c.Settle()
	c.SendSneak(true)
	c.Settle()
	if pos := srv.PlayerPosition("Rider"); pos.Y < 65 {
		t.Errorf("rider stands at y=%v, want on top of the strider above 65", pos.Y)
	}
}
