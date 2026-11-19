package growth

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
	"testing"
)

// TestFrogspawnHatches exercises /setblock's scheduled hatch, wire sound and spawn positions.
func TestFrogspawnHatches(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Keeper", servertest.OpsListing(t, "Keeper"))
	srv.SetGameRuleValue(world.RuleRandomTickSpeed.String(), 0)
	servertest.SetFarmBlock(t, c, srv, 2, 63, 2, "minecraft:water", nil)
	servertest.SetFarmBlock(t, c, srv, 2, 64, 2, "minecraft:frogspawn", nil)
	servertest.HoldTicks(t, srv)
	tadpoleType, _ := registry.EntityTypeID("minecraft:tadpole")
	sound := false
	var hatchlings []int32
	c.ReadTicks(srv, 12000, func(id int32, data []byte) bool {
		switch id {
		case protocol.IDPlaySoundEffect:
			name, category, _, _, _, volume, pitch := servertest.ReadAmbient(t, data)
			if name == "minecraft:block.frogspawn.hatch" {
				sound = true
				if category != protocol.SoundCategoryBlock || volume != 1 || pitch != 1 {
					t.Errorf("hatch sound: category=%d volume=%v pitch=%v", category, volume, pitch)
				}
			}
		case protocol.IDPlaySpawnEntity:
			r := protocol.NewReader(data)
			eid := r.ReadVarInt()
			r.ReadUUID()
			kind := r.ReadVarInt()
			if kind != tadpoleType {
				break
			}
			x, y, z := r.ReadF64(), r.ReadF64(), r.ReadF64()
			if x < 2.2 || x > 2.8 || z < 2.2 || z > 2.8 || y != 63.5 {
				t.Errorf("hatch position = %v,%v,%v", x, y, z)
			}
			hatchlings = append(hatchlings, eid)
		}
		return false
	})
	if got := srv.BlockAt(2, 64, 2); registry.BlockOf(got) != registry.BlockAir {
		t.Fatalf("spawn remains: %d", got)
	}
	if !sound || len(hatchlings) < 2 || len(hatchlings) > 5 {
		t.Fatalf("sound=%v hatchlings=%v", sound, hatchlings)
	}
	for _, id := range hatchlings {
		if !srv.MobPersistent(id) {
			t.Errorf("tadpole %d is not persistent", id)
		}
	}
}

func TestFallingBlockDestroysFrogspawn(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Keeper", servertest.OpsListing(t, "Keeper"))
	servertest.SetFarmBlock(t, c, srv, 2, 63, 2, "minecraft:water", nil)
	servertest.SetFarmBlock(t, c, srv, 2, 64, 2, "minecraft:frogspawn", nil)
	servertest.HoldTicks(t, srv)
	srv.SpawnFallingBlock(2, 66, 2, registry.BlockSand.DefaultState())
	servertest.RunTicks(t, srv, 20)
	if got := srv.BlockAt(2, 64, 2); got == registry.BlockFrogspawn.DefaultState() {
		t.Fatal("falling sand left frogspawn intact")
	}
	if len(srv.ItemsInDimension("minecraft:overworld")) != 0 {
		t.Fatal("destroyed frogspawn dropped an item")
	}
}
