package world

import (
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

func joinDragonFight(t *testing.T) (*server.Server, *servertest.Client, int32) {
	t.Helper()
	return joinDragonFightWith(t, servertest.CrossingTestConfig(t))
}

// joinDragonFightWith is joinDragonFight on the given config.
func joinDragonFightWith(t *testing.T, cfg *config.Config) (*server.Server, *servertest.Client, int32) {
	t.Helper()
	srv, c := servertest.Join(t, cfg, "DragonWatcher", servertest.OpsListing(t, "DragonWatcher"), func(s *server.Server) { servertest.FreezeUntilHeld(t, s) })
	srv.MovePlayerToDimension("DragonWatcher", world.EndName, 0, 70, 0)
	if _, ok := servertest.ReadUntilViewSent(t, c); !ok {
		t.Fatal("no End view")
	}
	servertest.HoldTicks(t, srv)
	var dragon int32
	// The fight's player scan runs every 20 ticks. Stop on its spawn tick so
	// the movement tick cannot obscure the spawn's y=128.
	for range 40 {
		// The arena generates off the tick, in wall time no tick count bounds.
		servertest.PollUntil(t, 20*time.Second, srv.DragonArenaSettled)
		servertest.RunTicks(t, srv, 1)
		for _, m := range srv.MobsInDimension(world.EndName) {
			if m.Kind == "minecraft:ender_dragon" {
				dragon = m.ID
				break
			}
		}
		if dragon != 0 {
			break
		}
	}
	if dragon == 0 {
		t.Fatal("no dragon within 40 ticks")
	}
	// One tick positions the parts, which a NoAI dragon keeps.
	servertest.RunTicks(t, srv, 1)
	srv.SetMobNoAI(dragon)
	return srv, c, dragon
}

func TestEndDragonSpawnAndBossBar(t *testing.T) {
	_, c, id := joinDragonFight(t)
	// The bar is added with the watcher, the dragon once its arena has
	// loaded, so the two arrive in either order.
	var boss []byte
	spawned := false
	for gotID, data := range c.Packets(servertest.ExpectTimeout) {
		switch gotID {
		case protocol.IDPlayBossEvent:
			if boss == nil {
				boss = data
			}
		case protocol.IDPlaySpawnEntity:
			r := protocol.NewReader(data)
			got := r.ReadVarInt()
			r.ReadUUID()
			if r.ReadVarInt() != dragonTypeID() {
				continue
			}
			r.ReadF64()
			if y := r.ReadF64(); got != id || y != 128 {
				t.Fatalf("dragon spawn id=%d y=%v, want %d at 128", got, y, id)
			}
			spawned = true
		}
		if boss != nil && spawned {
			break
		}
	}
	if boss == nil || !spawned {
		t.Fatalf("boss event arrived=%v, dragon spawn arrived=%v", boss != nil, spawned)
	}
	r := protocol.NewReader(boss)
	r.ReadUUID()
	if got := r.ReadVarInt(); got != protocol.BossEventAdd {
		t.Fatalf("boss action=%d", got)
	}
	name, ok := protocol.ReadText(r)
	if !ok || name.Translate != "entity.minecraft.ender_dragon" {
		t.Fatalf("boss title=%+v", name)
	}
	if fill := r.ReadF32(); fill != 1 {
		t.Errorf("fill=%v", fill)
	}
	if color := r.ReadVarInt(); color != 0 {
		t.Errorf("color=%d, want pink (0)", color)
	}
	if overlay := r.ReadVarInt(); overlay != 0 {
		t.Errorf("overlay=%d", overlay)
	}
	if flags := r.ReadU8(); flags != 6 {
		t.Errorf("flags=%d, want music and fog", flags)
	}
	if r.Err() != nil || r.Remaining() != 0 {
		t.Fatal("malformed boss event")
	}
}

func TestEndDragonBodyDamageAndCrystalHealing(t *testing.T) {
	srv, c, id := joinDragonFight(t)
	// The body's part is dragon+3. Stand beside its 5x3 box, within attack
	// range, with a fully recovered eight-damage netherite sword.
	c.MoveTo(2, 128, 0, 0, 0)
	c.Settle()
	srv.GiveSlot("DragonWatcher", 36, protocol.ItemStack{ItemID: int32(registry.ItemNetheriteSword), Count: 1})
	servertest.RunTicks(t, srv, 21)
	c.Attack(id + 3)
	c.Settle()
	if health, ok := srv.MobHealthInDimension(world.EndName, id); !ok || health != 197 {
		t.Fatalf("body hit health=%v, want 200-(8/4+1)=197", health)
	}
	servertest.RunTicks(t, srv, 1)
	for {
		r := protocol.NewReader(c.ExpectPlay(protocol.IDPlayBossEvent))
		r.ReadUUID()
		if r.ReadVarInt() != protocol.BossEventProgress {
			continue
		}
		if fill := r.ReadF32(); fill != float32(197.0/200) {
			t.Fatalf("body hit bar=%v", fill)
		}
		break
	}
	srv.SpawnEndCrystalInDimension(world.EndName, 0, 128, 10)
	// Selection is a 1/10 roll; wait for the first healed point, then verify
	// the exact ten-tick cadence while the dragon is held in place.
	healed := false
	for range 1000 {
		servertest.RunTicks(t, srv, 1)
		h, _ := srv.MobHealthInDimension(world.EndName, id)
		if h == 198 {
			healed = true
			break
		}
	}
	if !healed {
		t.Fatal("dragon did not select its nearby crystal")
	}
	servertest.RunTicks(t, srv, 9)
	if h, _ := srv.MobHealthInDimension(world.EndName, id); h != 198 {
		t.Fatalf("healed before ten ticks: %v", h)
	}
	servertest.RunTicks(t, srv, 1)
	if h, _ := srv.MobHealthInDimension(world.EndName, id); h != 199 {
		t.Fatalf("health after ten ticks=%v, want 199", h)
	}
}

func dragonTypeID() int32 { id, _ := registry.EntityTypeID("minecraft:ender_dragon"); return id }
