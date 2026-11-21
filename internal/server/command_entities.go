package server

import (
	"bytes"
	"cmp"
	"encoding/binary"
	"log/slog"
	"math"
	"reflect"
	"slices"
	"strings"
	"sync"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/effect"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/scoreboard"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// entitySnapshot is a non-player entity as a selector sees it, read under
// its kind's lock so a tick cannot leave it half-updated. Kill and Teleport
// act on ref, the live *mobEntity, *itemEntity, etc. Players are their own
// command.Entity (command_targets.go).
type entitySnapshot struct {
	effectCause effect.Cause
	srv         *Server
	mu          *sync.Mutex
	liveTags    *nbt.StringSet
	ops         *entityOps
	ref         any
	id          int32
	// alive is the liveness read with the rest of the snapshot, so the alive
	// filter sees the same moment.
	alive bool
	uuid  uuid.UUID
	typ   string
	// baseName overrides the kind's name with a custom or state-dependent name.
	// vanilla: Entity.getName, Villager.getTypeName, FallingBlockEntity.getTypeName
	baseName *protocol.Text
	nameOnce sync.Once
	// name is built on first use; a pointer so a snapshot nobody names stays
	// small.
	name  *protocol.Text
	plain string
	// An unnamed item entity is named by its stack's item, settled on first
	// use. vanilla: ItemEntity.getName
	stackItem int32
	byStack   bool
	plainOnce sync.Once
	dim       string
	pos       player.Position
	box       [6]float64
	// tags are the scoreboard tags, copied with the snapshot so later /tag
	// changes do not show through.
	tags []string
}

// DisplayName is vanilla's Entity.getDisplayName.
func (e *entitySnapshot) DisplayName() protocol.Text {
	e.nameOnce.Do(func() {
		name := entityDisplayName(e.srv.scoreboard, e.typ, e.uuid, e.baseName)
		if e.byStack {
			display, _ := registry.ItemTranslationKey(e.stackItem)
			name.Translate = display
			name.HoverEntity.Name.Translate = display
		}
		e.name = &name
	})
	return *e.name
}

// PlainName is Entity.getPlainTextName, matched by the name option.
func (e *entitySnapshot) PlainName() string {
	e.plainOnce.Do(func() {
		if e.byStack {
			name, _ := registry.ItemName(e.stackItem)
			e.plain = itemEnglishName(name)
		}
	})
	return e.plain
}

// EntityType is the minecraft:entity_type name.
func (e *entitySnapshot) EntityType() string { return e.typ }

func (e *entitySnapshot) UUID() uuid.UUID { return e.uuid }

// EntityID is the protocol entity ID a packet names this entity by.
func (e *entitySnapshot) EntityID() int32 { return e.id }

func (e *entitySnapshot) DimensionName() string { return e.dim }

func (e *entitySnapshot) Position() player.Position { return e.pos }

// EyePosition is the position itself.
func (e *entitySnapshot) EyePosition() player.Position { return e.pos }

// Box is what a selector's dx, dy, dz volume is crossed with.
func (e *entitySnapshot) Box() (minX, minY, minZ, maxX, maxY, maxZ float64) {
	return e.box[0], e.box[1], e.box[2], e.box[3], e.box[4], e.box[5]
}

// Alive is the captured liveness: a mob with health left and not removed,
// anything else not Dead.
func (e *entitySnapshot) Alive() bool { return e.alive }

// Tags are the snapshot's scoreboard tags. vanilla: Entity.entityTags
func (e *entitySnapshot) Tags() []string { return e.tags }

// AddTag adds a scoreboard tag to the live body. vanilla: Entity.addTag
func (e *entitySnapshot) AddTag(tag string) bool {
	return e.changeEntityTags(func(tags *nbt.StringSet) bool { return player.AddTag(tags, tag) })
}

// RemoveTag removes a scoreboard tag from the live body.
// vanilla: Entity.removeTag
func (e *entitySnapshot) RemoveTag(tag string) bool {
	return e.changeEntityTags(func(tags *nbt.StringSet) bool { return tags.Remove(tag) })
}

// changeEntityTags changes the live body's tags under its kind's lock.
func (e *entitySnapshot) changeEntityTags(change func(*nbt.StringSet) bool) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return change(e.liveTags)
}

// DataTag is the body's saved compound under its kind's lock, as /data and
// the nbt option read it: saveWithoutId, the entity file form without
// "id". A kind with no data here returns false.
// vanilla: Entity.saveWithoutId, EntityDataAccessor.getData
func (e *entitySnapshot) DataTag() (nbt.CompoundTag, bool) {
	tag, w, riders := e.tagAndRiders()
	e.srv.putVehicleRiders(tag, w, riders)
	return tag, true
}

// tagAndRiders builds the saved compound and collects a boat's or cart's mob
// riders under the kind's lock, released even if the kind's tag panics.
func (e *entitySnapshot) tagAndRiders() (tag nbt.CompoundTag, w *world.World, riders []*mobEntity) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if body, ok := e.ref.(*mobEntity); ok {
		// The mob lock covers the whole ride tree; boats and carts collect
		// their riders below to avoid holding their lock with the mob lock.
		// Only passengers use saveAsPassenger; inserting and deleting the
		// root's id can resize its compound even though the key is absent.
		return mobTagAs(body, e.srv.herdOf(body.w).book, ""), nil, nil
	}
	tag = e.ops.tag(e.ref)
	switch body := e.ref.(type) {
	case *boatEntity:
		w = body.w
		for _, p := range body.passengers {
			if p.ent != nil {
				riders = append(riders, p.ent)
			}
		}
	case *minecartEntity:
		w, riders = body.w, slices.Clone(body.mobs)
	}
	return tag, w, riders
}

// putVehicleRiders nests a boat's or cart's mob riders under Passengers,
// after the unmodelled riders already there; the vehicle's lock is released
// first, as the boat and mob locks are never held together.
// vanilla: Entity.saveWithoutId
func (s *Server) putVehicleRiders(tag nbt.CompoundTag, w *world.World, riders []*mobEntity) {
	if len(riders) == 0 {
		return
	}
	carried, _ := nbt.Get[nbt.List](tag.Entries(), "Passengers")
	list := nbt.List{Elem: nbt.TypeCompound, Items: slices.Clone(carried.Items)}
	s.mobsMu.Lock()
	book := s.herdOf(w).book
	for _, m := range riders {
		list.Items = append(list.Items, mobTagIn(m, book))
	}
	s.mobsMu.Unlock()
	tag.Set("Passengers", list)
}

// mobMetadataTurnedFor advances each watcher's baseline with the command's
// delta, including dependent packets, so the tick does not repeat it.
func mobMetadataTurnedFor(body *mobEntity) []turnedMobMetadata {
	now := body.metadataState()
	deltas := mobMetadataDeltas{ent: body, now: now}
	var turned []turnedMobMetadata
	for id, was := range body.tracked.all() {
		packets := deltas.from(was.own.meta)
		was.own.meta = now
		if len(packets) > 0 {
			turned = append(turned, turnedMobMetadata{watchers: []uuid.UUID{id}, packets: packets})
		}
	}
	return turned
}

type turnedMobMetadata struct {
	watchers []uuid.UUID
	packets  []outbound
}

