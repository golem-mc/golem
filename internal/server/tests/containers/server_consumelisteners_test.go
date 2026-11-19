package containers

import (
	"math"
	"slices"
	"sync"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// The consume listeners of components and the chorus fruit's
// teleport_randomly, black-box: eating the item sends the effects, the
// teleport moves the player.

func TestConsumeListenersFollowComponents(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"apple listeners", "plain apple", "zero amplifier", "stored zero amplifier", "default bottle", "removed amplifier", "listeners before actions"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			cfg := servertest.BlockTestConfig()
			cfg.ViewDistance, cfg.SimulationDistance = config.MinDistance, config.MinDistance
			srv, c, id := servertest.JoinAs(t, cfg, "Eater")
			c.Settle()
			servertest.HoldTicks(t, srv)
			stack := protocol.ItemStack{ItemID: int32(registry.ItemApple), Count: 1}
			use := protocol.ConsumableComponent{Sound: protocol.SoundHolder{Name: "minecraft:entity.generic.eat"}}
			var want []servertest.MobEffect
			omen := servertest.MobEffect{Entity: id, Effect: registry.MobEffectBadOmen, Duration: 120000, Flags: 4}
			switch name {
			case "apple listeners", "listeners before actions":
				stack.SetSuspiciousStewEffects([]protocol.StewEffect{{Effect: registry.MobEffectGlowing, Duration: 600}})
				stack.SetOminousBottleAmplifier(2)
				omen.Amplifier = 2
				want = []servertest.MobEffect{{Entity: id, Effect: registry.MobEffectGlowing, Duration: 600, Flags: 6}, omen}
				if name == "listeners before actions" {
					use.Effects = []protocol.ConsumeEffectComponent{{Type: protocol.ConsumeEffectClearAllEffects}}
				}
			case "zero amplifier", "stored zero amplifier":
				stack.SetOminousBottleAmplifier(0)
				want = []servertest.MobEffect{omen}
			case "default bottle", "removed amplifier":
				stack.ItemID = int32(registry.ItemOminousBottle)
				if name == "removed amplifier" {
					stack.Remove(protocol.DataComponentOminousBottleAmplifier)
				} else {
					want = []servertest.MobEffect{omen}
				}
			}
			stack.SetConsumable(use)
			if name == "stored zero amplifier" {
				var ok bool
				stack, ok = registry.StoredStack(registry.StackTag(stack))
				if !ok {
					t.Fatal("stored apple did not decode")
				}
			}
			srv.GiveSlot("Eater", player.HotbarSlot, stack)
			c.UseItemRightClicks(protocol.HandMain)
			var got []servertest.MobEffect
			var removed []int32
			c.ReadTicks(srv, 1, func(packet int32, data []byte) bool {
				r := protocol.NewReader(data)
				switch packet {
				case protocol.IDPlayUpdateMobEffect:
					if len(removed) != 0 {
						t.Error("listener ran after on_consume_effects")
					}
					got = append(got, servertest.MobEffect{Entity: r.ReadVarInt(), Effect: r.ReadVarInt(), Amplifier: r.ReadVarInt(), Duration: r.ReadVarInt(), Flags: r.ReadU8() & 7})
				case protocol.IDPlayRemoveMobEffect:
					if entity := r.ReadVarInt(); entity != id {
						t.Errorf("removed effect on entity %d, want %d", entity, id)
					}
					removed = append(removed, r.ReadVarInt())
				}
				if r.Err() != nil {
					t.Fatal(r.Err())
				}
				return false
			})
			if !slices.Equal(got, want) {
				t.Errorf("effect packets = %+v, want %+v", got, want)
			}
			var wantRemoved []int32
			if name == "listeners before actions" {
				wantRemoved = []int32{registry.MobEffectGlowing, registry.MobEffectBadOmen}
			}
			slices.Sort(removed)
			slices.Sort(wantRemoved)
			if !slices.Equal(removed, wantRemoved) {
				t.Errorf("removed effects = %v, want %v", removed, wantRemoved)
			}
		})
	}
}

func TestSuspiciousStewGivesItsEffectsAndLeavesABowl(t *testing.T) {
	t.Parallel()
	srv, c, id := servertest.JoinAs(t, servertest.SurvivalTestConfig(), "Stewer")
	c.Settle()

	night := registry.MustMobEffect("minecraft:night_vision")
	stew := protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:suspicious_stew"), Count: 1}
	stew.SetSuspiciousStewEffects([]protocol.StewEffect{{Effect: night, Duration: 100}})
	srv.GiveSlot("Stewer", player.HotbarSlot, stew)
	c.UseItemRightClicks(protocol.HandMain)

	got := c.AwaitMobEffect()
	if got.Entity != id || got.Effect != night || got.Duration != 100 || got.Amplifier != 0 {
		t.Errorf("effect = %+v, want night_vision 100 ticks on %d", got, id)
	}
	servertest.WaitFor(t, srv, 100, func() bool {
		return srv.InventorySlot("Stewer", player.HotbarSlot).ItemID == servertest.MustItemID(t, "minecraft:bowl")
	})
}

func TestChorusFruitMovesThePlayerWithinSixteenBlocks(t *testing.T) {
	t.Parallel()
	srv, c, id := servertest.JoinAs(t, servertest.SurvivalTestConfig(), "Chorus")
	c.Settle()

	var mu sync.Mutex
	var eaten [][3]float64
	srv.ObserveGameEvents(func(event string, x, y, z float64) {
		if event == "minecraft:eat" {
			mu.Lock()
			eaten = append(eaten, [3]float64{x, y, z})
			mu.Unlock()
		}
	})
	before := srv.PlayerPosition("Chorus")
	srv.GiveSlot("Chorus", player.HotbarSlot, protocol.ItemStack{ItemID: servertest.MustItemID(t, "minecraft:chorus_fruit"), Count: 1})
	c.UseItemRightClicks(protocol.HandMain)

	c.AwaitEntityEvent(id, protocol.EntityStatusUseFinished)
	c.AwaitEntityEvent(id, protocol.EntityStatusTeleportParticles)
	after := srv.PlayerPosition("Chorus")
	if after.X == before.X && after.Z == before.Z {
		t.Fatalf("position unchanged at %v", after)
	}
	if math.Abs(after.X-before.X) > 8 || math.Abs(after.Z-before.Z) > 8 || math.Abs(after.Y-before.Y) > 8 {
		t.Errorf("moved from %v to %v, beyond half the 16 diameter", before, after)
	}
	// The meal is heard where the teleport lands. vanilla: Consumable.onConsume
	mu.Lock()
	defer mu.Unlock()
	if want := [3]float64{after.X, after.Y, after.Z}; len(eaten) != 1 || eaten[0] != want {
		t.Errorf("eat events at %v, want one at %v", eaten, want)
	}
}
