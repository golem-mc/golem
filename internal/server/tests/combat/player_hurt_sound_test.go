package combat

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestOtherPlayersHearAPlayerHurtAndDie checks that a player's hurt sound,
// picked by the damage type's effects, and death sound reach the players
// nearby at the body and not the victim, whose client plays its own. A
// blow a shield turns aside whole still sounds; a totem save sounds nothing.
// vanilla: LivingEntity.hurtServer, Player.getHurtSound, Player.getDeathSound
func TestOtherPlayersHearAPlayerHurtAndDie(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.SurvivalTestConfig())
	alpha := connect().Join("Alpha")
	beta := connect().Join("Beta")
	alpha.MoveToGround(0.5, 64, 0.5, true)
	beta.MoveToGround(2.5, 64, 0.5, true)
	alpha.Settle()
	beta.Settle()
	servertest.HoldTicks(t, srv)
	pos := srv.PlayerPosition("Beta")
	wantX, wantY, wantZ := int32(pos.X*8), int32(pos.Y*8), int32(pos.Z*8)

	// landed is what hurt reports: false for a blow a shield blocks whole.
	expect := func(step string, hurt func() bool, landed bool, want string) {
		t.Helper()
		if got := hurt(); got != landed {
			t.Fatalf("%s: the hurt landed = %v, want %v", step, got, landed)
		}
		var heard bool
		alpha.ReadTicks(srv, 5, func(id int32, data []byte) bool {
			if id != protocol.IDPlaySoundEffect {
				return false
			}
			name, category, x, y, z, volume, pitch := servertest.ReadAmbient(t, data)
			if name != want {
				return false
			}
			heard = true
			if category != protocol.SoundCategoryPlayer || volume != 1 || pitch != 1 {
				t.Errorf("%s: %s category %d volume %v pitch %v, want %d 1 1", step, name, category, volume, pitch, protocol.SoundCategoryPlayer)
			}
			if x != wantX || y != wantY || z != wantZ {
				t.Errorf("%s: %s at %d,%d,%d eighths, want %d,%d,%d", step, name, x, y, z, wantX, wantY, wantZ)
			}
			return true
		})
		if !heard {
			t.Errorf("%s: Alpha heard no %s", step, want)
		}
		beta.ReadTicks(srv, 0, func(id int32, data []byte) bool {
			if id == protocol.IDPlaySoundEffect {
				if name, _, _, _, _, _, _ := servertest.ReadAmbient(t, data); name == want {
					t.Errorf("%s: the victim was sent its own %s", step, name)
				}
			}
			return false
		})
	}

	expect("mob blow", func() bool { return srv.Hurt("Beta", 2) }, true, "minecraft:entity.player.hurt")
	servertest.RunTicks(t, srv, 11)
	expect("on fire", func() bool { return srv.HurtAs("Beta", 2, "minecraft:on_fire") }, true, "minecraft:entity.player.hurt_on_fire")
	servertest.RunTicks(t, srv, 11)

	// Beta faces +z; the shield blocks once its 5-tick block delay is up.
	srv.GiveSlot("Beta", player.OffHandSlot, protocol.ItemStack{ItemID: servertest.ItemIDOrDie(t, "minecraft:shield"), Count: 1})
	beta.UseItemRightClicks(protocol.HandOff)
	beta.Settle()
	servertest.RunTicks(t, srv, 6)
	expect("shield-blocked blow", func() bool { return srv.HurtFrom("Beta", 3, 2.5, 1.5) }, false, "minecraft:entity.player.hurt")
	beta.ReleaseUseItem()
	beta.Settle()
	servertest.RunTicks(t, srv, 11)

	srv.GiveSlot("Beta", player.HotbarSlot, totemStack(t))
	if !srv.Hurt("Beta", 100) {
		t.Fatal("totem save: the blow did not land")
	}
	alpha.ReadTicks(srv, 5, func(id int32, data []byte) bool {
		if id == protocol.IDPlaySoundEffect {
			if name, _, _, _, _, _, _ := servertest.ReadAmbient(t, data); name == "minecraft:entity.player.hurt" || name == "minecraft:entity.player.death" {
				t.Errorf("totem save: Alpha heard %s", name)
			}
		}
		return false
	})
	servertest.RunTicks(t, srv, 11)

	expect("lethal blow", func() bool { return srv.Hurt("Beta", 100) }, true, "minecraft:entity.player.death")
}