// SetDataTag loads a merged compound onto the live body, setting named
// fields and leaving the rest; false for a kind with no writable state. It
// writes under the kind's lock; an arrow or a cart leaves the change to its
// next tracker tick, the other kinds tell the changed metadata to the body's
// viewers after.
// vanilla: EntityDataAccessor.setData, ServerEntity.sendDirtyEntityData
func (e *entitySnapshot) SetDataTag(tag nbt.CompoundTag) bool {
	data := tag.Entries()
	switch body := e.ref.(type) {
	case *mobEntity:
		e.srv.mobsMu.Lock()
		beforeFields := mobTagFields(body)
		held, waited := body.mob.LeashHolderID, body.waitingLeash()
		applyMobTag(body.mob, body, data)
		e.srv.herdOf(body.w).pistonGrid.built = false
		// A mob already waiting is on the list once; a second entry would
		// count its grace twice a tick.
		if !waited && body.waitingLeash() {
			h := e.srv.herdOf(body.w)
			h.leashWaits = append(h.leashWaits, body)
		}
		letGo := held != 0 && body.mob.LeashHolderID == 0
		fields := metadataChanges(body.mob.ID, beforeFields, mobTagFields(body))
		watchers := trackedWatchers(body.tracked)
		turned := mobMetadataTurnedFor(body)
		w := body.w
		e.srv.mobsMu.Unlock()
		for _, t := range turned {
			e.srv.tellWatchers(w, t.watchers, t.packets)
		}
		e.srv.sendEntityMetadata(w, watchers, fields)
		e.srv.resolveVexOwners([]*mobEntity{body})
		if letGo {
			e.srv.leashLetGo(w, body.mob.ID, held)
		}
		return true
	case *itemEntity:
		e.srv.itemsMu.Lock()
		before := itemFields(body)
		applyItemTag(body, data)
		itemTracker.invalidatePistonGrid(e.srv, body)
		e.srv.tellItemWriteLocked(body, before)
		return true
	case *orbEntity:
		e.srv.orbsMu.Lock()
		before := orbFields(body)
		applyOrbTag(body, data)
		orbTracker.invalidatePistonGrid(e.srv, body)
		payload := metadataChanges(body.orb.ID, before, orbFields(body))
		watchers := trackedWatchers(body.tracked)
		w := body.w
		e.srv.orbsMu.Unlock()
		e.srv.sendEntityMetadata(w, watchers, payload)
		return true
	case *tntEntity:
		e.srv.tntMu.Lock()
		before := tntMetadataStateOf(body)
		applyTNTTag(body, data)
		tntTracker.invalidatePistonGrid(e.srv, body)
		now := tntMetadataStateOf(body)
		payload := tntMetadataTable(body.tnt.ID, &now, &before, false)
		watchers := trackedWatchers(body.tracked)
		w := body.w
		e.srv.tntMu.Unlock()
		e.srv.sendEntityMetadata(w, watchers, payload)
		return true
	case *endCrystalEntity:
		e.srv.endCrystalsMu.Lock()
		applyEndCrystalTag(body, data)
		endCrystalTracker.invalidatePistonGrid(e.srv, body)
		// Crystals have no movement schedule; wake the next tracking pass.
		body.teleported = true
		payload := endCrystalMetadata(body, true)
		watchers := trackedWatchers(body.tracked)
		w := body.w
		e.srv.endCrystalsMu.Unlock()
		e.srv.sendEntityMetadata(w, watchers, payload)
		return true
	case *arrowEntity:
		// The tracker tells the change on the next tick.
		e.srv.arrowsMu.Lock()
		applyArrowTag(body, data)
		arrowTracker.invalidatePistonGrid(e.srv, body)
		e.srv.arrowsMu.Unlock()
		return true
	case *boatEntity:
		e.srv.boatsMu.Lock()
		before := boatFields(body)
		held := body.holder
		applyBoatTag(body, data)
		boatTracker.invalidatePistonGrid(e.srv, body)
		payload := metadataChanges(body.boat.ID, before, boatFields(body))
		watchers := trackedWatchers(body.tracked)
		w, id, leashed := body.w, body.boat.ID, body.leash.set()
		e.srv.boatsMu.Unlock()
		e.srv.sendEntityMetadata(w, watchers, payload)
		if held != 0 && !leashed {
			e.srv.leashLetGo(w, id, held)
		}
		return true
	case *cloudEntity:
		e.srv.arrowsMu.Lock()
		before, name := body.cloudState(), body.custom
		applyCloudTag(body, data)
		body.teleported = true
		payload := cloudMetadataTable(body, before, true, name)
		watchers, w := trackedWatchers(body.tracked), body.w
		e.baseName, e.nameOnce = nil, sync.Once{}
		e.setCustomName(body.custom.tag())
		e.srv.arrowsMu.Unlock()
		e.srv.sendEntityMetadata(w, watchers, payload)
		return true
	case *fireworkEntity:
		owner := e.srv.resolveOwner(entityUUIDFieldOf(data, "Owner"))
		e.srv.fireworksMu.Lock()
		before := fireworkMetadataStateOf(body)
		applyFireworkTag(body, data)
		body.ownerID, body.rocket.OwnerID = owner, owner
		body.teleported = true
		payload := fireworkMetadataTable(body, &before)
		watchers, w := trackedWatchers(body.tracked), body.w
		e.baseName, e.nameOnce = nil, sync.Once{}
		e.setCustomName(body.custom.tag())
		e.srv.fireworksMu.Unlock()
		e.srv.sendEntityMetadata(w, watchers, payload)
		return true
	case *paintingEntity:
		e.srv.paintingsMu.Lock()
		before := paintingMetadataStateOf(body)
		applyPaintingTag(body, data)
		body.teleported = true
		payload := paintingMetadataTable(body, &before)
		watchers, w := trackedWatchers(body.tracked), body.w
		e.baseName, e.nameOnce = nil, sync.Once{}
		e.setCustomName(body.custom.tag())
		e.srv.paintingsMu.Unlock()
		e.srv.sendEntityMetadata(w, watchers, payload)
		return true
	case *frameEntity:
		e.srv.framesMu.Lock()
		before, rot := body.item, body.rotation
		applyFrameFields(body, data)
		if dir := frameFacingOf(data); dir != body.dir {
			e.srv.rehangFrameLocked(body, body.x, body.y, body.z, dir)
		}
		payload := frameMetadataPayload(body, true)
		tell := frameTellOf(body)
		changed := rot != body.rotation || !reflect.DeepEqual(before, body.item)
		watchers := trackedWatchers(body.tracked)
		e.srv.framesMu.Unlock()
		e.srv.sendEntityMetadata(tell.w, watchers, payload)
		if changed {
			e.srv.frameSignalChanged(tell)
		}
		return true
	case *minecartEntity:
		// The tracker tells the change on the next tick.
		e.srv.minecartsMu.Lock()
		applyMinecartTag(body, data)
		minecartTracker.invalidatePistonGrid(e.srv, body)
		e.srv.minecartsMu.Unlock()
		return true
	case *fallingEntity:
		e.srv.fallingMu.Lock()
		before := fallingFields(body)
		applyFallingTag(body, data)
		fallingTracker.invalidatePistonGrid(e.srv, body)
		payload := metadataChanges(body.fb.ID, before, fallingFields(body))
		watchers := trackedWatchers(body.tracked)
		w := body.w
		e.srv.fallingMu.Unlock()
		e.srv.sendEntityMetadata(w, watchers, payload)
		return true
	case *armorStandEntity:
		e.srv.armorStandsMu.Lock()
		before := armorStandEquipment(body)
		applyArmorStandTag(body, data)
		standTracker.invalidatePistonGrid(e.srv, body)
		payload := armorStandMetadataPayload(body, true)
		markArmorStandMetadata(body)
		equipment := armorStandEquipment(body)
		watchers := trackedWatchers(body.tracked)
		w := body.w
		e.srv.armorStandsMu.Unlock()
		// A merge can equip a stand, so the changed slots follow the metadata, as
		// LivingEntity.detectEquipmentUpdates sends them.
		packets := []outbound{{id: protocol.IDPlayEntityMetadata, data: payload}}
		if !bytes.Equal(before, equipment) {
			packets = append(packets, outbound{id: protocol.IDPlayEntityEquipment, data: equipment})
		}
		e.srv.tellWatchers(w, watchers, packets)
		return true
	case *mannequinEntity:
		e.srv.setMannequinData(body, data)
		return true
	case *fixtureEntity:
		e.srv.setFixtureData(body, data)
		return true
	case *fangsEntity:
		owner := e.srv.resolveOwner(entityUUIDFieldOf(data, "Owner"))
		e.srv.fangs.mu.Lock()
		before := body.fields()
		applyFangsTag(body, data, owner)
		e.baseName, e.nameOnce = nil, sync.Once{}
		e.setCustomName(body.custom.tag())
		body.teleported = true
		payload := metadataChanges(body.fangs.ID, before, body.fields())
		watchers, w := trackedWatchers(body.tracked), body.w
		e.srv.fangs.mu.Unlock()
		e.srv.sendEntityMetadata(w, watchers, payload)
		return true
	case *eyeEntity:
		e.srv.eyesMu.Lock()
		before := eyeFields(body)
		vx, vy, vz := body.eye.VX, body.eye.VY, body.eye.VZ
		applyEyeTag(body, data)
		e.baseName, e.nameOnce = nil, sync.Once{}
		e.setCustomName(body.custom.tag())
		body.teleported = true
		payload := metadataChanges(body.eye.ID, before, eyeFields(body))
		var motion []byte
		if vx != body.eye.VX || vy != body.eye.VY || vz != body.eye.VZ {
			motion = entityVelocityPayload(body.eye.ID, body.eye.VX, body.eye.VY, body.eye.VZ)
		}
		watchers, w := trackedWatchers(body.tracked), body.w
		e.srv.eyesMu.Unlock()
		e.srv.sendEntityMetadata(w, watchers, payload)
		if motion != nil {
			e.srv.tellWatchers(w, watchers, []outbound{{id: protocol.IDPlaySetEntityMotion, data: motion}})
		}
		return true
	case *boltEntity:
		e.srv.boltMu.Lock()
		before := body.fields()
		applyBoltTag(body, data)
		e.baseName, e.nameOnce = nil, sync.Once{}
		e.setCustomName(body.custom.tag())
		body.teleported = true
		payload := metadataChanges(body.bolt.ID, before, body.fields())
		watchers, w := trackedWatchers(body.tracked), body.w
		e.srv.boltMu.Unlock()
		e.srv.sendEntityMetadata(w, watchers, payload)
		return true
	case *thrownEntity:
		owner := e.srv.resolveOwner(entityUUIDFieldOf(data, "Owner"))
		e.srv.arrowsMu.Lock()
		applyThrownTag(body, data)
		body.throwable.OwnerID = owner
		payload := thrownMetadataPayload(body, true)
		watchers := trackedWatchers(body.tracked)
		w := body.w
		e.srv.arrowsMu.Unlock()
		e.srv.sendEntityMetadata(w, watchers, payload)
		return true
	}
	return false
}

// trackedWatchers are a body's tracked clients, the metadata recipients.
func trackedWatchers[V any](tracked watcherSet[V]) []uuid.UUID {
	var watchers []uuid.UUID
	for id := range tracked.all() {
		watchers = append(watchers, id)
	}
	return watchers
}

// sendEntityMetadata sends a write's metadata to watchers still in the
// body's world; nil sends nothing.
func (s *Server) sendEntityMetadata(w *world.World, watchers []uuid.UUID, payload []byte) {
	if payload == nil {
		return
	}
	s.tellWatchers(w, watchers, []outbound{{id: protocol.IDPlayEntityMetadata, data: payload}})
}

// LootEntity is what a loot table asks about the mob: age, burning, sheep
// colour, size. Other kinds are known by type only.
func (e *entitySnapshot) LootEntity() rules.LootEntity {
	ref, ok := e.ref.(*mobEntity)
	if !ok {
		le := e.otherLootEntity()
		le.NBT = e.lootNBT
		return le
	}
	centers := e.srv.lootRaidCenters()
	e.srv.mobsMu.Lock()
	defer e.srv.mobsMu.Unlock()
	le := ref.mob.LootEntity()
	le.HasRaid = mobHasLootRaid(ref, centers[ref.w])
	le.NBT = e.lootNBT
	le.Component = func(name string) (string, bool) {
		e.srv.mobsMu.Lock()
		defer e.srv.mobsMu.Unlock()
		return mobComponent(ref.mob, name)
	}
	if name, _, ok := mobCustomName(ref); ok {
		le.Name = &name
	}
	return le
}

// lootNBT is the body's saved compound, read when a predicate asks for a
// component held in a saved field.
func (e *entitySnapshot) lootNBT() nbt.Compound {
	tag, _ := e.DataTag()
	return tag.Entries()
}

// DeathLootTable is a mob's loot table override, "" for none or another
// kind.
func (e *entitySnapshot) DeathLootTable() string {
	ref, ok := e.ref.(*mobEntity)
	if !ok {
		return ""
	}
	e.srv.mobsMu.Lock()
	defer e.srv.mobsMu.Unlock()
	return ref.mob.DeathLootTable
}

