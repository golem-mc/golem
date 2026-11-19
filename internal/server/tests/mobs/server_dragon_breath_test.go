package mobs

import (
	"github.com/google/uuid"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

func dragonBreathWorld(t *testing.T) (*server.Server, *servertest.Client, int32) {
	t.Helper()
	cfg := servertest.HostileTestConfig()
	cfg.AllowFlight = true
	cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
	srv, c := servertest.Join(t, cfg, "Breather")
	servertest.HoldTicks(t, srv)
	srv.SetSpawning(false)
	srv.SetGameRule("mob_griefing", false)
	c.MoveTo(8, 128, 5, 0, 0)
	c.Settle()
	id := srv.SpawnMob("minecraft:ender_dragon", 8, 128, 35)
	srv.SetDragonPhase(id, entity.DragonStrafe, "Breather")
	return srv, c, id
}

type dragonBreathWire struct {
	cloudUUID             uuid.UUID
	fireball, cloud       int32
	radius                float32
	shot, impact, removed bool
}

func (w *dragonBreathWire) collect(t *testing.T, c *servertest.Client) {
	t.Helper()
	c.SettleAndCollect(func(id int32, data []byte) {
		r := protocol.NewReader(data)
		switch id {
		case protocol.IDPlaySpawnEntity:
			eid := r.ReadVarInt()
			identity := r.ReadUUID()
			typ := r.ReadVarInt()
			if registry.EntityTypes[typ] == "minecraft:dragon_fireball" {
				w.fireball = eid
			}
			if typ == protocol.EntityTypeAreaEffectCloud {
				w.cloud = eid
				w.cloudUUID = identity
			}
		case protocol.IDPlayWorldEvent:
			event := r.ReadI32()
			if event == 1017 {
				w.shot = true
			}
			if event == 2006 {
				w.impact = true
			}
		case protocol.IDPlayEntityMetadata:
			eid := r.ReadVarInt()
			if eid == w.fireball && eid != 0 {
				entries, ok := servertest.ReadMetadata(r)
				if !ok || servertest.MetaOf(entries, protocol.MetadataIndexItemSlot) != nil {
					t.Error("dragon fireball has invalid metadata")
				}
				return
			}
			if eid != w.cloud {
				return
			}
			entries, ok := servertest.ReadMetadata(r)
			if !ok {
				t.Error("invalid cloud metadata")
			}
			if radius := servertest.MetaOf(entries, protocol.MetadataIndexCloudRadius); radius != nil {
				w.radius = radius.FloatVal
			}
		case protocol.IDPlayRemoveEntities:
			for range r.ReadVarInt() {
				if r.ReadVarInt() == w.cloud {
					w.removed = true
				}
			}
		}
	})
}

// vanilla: DragonStrafePlayerPhase.doServerTick, DragonFireball.onHit, BottleItem.use
func TestADragonStrafesWithABreathCloudThatFillsABottle(t *testing.T) {
	t.Parallel()
	srv, c, id := dragonBreathWorld(t)
	// Normal leaves the dragon-owned breath's 6 unscaled.
	srv.SetDifficulty(rules.DifficultyNormal)
	var wire dragonBreathWire
	for range 200 {
		servertest.RunTicks(t, srv, 1)
		wire.collect(t, c)
		if wire.fireball != 0 {
			break
		}
	}
	if !wire.shot || wire.fireball == 0 {
		t.Fatalf("shot event=%v, dragon fireball=%d", wire.shot, wire.fireball)
	}
	srv.SetMobNoAI(id)
	// Only the projectile flies: the dragon cannot contact the player during this check.
	srv.TickEntities(100)
	wire.collect(t, c)
	if wire.cloud == 0 || wire.radius != 3 || !wire.impact {
		t.Fatalf("impact cloud=%d, radius=%v, event=%v", wire.cloud, wire.radius, wire.impact)
	}
	servertest.RunTicks(t, srv, 22)
	if health := srv.PlayerHealth("Breather"); health != 14 {
		t.Fatalf("breath damage left health %v, want 14", health)
	}
	// A cloud retains its resolved dragon owner after the corpse is removed.
	srv.KillMob(id)
	servertest.RunTicks(t, srv, 21)
	srv.GiveSlot("Breather", player.HotbarSlot, protocol.ItemStack{ItemID: int32(registry.ItemGlassBottle), Count: 1})
	c.Settle()
	// Clouds are checked around the body, independently of the water ray.
	_, before, _ := srv.CloudState(wire.cloudUUID)
	events := &gameEvents{seen: map[string][][3]float64{}}
	srv.ObserveGameEvents(events.add)
	servertest.UseHeld(c, 0, -90)
	c.Settle()
	// At the player's exact position, not a block centre.
	// vanilla: BottleItem.use
	if at := srv.PlayerPosition("Breather"); !events.at("minecraft:fluid_pickup", at.X, at.Y, at.Z) {
		t.Errorf("fluid_pickup at %v, want at the player's %v", events.seen["minecraft:fluid_pickup"], at)
	}
	if stack := srv.InventorySlot("Breather", player.HotbarSlot); stack.ItemID != int32(registry.ItemDragonBreath) || stack.Count != 1 {
		t.Fatalf("filled bottle=%+v", stack)
	}
	_, after, _ := srv.CloudState(wire.cloudUUID)
	if after != before-0.5 {
		t.Fatalf("bottling radius %v -> %v, want a loss of 0.5", before, after)
	}
}
