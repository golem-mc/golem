package redstone

// The sculk sensor's later half: more event sources, wool in the way, the
// particle that carries a vibration, and the frequency kept in the block
// entity. vanilla: VibrationSystem, SculkSensorBlockEntity

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// comparatorSensorScene is sensorScene with a comparator reading the sensor.
func comparatorSensorScene(t *testing.T) (*server.Server, *servertest.Client) {
	t.Helper()
	srv, c := servertest.AttachActor(t)
	standAtScene(c)
	servertest.LayComparatorScene(t, c, srv, 5, 64, 4)
	srv.SetBlock(4, 64, 4, servertest.MustState(t, "minecraft:sculk_sensor", nil))
	servertest.HoldTicks(t, srv)
	return srv, c
}

func expectComparator(t *testing.T, srv *server.Server, want string) {
	t.Helper()
	if got := servertest.WirePowerAt(srv, 6, 64, 4); got != want {
		t.Fatalf("the comparator gives %q, want %s", got, want)
	}
}

// TestAnExplosionActivatesASculkSensor: the blast's frequency is 15.
// vanilla: ServerExplosion.explode
func TestAnExplosionActivatesASculkSensor(t *testing.T) {
	t.Parallel()
	srv, _ := comparatorSensorScene(t)
	srv.RunExplosion(2.5, 64.5, 4.5, 0.5)
	servertest.RunTicks(t, srv, 4)
	expectSensor(t, srv, 4, 64, 4, "active", "12")
	expectComparator(t, srv, "15")
}

// TestADoorSwingingActivatesASculkSensor: opening a door is block_open,
// frequency 10.
// vanilla: DoorBlock.setOpen
func TestADoorSwingingActivatesASculkSensor(t *testing.T) {
	t.Parallel()
	srv, c := comparatorSensorScene(t)
	srv.SetBlock(2, 64, 4, servertest.MustState(t, "minecraft:oak_door", map[string]string{"half": "lower"}))
	srv.SetBlock(2, 65, 4, servertest.MustState(t, "minecraft:oak_door", map[string]string{"half": "upper"}))
	c.MoveToGround(0.5, 64, 4.5, true)
	c.Settle()
	c.UseBlock(2, 64, 4)
	c.Settle()
	servertest.RunTicks(t, srv, 4)
	expectSensor(t, srv, 4, 64, 4, "active", "12")
	expectComparator(t, srv, "10")
}

// TestWoolBlocksTheWayOfAVibration: a wool block between the source and the
// sensor stops the vibration; without it the vibration arrives.
// vanilla: VibrationSystem.Listener.isOccluded
func TestWoolBlocksTheWayOfAVibration(t *testing.T) {
	t.Parallel()
	srv, c := sensorScene(t)
	srv.SetBlock(3, 64, 4, servertest.MustState(t, "minecraft:white_wool", nil))
	placeStoneAt(c, 2)
	servertest.RunTicks(t, srv, 6)
	expectSensor(t, srv, 4, 64, 4, "inactive", "0")

	srv.SetBlock(3, 64, 4, servertest.MustState(t, "minecraft:air", nil))
	c.BreakBlock(2, 64, 4)
	c.Settle()
	servertest.RunTicks(t, srv, 6)
	expectSensor(t, srv, 4, 64, 4, "active", "12")
}

func sensorFrequency(t *testing.T, srv *server.Server) int32 {
	t.Helper()
	tag, ok := srv.BlockEntityTagAt(4, 64, 4)
	if !ok {
		t.Fatal("the sensor has no block entity")
	}
	got, _ := nbt.Get[nbt.Int](tag, "last_vibration_frequency")
	return int32(got)
}

