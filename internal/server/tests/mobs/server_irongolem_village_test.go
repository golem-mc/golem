package mobs

import (
	"math"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

func TestAnIronGolemPatrolsAndReturnsToItsVillage(t *testing.T) {
	for _, outside := range []bool{false, true} {
		name := "patrol"
		if outside {
			name = "return"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			srv, c, _ := servertest.JoinAs(t, servertest.MobTestConfig(), "GolemVillage", servertest.OpsListing(t, "GolemVillage"))
			c.MoveToGround(0.5, 64, 0.5, true)
			c.Settle()
			servertest.HoldTicks(t, srv)
			srv.SetDayTime(2000)
			srv.SetBlock(3, 64, 0, servertest.BedStateName("red", "head", "east"))
			srv.SetBlock(2, 64, 0, servertest.BedStateName("red", "foot", "east"))
			srv.SetBlock(3, 64, 3, registry.MustState("minecraft:lectern", nil))
			c.ChatCommand(`summon minecraft:villager 3.5 64 1.5 {NoAI:1b,Brain:{memories:{"minecraft:home":{value:{dimension:"minecraft:overworld",pos:[I;3,64,0]}},"minecraft:job_site":{value:{dimension:"minecraft:overworld",pos:[I;3,64,3]}}}},VillagerData:{type:"minecraft:plains",profession:"minecraft:librarian",level:1}}`)
			c.AwaitSpawn(villagerType(t))
			startX := 6.5
			if outside {
				startX = 43.5
			}
			// Generate only the strip used by the return path.
			for x := int32(16); x <= 48; x += 16 {
				srv.LoadBlockAt(x, 63, 0)
			}
			id := srv.SpawnMobSeeded("minecraft:iron_golem", startX, 64, 0.5, 2162)
			moved := false
			for range 60 {
				servertest.RunTicks(t, srv, 10)
				x, _, z, ok := srv.MobPos(id)
				if !ok {
					t.Fatal("golem disappeared")
				}
				if math.Hypot(x-startX, z-0.5) >= 3 {
					moved = true
				}
			}
			x, _, z, _ := srv.MobPos(id)
			if outside {
				if math.Hypot(x-3.5, z-0.5) >= 40 {
					t.Fatalf("golem at (%g,%g), started 40 blocks from village", x, z)
				}
			} else if !moved {
				t.Fatalf("golem never moved three blocks in 600 ticks; ended at (%g,%g)", x, z)
			}
		})
	}
}

func TestAnIronGolemOffersAFlowerFor400Ticks(t *testing.T) {
	t.Parallel()
	srv, c := villagerYard(t)
	srv.SetDayTime(2000)
	villager := srv.SpawnMob("minecraft:villager", 2.5, 64, 0.5)
	srv.SetMobNoAI(villager)
	id := srv.SpawnMobSeeded("minecraft:iron_golem", 0.5, 64, 0.5, 3)
	c.WatchVariant(srv, "minecraft:iron_golem")
	srv.ForceGolemFlowerOffer(id)
	started, stopped := -1, -1
	tick := 0
	collect := func(pid int32, data []byte) {
		if pid != protocol.IDPlayEntityEvent {
			return
		}
		r := protocol.NewReader(data)
		if r.ReadI32() != id {
			return
		}
		switch r.ReadU8() {
		case protocol.EntityStatusIronGolemOfferFlower:
			if started < 0 {
				started = tick
			}
		case protocol.EntityStatusIronGolemStopFlower:
			if stopped < 0 {
				stopped = tick
			}
		}
	}
	for tick = 1; tick <= 410; tick++ {
		servertest.RunTicks(t, srv, 1)
		c.SettleAndCollect(collect)
	}
	if started < 1 || stopped-started != 400 {
		t.Fatalf("flower start/stop ticks = %d/%d, want a 400-tick offer", started, stopped)
	}
}

