package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// heldSpawnEgg reads a held spawn egg's entity type, its entity_data's id
// first; simulated is false for a type with no mob here or one no egg may
// spawn.
// vanilla: SpawnEggItem.getType
func heldSpawnEgg(stack protocol.ItemStack) (kind entity.MobType, simulated, egg bool) {
	if stack.Empty() {
		return entity.NoMobType, false, false
	}
	typ, ok := registry.SpawnEggEntity(registry.Item(stack.ItemID))
	if !ok {
		return entity.NoMobType, false, false
	}
	if stack.Removes(protocol.DataComponentEntityData) {
		return entity.NoMobType, false, true
	}
	if id, has := itemEntityType(stack); has {
		if id < 0 || int(id) >= len(registry.EntityTypes) {
			return entity.NoMobType, false, true
		}
		typ = registry.EntityTypes[id]
	}
	return entity.MobTypeOf(typ), entity.SpawnableKind(entity.MobTypeOf(typ)), true
}

// useSpawnEgg is SpawnEggItem.useOn. It returns false when no spawn egg is
// held; every egg click is answered here.
func (c *client) useSpawnEgg(hand, x, y, z, face int32) bool {
	held := c.heldItem(hand)
	kind, simulated, egg := heldSpawnEgg(held)
	if !egg {
		return false
	}
	// useOn's first check, before reading the block: an unsimulated kind, one
	// no egg may spawn, or one canSpawn refuses (a monster in peaceful) fails
	// silently and keeps the egg.
	if !simulated || !entity.AllowedAt(kind, c.theWorld().Difficulty()) {
		c.useSwing = swingNone
		return true
	}
	w := c.theWorld()
	block := registry.BlockOf(w.BlockAt(x, y, z))
	if block == registry.BlockSpawner || block == registry.BlockTrialSpawner {
		// A Spawner block entity: with the game rule off, vanilla's refusal
		// message.
		if !c.theWorld().BoolRule(world.RuleSpawnerBlocks) {
			c.useSwing = swingNone
			c.send(protocol.IDPlaySystemChat,
				systemChatPayload(protocol.Text{Translate: "advMode.notEnabled.spawner"}, false))
			return true
		}
		// Only an op's egg sets the whole spawn data.
		var data nbt.Compound
		if c.PermLevel() >= command.PermLevelModerators {
			if typ, ok := registry.EntityTypeID(kind.Name()); ok {
				data = itemEntityData(held, typ)
			}
		}
		c.setSpawnerEntity(hand, kind, data, w, x, y, z, block == registry.BlockTrialSpawner)
		return true
	}
	width, _ := entity.KindDims(kind)
	px, py, pz, cx, cy, cz := rules.SpawnEggPosition(w, x, y, z, face, width/2)
	c.spawnFromEgg(hand, held, kind, px, py, pz, cx, cy, cz)
	return true
}

// setSpawnerEntity is the rest of useOn's Spawner branch: setEntityId sets
// the next spawn's type, the block update and block entity data go to
// clients, and one egg is spent. data is an op egg's entity_data, merged into
// the spawn data. A trial spawner takes the type into its
// configs and goes inactive, sending its reset data's empty update tag.
func (c *client) setSpawnerEntity(hand int32, mob entity.MobType, data nbt.Compound, w *world.World, x, y, z int32, trial bool) {
	if !trial {
		be := w.EnsureBlockEntity(x, y, z, world.KindSpawner)
		if be == nil {
			return
		}
		be.Use(func(b *world.BlockEntity) {
			b.SetSpawnEntityID(mob.Name())
			if tag, ok := nbt.Get[nbt.CompoundTag](b.SpawnData.Entries(), "entity"); ok {
				forEntityDataKeys(data, tag.Set)
			}
		})
	} else {
		// vanilla: TrialSpawner.overrideEntityToSpawn
		stored, _ := w.BlockEntityTag(x, y, z)
		w.SetBlockEntityTag(x, y, z, "minecraft:trial_spawner", entity.OverrideTrialEntity(stored, mob, data))
		if next, ok := registry.WithProp(w.BlockAt(x, y, z), registry.PropTrialSpawnerState, "inactive"); ok {
			w.SetBlock(x, y, z, next)
		}
	}
	c.srv.markChanged(w, x, y, z)
	c.awardUsedHeld(hand)
	c.spendEgg(hand)
}

