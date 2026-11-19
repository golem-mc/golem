package plugins_test

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

// vanilla: MerchantResultSlot.onTake
func TestPluginPlayerTradeEvent(t *testing.T) {
	var asked atomic.Int32
	var cancel atomic.Bool
	cancel.Store(true)
	cfg := servertest.MobTestConfig()
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv := servertest.NewTestServerWithPlugins(t, cfg, []plugin.Registration{{Name: "trade", Plugin: &testPlugin{enable: func(s plugin.Server) error {
		plugin.Listen(s, func(e *plugin.PlayerTradeEvent) {
			asked.Add(1)
			_ = e.Merchant.Pos() // reads the villager: no lock may be held
			if e.Trade.Result.Item != registry.ItemBread || e.Trade.Result.Count != 6 || e.Trade.Cost.Item != registry.ItemEmerald {
				t.Errorf("trade %+v", e.Trade)
			}
			e.SetCancelled(cancel.Load())
		})
		return nil
	}}}})
	servertest.HoldTicks(t, srv)
	servertest.ReleaseTicks(t, srv)
	w := servertest.WindowedOf(t, "Buyer", servertest.DialServer(t, srv))
	offers := nbt.Compound{"Recipes": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
		nbt.Compound{"buy": nbt.Compound{"id": nbt.String("minecraft:emerald"), "count": nbt.Int(1)},
			"sell": nbt.Compound{"id": nbt.String("minecraft:bread"), "count": nbt.Int(6)}, "maxUses": nbt.Int(12)},
	}}}
	id := srv.SpawnVillagerWithOffers("minecraft:farmer", 1, offers, 1.5, 64, 0.5)
	w.Settle()
	srv.GiveSlot("Buyer", player.HotbarSlot, protocol.ItemStack{ItemID: int32(registry.ItemEmerald), Count: 1})
	w.UseOn(id)
	w.SettleAndCollect(func(pid int32, data []byte) {
		if pid == protocol.IDPlayOpenScreen {
			w.Window = protocol.NewReader(data).ReadVarInt()
		}
	})
	w.Send(protocol.IDPlaySelectTradeIn, func(b *protocol.Buffer) { b.PutVarInt(0) })
	w.CollectContainerSlots()
	const result = int16(2)

	w.Click(result, 0, protocol.ClickModePickup)
	w.SettleTrackingState() // the refused click is answered with the whole window
	if asked.Load() != 1 {
		t.Fatalf("asked %d times, want 1", asked.Load())
	}

	cancel.Store(false)
	w.Click(result, 0, protocol.ClickModePickup)
	w.ExpectClickResult(map[int16]protocol.ItemStack{0: {}, result: {}}, protocol.ItemStack{ItemID: int32(registry.ItemBread), Count: 6})
	if asked.Load() != 2 {
		t.Errorf("asked %d times, want 2", asked.Load())
	}
}

// vanilla: BlockAttachedEntity.skipAttackInteraction, ItemFrame.hurtServer
func TestPluginHangingBreakEvent(t *testing.T) {
	cancel, asked := true, 0
	var remover plugin.Entity
	srv, _, c := blockScene(t, "minecraft:dirt", false, func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.HangingBreakEvent) {
			asked++
			remover = e.Remover
			_ = e.Entity.Facing() // reads the frame: no lock may be held
			e.SetCancelled(cancel)
		})
	})
	servertest.BuildPaintingWall(srv, 4, 64, 65, 5, 5)
	c.MoveToGround(4.5, 64, 3.5, true)
	frame := srv.SpawnFrameEntity(4, 64, 4)
	c.Settle()
	c.Attack(frame.ID)
	fired(t, srv, c, "hanging break", &asked, 1)
	if n := srv.FrameCount(); n != 1 {
		t.Fatalf("a cancelled break left %d frames, want 1", n)
	}
	if remover == nil || remover.Type() != registry.EntityTypePlayer {
		t.Errorf("remover %v, want the player", remover)
	}
	cancel = false
	c.Attack(frame.ID)
	fired(t, srv, c, "second hanging break", &asked, 2)
	if n := srv.FrameCount(); n != 0 {
		t.Errorf("an allowed break left %d frames, want 0", n)
	}
}

// vanilla: Snowball.use, Projectile.spawnProjectile
func TestPluginProjectileLaunchEvent(t *testing.T) {
	cancel, asked := true, 0
	srv, _, c := blockScene(t, "minecraft:snowball", false, func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.ProjectileLaunchEvent) {
			asked++
			if shooter, ok := e.Projectile.Shooter(); !ok || shooter.Type() != registry.EntityTypePlayer {
				t.Errorf("shooter %v %v", shooter, ok)
			}
			e.SetCancelled(cancel)
		})
	})
	c.UseItemRightClicks(protocol.HandMain)
	fired(t, srv, c, "projectile launch", &asked, 1)
	servertest.RunTicks(t, srv, 2)
	if n := len(srv.ThrownInDimension("minecraft:overworld")); n != 0 {
		t.Fatalf("a cancelled launch left %d projectiles", n)
	}
	cancel = false
	srv.GiveSlot("Miner", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItem("minecraft:snowball"), Count: 1})
	c.UseItemRightClicks(protocol.HandMain)
	fired(t, srv, c, "second launch", &asked, 2)
	if n := len(srv.ThrownInDimension("minecraft:overworld")); n != 1 {
		t.Errorf("an allowed launch left %d projectiles, want 1", n)
	}
}