// ItemIn reads an entity's equipment, contents or chest-boat cargo slot.
// A stand's body and saddle are always empty.
// vanilla: LivingEntity.getSlot, ItemEntity.getSlot, ItemFrame.getSlot, Display.ItemDisplay.getSlot, ContainerEntity.getChestVehicleSlot
func (e *entitySnapshot) ItemIn(slot command.ItemSlot) (protocol.ItemStack, bool) {
	switch ref := e.ref.(type) {
	case *boatEntity:
		e.srv.boatsMu.Lock()
		defer e.srv.boatsMu.Unlock()
		if slot < 0 || int(slot) >= len(ref.cargo) {
			return protocol.ItemStack{}, false
		}
		e.srv.unpackBoatLoot(ref, lootOpener{})
		return ref.cargo[slot], true
	case *itemEntity:
		if slot != command.ItemSlotContents {
			return protocol.ItemStack{}, false
		}
		e.srv.itemsMu.Lock()
		defer e.srv.itemsMu.Unlock()
		return ref.item.Stack, true
	case *frameEntity:
		if slot != command.ItemSlotContents {
			return protocol.ItemStack{}, false
		}
		e.srv.framesMu.Lock()
		defer e.srv.framesMu.Unlock()
		return ref.item, true
	case *fixtureEntity:
		if ref.kind != entity.FixtureItemDisplay || slot != command.ItemSlotContents {
			return protocol.ItemStack{}, false
		}
		e.srv.fixturesMu.Lock()
		defer e.srv.fixturesMu.Unlock()
		var stack protocol.ItemStack
		if item, ok := nbt.CompoundView(ref.tag["item"]); ok {
			stack, _ = registry.StoredStack(item)
		}
		return stack, true
	case *mobEntity:
		e.srv.mobsMu.Lock()
		defer e.srv.mobsMu.Unlock()
		switch slot {
		case command.ItemSlotMainHand:
			return ref.mob.MainHand, true
		case command.ItemSlotOffHand:
			return ref.mob.OffHand, true
		case command.ItemSlotFeet, command.ItemSlotLegs, command.ItemSlotChest, command.ItemSlotHead:
			// Worn runs feet to head, as the slot ids do.
			return ref.mob.Worn[slot-command.ItemSlotFeet], true
		case command.ItemSlotBody:
			return ref.mob.Body, true
		case command.ItemSlotSaddle:
			return ref.mob.Horse.Saddle, true
		}
	case *armorStandEntity:
		e.srv.armorStandsMu.Lock()
		defer e.srv.armorStandsMu.Unlock()
		switch slot {
		case command.ItemSlotMainHand:
			return ref.stand.MainHand, true
		case command.ItemSlotOffHand:
			return ref.stand.OffHand, true
		case command.ItemSlotFeet, command.ItemSlotLegs, command.ItemSlotChest, command.ItemSlotHead:
			return ref.stand.Worn[slot-command.ItemSlotFeet], true
		case command.ItemSlotBody, command.ItemSlotSaddle:
			return protocol.ItemStack{}, true
		}
	case *mannequinEntity:
		e.srv.mannequinsMu.Lock()
		defer e.srv.mannequinsMu.Unlock()
		switch slot {
		case command.ItemSlotMainHand:
			return ref.mannequin.MainHand, true
		case command.ItemSlotOffHand:
			return ref.mannequin.OffHand, true
		case command.ItemSlotFeet, command.ItemSlotLegs, command.ItemSlotChest, command.ItemSlotHead:
			return ref.mannequin.Worn[slot-command.ItemSlotFeet], true
		case command.ItemSlotBody, command.ItemSlotSaddle:
			return protocol.ItemStack{}, true
		}
	}
	return protocol.ItemStack{}, false
}

// SetItemIn replaces an entity's equipment, contents or chest-boat cargo,
// telling viewers. The chest, legs, feet, body and saddle require compatible
// equipment; a mob's body and saddle are sent by its tick.
// vanilla: LivingEntity.createEquipmentSlotAccess, ItemEntity.getSlot, ItemFrame.getSlot, ContainerEntity.getChestVehicleSlot
func (e *entitySnapshot) SetItemIn(slot command.ItemSlot, stack protocol.ItemStack) bool {
	if ref, ok := e.ref.(*boatEntity); ok {
		e.srv.boatsMu.Lock()
		if slot < 0 || int(slot) >= len(ref.cargo) {
			e.srv.boatsMu.Unlock()
			return false
		}
		e.srv.unpackBoatLoot(ref, lootOpener{})
		stack.Count = min(stack.Count, registry.StackMaxStackSize(stack))
		boatCargo{it: ref}.Put(int(slot), stack)
		e.srv.boatsMu.Unlock()
		e.srv.syncBoatViewers(ref)
		return true
	}
	if slot == command.ItemSlotContents {
		return e.setContents(stack)
	}
	if slot == command.ItemSlotBody || slot == command.ItemSlotSaddle {
		ref, ok := e.ref.(*mobEntity)
		if !ok {
			return false
		}
		e.srv.mobsMu.Lock()
		defer e.srv.mobsMu.Unlock()
		name := "body"
		if slot == command.ItemSlotSaddle {
			name = "saddle"
		}
		if !ref.mob.TakesInSlot(name, stack) {
			return false
		}
		if slot == command.ItemSlotBody {
			ref.mob.SetEquipment(6, stack)
		} else {
			ref.mob.SetMetadataHorseSaddle(stack)
		}
		return true
	}
	if slot < command.ItemSlotMainHand || slot > command.ItemSlotHead || !itemSlotAccepts(slot, stack) {
		return false
	}
	// The /item slots 98-103 are the Set Equipment slots 0-5.
	wire := int(slot - command.ItemSlotMainHand)
	switch ref := e.ref.(type) {
	case *mobEntity:
		var old protocol.ItemStack
		e.srv.updateMobSlot(ref, wire, func(was protocol.ItemStack) (protocol.ItemStack, bool) {
			old = was
			return stack, true
		})
		e.srv.mobsMu.Lock()
		m := ref.mob
		fx := equipFx{x: m.X, y: m.Y, z: m.Z, category: mobSoundCategory(m.Type), silent: m.Silent,
			ctx: rules.GameEventContext{Mob: ref.uuid}}
		e.srv.mobsMu.Unlock()
		e.srv.slotEquipFeedback(ref.w, wire, old, stack, fx)
		return true
	case *armorStandEntity:
		e.srv.armorStandsMu.Lock()
		// A disabled armour slot is one the stand cannot use, so it takes
		// no armour. vanilla: ArmorStand.canUseSlot
		if _, armour := armorSlotName(slot); armour && !stack.Empty() && ref.stand.SlotLocked(wire) {
			e.srv.armorStandsMu.Unlock()
			return false
		}
		old := ref.stand.StackOf(wire)
		ref.stand.SetStack(wire, stack)
		equipment := armorStandEquipment(ref)
		w := ref.w
		fx := equipFx{x: ref.stand.X, y: ref.stand.Y, z: ref.stand.Z, category: protocol.SoundCategoryNeutral,
			silent: ref.look.silent}
		e.srv.armorStandsMu.Unlock()
		e.srv.broadcastWorld(w, protocol.IDPlayEntityEquipment, equipment)
		e.srv.slotEquipFeedback(w, wire, old, stack, fx)
		return true
	case *mannequinEntity:
		e.srv.mannequinsMu.Lock()
		m := ref.mannequin
		old := [6]protocol.ItemStack{m.MainHand, m.OffHand, m.Worn[0], m.Worn[1], m.Worn[2], m.Worn[3]}[wire]
		fx := equipFx{x: m.X, y: m.Y, z: m.Z, category: protocol.SoundCategoryNeutral, silent: ref.look.silent}
		switch wire {
		case 0:
			m.MainHand = stack
		case 1:
			m.OffHand = stack
		default:
			m.Worn[wire-2] = stack
		}
		m.SyncEquipmentModifiers()
		slots := mannequinEquipmentNow(m)
		equipment := equipmentEntriesPayload(m.ID, &slots, []int{wire})
		watchers, w := trackedWatchers(ref.tracked), ref.w
		e.srv.mannequinsMu.Unlock()
		e.srv.tellWatchers(w, watchers, []outbound{{id: protocol.IDPlayEntityEquipment, data: equipment}})
		e.srv.slotEquipFeedback(w, wire, old, stack, fx)
		return true
	}
	return false
}

// tellItemWriteLocked tells viewers about a write to it.item.Stack, made with
// itemsMu held since before was read; it releases itemsMu. An emptied stack
// discards the body. vanilla: ItemEntity.tick
func (s *Server) tellItemWriteLocked(it *itemEntity, before metaFields) {
	if it.item.Stack.Empty() {
		it.item.Dead, it.killed = true, true
		w, id := it.w, it.item.ID
		s.itemsMu.Unlock()
		s.broadcastWorld(w, protocol.IDPlayRemoveEntities, removeEntitiesPayload([]int32{id}))
		return
	}
	payload := metadataChanges(it.item.ID, before, itemFields(it))
	watchers := trackedWatchers(it.tracked)
	w := it.w
	s.itemsMu.Unlock()
	s.sendEntityMetadata(w, watchers, payload)
}

// setContents writes an item entity's, frame's or item display's slot 0,
// telling the viewers; other kinds have no such slot.
// vanilla: ItemEntity.getSlot, ItemFrame.getSlot, Display.ItemDisplay.getSlot
func (e *entitySnapshot) setContents(stack protocol.ItemStack) bool {
	switch ref := e.ref.(type) {
	case *itemEntity:
		e.srv.itemsMu.Lock()
		if ref.item.Dead {
			e.srv.itemsMu.Unlock()
			return false
		}
		before := itemFields(ref)
		ref.item.Stack = stack
		e.srv.tellItemWriteLocked(ref, before)
		return true
	case *frameEntity:
		// A frame holds one of the stack. vanilla: ItemFrame.setItem
		if !stack.Empty() {
			stack.Count = 1
		}
		e.srv.framesMu.Lock()
		ref.setItem(stack)
		payload := frameMetadataPayload(ref, false)
		tell := frameTellOf(ref)
		watchers := trackedWatchers(ref.tracked)
		e.srv.framesMu.Unlock()
		e.srv.sendEntityMetadata(tell.w, watchers, payload)
		if !stack.Empty() {
			e.srv.broadcastSoundID(tell.w, soundFrame(tell.glow, frameAddItem), protocol.SoundCategoryNeutral,
				tell.centreX, tell.centreY, tell.centreZ, 1.0, 1.0)
		}
		e.srv.frameSignalChanged(tell)
		return true
	case *fixtureEntity:
		if ref.kind != entity.FixtureItemDisplay {
			return false
		}
		e.srv.fixturesMu.Lock()
		if ref.dead {
			e.srv.fixturesMu.Unlock()
			return false
		}
		before := fixtureFields(ref)
		if stack.Empty() {
			delete(ref.tag, "item")
		} else {
			ref.tag["item"] = registry.StackTag(stack)
		}
		payload := metadataChanges(ref.id, before, fixtureFields(ref))
		watchers, w := trackedWatchers(ref.tracked), ref.w
		e.srv.fixturesMu.Unlock()
		e.srv.sendEntityMetadata(w, watchers, payload)
		return true
	}
	return false
}

// UpdateItemIn runs update on a mob's hand slot and, if asked, stores the
// result and tells viewers, all in one hold of the mob lock; other kinds
// and slots return false.
func (e *entitySnapshot) UpdateItemIn(slot command.ItemSlot, update func(protocol.ItemStack) (protocol.ItemStack, bool)) bool {
	ref, ok := e.ref.(*mobEntity)
	if !ok || (slot != command.ItemSlotMainHand && slot != command.ItemSlotOffHand) {
		return false
	}
	e.srv.updateMobSlot(ref, int(slot-command.ItemSlotMainHand), update)
	return true
}

// SetEffectCause names what the next effect change is, for
// EntityPotionEffectEvent.
func (e *entitySnapshot) SetEffectCause(c effect.Cause) { e.effectCause = c }

// AddEffect gives a living body an effect and tells its viewers.
// vanilla: LivingEntity.addEffect
func (e *entitySnapshot) AddEffect(in effect.Instance) bool {
	return e.changeLivingEffects(func(m effectBody) []effect.Change {
		ch, ok := m.AddEffect(in)
		if !ok {
			return nil
		}
		return []effect.Change{ch}
	})
}

