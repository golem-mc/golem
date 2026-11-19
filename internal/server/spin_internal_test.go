package server

import (
	"runtime"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

func TestSpinReleasePublishesAfterGroundedLaunch(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.LevelName = t.TempDir()
	cfg.LevelType = TestFlatLevelType
	s, err := newServer(cfg, 1)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Shutdown)
	w := s.allDims()[0].w
	w.Acquire(world.ChunkPos{})
	t.Cleanup(func() { w.Release(world.ChunkPos{}) })
	p := &player.Player{UUID: protocol.OfflineUUID("Keeper"), EntityID: s.nextEntity()}
	p.SetPosition(player.Position{X: 0.5, Y: 64, Z: 0.5, OnGround: true})
	p.Inventory(func(inv *player.Inventory) {
		inv.Slots[player.HotbarSlot] = protocol.ItemStack{
			ItemID: int32(registry.ItemTrident), Count: 1,
			Enchantments: map[int32]int32{riptideID(t): 1},
		}
	})
	age, _ := w.Time()
	p.SetShoulders(player.Shoulders{SatAt: age - shoulderSettle - 1, Left: nbt.Compound{
		"id": nbt.String("minecraft:parrot"), "Health": nbt.Float(6),
	}})
	c := &client{srv: s, player: p}
	c.dim.Store(s.allDims()[0])

	// Hold the launch at move while the tick loop can see the dropped parrot.
	c.tp.mu.Lock()
	released := make(chan struct{})
	go func() {
		c.startSpinAttack(protocol.HandMain, 1.5)
		close(released)
	}()
	func() {
		defer func() { c.tp.mu.Unlock(); <-released }()
		for {
			s.mobsMu.Lock()
			dropped := len(s.herdOf(w).herd) == 1
			s.mobsMu.Unlock()
			if dropped {
				break
			}
			runtime.Gosched()
		}
		c.tickSpinAttack()
		c.spinMu.Lock()
		pending := c.spin.ticks
		c.spinMu.Unlock()
		if pending != 0 || p.Appearance().SpinAttack {
			t.Error("spin became active before the grounded launch completed")
		}
	}()

	pos := p.Position()
	if want := 64 + rules.SpinAttackGroundLift; pos.Y != want {
		t.Fatalf("launch Y = %v, want %v", pos.Y, want)
	}
	c.spinMu.Lock()
	baseline, ticks := c.spin.last, c.spin.ticks
	c.spinMu.Unlock()
	if baseline != pos || ticks != rules.SpinAttackTicks {
		t.Errorf("published spin = (%+v, %d), want (%+v, %d)", baseline, ticks, pos, rules.SpinAttackTicks)
	}
	// The client's first flight step follows its predicted grounded lift.
	pos.Y += 0.48
	pos.Z = 1
	c.move(pos, false)
	c.tickSpinAttack()
	s.mobsMu.Lock()
	herd := s.herdOf(w).herd
	alive := len(herd) == 1 && herd[0].Type == entity.TypeParrot && herd[0].Alive() && herd[0].Health == 6
	s.mobsMu.Unlock()
	if !alive {
		t.Error("the dropped parrot did not survive the first post-launch tick at 6 health")
	}
	if !p.Appearance().SpinAttack {
		t.Error("spin stopped on the first post-launch tick")
	}
}

func riptideID(t *testing.T) int32 {
	t.Helper()
	id, ok := registry.EnchantmentID("minecraft:riptide")
	if !ok {
		t.Fatal("no riptide enchantment")
	}
	return id
}