// blockEntityData builds a Block Entity Data payload.
func blockEntityData(x, y, z int32, beType int32, data nbt.Compound) []byte {
	payload, err := nbt.EncodeNetwork(data)
	if err != nil {
		return nil
	}
	var b protocol.Buffer
	b.PutPosition(x, y, z)
	b.PutVarInt(beType)
	b.PutRaw(payload)
	return b.Bytes()
}

// handleUseSpawnEgg is SpawnEggItem.use: used in the air, an egg spawns
// into the water or lava source looked at.
func (c *client) handleUseSpawnEgg(hand int32) bool {
	if c.player.Vitals().Dead {
		return false
	}
	mode := c.player.GameMode()
	held := c.heldItem(hand)
	kind, simulated, egg := heldSpawnEgg(held)
	if !egg || !simulated || !entity.AllowedAt(kind, c.theWorld().Difficulty()) {
		return false
	}
	pos := c.player.Position()
	reach := c.blockRange()
	dx, dy, dz := entity.LookVector(pos.Yaw, pos.Pitch)
	x, y, z, face, liquid, hit := c.theWorld().ClipSourceFluid(pos.X,
		pos.Y+c.dims(c.player.Appearance().Pose).EyeHeight, pos.Z, dx, dy, dz, reach)
	if !hit || !liquid {
		return false
	}
	// mayUseItemAt checks the block behind the face the ray entered the liquid
	// through. vanilla: SpawnEggItem.use, Player.mayUseItemAt
	bx, by, bz := faceOffset(x, y, z, oppositeFace(face))
	if !rules.MayUseItemAt(mode, held, adventureTarget(c.theWorld(), bx, by, bz, c.theWorld().BlockAt(bx, by, bz))) {
		return false
	}
	c.spawnFromEgg(hand, held, kind, float64(x)+0.5, float64(y), float64(z)+0.5, x, y, z)
	return true
}

// spawnFromEgg is EntityType.spawn with the egg, then spends one egg
// outside creative. The event goes at the resolved spawn cell (cx, cy, cz),
// not the spawn point, which SpawnYOffset may move.
func (c *client) spawnFromEgg(hand int32, egg protocol.ItemStack, kind entity.MobType, x, y, z float64, cx, cy, cz int32) {
	c.srv.standEggMob(c.dimension(), egg, kind, x, y, z, plugin.SpawnSpawnerEgg)
	c.gameEventAtCell(rules.GameEventEntityPlace, cx, cy, cz)
	// vanilla: SpawnEggItem.useOn, SpawnEggItem.use
	c.awardItemStat(registry.StatTypeUsed, egg.ItemID, 1)
	c.spendEgg(hand)
}

// standEggMob is EntityType.spawn with the egg: random facing, finalize,
// the egg's custom name, ambient sound.
func (s *Server) standEggMob(dim *dimension, egg protocol.ItemStack, kind entity.MobType, x, y, z float64, reason plugin.SpawnReason) {
	var sound registry.Sound
	var volume, pitch float32
	var identity uuid.UUID
	var data nbt.Compound
	if typ, ok := registry.EntityTypeID(kind.Name()); ok {
		data = itemEntityData(egg, typ)
	}
	s.spawnMobArriving(dim, kind, x, y, z, 0, nil, false, "", true, func(ent *mobEntity) {
		m := ent.mob
		identity = ent.uuid
		m.Yaw = s.levelRng.Float32() * 360
		if m.Yaw >= 180 {
			m.Yaw -= 360
		}
		m.HeadYaw = m.Yaw
		m.SeedBodyYaw(m.Yaw)
		applyEggComponents(m, egg)
		if egg.CustomName != nil {
			ent.name = protocol.TextTag(*egg.CustomName)
			m.Named = true
		}
		if data != nil {
			tag := mobTag(ent)
			forEntityDataKeys(data, func(k string, v nbt.Tag) { tag[k] = v })
			applyMobTag(m, ent, tag)
		}
		ent.spawnReason = spawnReasonCode(reason)
		// Mob.makeSound plays nothing for a kind without an ambient event.
		if entity.MobSoundsOf(kind).Ambient != registry.SoundNone {
			sound, volume, pitch = m.AmbientVoice()
		}
	})
	s.broadcastVoiceID(dim.w, kind, false, x, y, z, sound, volume, pitch)
	// vanilla: EntityType.updateCustomEntityTag, Entity.load
	if data != nil && identity != uuid.Nil {
		s.joinTagTeam(tagTeam{identity: identity, tag: data})
	}
}