// TestAThrownSnowballShootsAndLandsAsVibrations: the throw is
// projectile_shoot (3), the landing on a block projectile_land (2).
// vanilla: Projectile.tick, Projectile.hitTargetOrDeflectSelf
func TestAThrownSnowballShootsAndLandsAsVibrations(t *testing.T) {
	t.Parallel()
	srv, c := sensorScene(t)
	id, _ := registry.ItemID("minecraft:snowball")
	c.SetCreativeSlot(36, protocol.ItemStack{ItemID: id, Count: 1})
	c.MoveToGround(0.5, 64, 4.5, true)
	c.Settle()
	c.UseItemRightClicks(protocol.HandMain)
	c.Settle()
	servertest.RunTicks(t, srv, 8)
	if got := sensorFrequency(t, srv); got != 3 {
		t.Errorf("the throw gave frequency %d, want 3", got)
	}

	servertest.RunTicks(t, srv, 40)
	srv.SetBlock(2, 64, 4, servertest.MustState(t, "minecraft:stone", nil))
	srv.ThrowThrown(entity.ThrownSnowball, 2.5, 66, 4.5, 0, 90, 0, 1)
	servertest.RunTicks(t, srv, 8)
	if got := sensorFrequency(t, srv); got != 2 {
		t.Errorf("the landing gave frequency %d, want 2", got)
	}
}

// TestAPlayersHurtAndDeathAreVibrations: entity_damage is 7, entity_die 15.
// vanilla: LivingEntity.hurtServer, LivingEntity.die
func TestAPlayersHurtAndDeathAreVibrations(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Join(t, servertest.SurvivalTestConfig(), "Lander")
	srv.SetBlock(4, 64, 4, servertest.MustState(t, "minecraft:sculk_sensor", nil))
	servertest.HoldTicks(t, srv)

	srv.RunLine("damage Lander 1")
	servertest.RunTicks(t, srv, 8)
	if got := sensorFrequency(t, srv); got != 7 {
		t.Errorf("the hurt gave frequency %d, want 7", got)
	}
	servertest.RunTicks(t, srv, 40)
	srv.RunLine("kill Lander")
	servertest.RunTicks(t, srv, 8)
	if got := sensorFrequency(t, srv); got != 15 {
		t.Errorf("the death gave frequency %d, want 15", got)
	}
}

// TestWalkingActivatesASculkSensor: a step event is posted each time the walk
// distance (0.6 per block) passes the next whole number, so the second block
// walked gives one, frequency 1 and power 12 at two blocks.
// vanilla: Entity.applyMovementEmissionAndPlaySound
func TestWalkingActivatesASculkSensor(t *testing.T) {
	t.Parallel()
	srv, c := sensorScene(t)
	c.MoveToGround(1.5, 64, 4.5, true)
	c.Settle()
	servertest.RunTicks(t, srv, 4)
	expectSensor(t, srv, 4, 64, 4, "inactive", "0")
	c.MoveToGround(2.5, 64, 4.5, true)
	c.Settle()
	servertest.RunTicks(t, srv, 4)
	expectSensor(t, srv, 4, 64, 4, "active", "12")
}

// TestALeverFlipActivatesASculkSensor: switching a lever on is
// block_activate (frequency 10), the flip back block_deactivate (9).
// vanilla: LeverBlock.pull
func TestALeverFlipActivatesASculkSensor(t *testing.T) {
	t.Parallel()
	srv, c := comparatorSensorScene(t)
	srv.SetBlock(2, 64, 4, servertest.MustState(t, "minecraft:lever", map[string]string{"face": "floor"}))
	c.MoveToGround(0.5, 64, 4.5, true)
	c.Settle()
	c.UseBlock(2, 64, 4)
	c.Settle()
	servertest.RunTicks(t, srv, 4)
	expectSensor(t, srv, 4, 64, 4, "active", "12")
	expectComparator(t, srv, "10")

	servertest.RunTicks(t, srv, 40)
	c.UseBlock(2, 64, 4)
	c.Settle()
	servertest.RunTicks(t, srv, 4)
	expectComparator(t, srv, "9")
}
