package fluids

import (
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// TestWaterPouredInTheNetherSendsLargeSmoke: the evaporating pour sends eight
// large smoke at the cell's corner, spread a block each way with ALTERNATIVE
// randomization.
// vanilla: BucketItem.emptyContents
func TestWaterPouredInTheNetherSendsLargeSmoke(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	srv, c := servertest.Join(t, cfg, "Watcher")
	servertest.CarryIntoNether(t, srv, c, "Watcher", 8, 10)

	if !srv.PlaceContainerInDimension(world.NetherName, 8, 40, 8, "minecraft:dispenser") {
		t.Fatal("the nether did not take the dispenser")
	}
	stack := protocol.ItemStack{ItemID: servertest.ItemIDOrDie(t, "minecraft:water_bucket"), Count: 1}
	if !srv.SetInventoryInDimension(world.NetherName, 8, 40, 8, 0, stack) {
		t.Fatal("the dispenser took no water bucket")
	}
	srv.SetBlockInDimension(world.NetherName, 8, 40, 9, registry.MustState("minecraft:air", nil))
	c.Settle()

	srv.DispenseInDimension(world.NetherName, 8, 40, 8, protocol.FaceSouth)

	var smoke []servertest.LevelParticles
	c.SettleAndCollect(func(id int32, data []byte) {
		if id != protocol.IDPlayLevelParticles {
			return
		}
		p, ok := servertest.ParseLevelParticles(data)
		if !ok {
			t.Fatalf("level particles: % X", data)
		}
		if p.Type == protocol.ParticleLargeSmoke {
			smoke = append(smoke, p)
		}
	})
	want := servertest.LevelParticles{Type: protocol.ParticleLargeSmoke, X: 8, Y: 40, Z: 9,
		DX: 1, DY: 1, DZ: 1, Count: 8, Randomization: 1}
	if len(smoke) != 1 || smoke[0] != want {
		t.Errorf("large smoke = %+v, want one %+v", smoke, want)
	}
	c.Disconnect()
}