// eggComponent applies one component an egg carries to the mob it hatches.
type eggComponent struct {
	component int32
	apply     func(m *entity.Mob, egg protocol.ItemStack)
}

// variantRow applies a variant component through its setter, by the entry's
// id (an enum) or name (a colour or a registry holder).
func variantRow(component int32, set func(m *entity.Mob, id int32, name string)) eggComponent {
	return eggComponent{component, func(m *entity.Mob, egg protocol.ItemStack) {
		if id, name, ok := registry.StackVariant(egg, component); ok {
			set(m, id, name)
		}
	}}
}

// eggComponents is Entity.applyComponentsFromItemStack: each setter ignores a
// kind that does not read the component.
// vanilla: Entity.applyImplicitComponent and its overrides
var eggComponents = [...]eggComponent{
	{protocol.DataComponentCustomData, func(m *entity.Mob, egg protocol.ItemStack) {
		if data := egg.CustomData(); data != nil {
			m.SetCustomData(data)
		}
	}},
	variantRow(protocol.DataComponentWolfVariant, func(m *entity.Mob, _ int32, n string) { m.SetVariantEntry(entity.TypeWolf, n) }),
	variantRow(protocol.DataComponentWolfSoundVariant, func(m *entity.Mob, _ int32, n string) { m.SetSoundVariantEntry(entity.TypeWolf, n) }),
	variantRow(protocol.DataComponentWolfCollar, func(m *entity.Mob, id int32, _ string) {
		m.SetCollarColour(registry.DyeNames[id], entity.TypeWolf)
	}),
	variantRow(protocol.DataComponentCatVariant, func(m *entity.Mob, _ int32, n string) { m.SetVariantEntry(entity.TypeCat, n) }),
	variantRow(protocol.DataComponentCatSoundVariant, func(m *entity.Mob, _ int32, n string) { m.SetSoundVariantEntry(entity.TypeCat, n) }),
	variantRow(protocol.DataComponentCatCollar, func(m *entity.Mob, id int32, _ string) {
		m.SetCollarColour(registry.DyeNames[id], entity.TypeCat)
	}),
	variantRow(protocol.DataComponentSheepColor, func(m *entity.Mob, _ int32, n string) { m.SetSheepColour(n) }),
	variantRow(protocol.DataComponentShulkerColor, func(m *entity.Mob, id int32, _ string) { m.SetShulkerDye(uint8(id)) }),
	variantRow(protocol.DataComponentFoxVariant, func(m *entity.Mob, id int32, _ string) { m.SetFoxVariant(id) }),
	variantRow(protocol.DataComponentParrotVariant, func(m *entity.Mob, id int32, _ string) {
		if m.Type == entity.TypeParrot {
			m.SetParrotVariant(int(id))
		}
	}),
	variantRow(protocol.DataComponentMooshroomVariant, func(m *entity.Mob, _ int32, n string) { m.SetMooshroomVariant(n) }),
	variantRow(protocol.DataComponentHorseVariant, func(m *entity.Mob, id int32, _ string) { m.SetHorseColour(id) }),
	variantRow(protocol.DataComponentLlamaVariant, func(m *entity.Mob, id int32, _ string) { m.SetLlamaVariant(id) }),
	variantRow(protocol.DataComponentPigVariant, func(m *entity.Mob, _ int32, n string) { m.SetVariantEntry(entity.TypePig, n) }),
	variantRow(protocol.DataComponentPigSoundVariant, func(m *entity.Mob, _ int32, n string) { m.SetSoundVariantEntry(entity.TypePig, n) }),
	variantRow(protocol.DataComponentCowVariant, func(m *entity.Mob, _ int32, n string) { m.SetVariantEntry(entity.TypeCow, n) }),
	variantRow(protocol.DataComponentCowSoundVariant, func(m *entity.Mob, _ int32, n string) { m.SetSoundVariantEntry(entity.TypeCow, n) }),
	variantRow(protocol.DataComponentChickenVariant, func(m *entity.Mob, _ int32, n string) { m.SetVariantEntry(entity.TypeChicken, n) }),
	variantRow(protocol.DataComponentChickenSoundVariant, func(m *entity.Mob, _ int32, n string) { m.SetSoundVariantEntry(entity.TypeChicken, n) }),
	variantRow(protocol.DataComponentFrogVariant, func(m *entity.Mob, _ int32, n string) { m.SetVariantEntry(entity.TypeFrog, n) }),
	variantRow(protocol.DataComponentZombieNautilusVariant, func(m *entity.Mob, _ int32, n string) {
		m.SetVariantEntry(entity.TypeZombieNautilus, n)
	}),
	variantRow(protocol.DataComponentVillagerVariant, func(m *entity.Mob, _ int32, n string) { m.SetVillagerVariant(n) }),
	variantRow(protocol.DataComponentAxolotlVariant, func(m *entity.Mob, id int32, _ string) {
		if m.Type == entity.TypeAxolotl {
			m.SetAxolotlVariant(int(id))
		}
	}),
	variantRow(protocol.DataComponentSalmonSize, func(m *entity.Mob, id int32, _ string) { m.SetSalmonVariant(int(id)) }),
	variantRow(protocol.DataComponentTropicalFishPattern, func(m *entity.Mob, id int32, _ string) { m.SetTropicalPart(0xFFFF, 0, id) }),
	variantRow(protocol.DataComponentTropicalFishBaseColor, func(m *entity.Mob, id int32, _ string) { m.SetTropicalPart(0xFF, 16, id) }),
	variantRow(protocol.DataComponentTropicalFishPatternColor, func(m *entity.Mob, id int32, _ string) { m.SetTropicalPart(0xFF, 24, id) }),
	variantRow(protocol.DataComponentRabbitVariant, func(m *entity.Mob, id int32, _ string) { m.SetRabbitVariant(id) }),
}