// RemoveEffect removes one effect from a living body for /effect clear.
func (e *entitySnapshot) RemoveEffect(id int32) bool {
	return e.changeLivingEffects(func(m effectBody) []effect.Change {
		ch, ok := m.RemoveEffect(id)
		if !ok {
			return nil
		}
		return []effect.Change{ch}
	})
}

// ClearEffects takes every effect off a living body, for /effect clear said bare.
func (e *entitySnapshot) ClearEffects() bool {
	return e.changeLivingEffects(func(m effectBody) []effect.Change {
		return m.ClearEffects()
	})
}

type effectBody interface {
	AddEffect(effect.Instance) (effect.Change, bool)
	RemoveEffect(int32) (effect.Change, bool)
	ClearEffects() []effect.Change
}

// changeLivingEffects changes effects under the body lock and sends after unlocking.
func (e *entitySnapshot) changeLivingEffects(change func(effectBody) []effect.Change) bool {
	s := e.srv
	if ref, ok := e.ref.(*mannequinEntity); ok {
		s.mannequinsMu.Lock()
		changes := change(ref.mannequin)
		packets := mannequinEffectPackets(ref)
		watchers := trackedWatchers(ref.tracked)
		s.mannequinsMu.Unlock()
		s.tellWatchers(ref.w, watchers, packets)
		return len(changes) > 0
	}
	ref, ok := e.ref.(*mobEntity)
	if !ok {
		return false
	}
	s.mobsMu.Lock()
	ref.mob.EffectCause, e.effectCause = e.effectCause, effect.CauseOther
	changes := change(ref.mob)
	var tell splashMobTell
	if len(changes) > 0 {
		tell = mobEffectTellOf(ref, changes)
	}
	s.mobsMu.Unlock()
	s.tellSplashMob(ref.w, tell)
	return len(changes) > 0
}

// Swing is /swing, for mobs and armor stands, the living kinds here.
// vanilla: LivingEntity.swing
func (e *entitySnapshot) Swing(offHand bool, anim registry.SwingAnimation) bool {
	hand := handOf(offHand)
	switch ref := e.ref.(type) {
	case *mobEntity:
		e.srv.broadcastWorld(ref.w, protocol.IDPlaySwingAnimation, swingAnimationWith(ref.mob.ID, hand, anim))
	case *armorStandEntity:
		e.srv.broadcastWorld(ref.w, protocol.IDPlaySwingAnimation, swingAnimationWith(ref.stand.ID, hand, anim))
	default:
		return false
	}
	return true
}

// Kill is /kill: a mob dies via LivingEntity.kill (animation, drops,
// sounds); anything else is discarded as by Entity.kill.
func (e *entitySnapshot) Kill() {
	e.ops.kill(e.srv, e.ref)
	// The reset lands before the next command, so a body summoned with the
	// same UUID keeps its new scores.
	// vanilla: PersistentEntitySectionManager.Callback.onRemove
	e.srv.flushDestroyed()
}

// Teleport is Entity.teleportTo: an absolute Teleport Entity to the
// entity's viewers, or teleportAcross for another dimension. A non-player
// always gets its absolute destination, so a facing clause turns it here
// rather than via the client-only Player Look At.
func (e *entitySnapshot) Teleport(move command.Move, dim string, by string) {
	move, dim, ok := e.srv.pluginTeleport(e, move, dim, by)
	if !ok {
		return
	}
	to := move.Pos
	if d := e.srv.findDimension(dim); d != nil && d.name != e.dim {
		e.teleportAcross(d, move)
		return
	}
	switch ref := e.ref.(type) {
	case *mobEntity:
		e.srv.teleportMob(ref, to, to.Yaw, to.Pitch, by)
	case *itemEntity:
		e.srv.teleportItem(ref, to, by)
	case *arrowEntity:
		e.srv.teleportArrow(ref, to, by)
	case *orbEntity:
		e.srv.teleportOrb(ref, to, by)
	case *tntEntity:
		e.srv.teleportTNT(ref, to, by)
	case *endCrystalEntity:
		e.srv.teleportEndCrystal(ref, to, by)
	case *boatEntity:
		e.srv.teleportBoat(ref, to, to.Yaw, by)
	case *minecartEntity:
		e.srv.teleportMinecart(ref, to, to.Yaw, by)
	case *fallingEntity:
		e.srv.teleportFallingBlock(ref, to, by)
	case *thrownEntity:
		e.srv.teleportThrown(ref, to, by)
	case *armorStandEntity:
		e.srv.teleportArmorStand(ref, to, by)
	case *mannequinEntity:
		e.srv.teleportMannequin(ref, to, by)
	case *cloudEntity:
		e.srv.teleportCloud(ref, to, by)
	case *fangsEntity:
		e.srv.teleportFangs(ref, move)
	case *eyeEntity:
		e.srv.teleportEye(ref, move)
	case *boltEntity:
		e.srv.teleportBolt(ref, move)
	case *fireworkEntity:
		e.srv.teleportFirework(ref, to, by)
	case *bobberEntity:
		e.srv.teleportBobber(ref, to, by)
	case *knotEntity:
		e.srv.teleportKnot(ref, to, by)
	case *paintingEntity:
		e.srv.teleportPainting(ref, to, by)
	case *frameEntity:
		e.srv.teleportFrame(ref, to, by)
	case *fixtureEntity:
		e.srv.teleportFixture(ref, to)
	}
}

// Rotate turns the entity in place, through each kind's teleport path (a
// fresh Teleport Entity carrying angles), except mobs, whose watchers get
// the movement path's rotation-only packets. Kinds never shown turning
// (items, orbs, primed TNT, falling blocks) stay still; none of their
// packets carry an angle. vanilla: RotateCommand.rotate, Entity.setRot
func (e *entitySnapshot) Rotate(yaw, pitch float32) {
	to := e.pos
	to.Yaw, to.Pitch = yaw, pitch
	switch ref := e.ref.(type) {
	case *mobEntity:
		e.srv.rotateMob(ref, yaw, pitch)
	case *arrowEntity:
		e.srv.arrowsMu.Lock()
		ref.arrow.Yaw, ref.arrow.Pitch = yaw, pitch
		e.srv.arrowsMu.Unlock()
		e.srv.teleportArrow(ref, to, "rotate")
	case *thrownEntity:
		e.srv.arrowsMu.Lock()
		ref.throwable.Yaw, ref.throwable.Pitch = yaw, pitch
		e.srv.arrowsMu.Unlock()
		e.srv.teleportThrown(ref, to, "rotate")
	case *boatEntity:
		e.srv.teleportBoat(ref, to, yaw, "rotate")
	case *minecartEntity:
		e.srv.teleportMinecart(ref, to, yaw, "rotate")
	case *armorStandEntity:
		e.srv.armorStandsMu.Lock()
		ref.stand.Yaw = yaw
		e.srv.armorStandsMu.Unlock()
		e.srv.teleportArmorStand(ref, to, "rotate")
	case *mannequinEntity:
		e.srv.teleportMannequin(ref, to, "rotate")
	case *fixtureEntity:
		e.srv.teleportFixture(ref, to)
	}
}

