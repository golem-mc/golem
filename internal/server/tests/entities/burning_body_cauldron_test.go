package entities

import (
	"fmt"
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// vanilla: LayeredCauldronBlock.entityInside, PowderSnowBlock.entityInside
func TestBurningBodyPutOutInCauldron(t *testing.T) {
	blocks := []struct {
		name     string
		state    map[string]string
		cauldron bool
	}{
		{"water_cauldron", map[string]string{"level": "3"}, true},
		{"powder_snow_cauldron", map[string]string{"level": "3"}, true},
		{"powder_snow", nil, false},
	}
	bodies := []struct {
		kind string
		y    string
	}{
		{"minecart", "64.5"},
		{"armor_stand", "64.5"},
		{"experience_orb", "64.1"},
		{"oak_boat", "64.5"},
	}
	for _, b := range blocks {
		for _, e := range bodies {
			t.Run(b.name+"/"+e.kind, func(t *testing.T) {
				t.Parallel()
				srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Dowser", servertest.OpsListing(t, "Dowser"))
				servertest.HoldTicks(t, srv)
				state := registry.MustState("minecraft:"+b.name, b.state)
				srv.SetBlock(4, 64, 4, state)
				c.ChatCommand("summon minecraft:" + e.kind + " 4.5 " + e.y + " 4.5 {Fire:200s}")
				typ, _ := registry.EntityTypeID("minecraft:" + e.kind)
				c.AwaitSpawn(typ)
				c.Settle()
				servertest.RunTicks(t, srv, 3)
				target := fmt.Sprintf("@e[type=minecraft:%s,limit=1]", e.kind)
				if got := awaitEntityDataCompound(t, srv, c, target, "Fire"); got != "0s" {
					t.Errorf("Fire = %s, want 0s", got)
				}
				if !b.cauldron {
					if got := srv.BlockAt(4, 64, 4); got != registry.BlockAir.DefaultState() {
						t.Errorf("powder snow state = %d, want air", got)
					}
				}
				if b.cauldron {
					if lvl, _ := registry.StateInt(srv.BlockAt(4, 64, 4), registry.PropLevel); lvl != 2 {
						t.Errorf("cauldron level = %d, want 2", lvl)
					}
				}
			})
		}
	}
}
