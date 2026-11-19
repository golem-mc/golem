package world

import (
	"fmt"
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// One arena covers death, abort and a full ritual; generating the arena is
// the expensive part, so these checks share its chunks.
func TestEndDragonDeathAndCrystalRespawn(t *testing.T) {
	cfg := servertest.CrossingTestConfig(t)
	cfg.LevelName = t.TempDir()
	cfg.AutoSaveTicks = 0 // on once the loop is held, so no save falls in the join
	srv, c, id := joinDragonFightWith(t, cfg)
	servertest.OpsListing(t, "DragonWatcher")(srv)
	servertest.ReleaseTicks(t, srv)
	other := servertest.DialServer(t, srv).Join("DeathHearer")
	servertest.HoldTicks(t, srv)
	var y int32
	for by := int32(100); by >= 0; by-- {
		if registry.BlockOf(srv.BlockInDimension(world.EndName, 0, by, 0)) == registry.BlockBedrock {
			y = by - 3
			break
		}
	}
	if y == 0 {
		t.Fatal("no podium")
	}
	srv.SetBlockInDimension(world.EndName, 1, y+1, 0, registry.BlockOakPlanks.DefaultState())
	// The punch must land at full strength to be lethal; a loaded machine runs
	// too few ticks in the join's free window to wind the fist's recovery.
	servertest.UntilTicks(t, srv, 2*servertest.SwingRecovered, "swing recovered", func() bool {
		return srv.AttackTicks("DragonWatcher") >= servertest.SwingRecovered
	})
	// Put the mortally wounded dragon at its death flight's destination.
	c.ChatCommand(fmt.Sprintf("data merge entity @e[type=minecraft:ender_dragon,limit=1] {Health:1f,NoAI:0b,Pos:[0.5d,%dd,0.5d]}", y+6))
	c.Settle()
	if h, _ := srv.MobHealthInDimension(world.EndName, id); h != 1 {
		t.Fatalf("data merge left health %v", h)
	}
	servertest.RunTicks(t, srv, 1)
	c.MoveTo(2, float64(y+6), 0.5, 0, 0)
	c.Settle()
	c.Attack(id + 3)
	c.Settle()
	if h, _ := srv.MobHealthInDimension(world.EndName, id); h != 1 {
		t.Fatalf("killing blow left health %v", h)
	}
	servertest.RunTicks(t, srv, 1)
	c.MoveTo(20, float64(y+20), 20, 0, 0)
	c.Settle()
	servertest.RunTicks(t, srv, 200)
	heard := false
	other.SettleAndCollect(func(packet int32, data []byte) {
		if packet != protocol.IDPlayWorldEvent {
			return
		}
		r := protocol.NewReader(data)
		if r.ReadI32() != 1028 {
			return
		}
		r.ReadPosition()
		r.ReadI32()
		if !r.ReadBool() {
			t.Error("cross-dimension death sound was not global")
		}
		heard = true
	})
	if !heard {
		t.Fatal("death sound did not reach the overworld")
	}
	dropped := false
	for _, item := range srv.ItemsInDimension(world.EndName) {
		if item.Item == "minecraft:oak_planks" {
			dropped = true
		}
	}
	if registry.BlockOf(srv.BlockInDimension(world.EndName, 1, y+1, 0)) != registry.BlockAir {
		t.Fatal("podium obstruction was not cleared")
	}
	if !dropped {
		t.Fatal("podium obstruction did not drop its item")
	}
	if _, ok := srv.MobHealthInDimension(world.EndName, id); ok {
		t.Fatal("dragon still present after death")
	}
	block := func(x, by, z int32) registry.Block {
		return registry.BlockOf(srv.BlockInDimension(world.EndName, x, by, z))
	}
	if block(1, y, 0) != registry.BlockEndPortal || block(0, y+4, 0) != registry.BlockDragonEgg {
		t.Fatalf("portal=%v egg=%v", block(1, y, 0), block(0, y+4, 0))
	}
	gateway := false
	for n := range 20 {
		angle := 2 * (-math.Pi + 0.15707963267948966*float64(n))
		x, z := int32(math.Floor(96*math.Cos(angle))), int32(math.Floor(96*math.Sin(angle)))
		if block(x, 75, z) == registry.BlockEndGateway {
			gateway = true
		}
	}
	if !gateway {
		t.Fatal("no ring gateway")
	}
	xp := 0
	for _, orb := range srv.OrbsInWorld() {
		xp += orb.XP
	}
	if xp != 12000 {
		t.Fatalf("death awarded %d experience, want 12000", xp)
	}
	// The next auto-save writes the fight file, without a shutdown.
	servertest.AutoSaveEveryTick(srv)
	servertest.RunTicks(t, srv, servertest.AutoSaveEvery)
	if data := readDragonFightFile(t, cfg.LevelName); data == nil || !nbt.BoolField(data, "dragon_killed") {
		t.Fatalf("auto-save left fight file %v, want dragon_killed:1b", data)
	}
	srv.SetAutoSave(0)
	removed := false
	for packet, data := range c.Packets(servertest.ExpectTimeout) {
		if packet == protocol.IDPlayBossEvent {
			r := protocol.NewReader(data)
			r.ReadUUID()
			if r.ReadVarInt() == protocol.BossEventRemove {
				removed = true
				break
			}
		}
	}
	if !removed {
		t.Fatal("boss bar was not removed")
	}
	// A second entry lets the player place crystals from above the ring.
	c.MoveTo(0, float64(y+5), 0, 0, 0)
	c.Settle()
	srv.GiveSlot("DragonWatcher", 36, protocol.ItemStack{ItemID: int32(registry.ItemEndCrystal), Count: 4})
	var crystalID int32
	seen := 0
	crystalType, _ := registry.EntityTypeID("minecraft:end_crystal")
	for i, p := range [][2]int32{{0, -3}, {3, 0}, {0, 3}, {-3, 0}} {
		c.MoveTo(float64(p[0])*1.7, float64(y+1), float64(p[1])*1.7, 0, 0)
		c.Settle()
		c.UseBlockWith(protocol.HandMain, p[0], y, p[1])
		c.SettleAndCollect(func(packet int32, data []byte) {
			if packet == protocol.IDPlaySpawnEntity {
				r := protocol.NewReader(data)
				spawned := r.ReadVarInt()
				r.ReadUUID()
				if r.ReadVarInt() == crystalType {
					crystalID = spawned
					seen++
				}
			}
		})
		want := registry.BlockEndPortal
		if i == 3 {
			want = registry.BlockAir
		}
		if block(1, y, 0) != want {
			t.Fatalf("after crystal %d: portal=%v crystals=%d", i+1, block(1, y, 0), srv.EndCrystalCount())
		}
	}
	// Breaking a ritual crystal aborts and restores the portal.
	if seen != 4 {
		t.Fatalf("saw %d ritual crystals", seen)
	}
	c.Attack(crystalID)
	c.Settle()
	if block(1, y, 0) != registry.BlockEndPortal {
		t.Fatal("destroying a ritual crystal did not restore portal")
	}
	srv.GiveSlot("DragonWatcher", 36, protocol.ItemStack{ItemID: int32(registry.ItemEndCrystal), Count: 4})
	for _, p := range [][2]int32{{0, -3}, {3, 0}, {0, 3}, {-3, 0}} {
		srv.SetBlockInDimension(world.EndName, p[0], y+1, p[1], registry.BlockAir.DefaultState())
		c.MoveTo(float64(p[0])*1.7, float64(y+1), float64(p[1])*1.7, 0, 0)
		c.Settle()
		c.UseBlockWith(protocol.HandMain, p[0], y, p[1])
		c.Settle()
	}
	c.MoveTo(0, float64(y+20), 0, 0, 0)
	c.Settle()
	// Corner cells lie outside the cylinder; y=66 is below taller spikes' clearing boxes.
	var corners [][2]int32
	for i := range 10 {
		angle := 2 * (-math.Pi + 0.3141592653589793*float64(i))
		x, z := int32(math.Floor(42*math.Cos(angle))), int32(math.Floor(42*math.Sin(angle)))
		var radius int32
		for dx := int32(1); dx <= 5; dx++ {
			if block(x+dx, 66, z) == registry.BlockObsidian {
				radius = dx
			}
		}
		corners = append(corners, [2]int32{x + radius, z + radius})
		srv.SetBlockInDimension(world.EndName, x+radius, 66, z+radius, registry.BlockDiamondBlock.DefaultState())
	}
	for range 30 {
		servertest.RunTicks(t, srv, 20)
		c.Settle()
		other.Settle()
	}
	servertest.RunTicks(t, srv, 4)
	dragon := false
	for _, mob := range srv.MobsInDimension(world.EndName) {
		if mob.Kind == "minecraft:ender_dragon" && mob.ID != id {
			dragon = true
		}
	}
	if !dragon {
		t.Fatal("ritual did not spawn a new dragon")
	}
	if block(1, y, 0) != registry.BlockAir {
		t.Fatal("ritual reopened portal")
	}
	for _, p := range corners {
		if block(p[0], 66, p[1]) != registry.BlockAir {
			t.Fatalf("lower spike corner %v survived rebuild", p)
		}
	}
	for i := range 10 {
		angle := 2 * (-math.Pi + 0.3141592653589793*float64(i))
		x, z := int32(math.Floor(42*math.Cos(angle))), int32(math.Floor(42*math.Sin(angle)))
		rebuilt := false
		for top := int32(76); top <= 103; top++ {
			if block(x, top, z) == registry.BlockBedrock {
				rebuilt = true
			}
		}
		if !rebuilt {
			t.Fatalf("spike %d was not rebuilt", i)
		}
	}
	srv.SetGameRule(world.RuleGlobalSoundEvents.String(), false)
	c.Settle()
	other.Settle()
	c.ChatCommand(fmt.Sprintf("data merge entity @e[type=minecraft:ender_dragon,limit=1] {Health:0f,NoAI:0b,DragonDeathTime:0,Pos:[0.5d,%dd,0.5d]}", y+6))
	c.Settle()
	servertest.RunTicks(t, srv, 1)
	local := false
	c.SettleAndCollect(func(packet int32, data []byte) {
		if packet != protocol.IDPlayWorldEvent {
			return
		}
		r := protocol.NewReader(data)
		if r.ReadI32() != 1028 {
			return
		}
		r.ReadPosition()
		r.ReadI32()
		if r.ReadBool() {
			t.Error("rule-off death sound was global")
		}
		local = true
	})
	if !local {
		t.Fatal("nearby player did not hear rule-off death sound")
	}
	other.SettleAndCollect(func(packet int32, data []byte) {
		if packet == protocol.IDPlayWorldEvent && protocol.NewReader(data).ReadI32() == 1028 {
			t.Error("rule-off death sound reached another dimension")
		}
	})

}