// teleportAcross is Entity.teleportTo into another dimension for a
// non-player: a twin of the same kind at rest at the position, facing yaw
// and pitch, and the old body removed. The liveness check, tag and removal
// share one hold of the kind's lock, so a body taken since the selector saw
// it (merged, despawned, dead, through a portal, or claimed by a pickup,
// which marks the item dead under this lock before giving the stack) is not
// carried twice. The twin registers after, as registering takes the player
// list lock, which is outside the kind's.
// vanilla: Entity.teleportCrossDimension
func (e *entitySnapshot) teleportAcross(to *dimension, move command.Move) {
	at := move.Pos
	yaw, pitch := at.Yaw, at.Pitch
	s := e.srv
	var (
		alive    bool
		from     *world.World
		oldID    int32
		register func()
		// severed are mob vehicles the travel removed a passenger from, told after
		// the removal.
		severed []seatNews
	)
	switch ref := e.ref.(type) {
	case *mobEntity:
		alive, from, oldID, register, severed = s.teleportMobAcross(ref, to, at, yaw, pitch)
	case *itemEntity:
		s.itemsMu.Lock()
		var twin *itemEntity
		if alive = !ref.item.Dead && slices.Contains(s.items, ref); alive {
			if twin = s.itemTwinAt(ref, to, at); twin != nil {
				ref.item.Dead = true
			}
		}
		s.itemsMu.Unlock()
		if twin != nil {
			twin.item.VX, twin.item.VY, twin.item.VZ = 0, 0, 0
			from, oldID = ref.w, ref.item.ID
			register = func() { s.registerItem(twin) }
		}
	case *arrowEntity:
		s.arrowsMu.Lock()
		var twin *arrowEntity
		if alive = !ref.arrow.Dead && slices.Contains(s.arrows, ref); alive {
			if twin = s.arrowTwinAt(ref, to, at); twin != nil {
				ref.arrow.Dead = true
			}
		}
		s.arrowsMu.Unlock()
		if twin != nil {
			twin.arrow.VX, twin.arrow.VY, twin.arrow.VZ = 0, 0, 0
			twin.arrow.Yaw, twin.arrow.Pitch = yaw, pitch
			from, oldID = ref.w, ref.arrow.ID
			register = func() {
				twin.arrow.OwnerID = s.resolveOwner(twin.ownerUUID)
				s.registerArrow(twin)
			}
		}
	case *orbEntity:
		s.orbsMu.Lock()
		var twin *orbEntity
		if alive = !ref.orb.Dead && slices.Contains(s.orbs, ref); alive {
			if twin = s.orbTwinAt(ref, to, at); twin != nil {
				ref.orb.Dead = true
			}
		}
		s.orbsMu.Unlock()
		if twin != nil {
			twin.orb.VX, twin.orb.VY, twin.orb.VZ = 0, 0, 0
			from, oldID = ref.w, ref.orb.ID
			register = func() { s.registerOrb(twin) }
		}
	case *tntEntity:
		s.tntMu.Lock()
		var twin *tntEntity
		if alive = !ref.tnt.Dead && slices.Contains(s.tnts, ref); alive {
			if twin = s.tntTwinAt(ref, to, at); twin != nil {
				ref.tnt.Dead = true
			}
		}
		s.tntMu.Unlock()
		if twin != nil {
			twin.tnt.VX, twin.tnt.VY, twin.tnt.VZ = 0, 0, 0
			from, oldID = ref.w, ref.tnt.ID
			register = func() {
				twin.tnt.OwnerID = s.resolveOwner(twin.ownerUUID)
				s.registerTNT(twin)
			}
		}
	case *boatEntity:
		s.boatsMu.Lock()
		var twin *boatEntity
		var leftHolder int32
		var mobs []*mobEntity
		var turn float32
		if alive = !ref.boat.Dead && slices.Contains(s.boats, ref); alive {
			if twin = s.boatTwinAt(ref, to, at); twin != nil {
				// The husk left behind marks itself dead with its loot handed over (the
				// twin carries it), so the serving pass breaks nothing; its players step
				// off where they sat, done by the pass, and its mobs are taken off here,
				// before the pass can eject them, to follow as twins.
				ref.boat.Dead = true
				ref.boat.NoLoot = true
				ref.scoresSettled = true // the twin keeps the UUID's scores
				ref.cargo = nil
				mobs, turn = ref.takeMobs(), yaw-ref.boat.Yaw
				// Its lead went with the twin: the husk lets go without
				// laying the item.
				// vanilla: Entity.removeAfterChangingDimensions
				leftHolder = ref.untie()
			}
		}
		s.boatsMu.Unlock()
		if leftHolder != 0 {
			s.leashLetGo(ref.w, ref.boat.ID, leftHolder)
		}
		if twin != nil {
			twin.boat.VX, twin.boat.VY, twin.boat.VZ = 0, 0, 0
			twin.boat.Yaw = yaw
			from, oldID = ref.w, ref.boat.ID
			register = func() {
				s.registerBoat(twin)
				s.reseatBoatMobs(mobs, ref, twin, turn)
			}
		}
	case *minecartEntity:
		alive, from, oldID, register = s.teleportCartAcross(ref, to, at, yaw, pitch)
	case *fallingEntity:
		s.fallingMu.Lock()
		var twin *fallingEntity
		if alive = !ref.fb.Dead && slices.Contains(s.fallings, ref); alive {
			if twin = s.fallingTwinAt(ref, to, at); twin != nil {
				ref.fb.Dead = true
			}
		}
		s.fallingMu.Unlock()
		if twin != nil {
			twin.fb.VX, twin.fb.VY, twin.fb.VZ = 0, 0, 0
			from, oldID = ref.w, ref.fb.ID
			register = func() { s.registerFalling(twin) }
		}
	case *thrownEntity:
		s.arrowsMu.Lock()
		var twin *thrownEntity
		if alive = !ref.throwable.Dead && slices.Contains(s.thrown, ref); alive {
			if twin = s.thrownTwinAt(ref, to, at); twin != nil {
				ref.throwable.Dead = true
				// The tick's sweep of dead bodies would forget the twin's scores.
				s.thrown = slices.DeleteFunc(s.thrown, func(o *thrownEntity) bool { return o == ref })
			}
		}
		s.arrowsMu.Unlock()
		if twin != nil {
			twin.throwable.VX, twin.throwable.VY, twin.throwable.VZ = 0, 0, 0
			from, oldID = ref.w, ref.throwable.ID
			register = func() {
				twin.throwable.OwnerID = s.resolveOwner(twin.ownerUUID)
				s.registerThrown(twin)
			}
		}
	case *armorStandEntity:
		s.armorStandsMu.Lock()
		var twin *armorStandEntity
		if alive = !ref.stand.Dead && slices.Contains(s.armorStands, ref); alive {
			if twin = s.armorStandTwinAt(ref, to, at); twin != nil {
				ref.stand.Dead = true
			}
		}
		s.armorStandsMu.Unlock()
		if twin != nil {
			twin.stand.VX, twin.stand.VY, twin.stand.VZ = 0, 0, 0
			from, oldID = ref.w, ref.stand.ID
			register = func() { s.registerArmorStand(twin) }
		}
	case *mannequinEntity:
		s.mannequinsMu.Lock()
		var twin *mannequinEntity
		if alive = !ref.mannequin.Dead && slices.Contains(s.mannequins, ref); alive {
			at.Yaw, at.Pitch = yaw, pitch
			if twin = s.mannequinTwinAt(ref, to, at); twin != nil {
				ref.mannequin.Dead = true
				s.mannequins = slices.DeleteFunc(s.mannequins, func(o *mannequinEntity) bool { return o == ref })
			}
		}
		s.mannequinsMu.Unlock()
		if twin != nil {
			from, oldID = ref.w, ref.mannequin.ID
			register = func() { s.registerMannequin(twin) }
		}
	case *cloudEntity:
		s.arrowsMu.Lock()
		var twin *cloudEntity
		if alive = !ref.cloud.Dead && slices.Contains(s.clouds, ref); alive {
			if twin = s.cloudTwinAt(ref, to, at); twin != nil {
				ref.cloud.Dead = true
				s.clouds = slices.DeleteFunc(s.clouds, func(o *cloudEntity) bool { return o == ref })
			}
		}
		s.arrowsMu.Unlock()
		if twin != nil {
			from, oldID = ref.w, ref.cloud.ID
			register = func() { s.registerCloud(twin) }
		}
	case *fangsEntity:
		alive, from, oldID, register = s.teleportFangsAcross(ref, to, move)
	case *eyeEntity:
		alive, from, oldID, register = s.teleportEyeAcross(ref, to, move)
	case *boltEntity:
		alive, from, oldID, register = s.teleportBoltAcross(ref, to, move)
	case *fireworkEntity:
		s.fireworksMu.Lock()
		var twin *fireworkEntity
		if alive = !ref.rocket.Dead && slices.Contains(s.fireworks, ref); alive {
			if twin = s.fireworkTwinAt(ref, to, at); twin != nil {
				ref.rocket.Dead = true
				s.fireworks = slices.DeleteFunc(s.fireworks, func(o *fireworkEntity) bool { return o == ref })
			}
		}
		s.fireworksMu.Unlock()
		if twin != nil {
			twin.rocket.VX, twin.rocket.VY, twin.rocket.VZ = 0, 0, 0
			twin.rocket.Yaw, twin.rocket.Pitch = yaw, pitch
			from, oldID = ref.w, ref.rocket.ID
			register = func() { s.registerFirework(twin) }
		}
	case *bobberEntity:
		s.bobbersMu.Lock()
		var twin *bobberEntity
		if alive = !ref.hook.Dead && slices.Contains(s.bobbers, ref); alive {
			if twin = s.bobberTwinAt(ref, to, at); twin != nil {
				ref.hook.Dead = true
				s.bobbers = slices.DeleteFunc(s.bobbers, func(o *bobberEntity) bool { return o == ref })
			}
		}
		s.bobbersMu.Unlock()
		if twin != nil {
			twin.hook.VX, twin.hook.VY, twin.hook.VZ = 0, 0, 0
			from, oldID = ref.w, ref.hook.ID
			register = func() { s.registerBobber(twin) }
		}
	case *knotEntity:
		s.knotsMu.Lock()
		var twin *knotEntity
		if alive = slices.Contains(s.knots, ref); alive {
			if twin = s.knotTwinAt(ref, to, at); twin != nil {
				s.knots = slices.DeleteFunc(s.knots, func(o *knotEntity) bool { return o == ref })
			}
		}
		s.knotsMu.Unlock()
		if twin != nil {
			from, oldID = ref.w, ref.id
			register = func() { s.registerKnot(twin) }
		}
	case *paintingEntity:
		s.paintingsMu.Lock()
		var twin *paintingEntity
		if alive = slices.Contains(s.paintings, ref); alive {
			if twin = s.paintingTwinAt(ref, to, at); twin != nil {
				s.unlistPaintingLocked(ref)
			}
		}
		s.paintingsMu.Unlock()
		if twin != nil {
			from, oldID = ref.w, ref.id
			register = func() { s.registerPainting(twin) }
		}
	case *frameEntity:
		s.framesMu.Lock()
		var twin *frameEntity
		if alive = slices.Contains(s.frames, ref); alive {
			if twin = s.frameTwinAt(ref, to, at); twin != nil {
				s.unlistFrameLocked(ref)
			}
		}
		s.framesMu.Unlock()
		if twin != nil {
			from, oldID = ref.w, ref.id
			register = func() { s.registerFrame(twin) }
		}
	case *fixtureEntity:
		s.fixturesMu.Lock()
		var twin *fixtureEntity
		if alive = !ref.dead && slices.Contains(s.fixtures, ref); alive {
			at.Yaw, at.Pitch = yaw, pitch
			if twin = s.fixtureTwinAt(ref, to, at); twin != nil {
				s.unlistFixtureLocked(ref)
			}
		}
		s.fixturesMu.Unlock()
		if twin != nil {
			from, oldID = ref.w, ref.id
			register = func() { s.registerFixture(twin) }
		}
	}
	if register == nil {
		if alive {
			slog.Error("could not carry an entity across a teleport", "type", e.EntityType())
		}
		return
	}
	s.wakeArrivalChunk(to, world.SpawnPos{X: rules.BlockOf(at.X), Y: rules.BlockOf(at.Y), Z: rules.BlockOf(at.Z)})
	register()
	s.teleportSpectatorsOf(from, oldID, to, at)
	s.broadcastWorld(from, protocol.IDPlayRemoveEntities, removeEntitiesPayload([]int32{oldID}))
	s.sendSeatNews(severed)
}

// Entities reads each kind under its own lock, returning a non-nil pool.
// Results follow entity ID order; limited queries snapshot non-players only
// until enough entities pass Accept.
func (ws worldScope) Entities(filters command.EntityQuery) []command.Entity {
	return ws.query(newEntityQuery(&filters))
}

