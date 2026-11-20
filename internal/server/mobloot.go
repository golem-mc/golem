package server

import (
	"github.com/google/uuid"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/advancement"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// The mobs' half of loot pickup. Reach, which piece is better and where a
// stack goes (Mob.pickUpItem's questions) are the entity package's; this pass owns the item bodies,
// locks and packets: wanting under the mob lock, claims under the item
// lock, never both at once, and packets with neither held.

// mobLootItem is an item entity with the mob collection's view of it.
type mobLootItem struct {
	it   *itemEntity
	loot entity.LootItem
}

// mobLootTake is a decided pickup: the item claimed from, the take, the
// stack as decided (the copy vanilla's pickUpItem equips) and, once
// claimed, whether it emptied the item.
type mobLootTake struct {
	w        *world.World
	it       *itemEntity
	take     entity.LootTake
	stack    protocol.ItemStack
	consumed bool
	// mobUUID is the taking mob's UUID, for its equip game event.
	mobUUID uuid.UUID
	// leads is the raider that takes a captain's banner, set to lead its wave
	// once it wears it.
	leads *raiderRef
	// thrower is the claimed item's thrower, uuid.Nil for none.
	thrower uuid.UUID
}

// mobEquipped is a mob's slot change from a take, for its equip sound and
// game event once the locks are released.
// vanilla: LivingEntity.onEquipItem
type mobEquipped struct {
	w          *world.World
	uuid       uuid.UUID
	fx         equipFx
	item       registry.EquippableItem
	equippable bool // the new stack has an equippable component
}

// mobThrownPickup is a mob's take of a thrown item, for the thrower's
// thrown_item_picked_up_by_entity.
type mobThrownPickup struct {
	w         *world.World
	thrower   uuid.UUID
	item      int32
	collector advancement.Entity
}

// foxSpitPickupDelay is the pickup delay of a stack a fox spat out.
// vanilla: Fox.spitOutItem
const foxSpitPickupDelay = 40

// mobLootDropped is a piece a take displaced, dropped where the mob stands.
type mobLootDropped struct {
	w       *world.World
	stack   protocol.ItemStack
	x, y, z float64
	// spat marks a fox's spat stack: it lands a block ahead of the mouth, out
	// of reach for a second, with the spit sound.
	spat       bool
	spatSilent bool
	// mouthX, mouthY, mouthZ is the fox itself, where the spit is heard.
	mouthX, mouthY, mouthZ float64
}

// tickMobLoot runs the item pickups of mobs with CanPickUpLoot, where
// mob_griefing allows, keeping better pieces. It runs before the advance so
// the sweep's equipment diff tells watchers.
// vanilla: Mob.aiStep, pickUpItem
func (s *Server) tickMobLoot() {
	// Only worlds with a looting mob are worth the item lock.
	var picking []*world.World
	grabbing := map[*world.World]bool{}
	s.lockTick(&s.mobsMu)
	for _, dim := range s.allDims() {
		for _, m := range dim.mobs.herd {
			if m.Alive() && m.BannerGrabbing() {
				grabbing[dim.w] = true
			}
			if m.Alive() && m.LootPickupEnabled() {
				picking = append(picking, dim.w)
				break
			}
		}
	}
	s.unlockTick(&s.mobsMu)
	if len(picking) == 0 && len(grabbing) == 0 {
		return
	}
	lootWorlds := make(map[*world.World]bool, len(picking))
	groundBy := make(map[*world.World][]mobLootItem, len(picking))
	for _, w := range picking {
		lootWorlds[w] = true
	}
	// The banner goal's pickup is not a looting pass, so mob_griefing does not
	// gate it.
	// vanilla: Raider.ObtainRaidLeaderBannerGoal.tick
	for w := range grabbing {
		lootWorlds[w] = true
	}

	// Under the item lock, snapshot the claimable items: due, not empty, not
	// Dead.
	s.lockTick(&s.itemsMu)
	for _, it := range s.items {
		if it.item.Dead || it.item.Stack.Empty() || !it.item.PickupDue() {
			continue
		}
		if !lootWorlds[it.w] {
			continue
		}
		groundBy[it.w] = append(groundBy[it.w], mobLootItem{it: it, loot: entity.LootItem{
			X: it.item.X, Y: it.item.Y, Z: it.item.Z, Stack: it.item.Stack,
		}})
	}
	s.unlockTick(&s.itemsMu)

	// Decide under the mob lock without equipping, so an item claimed away in
	// between equips nobody.
	var takes []mobLootTake
	s.lockTick(&s.mobsMu)
	for _, dim := range s.allDims() {
		ground := groundBy[dim.w]
		if len(ground) == 0 {
			continue
		}
		view := make([]entity.LootItem, len(ground))
		for i, l := range ground {
			view[i] = l.loot
		}
		dim.mobs.herd.SenseWantedItems(dim.w, view)
		// Captain claims precede ordinary banner equipment, even without a goal.
		// vanilla: Raider.pickUpItem
		mobGriefing := dim.w.BoolRule(world.RuleMobGriefing)
		takes = append(takes, s.bannerTakes(dim, ground, mobGriefing)...)
		if mobGriefing {
			for _, take := range dim.mobs.herd.TakeLoot(view) {
				takes = append(takes, mobLootTake{
					w:     dim.w,
					it:    ground[take.Item].it,
					take:  take,
					stack: ground[take.Item].loot.Stack,
				})
				if ent := dim.mobs.book[take.Mob]; ent != nil {
					takes[len(takes)-1].mobUUID = ent.uuid
				}
			}
		}
	}
	s.unlockTick(&s.mobsMu)
	if len(takes) == 0 {
		return
	}

	// Claim under the item lock: take the count, kill an emptied item. A stack
	// an earlier take left short, or an item gone since, claims nothing.
	var claimed []mobLootTake
	var shrunk []itemMetadataUpdate
	s.lockTick(&s.itemsMu)
	for _, t := range takes {
		if t.it.item.Dead || t.it.item.Stack.Count < t.take.Count {
			continue
		}
		t.it.item.Stack.Count -= t.take.Count
		if empty := t.it.item.Stack.Empty(); !empty {
			update := itemMetadataUpdate{it: t.it, metadata: itemMetadataPayload(t.it)}
			for id := range t.it.tracked.all() {
				update.watchers = append(update.watchers, id)
			}
			shrunk = append(shrunk, update)
		} else {
			t.it.item.Dead = true
			t.consumed = true
		}
		t.thrower = t.it.thrower
		claimed = append(claimed, t)
	}
	s.unlockTick(&s.itemsMu)

	// Equip under the mob lock again; a mob that died meanwhile takes nothing.
	// Displaced pieces are dropped once both locks are released.
	var dropped []mobLootDropped
	var sounds []mobSend
	var equips []mobEquipped
	s.lockTick(&s.mobsMu)
	var thrown []mobThrownPickup
	for _, t := range claimed {
		if t.thrower != uuid.Nil && t.take.Mob.Alive() {
			thrown = append(thrown, mobThrownPickup{w: t.w, thrower: t.thrower, item: t.stack.ItemID,
				collector: advancement.Entity{Type: t.take.Mob.Type.Name(), Baby: t.take.Mob.IsBaby(), Loot: s.mobLoot(s.herdOf(t.w).book[t.take.Mob])}})
		}
		m := t.take.Mob
		before, worn := m.LootSlotStack(t.take.Slot)
		stack := m.TookLoot(t.take, t.stack)
		if after, _ := m.LootSlotStack(t.take.Slot); worn && m.Alive() && !before.Same(after) {
			ev := mobEquipped{w: t.w, uuid: t.mobUUID, fx: equipFx{x: m.X, y: m.Y, z: m.Z, category: mobSoundCategory(m.Type),
				silent: m.Silent, sound: m.EquipSoundOverride(t.take.Slot)}}
			if eq, ok := registry.StackEquippable(after); ok && !after.Empty() {
				ev.item, ev.equippable = eq, true
				ev.fx.skipSound = eq.Slot != t.take.Slot
			}
			ev.fx.event = true
			equips = append(equips, ev)
		}
		if !stack.Empty() {
			d := mobLootDropped{w: t.w, stack: stack, x: t.take.Mob.X, y: t.take.Mob.Y, z: t.take.Mob.Z}
			if t.take.Mob.Type == entity.TypeFox {
				lx, _, lz := entity.LookVector(t.take.Mob.Yaw, t.take.Mob.Pitch)
				d.mouthX, d.mouthY, d.mouthZ = d.x, d.y, d.z
				d.x, d.y, d.z, d.spat, d.spatSilent = d.x+lx, d.y+1, d.z+lz, true, t.take.Mob.Silent
			}
			dropped = append(dropped, d)
		}
	}
	for _, t := range claimed {
		if t.take.Mob.Type == entity.TypeSulfurCube {
			if sound, ok := mobVoiceIDSend(t.w, t.take.Mob, registry.SoundEntitySulfurCubeAbsorb, 1, 1); ok {
				sounds = append(sounds, sound)
			}
		}
	}
	var leaders []*raiderRef
	for _, t := range claimed {
		if t.leads != nil && t.take.Mob.Alive() {
			leaders = append(leaders, t.leads)
		}
	}
	s.unlockTick(&s.mobsMu)
	s.sendMobSends(sounds)
	for _, e := range equips {
		e.fx.ctx = rules.GameEventContext{Mob: e.uuid}
		if e.equippable {
			s.equipFeedback(e.w, e.item, e.fx)
		} else {
			s.gameEvent(e.w, rules.GameEventUnequip, e.fx.x, e.fx.y, e.fx.z, e.fx.ctx)
		}
	}
	for _, ref := range leaders {
		s.raiderTookBanner(ref)
	}

	// Send the pickup animation, emptied items' removal and survivors' smaller
	// stacks; Set Equipment goes with the advance's equipment diff.
	removals := make(map[*world.World][]int32)
	for _, t := range claimed {
		s.lockTick(&s.itemsMu)
		watchers := trackedWatchers(t.it.tracked)
		s.unlockTick(&s.itemsMu)
		s.tellPickup(t.w, watchers, nil, pickupItemPayload(t.it.item.ID, t.take.Mob.ID, t.take.Count))
		if t.consumed {
			removals[t.w] = append(removals[t.w], t.it.item.ID)
			s.entityDestroyed(t.it.uuid)
		}
	}
	for w, ids := range removals {
		s.broadcastWorld(w, protocol.IDPlayRemoveEntities, removeEntitiesPayload(ids))
	}
	s.sendItemMetadata(shrunk)
	for _, p := range thrown {
		s.thrownItemPickedUpAdvancements(p.w, p.thrower, p.item, nil, p.collector)
	}

	// Displaced pieces drop at the body with ItemEntity's toss; the pickup
	// delay stops the mob taking them straight back.
	// vanilla: Entity.spawnAtLocation
	for _, d := range dropped {
		vx, vy, vz := entity.PopVelocity(s.popRng)
		delay := entity.BreakPickupDelay
		if d.spat {
			delay = foxSpitPickupDelay
			if !d.spatSilent {
				s.broadcastSoundID(d.w, registry.SoundEntityFoxSpit, mobSoundCategory(entity.TypeFox), d.mouthX, d.mouthY, d.mouthZ, 1, 1)
			}
		}
		s.spawnItem(d.w, d.stack, d.x, d.y, d.z, vx, vy, vz, delay)
	}
}
