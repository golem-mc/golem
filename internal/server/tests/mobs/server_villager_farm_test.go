package mobs

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// farmYard confines the farmer at the crop, with its workstation beside it.
func farmYard(t *testing.T, inventory, level string, cells int32) (*server.Server, *servertest.Client, int32) {
	t.Helper()
	const name = "Gardener"
	srv, c, _ := servertest.JoinAs(t, servertest.MobTestConfig(), name, servertest.OpsListing(t, name))
	servertest.HoldTicks(t, srv)
	c.MoveToGround(0.5, 64, 0.5, true)
	srv.SetDayTime(3000)
	c.ChatCommand("gamerule random_tick_speed 0")
	for x := int32(4); x <= 5+cells; x++ {
		for z := int32(-1); z <= 1; z++ {
			if x >= 5 && x < 5+cells && z == 0 {
				continue
			}
			for y := int32(64); y <= 65; y++ {
				srv.SetBlock(x, y, z, stateOf(t, "minecraft:stone", nil))
			}
		}
	}
	srv.SetBlock(5+cells, 64, 0, stateOf(t, "minecraft:composter", map[string]string{"level": level}))
	id := srv.SpawnVillagerAtJobSite("minecraft:farmer", nil, 5.5, 64, 0.5, 5+cells, 64, 0)
	c.ChatCommand(`data merge entity @e[type=minecraft:villager,limit=1] {Inventory:[` + inventory + `]}`)
	c.Settle()
	return srv, c, id
}

func TestAFarmerHarvestsAndReplantsItsCrop(t *testing.T) {
	t.Parallel()
	srv, c, _ := farmYard(t, `{id:"minecraft:wheat_seeds",count:2}`, "0", 2)
	srv.SetBlock(6, 63, 0, stateOf(t, "minecraft:farmland", map[string]string{"moisture": "7"}))
	ripe := stateOf(t, "minecraft:wheat", map[string]string{"age": "7"})
	srv.SetBlock(6, 64, 0, ripe)
	broken := false
	for range 120 {
		c.ReadTicks(srv, 10, func(pid int32, data []byte) bool {
			if pid == protocol.IDPlayWorldEvent {
				r := protocol.NewReader(data)
				if r.ReadI32() == protocol.WorldEventBlockBreak {
					x, y, z := r.ReadPosition()
					broken = broken || x == 6 && y == 64 && z == 0 && r.ReadI32() == int32(ripe)
				}
			}
			return false
		})
		if got := srv.BlockAt(6, 64, 0); broken && got == stateOf(t, "minecraft:wheat", map[string]string{"age": "0"}) {
			return
		}
	}
	t.Fatalf("harvest event=%v, crop=%d", broken, srv.BlockAt(6, 64, 0))
}

func TestAWorkingVillagerGrowsACropWithBoneMeal(t *testing.T) {
	t.Parallel()
	srv, c, id := farmYard(t, `{id:"minecraft:bone_meal",count:2}`, "0", 1)
	c.ChatCommand("gamerule mob_griefing false")
	c.Settle()
	srv.SetBlock(5, 63, 0, stateOf(t, "minecraft:farmland", map[string]string{"moisture": "7"}))
	srv.SetBlock(5, 64, 0, stateOf(t, "minecraft:wheat", map[string]string{"age": "0"}))
	particles := false
	for range 120 {
		c.ReadTicks(srv, 10, func(pid int32, data []byte) bool {
			if pid == protocol.IDPlayWorldEvent && protocol.NewReader(data).ReadI32() == protocol.WorldEventBonemeal {
				particles = true
			}
			return false
		})
		if age, _ := registry.StateInt(srv.BlockAt(5, 64, 0), registry.PropAge); particles && age >= 2 {
			inv := srv.MobInventory(id)
			if len(inv) == 1 && inv[0].ItemID == registry.MustItem("minecraft:bone_meal") && inv[0].Count == 1 || len(inv) == 0 {
				return
			}
			t.Fatalf("bone meal not consumed: %v", inv)
		}
	}
	t.Fatalf("bone meal particles=%v, crop=%d", particles, srv.BlockAt(5, 64, 0))
}

func TestAFarmerMakesBreadAndCompostsSurplusSeeds(t *testing.T) {
	t.Parallel()
	srv, c, id := farmYard(t, `{id:"minecraft:wheat",count:12},{id:"minecraft:wheat_seeds",count:64}`, "0", 1)
	filled := false
	for range 120 {
		c.ReadTicks(srv, 10, func(pid int32, data []byte) bool {
			if pid == protocol.IDPlayWorldEvent && protocol.NewReader(data).ReadI32() == rules.LevelEventComposterFill {
				filled = true
			}
			return false
		})
		if !filled {
			continue
		}
		var wheat, bread, seeds int32
		for _, s := range srv.MobInventory(id) {
			switch s.ItemID {
			case registry.MustItem("minecraft:wheat"):
				wheat += s.Count
			case registry.MustItem("minecraft:bread"):
				bread += s.Count
			case registry.MustItem("minecraft:wheat_seeds"):
				seeds += s.Count
			}
		}
		if wheat != 3 || bread != 3 || seeds < 44 || seeds >= 64 {
			t.Fatalf("wheat=%d bread=%d seeds=%d", wheat, bread, seeds)
		}
		if level, _ := registry.StateInt(srv.BlockAt(6, 64, 0), registry.PropLevel); level <= 0 {
			t.Fatal("composter did not fill")
		}
		return
	}
	t.Fatal("farmer never worked at its composter")
}
