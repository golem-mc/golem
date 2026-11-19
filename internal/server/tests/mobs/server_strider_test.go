package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	server "github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// The strider: lava as its floor, the shiver outside the warm blocks, and the
// saddle and warped fungus on a stick that steer it.
// vanilla: Strider

const striderSaddleTag = `{equipment:{saddle:{id:"minecraft:saddle",count:1}}}`

// striderWorld joins a player into the mob test world with the tick stopped.
func striderWorld(t *testing.T) (*server.Server, *servertest.Client) {
	t.Helper()
	s, c := servertest.Join(t, servertest.MobTestConfig(), "Rider", servertest.OpsListing(t, "Rider"))
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()
	s.StopTick()
	return s, c
}

// layLava replaces the grass layer under a square with lava.
func layLava(t *testing.T, srv *server.Server, x0, z0, x1, z1 int32) {
	t.Helper()
	lava := servertest.BlockState(t, "minecraft:lava")
	for x := x0; x <= x1; x++ {
		for z := z0; z <= z1; z++ {
			if !srv.SetBlock(x, 63, z, lava) {
				t.Fatalf("chunk of (%d, %d) not loaded", x, z)
			}
		}
	}
}

// TestAStriderStandsOnLavaAndTakesNoFire: dropped into a lava pool, a strider
// ends up standing on the surface, half a block above the source, unburnt.
func TestAStriderStandsOnLavaAndTakesNoFire(t *testing.T) {
	t.Parallel()
	srv, _ := striderWorld(t)
	layLava(t, srv, 10, 10, 14, 14)
	id := srv.SpawnMob("minecraft:strider", 12.5, 65, 12.5)
	for range 80 {
		srv.TickMobs(1)
	}
	_, y, _, ok := srv.MobPos(id)
	if !ok {
		t.Fatal("the strider is gone")
	}
	if y < 63.45 || y > 63.55 {
		t.Errorf("strider feet at y=%v, want the lava's half-block surface 63.5", y)
	}
	if hp, _ := srv.MobHealth(id); hp != 20 {
		t.Errorf("strider health %v after 80 ticks on lava, want 20", hp)
	}
	if fire := srv.MobFire(id); fire != 0 {
		t.Errorf("strider burns %d ticks, want 0", fire)
	}
}

// striderSteer summons a saddled strider next to the rider, who holds the item
// given, and seats the rider.
func striderSteer(t *testing.T, held string) (*servertest.Client, *server.Server, int32) {
	t.Helper()
	srv, c := striderWorld(t)
	if held != "" {
		item, ok := registry.ItemID(held)
		if !ok {
			t.Fatalf("no item %s", held)
		}
		srv.GiveSlot("Rider", player.HotbarSlot, protocol.ItemStack{ItemID: item, Count: 1})
		c.Settle()
	}
	c.ChatCommand("summon minecraft:strider 5.5 64 5.5 " + striderSaddleTag)
	spawn := c.ExpectMobSpawn(striderTypeID(t))
	c.UseOn(spawn.EntityID)
	c.Settle()
	return c, srv, spawn.EntityID
}

func striderTypeID(t *testing.T) int32 {
	t.Helper()
	id, ok := registry.EntityTypeID("minecraft:strider")
	if !ok {
		t.Fatal("no strider entity type")
	}
	return id
}

func moveVehicle(c *servertest.Client, x, y, z float64, yaw float32) {
	c.Send(protocol.IDPlayMoveVehicleIn, func(b *protocol.Buffer) {
		b.PutF64(x)
		b.PutF64(y)
		b.PutF64(z)
		b.PutF32(yaw)
		b.PutF32(0)
		b.PutBool(true)
	})
	c.SettleKeeping()
}

// TestASaddledStriderFollowsARiderWithAFungusStick: the rider's reported
// vehicle move carries a saddled strider when the stick is in hand.
func TestASaddledStriderFollowsARiderWithAFungusStick(t *testing.T) {
	t.Parallel()
	c, srv, id := striderSteer(t, "minecraft:warped_fungus_on_a_stick")
	if got := srv.MobVehicleID(id); got != 0 {
		t.Fatalf("strider has vehicle %d", got)
	}
	if ids := srv.MobPassengerIDs(id); len(ids) != 1 {
		t.Fatalf("passengers %v, want the rider alone", ids)
	}
	for _, step := range []struct {
		x, z float64
		yaw  float32
	}{
		{7.5, 5.5, -90}, {7.5, 7.5, 0}, {5.5, 7.5, 90}, {5.5, 5.5, -180},
	} {
		moveVehicle(c, step.x, 64, step.z, step.yaw)
		m, ok := mobWithID(srv, id)
		if !ok || m.X != step.x || m.Z != step.z || m.Yaw != step.yaw || m.HeadYaw != step.yaw {
			t.Fatalf("rider report %+v: strider %+v", step, m)
		}
	}
}

