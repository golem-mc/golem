package admin

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"path/filepath"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// TestPlayerDataFromTheOldLayoutLoads checks that a world still keeping its
// player files where a server before 26.1 wrote them loads them: the startup
// moves the old playerdata file into players/data and the join hands the
// player the inventory it held.
func TestPlayerDataFromTheOldLayoutLoads(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()

	const name = "Oldtimer"
	servertest.WritePlayerFile(t, filepath.Join(cfg.LevelName, "playerdata",
		protocol.OfflineUUID(name).String()+".dat"), nbt.Compound{
		"DataVersion": nbt.Int(4440),
		"Inventory": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
			nbt.Compound{
				"id":    nbt.String("minecraft:stone"),
				"count": nbt.Int(7),
				"Slot":  nbt.Byte(9),
			},
		}},
	})

	_, connect := servertest.Start(t, cfg)
	c := connect()
	c.Login(name)
	c.Configure()
	slots := c.Inventory()

	stone, ok := registry.ItemID("minecraft:stone")
	if !ok {
		t.Fatal("the registry has no stone")
	}
	want := protocol.ItemStack{ItemID: stone, Count: 7}
	if !slots[9].Equal(want) {
		t.Errorf("slot 9 = %+v, want the old playerdata file's stack %+v", slots[9], want)
	}
	c.Disconnect()
}