// applyEggComponents is Entity.applyComponentsFromItemStack for an egg.
func applyEggComponents(m *entity.Mob, egg protocol.ItemStack) {
	for _, row := range eggComponents {
		row.apply(m, egg)
	}
}

// spawnEggOnMob is SpawnEggItem.spawnOffspringFromSpawnEgg (from
// Mob.interact): an egg of the mob's kind makes a baby at the parent. It
// reports whether one was made.
func (c *client) spawnEggOnMob(ent *mobEntity, hand int32) bool {
	held := c.heldItem(hand)
	kind, simulated, egg := heldSpawnEgg(held)
	if !egg || !simulated || kind != ent.mob.Type || !entity.AllowedAt(kind, c.theWorld().Difficulty()) {
		return false
	}
	s := c.srv
	parent := ent.mob
	s.mobsMu.Lock()
	ok := !parent.Dead && parent.TakesBabyEgg()
	x, y, z := parent.X, parent.Y, parent.Z
	s.mobsMu.Unlock()
	if !ok {
		return false
	}
	s.spawnMobArriving(c.dimension(), kind, x, y, z, 0, nil, true, "", false, func(ent *mobEntity) {
		ent.spawnReason = spawnReasonCode(plugin.SpawnSpawnerEgg)
		s.mobsMu.Lock()
		parent.InheritInto(ent.mob)
		applyEggComponents(ent.mob, held)
		if kind == entity.TypeFox {
			// vanilla: Fox.onOffspringSpawnedFromEgg
			ent.mob.FoxTrust(c.player.UUID)
		}
		s.mobsMu.Unlock()
		if held.CustomName != nil {
			ent.name = protocol.TextTag(*held.CustomName)
			ent.mob.Named = true
		}
	})
	c.spendEgg(hand)
	// InteractionResult.SUCCESS_SERVER: the server swings the arm.
	c.useSwing = swingServer
	return true
}

// spendEgg is ItemStack.consume(1, player): creative keeps its egg.
func (c *client) spendEgg(hand int32) {
	if c.player.GameMode() == protocol.GameModeCreative {
		return
	}
	if err := c.consumeHeld(hand); err != nil {
		c.log.Debug("spending a spawn egg failed", "error", err)
	}
}
