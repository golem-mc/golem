package mobs

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/nbt"
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

func TestADolphinSpawnsWithItsMoistness(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Keeper", servertest.OpsListing(t, "Keeper"))
	c.Settle()
	servertest.HoldTicks(t, srv)
	for _, tc := range []struct {
		nbt  string
		want int32
	}{
		{"0", 0},
		{"1", 1},
		{"2400", 2400},
	} {
		c.ChatCommand("summon minecraft:dolphin 3.5 64 0.5 {NoAI:1b,Moistness:" + tc.nbt + "}")
		spawn := c.AwaitSpawn(int32(registry.EntityTypeDolphin))
		moistness := servertest.MetaOf(c.AwaitMetadataFor(spawn.EntityID), protocol.MetadataIndexDolphinMoistness)
		if tc.want == 2400 {
			if moistness != nil {
				t.Errorf("default moistness = %+v, want omitted", moistness)
			}
		} else if moistness == nil || moistness.Typ != protocol.MetadataVarInt || moistness.VarintVal != tc.want {
			t.Errorf("moistness %s: spawn metadata = %+v, want varint %d", tc.nbt, moistness, tc.want)
		}
	}
}

func TestASummonedDolphinSwimsInItsPond(t *testing.T) {
	t.Parallel()
	srv, c := squidTests(t)
	c.ChatCommand("summon minecraft:dolphin 8.5 62 -8.5")
	servertest.AwaitSummonFeedback(t, c, "minecraft:dolphin", "entity.minecraft.dolphin")
	spawn := c.ExpectMobSpawn(fishType(t, "minecraft:dolphin"))
	id := spawn.EntityID
	if spawn.DolphinMoistness != 2400 || spawn.Air != 4800 {
		t.Fatalf("spawn moistness %d air %d, want 2400/4800", spawn.DolphinMoistness, spawn.Air)
	}
	start, _ := fishAt(srv, id)
	travelled := 0.0
	for range 200 {
		srv.TickMobs(1)
		m, ok := fishAt(srv, id)
		if !ok {
			t.Fatal("dolphin disappeared")
		}
		travelled += math.Hypot(m.X-start.X, m.Z-start.Z)
		start = m
	}
	c.Settle()
	if travelled < 1 {
		t.Errorf("swam %.2f blocks, want at least one", travelled)
	}
	if hp, _ := srv.MobHealth(id); hp != 10 {
		t.Errorf("health = %g, want 10", hp)
	}
}

func TestADolphinOnLandFlopsAndDriesOut(t *testing.T) {
	t.Parallel()
	srv, c := squidTests(t)
	c.ChatCommand("summon minecraft:dolphin 14.5 64 2.5")
	servertest.AwaitSummonFeedback(t, c, "minecraft:dolphin", "entity.minecraft.dolphin")
	id := c.ExpectMobSpawn(fishType(t, "minecraft:dolphin")).EntityID
	c.Settle()
	flopped := false
	for tick := 1; tick < 2400; tick++ {
		srv.TickMobs(1)
		m, ok := fishAt(srv, id)
		if !ok {
			t.Fatalf("dolphin disappeared at tick %d", tick)
		}
		flopped = flopped || m.Y > 64.25
		if tick%100 == 0 {
			c.Settle()
		}
	}
	if !flopped {
		t.Error("stranded dolphin never flopped")
	}
	if hp, _ := srv.MobHealth(id); hp != 10 {
		t.Fatalf("health before drying = %g, want 10", hp)
	}
	srv.TickMobs(1)
	if hp, _ := srv.MobHealth(id); hp != 9 {
		t.Fatalf("health at tick 2400 = %g, want 9", hp)
	}
	// Dolphin.tick attempts damage every tick; LivingEntity's hurt cooldown still applies.
	srv.TickMobs(10)
	if hp, _ := srv.MobHealth(id); hp != 8 {
		t.Fatalf("health after hurt cooldown = %g, want 8", hp)
	}
	c.Settle()
}

