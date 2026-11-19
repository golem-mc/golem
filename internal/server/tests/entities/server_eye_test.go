package entities

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

type fixedEyeTarget struct{ x, y, z int32 }

func (f fixedEyeTarget) NearestEyeTarget(_, _, _ int32, _ int) (int32, int32, int32, bool) {
	return f.x, f.y, f.z, true
}

type noEyeTarget struct{}

func (noEyeTarget) NearestEyeTarget(_, _, _ int32, _ int) (int32, int32, int32, bool) {
	return 0, 0, 0, false
}

func eyeFlight(t *testing.T, targets interface {
	NearestEyeTarget(x, y, z int32, radius int) (int32, int32, int32, bool)
}, mode string) (srv *server.Server, c *servertest.Client) {
	t.Helper()
	cfg := servertest.BowTestConfig()
	cfg.GameMode = mode
	srv, connect := servertest.Start(t, cfg)
	c = connect()
	c.Join("Thrower")
	srv.GiveSlot("Thrower", 36, protocol.ItemStack{ItemID: servertest.MustItem("minecraft:ender_eye"), Count: 3})
	srv.SetEyeTargets(targets)
	srv.StopTick()
	c.MoveTo(0.5, 64, 0.5, -90, 0)
	return srv, c
}

// TestAnEyeOfEnderFliesTowardTheStrongholdAndEnds throws an eye with a
// target in reach: it spawns as its own entity type with the launch sound,
// one eye is spent, and after 81 ticks it is removed with its death sound
// and either drops as an item or shatters in the world event 2003.
// vanilla: EnderEyeItem.use, EyeOfEnder.tick
func TestAnEyeOfEnderFliesTowardTheStrongholdAndEnds(t *testing.T) {
	t.Parallel()
	srv, c := eyeFlight(t, fixedEyeTarget{40, 30, 0}, "survival")
	servertest.UseHeld(c, -90, 0)
	eye := c.ExpectThrownSpawn(srv, protocol.EntityTypeEyeOfEnder)
	if d := eye.Y - (64 + 0.9); d < -0.01 || d > 0.01 {
		t.Errorf("the eye was born at y=%v, want half the player's height up", eye.Y)
	}
	if got := srv.InventorySlot("Thrower", 36).Count; got != 2 {
		t.Errorf("held eyes after the throw = %d, want 2", got)
	}
	srv.TickEntities(81)

	launch, _ := registry.SoundEventID("minecraft:entity.ender_eye.launch")
	death, _ := registry.SoundEventID("minecraft:entity.ender_eye.death")
	var launches, deaths, removed, drops, shatters, swings int
	c.SettleAndCollect(func(id int32, data []byte) {
		r := protocol.NewReader(data)
		switch id {
		case protocol.IDPlaySoundEffect:
			switch r.ReadVarInt() - 1 {
			case launch:
				launches++
			case death:
				deaths++
			}
		case protocol.IDPlaySwingAnimation:
			if r.ReadVarInt() == c.PlayerEntityID && r.ReadVarInt() == protocol.HandMain {
				swings++
			}
		case protocol.IDPlayRemoveEntities:
			for range r.ReadVarInt() {
				if r.ReadVarInt() == eye.EntityID {
					removed++
				}
			}
		case protocol.IDPlaySpawnEntity:
			r.ReadVarInt()
			r.ReadUUID()
			if r.ReadVarInt() == protocol.EntityTypeItem {
				drops++
			}
		case protocol.IDPlayWorldEvent:
			if r.ReadI32() == 2003 {
				shatters++
			}
		}
	})
	if launches != 1 || deaths != 1 || removed != 1 {
		t.Errorf("launch sounds %d, death sounds %d, removals %d; want one each", launches, deaths, removed)
	}
	if swings != 1 {
		t.Errorf("thrower's arm swung %d times, want once", swings)
	}
	if drops+shatters != 1 {
		t.Errorf("the flight ended with %d item drops and %d shatters, want exactly one of them", drops, shatters)
	}
}

// TestAnEyeOfEnderWithNothingToLocateDoesNothing uses an eye where no
// structure is found: nothing flies and the stack is kept.
// vanilla: EnderEyeItem.use
func TestAnEyeOfEnderWithNothingToLocateDoesNothing(t *testing.T) {
	t.Parallel()
	srv, c := eyeFlight(t, noEyeTarget{}, "survival")
	servertest.UseHeld(c, -90, 0)
	srv.TickEntities(2)
	c.SettleAndCollect(func(id int32, data []byte) {
		if id == protocol.IDPlaySpawnEntity {
			r := protocol.NewReader(data)
			r.ReadVarInt()
			r.ReadUUID()
			if typ := r.ReadVarInt(); typ == protocol.EntityTypeEyeOfEnder {
				t.Errorf("an eye flew with no target to locate")
			}
		}
		if id == protocol.IDPlayContainerSlot {
			t.Errorf("a container slot changed though nothing was thrown")
		}
	})
}

// vanilla: SnowballItem.use
func TestAThrownSnowballSwingsForOthersOnly(t *testing.T) {
	t.Parallel()
	srv, watcher := servertest.Join(t, servertest.BowTestConfig(), "Watcher")
	c := servertest.DialServer(t, srv)
	id := c.JoinAs("Thrower")
	watcher.ExpectPlayerAdded("Thrower")
	c.Settle()
	servertest.HoldTicks(t, srv)
	srv.GiveSlot("Thrower", 36, protocol.ItemStack{ItemID: int32(registry.ItemSnowball), Count: 4})
	servertest.RunTicks(t, srv, 30)
	servertest.UseHeld(c, -90, 0)
	c.SettleKeeping()
	if got := srv.AttackTicks("Thrower"); got != 0 {
		t.Errorf("attack ticks after throw = %d, want 0", got)
	}
	r := protocol.NewReader(watcher.ExpectWithinTicks(srv, 20, protocol.IDPlaySwingAnimation))
	if got := r.ReadVarInt(); got != id {
		t.Errorf("swing entity = %d, want %d", got, id)
	}
	if got := r.ReadVarInt(); got != protocol.HandMain {
		t.Errorf("swing hand = %d", got)
	}
	c.ExpectNoneWithinTicks(srv, 5, servertest.PacketIs(protocol.IDPlaySwingAnimation))
}
