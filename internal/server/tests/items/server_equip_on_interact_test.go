package items

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// bareZombie stands the client beside a still zombie with nothing on its
// head and returns the zombie's entity ID.
func bareZombie(t *testing.T, srv *server.Server, w *servertest.WindowedClient) int32 {
	t.Helper()
	w.MoveToGround(2.5, 64, 5.5, true)
	w.Settle()
	srv.SpawnMob("minecraft:zombie", 4.5, 64, 5.5)
	id := w.ExpectMobSpawn(protocol.EntityTypeZombie).EntityID
	srv.SetMobNoAI(id)
	srv.SetMobEquipment(id, "head", protocol.ItemStack{})
	w.Settle()
	return id
}

// equipmentUpdates runs n ticks and returns the stacks sent for the mob's wire slot.
func equipmentUpdates(t *testing.T, srv *server.Server, w *servertest.WindowedClient, id int32, n int, wireSlot uint8) []protocol.ItemStack {
	t.Helper()
	var updates []protocol.ItemStack
	w.ReadTicks(srv, n, func(pid int32, data []byte) bool {
		if pid != protocol.IDPlayEntityEquipment {
			return false
		}
		r := protocol.NewReader(data)
		if r.ReadVarInt() != id {
			return false
		}
		for {
			slot := r.ReadU8()
			stack, _ := r.ReadSlot()
			if slot&0x7F == wireSlot {
				updates = append(updates, stack)
			}
			if slot&0x80 == 0 || r.Err() != nil {
				break
			}
		}
		return false
	})
	return updates
}

// TestEquipOnInteractPutsTheHelmetOnAZombie: a helmet whose own equippable
// has equip_on_interact goes on a zombie's bare head when used on it, and
// the hand is emptied.
// vanilla: ItemStack.interactLivingEntity, Equippable.equipOnTarget
func TestEquipOnInteractPutsTheHelmetOnAZombie(t *testing.T) {
	t.Parallel()
	srv, w := givenWindowed(t, servertest.SurvivalTestConfig(), `diamond_helmet[equippable={slot:"head",equip_on_interact:true}]`)
	id := bareZombie(t, srv, w)

	w.UseOn(id)
	heads := equipmentUpdates(t, srv, w, id, 2, 5)
	helmet := servertest.MustItemID(t, "minecraft:diamond_helmet")
	if len(heads) == 0 || heads[len(heads)-1].ItemID != helmet {
		t.Errorf("head updates %+v, want the diamond helmet", heads)
	}
	if got := srv.MobEquipment(id, "head"); got.ItemID != helmet || got.Count != 1 {
		t.Errorf("zombie head = %+v, want one diamond helmet", got)
	}
	expectSlot(t, srv, player.HotbarSlot, "")
}

// TestAPlainHelmetUsedOnAZombieStaysInHand: without equip_on_interact the
// helmet is not put on.
// vanilla: ItemStack.interactLivingEntity
func TestAPlainHelmetUsedOnAZombieStaysInHand(t *testing.T) {
	t.Parallel()
	srv, w := givenWindowed(t, servertest.SurvivalTestConfig(), `diamond_helmet`)
	id := bareZombie(t, srv, w)

	w.UseOn(id)
	if heads := equipmentUpdates(t, srv, w, id, 2, 5); len(heads) != 0 {
		t.Errorf("head updates %+v, want none", heads)
	}
	if got := srv.MobEquipment(id, "head"); !got.Empty() {
		t.Errorf("zombie head = %+v, want bare", got)
	}
	expectSlot(t, srv, player.HotbarSlot, "minecraft:diamond_helmet")
}