// TestSaddlingAStriderFitsAndSpendsTheSaddle: a saddle clicked onto a bare
// strider is fitted, told as equipment slot 7, and spent.
func TestSaddlingAStriderFitsAndSpendsTheSaddle(t *testing.T) {
	t.Parallel()
	srv, c := striderWorld(t)
	saddle, _ := registry.ItemID("minecraft:saddle")
	srv.GiveSlot("Rider", player.HotbarSlot, protocol.ItemStack{ItemID: saddle, Count: 1})
	c.Settle()
	id := srv.SpawnMob("minecraft:strider", 5.5, 64, 5.5)
	srv.TickMobs(1)
	spawn := c.ExpectMobSpawn(striderTypeID(t))
	if spawn.EntityID != id {
		t.Fatalf("spawned %d, want %d", spawn.EntityID, id)
	}
	c.UseOn(id)
	told := false
	record := func(packet int32, data []byte) {
		if packet != protocol.IDPlayEntityEquipment {
			return
		}
		r := protocol.NewReader(data)
		if r.ReadVarInt() == id && r.ReadU8() == 7 {
			told = true
		}
	}
	c.SettleAndCollect(record)
	srv.TickMobs(1)
	c.SettleAndCollect(record)
	if !told {
		t.Error("the saddle was not told in equipment slot 7")
	}
	if got := srv.InventorySlot("Rider", player.HotbarSlot); got.Count != 0 {
		t.Errorf("%d saddles left in hand, want 0", got.Count)
	}
}

// TestAStriderInWaterTakesDrownDamage: water hurts a strider a point, and the
// hurt's invulnerability ticks hold off the next one past 5 ticks.
// vanilla: LivingEntity.aiStep (isSensitiveToWater)
func TestAStriderInWaterTakesDrownDamage(t *testing.T) {
	t.Parallel()
	srv, _ := striderWorld(t)
	water := servertest.BlockState(t, "minecraft:water")
	for x := int32(10); x <= 14; x++ {
		for z := int32(10); z <= 14; z++ {
			srv.SetBlock(x, 64, z, water)
		}
	}
	id := srv.SpawnMob("minecraft:strider", 12.5, 64, 12.5)
	srv.TickMobs(5)
	if hp, _ := srv.MobHealth(id); hp != 19 {
		t.Errorf("strider health %v after 5 ticks in water, want 19 of 20: one point", hp)
	}
}

func TestAStriderWithLavaAtItsEyesRefusesARider(t *testing.T) {
	t.Parallel()
	srv, c := striderWorld(t)
	lava := servertest.BlockState(t, "minecraft:lava")
	srv.SetBlock(5, 64, 5, lava)
	srv.SetBlock(5, 65, 5, lava)
	c.ChatCommand("summon minecraft:strider 5.5 64 5.5 {NoAI:1b,equipment:{saddle:{id:\"minecraft:saddle\",count:1}}}")
	spawn := c.ExpectMobSpawn(striderTypeID(t))
	srv.TickMobs(1)
	c.UseOn(spawn.EntityID)
	c.Settle()
	if ids := srv.MobPassengerIDs(spawn.EntityID); len(ids) != 0 {
		t.Fatalf("submerged strider took passengers %v", ids)
	}
	srv.SetBlock(5, 65, 5, 0)
	srv.TickMobs(1)
	c.UseOn(spawn.EntityID)
	c.Settle()
	if ids := srv.MobPassengerIDs(spawn.EntityID); len(ids) != 1 {
		t.Fatalf("strider with eyes above lava has passengers %v, want one", ids)
	}
}

func TestWarpedFungusBreedsTwoStriders(t *testing.T) {
	t.Parallel()
	srv, c := servertest.BreedingPlayer(t, "survival", "Rancher", "minecraft:warped_fungus", 2)
	srv.StopTick()
	srv.SetGameRule("mob_drops", false)
	srv.SpawnMob("minecraft:strider", 1.5, 64, 2.5)
	srv.SpawnMob("minecraft:strider", 2.5, 64, 2.5)
	first, second := c.ExpectMobSpawn(striderTypeID(t)), c.ExpectMobSpawn(striderTypeID(t))
	c.UseOn(first.EntityID)
	c.AwaitEntityEvent(first.EntityID, loveHearts)
	c.UseOn(second.EntityID)
	c.AwaitEntityEvent(second.EntityID, loveHearts)
	if got := srv.InventorySlot("Rancher", player.HotbarSlot).Count; got != 0 {
		t.Fatalf("fungus count=%d, want both spent", got)
	}
	for range 100 {
		srv.TickMobs(1)
	}
	kind, baby := c.AwaitNextSpawn()
	if kind != striderTypeID(t) {
		t.Fatalf("bred entity type=%d, want a strider", kind)
	}
	if baby.EntityID == first.EntityID || baby.EntityID == second.EntityID {
		t.Fatal("received a parent instead of a child")
	}
	if flag := servertest.MetaOf(c.AwaitMetadataFor(baby.EntityID), protocol.MetadataIndexBaby); flag == nil || !flag.FlagVal {
		t.Error("bred strider is not a baby")
	}
}

func TestAnIdleStriderWalksAndSoundsAcrossALavaLake(t *testing.T) {
	t.Parallel()
	srv, c := striderWorld(t)
	layLava(t, srv, 0, 0, 24, 24)
	id := srv.SpawnMobSeeded("minecraft:strider", 12.5, 63.5, 12.5, 3)
	for range 120 {
		srv.TickMobs(1)
	}
	_, _, _, volume := c.AwaitSoundNamed("minecraft:entity.strider.step_lava")
	if volume != 1 {
		t.Errorf("lava step volume=%g, want 1", volume)
	}
	x, y, z, ok := srv.MobPos(id)
	if !ok || (x-12.5)*(x-12.5)+(z-12.5)*(z-12.5) < 1 || y < 63.45 || y > 63.55 {
		t.Errorf("idle strider reached (%g, %g, %g), want movement across the lava surface", x, y, z)
	}
}