// query answers q for every kind of body.
func (ws worldScope) query(q entityQuery) []command.Entity {
	s := ws.srv
	// Bound maintenance is linear in the batch size; larger limits collect once.
	if q.Limit > 0 && q.Limit <= 16 {
		q.batch = q.Limit
	}
	if q.Limit > 0 {
		candidates := entityCandidates.Get().(*[]entityCandidate)
		q.storage = candidates
		q.candidates = candidates
		defer func() {
			clear(*candidates)
			*candidates = (*candidates)[:0]
			entityCandidates.Put(candidates)
		}()
	}
	entities := make([]command.Entity, 0, 16)
	for _, c := range s.playerClients() {
		if q.Rejects(c.EntityType(), c.DimensionName(), box6(c.Box()), c.UUID()) {
			continue
		}
		if len(q.Tags) != 0 {
			tags := c.Tags()
			if q.RejectsTags(len(tags), func(name string) bool { return slices.Contains(tags, name) }) {
				continue
			}
		}
		if q.Limit > 0 {
			if !q.skipCandidate(c.EntityID()) {
				q.collect(entityCandidate{id: c.EntityID(), ref: c})
			}
			continue
		}
		entities = append(entities, c)
		if q.UUID != nil {
			return entities
		}
	}
	// Each step takes only its own lock; thrown bodies and clouds share the arrow lock.
	// Mobs come first because their UUID lookup is a map.
	kinds := []func([]command.Entity) []command.Entity{
		func(out []command.Entity) []command.Entity { return s.appendMobSnapshots(out, &q) },
		snapshotAppender(s, &q, &s.itemsMu, &s.items, itemTracker.id, (*Server).itemSnapshot),
		snapshotAppender(s, &q, &s.orbsMu, &s.orbs, orbTracker.id, (*Server).orbSnapshot),
		snapshotAppender(s, &q, &s.arrowsMu, &s.arrows, arrowTracker.id, (*Server).arrowSnapshot),
		snapshotAppender(s, &q, &s.arrowsMu, &s.thrown, thrownTracker.id, (*Server).thrownSnapshot),
		snapshotAppender(s, &q, &s.tntMu, &s.tnts, tntTracker.id, (*Server).tntSnapshot),
		snapshotAppender(s, &q, &s.boatsMu, &s.boats, boatTracker.id, (*Server).boatSnapshot),
		snapshotAppender(s, &q, &s.minecartsMu, &s.minecarts, minecartTracker.id, (*Server).minecartSnapshot),
		snapshotAppender(s, &q, &s.armorStandsMu, &s.armorStands, standTracker.id, (*Server).armorStandSnapshot),
		snapshotAppender(s, &q, &s.mannequinsMu, &s.mannequins, mannequinTracker.id, (*Server).mannequinSnapshot),
		snapshotAppender(s, &q, &s.endCrystalsMu, &s.endCrystals, endCrystalTracker.id, (*Server).endCrystalSnapshot),
		snapshotAppender(s, &q, &s.arrowsMu, &s.clouds, cloudTracker.id, (*Server).cloudSnapshot),
		snapshotAppender(s, &q, &s.fangs.mu, &s.fangs.list, fangsTracker.id, (*Server).fangsSnapshot),
		snapshotAppender(s, &q, &s.eyesMu, &s.eyes, eyeTracker.id, (*Server).eyeSnapshot),
		snapshotAppender(s, &q, &s.boltMu, &s.bolts, boltTracker.id, (*Server).boltSnapshot),
		snapshotAppender(s, &q, &s.fireworksMu, &s.fireworks, fireworkTracker.id, (*Server).fireworkSnapshot),
		snapshotAppender(s, &q, &s.bobbersMu, &s.bobbers, bobberTracker.id, (*Server).bobberSnapshot),
		snapshotAppender(s, &q, &s.knotsMu, &s.knots, knotTracker.id, (*Server).knotSnapshot),
		snapshotAppender(s, &q, &s.paintingsMu, &s.paintings, paintingTracker.id, (*Server).paintingSnapshot),
		snapshotAppender(s, &q, &s.framesMu, &s.frames, frameTracker.id, (*Server).frameSnapshot),
		func(out []command.Entity) []command.Entity {
			if q.Type == "" || entity.FixtureKind(q.Type) {
				return snapshotAppender(s, &q, &s.fixturesMu, &s.fixtures, fixtureTracker.id, (*Server).fixtureSnapshot)(out)
			}
			return out
		},
		snapshotAppender(s, &q, &s.fallingMu, &s.fallings, fallingTracker.id, (*Server).fallingSnapshot),
	}
	for {
		for _, appendKind := range kinds {
			entities = appendKind(entities)
			if q.UUID != nil && len(entities) != 0 {
				return entities
			}
		}
		if q.Limit > 0 {
			candidates := *q.candidates
			slices.SortFunc(candidates, func(a, b entityCandidate) int { return cmp.Compare(a.id, b.id) })
			// Disable collection while taking each accepted body's full snapshot.
			q.candidates = nil
			for _, candidate := range candidates {
				var e command.Entity
				if candidate.kind == nil {
					e = candidate.ref.(command.Entity)
				} else {
					candidate.kind.mu.Lock()
					snapshot := candidate.kind.snapshot(candidate.ref, &q)
					candidate.kind.mu.Unlock()
					if snapshot == nil {
						continue
					}
					e = snapshot
				}
				// Accept can read NBT, teams or scores: never hold a body's lock here.
				if q.Accept == nil || q.Accept(e) {
					entities = append(entities, e)
					if len(entities) == q.Limit {
						break
					}
				}
			}
			if len(entities) == q.Limit || !q.more {
				return entities
			}
			// The first batch can all fail Accept. Scan the remaining IDs once,
			// without a batch bound, rather than rescanning for every rejected body.
			q.after, q.before = int64(candidates[len(candidates)-1].id), math.MaxInt32+1
			clear(candidates)
			*q.storage = candidates[:0]
			q.batch, q.more = 0, false
			q.candidates = q.storage
		} else {
			break
		}
	}
	// IDs are handed out in spawn order, the order vanilla's level lookup
	// keeps; the herd's map and the per-kind lists above are not.
	// vanilla: EntityLookup.byId
	slices.SortStableFunc(entities, func(a, b command.Entity) int { return cmp.Compare(a.EntityID(), b.EntityID()) })
	return entities
}

// snapshotAppender binds a kind's query step; the slice header is read under
// its lock because the tick reassigns it.
func snapshotAppender[E comparable](s *Server, q *entityQuery, mu *sync.Mutex, list *[]E, id func(E) int32, snap func(*Server, E, *entityQuery) *entitySnapshot) func([]command.Entity) []command.Entity {
	return func(out []command.Entity) []command.Entity {
		mu.Lock()
		defer mu.Unlock()
		if q.candidates != nil {
			if len(*list) == 0 {
				return out
			}
			q.kind = &entityQueryKind{mu: mu, snapshot: func(ref any, query *entityQuery) *entitySnapshot {
				body := ref.(E)
				if !slices.Contains(*list, body) {
					return nil
				}
				return snap(s, body, query)
			}}
			for _, body := range *list {
				if !q.skipCandidate(id(body)) {
					snap(s, body, q)
				}
			}
			return out
		}
		for i := range len(*list) {
			// UUIDs are unique; newest first reaches a just-summoned body at once.
			if q.UUID != nil {
				i = len(*list) - 1 - i
			}
			body := (*list)[i]
			if v := snap(s, body, q); v != nil {
				out = append(out, v)
				if q.UUID != nil {
					return out
				}
			}
		}
		return out
	}
}

// rejectsEntityHead filters fields that do not need a box, pose or dimension.
func rejectsEntityHead(q *entityQuery, typ string, id uuid.UUID, tags nbt.StringSet) bool {
	return q != nil && (q.UUID != nil && id != *q.UUID || q.Type != "" && typ != q.Type || rejectsEntityTags(q.filters(), tags))
}

func (s *Server) appendMobSnapshots(out []command.Entity, q *entityQuery) []command.Entity {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for _, dim := range s.allDims() {
		if q.Dimension != "" && dim.name != q.Dimension {
			continue
		}
		if q.candidates != nil && (len(dim.mobs.book) > 0 || q.IncludeDead && len(dim.mobs.dying) > 0) {
			// Recheck membership on the second pass: mobs leave the live index at death.
			q.kind = &entityQueryKind{mu: &s.mobsMu, snapshot: func(ref any, query *entityQuery) *entitySnapshot {
				ent := ref.(*mobEntity)
				if dim.mobs.book[ent.mob] != ent {
					return nil
				}
				return s.mobSnapshot(ent, dim.name, query)
			}}
		}
		appendMob := func(ent *mobEntity, dim string) {
			if q.candidates != nil && q.skipCandidate(ent.mob.ID) {
				return
			}
			if q.chunk != nil && !ent.mob.InChunk(*q.chunk) {
				return
			}
			if e := s.mobSnapshot(ent, dim, q); e != nil {
				out = append(out, e)
			}
		}
		if q.UUID != nil {
			// vanilla: EntityLookup.getEntity(UUID)
			if ent := dim.mobs.book[dim.mobs.byUUID[*q.UUID]]; ent != nil {
				appendMob(ent, dim.name)
			}
		} else if q.chunk != nil {
			for _, m := range dim.mobs.chunks.In(*q.chunk) {
				if ent := dim.mobs.record(m); ent != nil {
					appendMob(ent, dim.name)
				}
			}
		} else {
			for _, ent := range dim.mobs.book {
				appendMob(ent, dim.name)
			}
		}
		if q.UUID != nil && len(out) != 0 {
			return out
		}
		if q.IncludeDead {
			for _, body := range dim.mobs.dying {
				appendMob(body.ent, dim.name)
				if q.UUID != nil && len(out) != 0 {
					return out
				}
			}
		}
	}
	return out
}

// snapshot checks shared availability, then collects an identity or builds the snapshot's base.
func (s *Server) snapshot(q *entityQuery, tags nbt.StringSet, ref any, id int32, typ, plain string, identity uuid.UUID, dim string, pos player.Position, box [6]float64) *entitySnapshot {
	if s.rejectsSnapshot(q.filters(), tags, typ, identity, dim, pos, box) {
		return nil
	}
	if q != nil && q.candidates != nil {
		q.collect(entityCandidate{id: id, ref: ref, kind: q.kind})
		return nil
	}
	return &entitySnapshot{
		srv: s, ref: ref, id: id, uuid: identity, typ: typ,
		plain: plain, dim: dim, pos: pos, box: box, alive: true,
	}
}

func (s *Server) rejectsSnapshot(q *command.EntityQuery, tags nbt.StringSet, typ string, identity uuid.UUID, dim string, pos player.Position, box [6]float64) bool {
	if q.Rejects(typ, dim, box, identity) || rejectsEntityTags(q, tags) {
		return true
	}
	// A body can arrive before its terrain is loaded. Only accessible sections
	// enter the selector lookup; direct snapshots still serve summons and @s.
	// vanilla: PersistentEntitySectionManager.startTracking, Visibility.isAccessible
	if q != nil && !s.dimensionByName(dim).w.ChunkLoaded(world.ChunkPos{X: world.ChunkOf(pos.X), Z: world.ChunkOf(pos.Z)}) {
		return true
	}
	return false
}

func box6(minX, minY, minZ, maxX, maxY, maxZ float64) [6]float64 {
	return [6]float64{minX, minY, minZ, maxX, maxY, maxZ}
}

// Per-kind snapshots, nil for a dead entity, taken under the kind's lock.

func (s *Server) mobSnapshot(ent *mobEntity, dim string, q *entityQuery) *entitySnapshot {
	typ := ent.mob.Type.Name()
	if rejectsEntityHead(q, typ, ent.uuid, ent.tags) {
		return nil
	}
	m := ent.mob
	pos := player.Position{X: m.X, Y: m.Y, Z: m.Z, Yaw: m.Yaw, Pitch: m.Pitch}
	e := s.snapshot(q, ent.tags, ent, m.ID, typ, mobTypeEnglishName(m), ent.uuid, dim, pos, box6(m.BlastBox()))
	if e == nil {
		return nil
	}
	e.alive = m.Alive()
	e.bind(&s.mobsMu, &ent.tags, &mobEntityOps)
	if m.Type == entity.TypeVillager {
		name := mobTypeName(m)
		e.baseName = &name
	}
	e.setCustomName(ent.name)
	return e
}

// setCustomName names the entity by a CustomName tag; a nil tag leaves the
// type's name. vanilla: Entity.getName
func (e *entitySnapshot) setCustomName(tag nbt.Tag) {
	if tag == nil {
		return
	}
	if custom, plain, ok := customNameTag(tag); ok {
		c := custom // Copied to the heap only for a named entity.
		e.baseName, e.plain = &c, plain
	}
}

// customNameText is a CustomName tag's text, nil for no tag.
func customNameText(tag nbt.Tag) *protocol.Text {
	if custom, _, ok := customNameTag(tag); ok {
		return &custom
	}
	return nil
}

