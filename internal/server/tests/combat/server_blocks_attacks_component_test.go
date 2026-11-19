package combat

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
)

// A stack's own blocks_attacks and weapon components: any item can be given
// the ability to block, with its own delay, reductions and wear, and a
// weapon's disable_blocking_for_seconds is read from the stack it holds.
// vanilla: BlocksAttacks, Weapon

// blockerHolding joins a survival player with the /give-n item in the first
// hotbar slot, uses it, and holds the loop once the raise is seen.
func blockerHolding(t *testing.T, give string) (*server.Server, *servertest.Client) {
	t.Helper()
	srv, connect := servertest.Start(t, servertest.HostileTestConfig())
	servertest.FreezeUntilHeld(t, srv)
	srv.SetDifficulty(2)
	c := connect().Join("Blocker")
	c.MoveToGround(0.5, 64, 0.5, true)
	srv.RunLine("give Blocker " + give)
	c.Settle()
	c.UseItemRightClicks(protocol.HandMain)
	c.Settle()
	servertest.HoldTicks(t, srv)
	return srv, c
}

// hitFromFront has a zombie ahead of the holder deal a mob_attack hit.
func hitFromFront(srv *server.Server, damage string) {
	srv.SpawnMob("minecraft:zombie", 0.5, 64, 3.5)
	srv.RunLine("damage Blocker " + damage + " minecraft:mob_attack by @e[type=zombie,limit=1]")
}

// TestAGivenItemBlocksAtOnceAndWearsPerHit has a used stick (blocks_attacks
// with its defaults: no delay, everything blocked) turn a zombie's blow from
// the front on the next tick, and a sword given the same component wear by
// the blocked amount.
// vanilla: BlocksAttacks.CODEC, ItemDamageFunction.DEFAULT
func TestAGivenItemBlocksAtOnceAndWearsPerHit(t *testing.T) {
	t.Parallel()
	srv, _ := blockerHolding(t, "stick[blocks_attacks={}]")
	hitFromFront(srv, "3")
	if got := srv.PlayerHealth("Blocker"); got != 20 {
		t.Errorf("health after a frontal blow on the used stick = %v, want 20", got)
	}

	srv, _ = blockerHolding(t, "iron_sword[blocks_attacks={}]")
	hitFromFront(srv, "3")
	if got := srv.PlayerHealth("Blocker"); got != 20 {
		t.Errorf("health after a frontal blow on the used sword = %v, want 20", got)
	}
	if got := srv.InventorySlot("Blocker", player.HotbarSlot).Damage; got != 3 {
		t.Errorf("the sword wore by %d for three blocked damage, want floor(0+1*3)=3", got)
	}
}

// TestAGivenShieldHalvesTheBlowByItsReduction has a shield given a half
// reduction and a quarter-second delay stand for five ticks, then take a
// frontal mob attack: half of it lands.
// vanilla: BlocksAttacks.resolveBlockedDamage
func TestAGivenShieldHalvesTheBlowByItsReduction(t *testing.T) {
	t.Parallel()
	srv, c := blockerHolding(t, "shield[blocks_attacks={damage_reductions:[{base:0,factor:0.5}],block_delay_seconds:0.25,block_sound:\"item.shield.block\"}]")
	servertest.RunTicks(t, srv, 5)
	hitFromFront(srv, "4")
	var blockSound, damageEvent bool
	c.SettleAndCollect(func(packet int32, data []byte) {
		switch packet {
		case protocol.IDPlaySoundEffect:
			if wireID := protocol.NewReader(data).ReadVarInt(); wireID > 0 {
				if name, ok := servertest.SoundEventName(wireID - 1); ok && name == "minecraft:item.shield.block" {
					blockSound = true
				}
			}
		case protocol.IDPlayDamageEvent:
			if protocol.NewReader(data).ReadVarInt() == c.PlayerEntityID {
				damageEvent = true
			}
		}
	})
	if !blockSound {
		t.Error("a partly blocked hit sent no minecraft:item.shield.block sound")
	}
	if damageEvent {
		t.Error("a partly blocked hit sent a Damage Event, want the block sound in its place")
	}
	if got := srv.PlayerHealth("Blocker"); got != 18 {
		t.Errorf("health after a frontal 4 on a half-reducing shield = %v, want 18", got)
	}
}

// restartedHolding gives a survival player the /give-n item in the first
// hotbar slot, restarts the server so the stack goes through the player file,
// and joins the player again with the loop frozen until held.
func restartedHolding(t *testing.T, give string) (*server.Server, *servertest.Client) {
	t.Helper()
	cfg := servertest.HostileTestConfig()
	cfg.LevelName = t.TempDir()
	srv, c := servertest.Join(t, cfg, "Blocker")
	srv.RunLine("give Blocker " + give)
	c.Settle()
	srv.Shutdown()

	restarted := *cfg
	srv, connect := servertest.Start(t, &restarted)
	servertest.FreezeUntilHeld(t, srv)
	srv.SetDifficulty(2)
	c = connect().Join("Blocker")
	c.MoveToGround(0.5, 64, 0.5, true)
	return srv, c
}

// TestAShieldWithoutBlocksAttacksTakesTheWholeBlow has a shield whose patch
// removes blocks_attacks come back from a restart still without it: used, it
// is not raised, and a zombie's frontal blow lands whole.
// vanilla: DataComponentPatch removal, Item.use
func TestAShieldWithoutBlocksAttacksTakesTheWholeBlow(t *testing.T) {
	t.Parallel()
	srv, c := restartedHolding(t, "shield[!blocks_attacks]")
	if kept := srv.InventorySlot("Blocker", player.HotbarSlot); !kept.Removes(protocol.DataComponentBlocksAttacks) {
		t.Fatalf("the reloaded shield %+v lost its blocks_attacks removal", kept)
	}
	c.UseItemRightClicks(protocol.HandMain)
	c.Settle()
	servertest.HoldTicks(t, srv)
	servertest.RunTicks(t, srv, 5)
	hitFromFront(srv, "3")
	if got := srv.PlayerHealth("Blocker"); got != 17 {
		t.Errorf("health after a frontal 3 on a shield without blocks_attacks = %v, want 17", got)
	}
}