func TestADolphinSurfacesForAirUnderARoof(t *testing.T) {
	t.Parallel()
	srv, c := squidTests(t)
	stone := registry.BlockStone.DefaultState()
	water := registry.BlockWater.DefaultState()
	// A deeper pond with an air pocket under its roof; only these loaded chunks are needed.
	for x := int32(pondXLo); x <= pondXHi; x++ {
		for z := int32(pondZLo); z <= pondZHi; z++ {
			for y := int32(58); y <= 63; y++ {
				srv.SetBlock(x, y, z, water)
			}
			srv.SetBlock(x, 65, z, stone)
		}
	}
	for x := int32(pondXLo - 1); x <= pondXHi+1; x++ {
		for z := int32(pondZLo - 1); z <= pondZHi+1; z++ {
			if x == pondXLo-1 || x == pondXHi+1 || z == pondZLo-1 || z == pondZHi+1 {
				for y := int32(58); y <= 65; y++ {
					srv.SetBlock(x, y, z, stone)
				}
			}
		}
	}
	c.ChatCommand("summon minecraft:dolphin 8.5 58 -8.5 {Air:150s,PersistenceRequired:1b}")
	servertest.AwaitSummonFeedback(t, c, "minecraft:dolphin", "entity.minecraft.dolphin")
	id := c.ExpectMobSpawn(fishType(t, "minecraft:dolphin")).EntityID
	c.Settle()
	surfaced := false
	// One full breath budget also catches repeated failures to resurface.
	for tick := range 4800 {
		srv.TickMobs(1)
		m, ok := fishAt(srv, id)
		if !ok {
			t.Fatalf("dolphin disappeared at tick %d", tick)
		}
		surfaced = surfaced || m.Y+0.3 >= 64
		if hp, _ := srv.MobHealth(id); hp != 10 {
			t.Fatalf("health at tick %d = %g at y %.3f, want no drowning", tick, hp, m.Y)
		}
		if tick%100 == 0 {
			c.Settle()
		}
	}
	c.Settle()
	if !surfaced {
		t.Error("dolphin never reached the surface air pocket")
	}
}

func TestAKilledDolphinDropsAtMostOneCod(t *testing.T) {
	t.Parallel()
	srv, c := squidTests(t)
	// Several kills exercise a table whose empty result is legitimate.
	cod := 0
	for range 32 {
		id := summonSquid(t, c, "minecraft:dolphin", fishType(t, "minecraft:dolphin"), pondCentreX, -8.5)
		if !srv.KillMob(id) {
			t.Fatal("no dolphin to kill")
		}
		srv.TickMobs(1)
		c.Settle()
		count := 0
		for _, it := range srv.ItemsInDimension("minecraft:overworld") {
			if it.Item != "minecraft:cod" {
				t.Fatalf("unexpected dolphin drop %s", it.Item)
			}
			count += int(it.Count)
		}
		if count < cod || count > cod+1 {
			t.Fatalf("kill dropped %d cod, want 0-1", count-cod)
		}
		cod = count
	}
	if cod == 0 {
		t.Fatal("thirty-two dolphins dropped no cod")
	}
}

func TestFeedingADolphinConsumesFishAndSavesGotFish(t *testing.T) {
	t.Parallel()
	srv, c := squidTests(t)
	id := summonSquidTagged(t, c, "minecraft:dolphin", fishType(t, "minecraft:dolphin"), pondCentreX, pondNorth, `{NoAI:1b}`)
	hotbarFish(t, srv, "minecraft:cod")
	servertest.HoldTicks(t, srv)
	c.UseOn(id)
	c.Settle()
	if held := srv.InventorySlot("Striker", player.HotbarSlot); !held.Empty() {
		t.Fatalf("fish was not consumed: %+v", held)
	}
	var gotFish bool
	servertest.RunTicks(t, srv, 1)
	c.SettleAndCollect(func(packetID int32, data []byte) {
		if packetID == protocol.IDPlayEntityMetadata {
			if protocol.NewReader(data).ReadVarInt() != id {
				return
			}
			m := servertest.SpawnedMob{EntityID: id, TypeID: fishType(t, "minecraft:dolphin")}
			c.ReadMobMetadataInto(&m, data)
			if m.EntityID == id {
				gotFish = gotFish || m.DolphinGotFish
			}
		}
	})
	if !gotFish {
		t.Error("metadata did not set GotFish")
	}
	tag := entityCompound(t, c, "@e[type=minecraft:dolphin,limit=1]")
	if !nbt.BoolField(tag, "GotFish") {
		t.Fatalf("saved GotFish = %v", tag["GotFish"])
	}
}

