package blocks

import (
	"fmt"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// vanilla: SculkBlock.attemptUseCharge
func TestSculkGrowthSoundsAtChargeCursor(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Sculker", servertest.OpsListing(t, "Sculker"))
	servertest.HoldTicks(t, srv)
	c.MoveToGround(2.5, 64, 5.5, true)
	c.Settle()
	srv.SetBlock(8, 63, 8, registry.BlockSculk.DefaultState())
	cursors := strings.TrimSuffix(strings.Repeat("{pos:[I;8,63,8],charge:1000,decay_delay:1,update_delay:0},", 32), ",")
	for _, tc := range []struct {
		seed  int64
		block registry.Block
	}{
		{1, registry.BlockSculkSensor},
		{20, registry.BlockSculkShrieker},
	} {
		srv.SetBlock(1, 64, 1, registry.BlockAir.DefaultState())
		servertest.RunTicks(t, srv, 1)
		srv.SetBlock(8, 64, 8, registry.BlockAir.DefaultState())
		srv.SetPhysicsSeed(tc.seed)
		// Multiple cursors give the decay roll enough attempts in one tick.
		c.ChatCommand(fmt.Sprintf("setblock 1 64 1 minecraft:sculk_catalyst{cursors:[%s]}", cursors))
		c.Settle()
		servertest.RunTicks(t, srv, 1)
		heard := 0
		c.SettleAndCollect(func(id int32, data []byte) {
			if id != protocol.IDPlaySoundEffect {
				return
			}
			sound := servertest.DecodePlaySoundEffect(t, data)
			var block registry.Block
			switch registry.Sound(sound.RegistryID + 1) {
			case registry.SoundBlockSculkSensorPlace:
				block = registry.BlockSculkSensor
			case registry.SoundBlockSculkShriekerPlace:
				block = registry.BlockSculkShrieker
			default:
				return
			}
			heard++
			if block != tc.block {
				t.Errorf("seed %d grew %s, want %s", tc.seed, block.Name(), tc.block.Name())
			}
			if got := registry.BlockOf(srv.BlockAt(8, 64, 8)); got != block {
				t.Errorf("growth is %s, sound belongs to %s", got.Name(), block.Name())
			}
			if sound.X != 68 || sound.Y != 508 || sound.Z != 68 {
				t.Errorf("%s sound at (%d,%d,%d) eighths, want charge cursor center (68,508,68)", block.Name(), sound.X, sound.Y, sound.Z)
			}
		})
		if heard != 1 {
			t.Errorf("seed %d sent %d growth sounds, want 1", tc.seed, heard)
		}
	}
}