func (s *Server) itemSnapshot(it *itemEntity, q *entityQuery) *entitySnapshot {
	typ := "minecraft:item"
	if rejectsEntityHead(q, typ, it.uuid, it.tags) {
		return nil
	}
	if it.item.Dead {
		return nil
	}
	x, y, z := it.item.FeetPosition()
	e := s.snapshot(q, it.tags, it, it.item.ID, typ, "", it.uuid,
		s.dimensionOfWorld(it.w).name, player.Position{X: x, Y: y, Z: z}, box6(it.item.BlastBox()))
	if e == nil {
		return nil
	}
	e.setCustomName(it.custom.tag())
	if e.baseName == nil {
		e.stackItem, e.byStack = it.item.Stack.ItemID, true
	}
	e.bind(&s.itemsMu, &it.tags, &itemEntityOps)
	return e
}

func (s *Server) arrowSnapshot(it *arrowEntity, q *entityQuery) *entitySnapshot {
	typ := arrowTypeName(it)
	if rejectsEntityHead(q, typ, it.uuid, it.tags) {
		return nil
	}
	a := it.arrow
	if a.Dead {
		return nil
	}
	pos := player.Position{X: a.X, Y: a.Y, Z: a.Z, Yaw: a.Yaw, Pitch: a.Pitch}
	plain := itemEnglishName(typ)
	e := s.snapshot(q, it.tags, it, it.arrow.ID, typ, plain, it.uuid, s.dimensionOfWorld(it.w).name, pos, box6(a.BlastBox()))
	if e == nil {
		return nil
	}
	e.bind(&s.arrowsMu, &it.tags, &arrowEntityOps)
	e.setCustomName(it.custom.tag())
	return e
}

func (s *Server) orbSnapshot(it *orbEntity, q *entityQuery) *entitySnapshot {
	typ := "minecraft:experience_orb"
	if rejectsEntityHead(q, typ, it.uuid, it.tags) {
		return nil
	}
	o := it.orb
	if o.Dead {
		return nil
	}
	pos := player.Position{X: o.X, Y: o.Y, Z: o.Z}
	e := s.snapshot(q, it.tags, it, it.orb.ID, typ, "Experience Orb", it.uuid, s.dimensionOfWorld(it.w).name, pos, box6(o.BlastBox()))
	if e == nil {
		return nil
	}
	e.bind(&s.orbsMu, &it.tags, &orbEntityOps)
	e.setCustomName(it.custom.tag())
	return e
}

func (s *Server) thrownSnapshot(it *thrownEntity, q *entityQuery) *entitySnapshot {
	// The registry name, not the drawn stack: every egg variant has one entity type.
	typ := registry.EntityTypes[it.throwable.Kind.EntityType()]
	if rejectsEntityHead(q, typ, it.uuid, it.tags) {
		return nil
	}
	t := it.throwable
	if t.Dead {
		return nil
	}
	pos := player.Position{X: t.X, Y: t.Y, Z: t.Z, Yaw: t.Yaw, Pitch: t.Pitch}
	e := s.snapshot(q, it.tags, it, t.ID, typ, "", it.uuid,
		s.dimensionOfWorld(it.w).name, pos, box6(t.BlastBox()))
	if e == nil {
		return nil
	}
	e.plain = itemEnglishName(typ)
	e.bind(&s.arrowsMu, &it.tags, &thrownEntityOps)
	e.setCustomName(it.custom.tag())
	return e
}

func (s *Server) tntSnapshot(it *tntEntity, q *entityQuery) *entitySnapshot {
	typ := "minecraft:tnt"
	if rejectsEntityHead(q, typ, it.uuid, it.tags) {
		return nil
	}
	t := it.tnt
	if t.Dead {
		return nil
	}
	pos := player.Position{X: t.X, Y: t.Y, Z: t.Z}
	e := s.snapshot(q, it.tags, it, it.tnt.ID, typ, "Primed TNT", it.uuid, s.dimensionOfWorld(it.w).name, pos, box6(t.BlastBox()))
	if e == nil {
		return nil
	}
	e.bind(&s.tntMu, &it.tags, &tntEntityOps)
	e.setCustomName(it.custom.tag())
	return e
}

func (s *Server) boatSnapshot(it *boatEntity, q *entityQuery) *entitySnapshot {
	typ := it.entityType()
	if rejectsEntityHead(q, typ, it.uuid, it.tags) {
		return nil
	}
	b := it.boat
	if b.Dead {
		return nil
	}
	pos := player.Position{X: b.X, Y: b.Y, Z: b.Z, Yaw: b.Yaw}
	e := s.snapshot(q, it.tags, it, it.boat.ID, typ, "", it.uuid,
		s.dimensionOfWorld(it.w).name, pos, box6(b.BlastBox()))
	if e == nil {
		return nil
	}
	e.plain = itemEnglishName(it.kind.Name())
	e.bind(&s.boatsMu, &it.tags, &boatEntityOps)
	e.setCustomName(it.custom.tag())
	return e
}

// minecartSnapshot is a cart's command entity, typed and named by its kind.
func (s *Server) minecartSnapshot(it *minecartEntity, q *entityQuery) *entitySnapshot {
	typ := it.kind.Name()
	if rejectsEntityHead(q, typ, it.uuid, it.tags) {
		return nil
	}
	m := it.cart
	if m.Dead {
		return nil
	}
	pos := player.Position{X: m.X, Y: m.Y, Z: m.Z, Yaw: m.Yaw}
	e := s.snapshot(q, it.tags, it, m.ID, typ, it.kindInfo().Name, it.uuid,
		s.dimensionOfWorld(it.w).name, pos, box6(m.BlastBox()))
	if e == nil {
		return nil
	}
	e.bind(&s.minecartsMu, &it.tags, &minecartEntityOps)
	e.setCustomName(it.custom.tag())
	return e
}

func (s *Server) fallingSnapshot(it *fallingEntity, q *entityQuery) *entitySnapshot {
	typ := "minecraft:falling_block"
	if rejectsEntityHead(q, typ, it.uuid, it.tags) {
		return nil
	}
	f := it.fb
	if f.Dead {
		return nil
	}
	pos := player.Position{X: f.X, Y: f.Y, Z: f.Z}
	block := registry.BlockOf(f.BlockState).Name()
	e := s.snapshot(q, it.tags, it, it.fb.ID, typ, "Falling "+itemEnglishName(block), it.uuid, s.dimensionOfWorld(it.w).name, pos, box6(f.BlastBox()))
	if e == nil {
		return nil
	}
	e.bind(&s.fallingMu, &it.tags, &fallingEntityOps)
	// vanilla: FallingBlockEntity.getTypeName
	name := protocol.Text{Translate: "entity.minecraft.falling_block_type", With: []protocol.Text{
		{Translate: "block." + strings.ReplaceAll(block, ":", ".")},
	}}
	e.baseName = &name
	e.setCustomName(it.custom.tag())
	return e
}

func (s *Server) armorStandSnapshot(ent *armorStandEntity, q *entityQuery) *entitySnapshot {
	typ := "minecraft:armor_stand"
	if rejectsEntityHead(q, typ, ent.uuid, ent.tags) {
		return nil
	}
	stand := ent.stand
	if stand.Dead {
		return nil
	}
	pos := player.Position{X: stand.X, Y: stand.Y, Z: stand.Z, Yaw: stand.Yaw}
	e := s.snapshot(q, ent.tags, ent, stand.ID, typ, "Armor Stand", ent.uuid,
		s.dimensionOfWorld(ent.w).name, pos, box6(stand.BlastBox()))
	if e == nil {
		return nil
	}
	e.bind(&s.armorStandsMu, &ent.tags, &armorStandEntityOps)
	e.setCustomName(ent.custom.tag())
	return e
}

func (s *Server) cloudSnapshot(it *cloudEntity, q *entityQuery) *entitySnapshot {
	typ := "minecraft:area_effect_cloud"
	if rejectsEntityHead(q, typ, it.uuid, it.tags) {
		return nil
	}
	c := it.cloud
	if c.Dead {
		return nil
	}
	r := float64(c.Radius())
	pos := player.Position{X: c.X, Y: c.Y, Z: c.Z}
	e := s.snapshot(q, it.tags, it, c.ID, typ, "Area Effect Cloud", it.uuid,
		s.dimensionOfWorld(it.w).name, pos,
		box6(c.X-r, c.Y, c.Z-r, c.X+r, c.Y+0.5, c.Z+r))
	if e == nil {
		return nil
	}
	e.bind(&s.arrowsMu, &it.tags, &cloudEntityOps)
	e.setCustomName(it.custom.tag())
	return e
}

func (s *Server) fireworkSnapshot(it *fireworkEntity, q *entityQuery) *entitySnapshot {
	typ := "minecraft:firework_rocket"
	if rejectsEntityHead(q, typ, it.uuid, it.tags) {
		return nil
	}
	f := it.rocket
	if f.Dead {
		return nil
	}
	pos := player.Position{X: f.X, Y: f.Y, Z: f.Z, Yaw: f.Yaw, Pitch: f.Pitch}
	e := s.snapshot(q, it.tags, it, f.ID, typ, "Firework Rocket", it.uuid,
		s.dimensionOfWorld(it.w).name, pos,
		box6(f.X-entity.FireworkHalfWidth, f.Y, f.Z-entity.FireworkHalfWidth,
			f.X+entity.FireworkHalfWidth, f.Y+entity.FireworkHeight, f.Z+entity.FireworkHalfWidth))
	if e == nil {
		return nil
	}
	e.bind(&s.fireworksMu, &it.tags, &fireworkEntityOps)
	e.setCustomName(it.custom.tag())
	return e
}

func (s *Server) bobberSnapshot(it *bobberEntity, q *entityQuery) *entitySnapshot {
	typ := "minecraft:fishing_bobber"
	if rejectsEntityHead(q, typ, it.uuid, it.tags) {
		return nil
	}
	h := it.hook
	if h.Dead {
		return nil
	}
	pos := player.Position{X: h.X, Y: h.Y, Z: h.Z, Yaw: h.Yaw, Pitch: h.Pitch}
	e := s.snapshot(q, it.tags, it, h.ID, typ, "Fishing Bobber", it.uuid,
		s.dimensionOfWorld(it.w).name, pos,
		box6(h.X-entity.BobberHalfSize, h.Y, h.Z-entity.BobberHalfSize,
			h.X+entity.BobberHalfSize, h.Y+entity.BobberHeight, h.Z+entity.BobberHalfSize))
	if e == nil {
		return nil
	}
	e.bind(&s.bobbersMu, &it.tags, &bobberEntityOps)
	e.setCustomName(it.custom.tag())
	return e
}