func TestASwimmingPlayerGetsDolphinsGraceWithinFortyTicks(t *testing.T) {
	t.Parallel()
	srv, c := squidTests(t)
	c.MoveToGround(pondCentreX, 62, -5.5, false)
	c.Settle()
	c.SendSprint(c.PlayerEntityID, true)
	if m := c.ExpectMetadata(); m.Pose != protocol.PoseSwimming {
		t.Fatalf("pose %d, want SWIMMING", m.Pose)
	}
	summonSquid(t, c, "minecraft:dolphin", fishType(t, "minecraft:dolphin"), pondCentreX, -10.5)
	servertest.HoldTicks(t, srv)
	var duration, amp int32
	servertest.UntilTicks(t, srv, 40, "Dolphin's Grace", func() bool {
		var ok bool
		duration, amp, ok = srv.PlayerEffect("Striker", "minecraft:dolphins_grace")
		return ok
	})
	if duration != 100 || amp != 0 {
		t.Fatalf("Grace = %d ticks, amplifier %d", duration, amp)
	}
}

func TestADolphinPicksUpAndTossesWaterItemsWithPickupDelay(t *testing.T) {
	t.Parallel()
	srv, c := squidTests(t)
	id := summonSquid(t, c, "minecraft:dolphin", fishType(t, "minecraft:dolphin"), pondCentreX, -8.5)
	original := srv.SpawnItemEntityDelay(pondCentreX, 62.2, -8.5, "minecraft:stick", 12, 0)
	servertest.HoldTicks(t, srv)
	var tossed *server.TestItem
	servertest.UntilTicks(t, srv, 40, "the dolphin tossing the item", func() bool {
		for _, it := range srv.ItemsInDimension("minecraft:overworld") {
			if it.ID != original && it.Item == "minecraft:stick" {
				tossed = &it
				return true
			}
		}
		return false
	})
	// vanilla: Dolphin.ItemGoal.drop, EntityTickList.forEach
	if tossed.Count != 12 || tossed.PickupDelay != 40 {
		t.Fatalf("tossed item %+v, want stack of 12 and delay 40 on the toss tick", *tossed)
	}
	if held := srv.MobEquipment(id, "mainhand"); !held.Empty() {
		t.Fatalf("hand after toss %+v", held)
	}
	dolphinTag := entityCompound(t, c, "@e[type=minecraft:dolphin,limit=1]")
	itemTag := entityCompound(t, c, "@e[type=minecraft:item,limit=1]")
	dolphinUUID, _ := nbt.UUIDField(dolphinTag, "UUID")
	thrower, ok := nbt.UUIDField(itemTag, "Thrower")
	if !ok || thrower != dolphinUUID {
		t.Fatalf("thrower %v, want dolphin %v", thrower, dolphinUUID)
	}
	servertest.RunTicks(t, srv, 1)
	for _, it := range srv.ItemsInDimension("minecraft:overworld") {
		if it.ID == tossed.ID {
			if it.Count != 12 || it.PickupDelay != 39 {
				t.Fatalf("item after one tick %+v, want stack of 12 and delay 39", it)
			}
			return
		}
	}
	t.Fatal("tossed item disappeared after one tick")
}

func TestADolphinTurnsSmoothlyTowardANearbySwimmer(t *testing.T) {
	t.Parallel()
	srv, c := squidTests(t)
	servertest.HoldTicks(t, srv)
	c.MoveToGround(6.5, 62, -8.5, false)
	c.SendSprint(c.PlayerEntityID, true)
	if m := c.ExpectMetadata(); m.Pose != protocol.PoseSwimming {
		t.Fatalf("pose %d, want SWIMMING", m.Pose)
	}
	c.Settle()
	id := summonSquidTagged(t, c, "minecraft:dolphin", fishType(t, "minecraft:dolphin"), pondCentreX, -8.5, `{Rotation:[0f,0f]}`)
	servertest.RunTicks(t, srv, 1)
	m, ok := fishAt(srv, id)
	if !ok {
		t.Fatal("dolphin disappeared")
	}
	// vanilla: DolphinSwimWithPlayerGoal.tick, SmoothSwimmingLookControl.tick.
	if math.Abs(float64(m.Pitch)-1) > 0.001 || math.Abs(float64(m.HeadYaw)-21) > 0.001 {
		t.Fatalf("pitch/head yaw = %g/%g, want 1/21 after the first look tick", m.Pitch, m.HeadYaw)
	}
}
