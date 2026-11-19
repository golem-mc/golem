package world

import (
	"slices"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

func TestSleepingBlockEntityWakesOnWrites(t *testing.T) {
	for name, write := range map[string]func(*BlockEntity){
		"slot":               func(b *BlockEntity) { b.Put(1, protocol.ItemStack{ItemID: int32(registry.ItemCoal), Count: 1}) },
		"exclusive callback": func(b *BlockEntity) { b.Use(func(b *BlockEntity) { b.LitRemaining = 3 }) },
		"live callback":      func(b *BlockEntity) { b.UseLive(func(b *BlockEntity) { b.CookingSpent = 6 }) },
		"load":               func(b *BlockEntity) { b.loadFrom(nbt.Compound{"lit_time_remaining": nbt.Int(3)}) },
	} {
		t.Run(name, func(t *testing.T) {
			b := newBlockEntity(KindFurnace, 0, GroundLevel, 0)
			b.Tick(func(*BlockEntity) bool { return true })
			if !b.Sleeping() {
				t.Fatal("idle ticker did not sleep")
			}
			b.Tick(func(*BlockEntity) bool { t.Fatal("sleeping ticker ran"); return false })
			write(b)
			ran := false
			b.Tick(func(*BlockEntity) bool { ran = true; return false })
			if !ran || b.Sleeping() {
				t.Fatal("write did not restore the ticker")
			}
		})
	}
}

func TestWakeDuringBlockEntityTickIsNotLost(t *testing.T) {
	b := newBlockEntity(KindCampfire, 0, GroundLevel, 0)
	entered, resume, finished := make(chan struct{}), make(chan struct{}), make(chan struct{})
	go func() {
		b.Tick(func(*BlockEntity) bool {
			close(entered)
			<-resume
			return true
		})
		close(finished)
	}()
	<-entered
	b.Wake() // A world write cannot take the entity lock while holding World.mu.
	close(resume)
	<-finished
	if b.Sleeping() {
		t.Fatal("tick overwrote a concurrent wake")
	}
	b.Tick(func(*BlockEntity) bool { return true })
	if !b.Sleeping() {
		t.Fatal("next idle tick did not sleep")
	}
	b.removed.Store(true)
	b.Wake()
	b.Tick(func(*BlockEntity) bool { t.Fatal("removed ticker ran"); return false })
}

func TestSleepingHopperCanBeTheSourceOfAChainedTransfer(t *testing.T) {
	src := newBlockEntity(KindHopper, 0, GroundLevel+1, 0)
	src.HopperReady(4)
	src.Tick(func(*BlockEntity) bool { return true })
	dst := newBlockEntity(KindHopper, 0, GroundLevel, 0)
	dst.HopperReady(5)
	dst.HopperReceived(src)
	if dst.Cooldown != HopperMoveInterval-1 {
		t.Fatalf("cooldown after pulling from a sleeping hopper = %d, want 7", dst.Cooldown)
	}
}

func TestBlockWritesWakeWithoutChangingTickOrder(t *testing.T) {
	w := newTestWorld(t, NewFlatGenerator())
	w.Acquire(ChunkPos{})
	defer w.Release(ChunkPos{})
	state := registry.BlockCampfire.DefaultState()
	next, _ := registry.WithBool(state, registry.PropLit, false)
	for _, x := range []int32{2, 3, 4} {
		w.SetBlock(x, GroundLevel, 2, state)
		w.EnsureBlockEntity(x, GroundLevel, 2, KindCampfire).Tick(func(*BlockEntity) bool { return true })
	}
	before := w.TickingBlockEntities()
	w.SetBlock(3, GroundLevel, 2, next)
	w.SetBlocks([]BlockChange{{X: 4, Y: GroundLevel, Z: 2, State: next}})
	after := w.TickingBlockEntities()
	if !slices.Equal(before, after) {
		t.Fatal("waking reordered the tick list")
	}
	if !after[0].Sleeping() || after[1].Sleeping() || after[2].Sleeping() {
		t.Fatal("block writes did not wake exactly their entities")
	}
}