// vanilla: Player.interactOn, Equippable.equipOnTarget, SulfurCube.isEquippableInSlot
func TestEquipOnInteractAfterMobPass(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, kind, item, slot                            string
		baby, tame, carrying, consumed, refused, creative bool
	}{
		{name: "cat", kind: "cat"},
		{name: "wolf", kind: "wolf"},
		{name: "ocelot", kind: "ocelot"},
		{name: "parrot", kind: "parrot"},
		{name: "allay carrying an item", kind: "allay", carrying: true},
		{name: "baby horse", kind: "horse", baby: true, tame: true},
		{name: "baby happy ghast", kind: "happy_ghast", baby: true},
		{name: "pig", kind: "pig"},
		{name: "zombie saddle", kind: "zombie", slot: "saddle"},
		{name: "creative cat", kind: "cat", creative: true},
		{name: "adult horse", kind: "horse", tame: true},
		{name: "adult happy ghast", kind: "happy_ghast"},
		{name: "nautilus", kind: "nautilus"},
		{name: "mooshroom", kind: "mooshroom"},
		{name: "full health iron golem", kind: "iron_golem", item: "iron_ingot"},
		{name: "baby sulfur cube refuses helmet body", kind: "sulfur_cube", slot: "body", baby: true, refused: true},
		{name: "baby pig refuses saddle", kind: "pig", slot: "saddle", baby: true, refused: true},
		// SulfurCube's body override ignores both age and allowed_entities.
		{name: "baby sulfur cube", kind: "sulfur_cube", item: "stone", slot: "body", baby: true},
		{name: "cat owner sits", kind: "cat", tame: true, consumed: true},
		{name: "allay takes item", kind: "allay", consumed: true},
		{name: "villager talks", kind: "villager", consumed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			item, slot := tc.item, tc.slot
			if item == "" {
				item = "diamond_helmet"
			}
			if slot == "" {
				slot = "head"
			}
			allowed := ""
			if tc.kind == "sulfur_cube" {
				allowed = `,allowed_entities:["minecraft:zombie"]`
			}
			cfg := servertest.SurvivalTestConfig()
			if tc.creative {
				cfg.GameMode = "creative"
			}
			srv, w := givenWindowed(t, cfg, item+`[equippable={slot:"`+slot+`",equip_on_interact:true`+allowed+`}]`)
			w.MoveToGround(2.5, 64, 5.5, true)
			w.Settle()
			id := srv.SpawnMob("minecraft:"+tc.kind, 4.5, 64, 5.5)
			srv.SetMobNoAI(id)
			srv.SetMobBaby(id, tc.baby)
			if tc.tame {
				if tc.kind == "horse" {
					srv.TameHorse(id)
				} else {
					srv.TameMobFor(id, "Testificate")
				}
			}
			if tc.carrying {
				srv.SetMobHand(id, protocol.ItemStack{ItemID: int32(registry.ItemStick), Count: 1})
			}
			w.Settle()
			w.UseOn(id)
			wireSlot := uint8(5) // vanilla: EquipmentSlot ordinal in STREAM_CODEC
			if slot == "body" {
				wireSlot = 6
			} else if slot == "saddle" {
				wireSlot = 7
			}
			updates := equipmentUpdates(t, srv, w, id, 2, wireSlot)
			got := srv.MobEquipment(id, slot)
			if tc.consumed || tc.refused {
				if !got.Empty() {
					t.Errorf("mob equipped %s despite consuming or refusing the action: %+v", slot, got)
				}
				if tc.kind == "allay" {
					expectSlot(t, srv, player.HotbarSlot, "")
				} else {
					expectSlot(t, srv, player.HotbarSlot, "minecraft:"+item)
				}
				return
			}
			if got.ItemID != servertest.MustItemID(t, "minecraft:"+item) || got.Count != 1 {
				t.Errorf("%s %s = %+v, want one %s", tc.kind, slot, got, item)
			}
			if len(updates) == 0 || updates[len(updates)-1].ItemID != got.ItemID {
				t.Errorf("equipment updates = %+v, want %s in wire slot %d", updates, item, wireSlot)
			}
			if tc.creative {
				expectSlot(t, srv, player.HotbarSlot, "minecraft:"+item)
			} else {
				expectSlot(t, srv, player.HotbarSlot, "")
			}
		})
	}
}

// vanilla: Entity.interact's leash removal consumes the first click.
func TestEquipOnInteractWaitsForLeashRelease(t *testing.T) {
	t.Parallel()
	srv, w := givenWindowed(t, servertest.SurvivalTestConfig(), `diamond_helmet[equippable={slot:"head",equip_on_interact:true}]`)
	w.MoveToGround(2.5, 64, 5.5, true)
	w.Settle()
	id := srv.SpawnMob("minecraft:cat", 4.5, 64, 5.5)
	srv.SetMobNoAI(id)
	srv.SetMobLeashHolder(id, w.PlayerEntityID)
	w.Settle()
	w.UseOn(id)
	w.Settle()
	if got := srv.MobEquipment(id, "head"); !got.Empty() {
		t.Fatalf("untie also equipped helmet: %+v", got)
	}
	expectSlot(t, srv, player.HotbarSlot, "minecraft:diamond_helmet")
	w.UseOn(id)
	updates := equipmentUpdates(t, srv, w, id, 2, 5)
	if len(updates) == 0 || updates[len(updates)-1].ItemID != int32(registry.ItemDiamondHelmet) {
		t.Errorf("head updates = %+v, want diamond helmet", updates)
	}
	expectSlot(t, srv, player.HotbarSlot, "")
}