// antennaSlotSeen reads the Set Equipment packets in data and reports whether
// slot 7 (the saddle slot) of entity id carries the given item, or is empty
// when item is 0.
func antennaSlotSeen(t *testing.T, pid int32, data []byte, id int32, item int32) bool {
	t.Helper()
	if pid != protocol.IDPlayEntityEquipment {
		return false
	}
	r := protocol.NewReader(data)
	if r.ReadVarInt() != id {
		return false
	}
	for {
		slot := r.ReadU8()
		stack, ok := r.ReadSlot()
		if !ok {
			t.Fatalf("equipment: %v", r.Err())
		}
		if slot&0x7f == 7 && stack.ItemID == item && (item == 0) == stack.Empty() {
			return true
		}
		if slot&0x80 == 0 {
			return false
		}
	}
}

// vanilla: OfferFlowerGoal.stop, CopperGolem.EQUIPMENT_SLOT_ANTENNA
func TestAnIronGolemPutsAPoppyOnACopperGolemsAntenna(t *testing.T) {
	t.Parallel()
	srv, c := villagerYard(t)
	srv.SetDayTime(2000)
	// The iron golem stands on a 2-block pillar, above the copper golem's
	// head: only a box grown 2 down from its feet reaches the copper golem.
	stone := registry.BlockStates["minecraft:stone"]
	srv.SetBlock(-1, 64, 0, stone)
	srv.SetBlock(-1, 65, 0, stone)
	copper := srv.SpawnMob("minecraft:copper_golem", 1.5, 64, 0.5)
	srv.SetMobNoAI(copper)
	id := srv.SpawnMobSeeded("minecraft:iron_golem", -0.5, 66, 0.5, 3)
	c.WatchVariant(srv, "minecraft:copper_golem")
	srv.ForceGolemFlowerOffer(id)
	poppy := int32(registry.ItemPoppy)
	seen := false
	for tick := 1; tick <= 410; tick++ {
		servertest.RunTicks(t, srv, 1)
		c.SettleAndCollect(func(pid int32, data []byte) {
			seen = seen || antennaSlotSeen(t, pid, data, copper, poppy)
		})
	}
	if !seen {
		t.Fatal("no Set Equipment put a poppy in slot 7 of the copper golem")
	}
	query := ""
	for _, text := range srv.RunLine("data get entity @e[type=minecraft:copper_golem,limit=1] equipment.saddle") {
		if text.Translate == "commands.data.entity.query" && len(text.With) == 2 {
			query = text.With[1].Plain()
		}
	}
	if !strings.Contains(query, "minecraft:poppy") {
		t.Fatalf("equipment.saddle = %q, want a poppy", query)
	}
	giveHand(t, srv, "Villager", "minecraft:shears", 1)
	c.UseOn(copper)
	c.Settle()
	servertest.RunTicks(t, srv, 2)
	cleared := false
	c.SettleAndCollect(func(pid int32, data []byte) {
		cleared = cleared || antennaSlotSeen(t, pid, data, copper, 0)
	})
	if !cleared {
		t.Error("shearing sent no Set Equipment with slot 7 empty")
	}
}

func TestACopperGolemSummonedWithAnAntennaPairsWithIt(t *testing.T) {
	t.Parallel()
	srv, c := villagerYard(t)
	seen := false
	srv.RunLine(`summon minecraft:copper_golem 2.5 64 0.5 {NoAI:1b,equipment:{saddle:{id:"minecraft:poppy"}}}`)
	servertest.RunTicks(t, srv, 1)
	c.SettleAndCollect(func(pid int32, data []byte) {
		if pid != protocol.IDPlayEntityEquipment {
			return
		}
		r := protocol.NewReader(data)
		r.ReadVarInt()
		for {
			slot := r.ReadU8()
			stack, _ := r.ReadSlot()
			seen = seen || (slot&0x7f == 7 && stack.ItemID == int32(registry.ItemPoppy))
			if slot&0x80 == 0 {
				return
			}
		}
	})
	if !seen {
		t.Fatal("the pairing carried no poppy in slot 7")
	}
}