func (s *Server) knotSnapshot(it *knotEntity, q *entityQuery) *entitySnapshot {
	typ := "minecraft:leash_knot"
	if rejectsEntityHead(q, typ, it.uuid, it.tags) {
		return nil
	}
	pos := player.Position{X: knotX(it), Y: knotY(it), Z: knotZ(it)}
	e := s.snapshot(q, it.tags, it, it.id, typ, "Leash Knot", it.uuid,
		s.dimensionOfWorld(it.w).name, pos, box6(knotBox(it)))
	if e == nil {
		return nil
	}
	e.bind(&s.knotsMu, &it.tags, &knotEntityOps)
	e.setCustomName(it.custom.tag())
	return e
}

func (s *Server) paintingSnapshot(it *paintingEntity, q *entityQuery) *entitySnapshot {
	typ := paintingEntityID
	if rejectsEntityHead(q, typ, it.uuid, it.tags) {
		return nil
	}
	pos := player.Position{X: paintingCentreX(it), Y: paintingCentreY(it), Z: paintingCentreZ(it)}
	e := s.snapshot(q, it.tags, it, it.id, typ, "Painting", it.uuid,
		s.dimensionOfWorld(it.w).name, pos,
		box6(it.minX, it.minY, it.minZ, it.maxX, it.maxY, it.maxZ))
	if e == nil {
		return nil
	}
	e.bind(&s.paintingsMu, &it.tags, &paintingEntityOps)
	e.setCustomName(it.custom.tag())
	return e
}

func (s *Server) frameSnapshot(it *frameEntity, q *entityQuery) *entitySnapshot {
	typ, plain := frameEntityID, "Item Frame"
	if it.glow {
		typ, plain = glowFrameEntityID, "Glow Item Frame"
	}
	if rejectsEntityHead(q, typ, it.uuid, it.tags) {
		return nil
	}
	pos := player.Position{X: frameCentreX(it), Y: frameCentreY(it), Z: frameCentreZ(it)}
	e := s.snapshot(q, it.tags, it, it.id, typ, plain, it.uuid,
		s.dimensionOfWorld(it.w).name, pos,
		box6(it.minX, it.minY, it.minZ, it.maxX, it.maxY, it.maxZ))
	if e == nil {
		return nil
	}
	e.bind(&s.framesMu, &it.tags, &frameEntityOps)
	e.setCustomName(it.custom.tag())
	return e
}

// entityDisplayName is Entity.getDisplayName: the custom name, or the type's
// translation when custom is nil, formatted for the team the UUID is on,
// with UUID insertion and a show-entity hover carrying the undecorated name.
// vanilla: Entity.getDisplayName, createHoverEvent
func entityDisplayName(board *scoreboard.Board, entityType string, identity uuid.UUID, custom *protocol.Text) protocol.Text {
	name := protocol.Text{Translate: "entity.minecraft." + strings.TrimPrefix(entityType, "minecraft:")}
	if custom != nil {
		name = *custom
		// vanilla: Entity.getName strips a custom name's click events;
		// ItemEntity.getName keeps them.
		if entityType != "minecraft:item" {
			name = name.WithoutClick()
		}
	}
	return command.EntityDisplayName(board, entityType, identity, name)
}

// uuidWords is a UUID as the show-entity hover's four big-endian ints.
func uuidWords(identity uuid.UUID) [4]uint32 {
	var words [4]uint32
	for i := range words {
		words[i] = binary.BigEndian.Uint32(identity[i*4 : (i+1)*4])
	}
	return words
}

// itemEnglishName approximates an item's en_us name for the name option:
// underscores as spaces, words capitalised.
func itemEnglishName(name string) string {
	id := strings.TrimPrefix(name, "minecraft:")
	words := strings.Split(id, "_")
	for i, word := range words {
		if word == "" {
			continue
		}
		words[i] = strings.ToUpper(word[:1]) + word[1:]
	}
	return strings.Join(words, " ")
}

// HasAttributes reports whether the entity has living attributes. vanilla: AttributeCommand.getAttributeInstance, LivingEntity
func (e *entitySnapshot) HasAttributes() bool {
	switch e.ref.(type) {
	case *mobEntity, *mannequinEntity:
		return true
	}
	return false
}

// WithAttributes runs fn on a living body's map; its next tick sends changes.
func (e *entitySnapshot) WithAttributes(fn func(a *player.Attributes)) {
	if ref, ok := e.ref.(*mannequinEntity); ok {
		e.srv.mannequinsMu.Lock()
		defer e.srv.mannequinsMu.Unlock()
		fn(ref.mannequin.Attributes)
		ref.mannequin.Health = min(ref.mannequin.Health, ref.mannequin.HealthCap())
		return
	}
	ref, ok := e.ref.(*mobEntity)
	if !ok {
		return
	}
	e.srv.mobsMu.Lock()
	defer e.srv.mobsMu.Unlock()
	m := ref.mob
	if m.Attributes() == nil {
		m.SetAttributes(m.BuildAttributes(savedAttributes(ref.saved)))
	}
	fn(m.Attributes())
	m.OnAttributeUpdated()
}

func (s *Server) teleportMobAcross(ref *mobEntity, to *dimension, at player.Position, yaw, pitch float32) (alive bool, from *world.World, oldID int32, register func(), severed []seatNews) {
	s.mobsMu.Lock()
	h := s.herdOf(ref.w)
	var twin *mobEntity
	if alive = h.book[ref.mob] == ref && !ref.mob.Dead; alive {
		if twin = s.mobTwinAt(ref, to, at); twin != nil {
			// A travelling body's ride ends: the mount stays, the passenger is set down.
			// vanilla: Entity.teleport stops the riding
			if veh := ref.mob.Vehicle; veh != nil {
				entity.Dismount(veh, ref.mob)
				severed = append(severed, mobSeatNews(ref, veh, rules.GameEventEntityDismount))
			}
			if riding := slices.Clone(ref.mob.Passengers); len(riding) > 0 {
				n := seatNews{w: ref.w, vehicle: ref.mob.ID}
				for _, p := range riding {
					entity.Dismount(ref.mob, p)
					if ent := h.book[p]; ent != nil {
						n.event(rules.GameEventEntityDismount, ref.mob.X, ref.mob.Y, ref.mob.Z, rules.GameEventContext{Mob: ent.uuid})
					}
				}
				severed = append(severed, n)
			}
			h.remove(ref.mob)
			h.forget(ref.mob)
		}
	}
	s.mobsMu.Unlock()
	if twin != nil {
		m := twin.mob
		m.Trading = false // vanilla: AbstractVillager.teleport
		m.VX, m.VY, m.VZ = 0, 0, 0
		m.Yaw, m.HeadYaw, m.Pitch = yaw, yaw, pitch
		from, oldID = ref.w, ref.mob.ID
		register = func() { s.addMob(twin) }
	}
	return
}

func (s *Server) teleportCartAcross(ref *minecartEntity, to *dimension, at player.Position, yaw, pitch float32) (alive bool, from *world.World, oldID int32, register func()) {
	var riders []*client
	var mobs []*mobEntity
	var turn float32
	s.minecartsMu.Lock()
	var twin *minecartEntity
	if alive = !ref.cart.Dead && slices.Contains(s.minecarts, ref); alive {
		if twin = s.minecartTwinAt(ref, to, at); twin != nil {
			// The husk left behind marks itself dead with its loot handed over (the
			// twin carries it), so the serving pass breaks nothing; its seat is carried
			// by the crossing below, not left on the husk, and its mobs are taken off
			// before the pass can eject them.
			ref.cart.Dead = true
			ref.cart.NoLoot = true
			ref.scoresSettled = true // the twin keeps the UUID's scores
			ref.cargo = vehicleCargo{}
			riders = slices.Clone(ref.passengers)
			mobs, ref.mobs, turn = ref.mobs, nil, yaw-ref.cart.Yaw
		}
	}
	s.minecartsMu.Unlock()
	if twin != nil {
		twin.cart.VX, twin.cart.VY, twin.cart.VZ = 0, 0, 0
		twin.cart.Yaw = yaw
		from, oldID = ref.w, ref.cart.ID
		if seam := s.minecartCrossSeam; seam != nil {
			seam()
		}
		// Riders cross first, stepping off the husk, so seating below finds them
		// in the new world with the seat free.
		// vanilla: Entity.teleportCrossDimension's passengers
		var crossed []*client
		for _, r := range riders {
			if err := r.enterDimension(to, command.Move{Pos: at}, respawnNone, false); err != nil {
				r.log.Debug("carrying a rider across a cart's teleport failed", "error", err)
				continue
			}
			// A rider already mid-crossing is carried by that crossing, not this cart;
			// enterDimension left them in place, so they take no seat here.
			if r.dimension() != to {
				continue
			}
			crossed = append(crossed, r)
		}
		twin.boarding = true
		register = func() {
			s.registerMinecart(twin)
			if seam := s.minecartBoardSeam; seam != nil {
				seam()
			}
			for _, r := range crossed {
				s.mountMinecart(r, twin, false)
			}
			s.reseatMinecartMobs(mobs, ref, twin, turn)
			s.minecartsMu.Lock()
			twin.boarding = false
			s.minecartsMu.Unlock()
		}
	}
	return
}

// otherLootEntity is a non-mob entity as a loot predicate reads it: its type,
// and a bolt's or a bobber's own facts.
func (e *entitySnapshot) otherLootEntity() rules.LootEntity {
	le := rules.LootEntity{Type: e.EntityType()}
	switch body := e.ref.(type) {
	case *boltEntity:
		e.srv.boltMu.Lock()
		fires := body.fires
		hit := make([]int32, 0, len(body.hit))
		for id := range body.hit {
			hit = append(hit, id)
		}
		e.srv.boltMu.Unlock()
		le.Lightning = &rules.LootLightning{BlocksSetOnFire: fires, Struck: func() []rules.LootEntity {
			return e.srv.struckLootEntities(body.w, hit)
		}}
	case *bobberEntity:
		e.srv.bobbersMu.Lock()
		le.FishingHook = &rules.LootFishingHook{OpenWater: body.hook.OpenWater}
		e.srv.bobbersMu.Unlock()
	}
	return le
}

// struckLootEntities are the players and mobs of w with the entity IDs a bolt
// hurt, as loot predicates read them.
// vanilla: LightningBolt.getHitEntities
func (s *Server) struckLootEntities(w *world.World, ids []int32) []rules.LootEntity {
	var bodies []command.Entity
	s.mu.RLock()
	for _, c := range s.clients {
		if c.theWorld() == w && slices.Contains(ids, c.player.EntityID) {
			bodies = append(bodies, c)
		}
	}
	s.mu.RUnlock()
	dim := s.dimensionOfWorld(w)
	s.mobsMu.Lock()
	for _, ent := range dim.mobs.book {
		if slices.Contains(ids, ent.mob.ID) {
			if snap := s.mobSnapshot(ent, dim.name, nil); snap != nil {
				bodies = append(bodies, snap)
			}
		}
	}
	s.mobsMu.Unlock()
	out := make([]rules.LootEntity, len(bodies))
	for i, b := range bodies {
		out[i] = command.LootEntityOf(b)
	}
	return out
}