// vanilla: Pig.thunderHit, Mob.convertTo
func TestPluginEntityTransformEvent(t *testing.T) {
	cancel, asked := true, 0
	var reason plugin.TransformReason
	srv, _ := tickScene(t, func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.EntityTransformEvent) {
			asked++
			reason = e.Reason
			_ = e.Entity.Type() // reads the pig: no lock may be held
			e.SetCancelled(cancel)
		})
	})
	srv.SpawnMob("minecraft:pig", 8.5, 64, 8.5)
	srv.StrikeLightning(8.5, 64, 8.5)
	for range 3 {
		srv.RunTick()
	}
	if asked < 1 || reason != plugin.TransformLightning || len(srv.MobIDsOfKind("minecraft:pig")) != 1 {
		t.Fatalf("refused: asked %d, reason %v, pigs %d", asked, reason, len(srv.MobIDsOfKind("minecraft:pig")))
	}
	cancel = false
	srv.StrikeLightning(8.5, 64, 8.5)
	for range 3 {
		srv.RunTick()
	}
	if len(srv.MobIDsOfKind("minecraft:pig")) != 0 || len(srv.MobIDsOfKind("minecraft:zombified_piglin")) != 1 {
		t.Errorf("allowed: pigs %d, piglins %d", len(srv.MobIDsOfKind("minecraft:pig")), len(srv.MobIDsOfKind("minecraft:zombified_piglin")))
	}
}

// vanilla: EnderMan.EndermanTakeBlockGoal.tick
func TestPluginEntityChangeBlockEvent(t *testing.T) {
	cancel, asked := true, 0
	var to plugin.BlockState
	srv, _ := tickScene(t, func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.EntityChangeBlockEvent) {
			asked++
			to = e.NewState
			_ = e.Entity.Type() // reads the mob: no lock may be held
			e.SetCancelled(cancel)
		})
	})
	stone := servertest.BlockState(t, "minecraft:stone")
	srv.SetBlock(8, 64, 8, stone)
	id := srv.SpawnMob("minecraft:enderman", 8.5, 70, 8.5)
	other := srv.SpawnMob("minecraft:enderman", 9.5, 70, 8.5)
	take := func() {
		// The move runs off the tick goroutine, so its listener call is
		// answered by the ticks driven here.
		done := make(chan struct{})
		go func() { srv.EndermenMoveBlockTogether(id, other, 8, 64, 8, true, 0); close(done) }()
		for range 400 {
			select {
			case <-done:
				return
			case <-time.After(5 * time.Millisecond):
				srv.RunTick()
			}
		}
		t.Fatal("the block move never finished")
	}
	take()
	if asked == 0 || to != 0 || srv.BlockAt(8, 64, 8) != stone {
		t.Fatalf("refused take: asked %d, to %d, block %d", asked, to, srv.BlockAt(8, 64, 8))
	}
	cancel = false
	take()
	if srv.BlockAt(8, 64, 8) == stone {
		t.Errorf("an allowed take left the block")
	}
}

// vanilla: Entity.igniteForTicks
func TestPluginEntityCombustEvent(t *testing.T) {
	cancel, asked := true, 0
	srv, _ := tickScene(t, func(s plugin.Server) {
		plugin.Listen(s, func(e *plugin.EntityCombustEvent) {
			asked++
			if e.Duration != 100 {
				t.Errorf("duration %d", e.Duration)
			}
			e.Duration = 50
			e.SetCancelled(cancel)
		})
	})
	ignite := func() {
		// The ignition runs off the tick goroutine, so its listener call is
		// answered by the ticks driven here.
		done := make(chan struct{})
		go func() { srv.IgnitePlayer("Victim", 100); close(done) }()
		for range 400 {
			select {
			case <-done:
				return
			case <-time.After(5 * time.Millisecond):
				srv.RunTick()
			}
		}
		t.Fatal("the ignition never finished")
	}
	ignite()
	if asked != 1 || srv.PlayerFire("Victim") != -20 {
		t.Fatalf("refused: asked %d, fire %d", asked, srv.PlayerFire("Victim"))
	}
	cancel = false
	ignite()
	if got := srv.PlayerFire("Victim"); got < 40 || got > 50 { // the ticks driven above burn some off
		t.Errorf("fire %d, want the edited 50", got)
	}
}
