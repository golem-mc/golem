package server

import (
	"github.com/golem-mc/golem/plugin"
	"maps"
	"slices"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// useBeehive harvests a full hive with shears (three honeycomb, one wear)
// or a glass bottle (honey bottle), resetting honey to 0. Without a lit
// campfire below, the bees come out at the player and nearby bees anger.
// vanilla: BeehiveBlock.useItemOn
func (c *client) useBeehive(hand, x, y, z int32) bool {
	held := c.heldItem(hand)
	if held.Empty() {
		return false
	}
	shear := held.ItemID == int32(registry.ItemShears)
	if !shear && held.ItemID != int32(registry.ItemGlassBottle) {
		return false
	}
	w := c.theWorld()
	state, full := rules.FullHiveAt(w, x, y, z)
	if !full {
		return false
	}
	pos := c.player.Position()
	// vanilla: BeehiveBlock.useItemOn
	c.awardUsedHeld(hand)
	if shear {
		c.srv.dropHiveHarvest(w, x, y, z, state, held, c.LootEntity())
		c.srv.broadcastSoundID(w, registry.SoundBlockBeehiveShear,
			protocol.SoundCategoryBlock, pos.X, pos.Y, pos.Z, 1.0, 1.0)
		c.gameEventAtCell(rules.GameEventShear, x, y, z)
		c.wearHeld(hand, 1)
	} else {
		honey := int32(registry.ItemHoneyBottle)
		// Unlike BottleItem, the creative player's bottle is spent too.
		bottle := protocol.ItemStack{ItemID: honey, Count: 1}
		if held.Count <= 1 {
			c.replaceHeldWith(hand, bottle)
		} else {
			_ = c.consumeHeld(hand)
			c.takeResult(bottle)
		}
		c.srv.broadcastSoundIDExcept(w, c, registry.SoundItemBottleFill,
			protocol.SoundCategoryBlock, pos.X, pos.Y, pos.Z, 1.0, 1.0)
		c.gameEventAtCell(rules.GameEventFluidPickup, x, y, z)
	}
	hive := w.BlockAt(x, y, z)
	c.srv.emptyHive(w, x, y, z, state)
	if !rules.HiveSedated(w, x, y, z) {
		if len(hiveBees(w, x, y, z).Items) > 0 {
			c.srv.angerBeesAround(w, x, y, z)
		}
		c.srv.releaseHiveBees(w, x, y, z, hive, c, true, true)
	}
	return true
}

// hiveOccupantIgnored are the tags a released occupant does not keep.
// vanilla: BeehiveBlockEntity.IGNORED_BEE_TAGS
var hiveOccupantIgnored = []string{"Air", "drop_chances", "equipment", "Brain", "CanPickUpLoot", "DeathTime", "fall_distance", "FallFlying", "Fire", "HurtTime", "LeftHanded", "Motion", "NoGravity", "OnGround", "PortalCooldown", "Pos", "Rotation", "sleeping_pos", "CannotEnterHiveTicks", "TicksSincePollination", "CropsGrownSincePollination", "hive_pos", "Passengers", "leash", "UUID"}

// hiveBees is the occupant list the hive at the cell stores.
func hiveBees(w *world.World, x, y, z int32) nbt.List {
	tag, ok := w.BlockEntityTag(x, y, z)
	if !ok {
		return nbt.List{}
	}
	list, _ := nbt.Get[nbt.List](tag, "bees")
	return list
}

// collectHive reads a hive about to break and returns a func freeing its
// bees once the cell is air, nil if none are freed (creative, or a tool
// with prevents_bee_spawns_when_mining). Either way a survival break fires
// bee_nest_destroyed.
// vanilla: BeehiveBlock.playerDestroy
func (c *client) collectHive(x, y, z int32, state uint16) func() {
	block := registry.BlockOf(state)
	if !block.InTag(registry.TagIDBeehives) ||
		c.player.GameMode() == protocol.GameModeCreative {
		return nil
	}
	held := c.heldItem(protocol.HandMain)
	c.beeNestDestroyedAdvancements(block.Name(), held.Enchantments, len(hiveBees(c.theWorld(), x, y, z).Items))
	for id := range held.Enchantments {
		if registry.EnchantmentInTag(id, "minecraft:prevents_bee_spawns_when_mining") {
			return nil
		}
	}
	w := c.theWorld()
	occupants := hiveBees(w, x, y, z)
	sedated := rules.HiveSedated(w, x, y, z)
	return func() {
		c.srv.spawnHiveBees(w, x, y, z, state, occupants, c, sedated, true)
		c.srv.angerBeesAround(w, x, y, z)
	}
}

// releaseHiveBees frees the stored bees and, with forget, removes them
// from the block entity. emergency frees them regardless of time, weather
// and a blocked front. vanilla: BeehiveBlockEntity.emptyAllLivingFromHive
func (s *Server) releaseHiveBees(w *world.World, x, y, z int32, state uint16, by *client, forget, emergency bool) {
	list := hiveBees(w, x, y, z)
	if len(list.Items) == 0 {
		return
	}
	kept := s.spawnHiveBees(w, x, y, z, state, list, by, rules.HiveSedated(w, x, y, z), emergency)
	if !forget {
		return
	}
	tag, _ := w.BlockEntityTag(x, y, z)
	next := maps.Clone(tag)
	next["bees"] = nbt.List{Elem: nbt.TypeCompound, Items: kept}
	w.ReplaceCarried(x, y, z, next)
}

// releaseHiveAndResetHoney empties a full hive and frees its bees with no
// player to anger them. vanilla: BeehiveBlock.releaseBeesAndResetHoneyLevel
func (s *Server) releaseHiveAndResetHoney(w *world.World, x, y, z int32, state uint16) {
	s.emptyHive(w, x, y, z, state)
	s.releaseHiveBees(w, x, y, z, w.BlockAt(x, y, z), nil, true, false)
}

// spawnHiveBees spawns stored bees at the hive's front and returns those
// that could not leave: without emergency, none leave while bees stay in
// the hive or the front block has collision. A bee within 4 blocks of the player attacks unless
// sedated. vanilla: BeehiveBlockEntity.releaseOccupant, emptyAllLivingFromHive
func (s *Server) spawnHiveBees(w *world.World, x, y, z int32, state uint16, list nbt.List, by *client, sedated, emergency bool) []nbt.Tag {
	dx, dz := rules.HiveFacingStep(registry.StateValue(state, registry.PropFacing))
	frontBlocked := len(world.CollisionBoxes(w.BlockAt(x+dx, y, z+dz))) > 0
	if !emergency && (frontBlocked || w.BeesStayInHive()) {
		return list.Items
	}
	var kept []nbt.Tag
	for _, item := range list.Items {
		if !s.spawnOccupant(w, x, y, z, dx, dz, frontBlocked, item, false, func(e *mobEntity) {
			if by == nil || sedated {
				return
			}
			p := by.player.Position()
			if ddx, ddy, ddz := p.X-e.mob.X, p.Y-e.mob.Y, p.Z-e.mob.Z; ddx*ddx+ddy*ddy+ddz*ddz <= 16 {
				e.mob.SetMetadataTargetID(by.player.EntityID)
			}
		}) {
			kept = append(kept, item)
		}
	}
	return kept
}

// spawnOccupant spawns one stored bee at the hive's front, reporting
// whether it left; a nectar-carrying bee delivers it.
// vanilla: BeehiveBlockEntity.releaseOccupant, Occupant.createEntity
func (s *Server) spawnOccupant(w *world.World, x, y, z, dx, dz int32, frontBlocked bool, item nbt.Tag, delivered bool, prep func(*mobEntity)) bool {
	occupant, ok := nbt.CompoundView(item)
	data, hasData := nbt.Get[nbt.Compound](occupant, "entity_data")
	id, _ := nbt.Get[nbt.String](data, "id")
	if !ok || !hasData || id != nbt.String(entity.TypeBee.Name()) {
		return false
	}
	tag := maps.Clone(data)
	for _, key := range hiveOccupantIgnored {
		delete(tag, key)
	}
	if delivered {
		tag["HasNectar"] = nbt.Byte(0)
		delete(tag, "CropsGrownSincePollination")
	}
	tag["hive_pos"] = nbt.IntArray{x, y, z}
	// vanilla: BeehiveBlockEntity.releaseOccupant
	if hiveTag, _ := w.BlockEntityTag(x, y, z); hiveTag != nil {
		if flower, ok := hiveTag["flower_pos"].(nbt.IntArray); ok && tag["flower_pos"] == nil && s.levelRng.Float32() < 0.9 {
			tag["flower_pos"] = flower
		}
	}
	// vanilla: Occupant.updateBeeAge: hive time ages a baby and counts an
	// adult's breeding delay down
	ticks, _ := nbt.IntValue(occupant["ticks_in_hive"])
	if !nbt.BoolField(tag, "AgeLocked") {
		if age, ok := nbt.IntField(tag, "Age"); ok && age < 0 {
			tag["Age"] = nbt.Int(min(0, age+ticks))
		} else if ok && age > 0 {
			tag["Age"] = nbt.Int(max(0, age-ticks))
		}
	}
	if love, ok := nbt.IntField(tag, "InLove"); ok {
		tag["InLove"] = nbt.Int(max(0, love-ticks))
	}
	px, py, pz := rules.HiveReleaseSpot(x, y, z, dx, dz, frontBlocked, 0.55, 0.5)
	if _, err := s.spawnSummonedMobFacing(s.dimensionOfWorld(w), entity.TypeBee, px, py, pz, tag, prep); err != nil {
		return false
	}
	s.broadcastSoundID(w, registry.SoundBlockBeehiveExit, protocol.SoundCategoryBlock,
		float64(x)+0.5, float64(y)+0.5, float64(z)+0.5, 1.0, 1.0)
	return true
}

// angerBeesAround sends every targetless bee near the hive after a random
// nearby non-spectator. vanilla: BeehiveBlock.angerNearbyBees
func (s *Server) angerBeesAround(w *world.World, x, y, z int32) {
	minX, maxX := float64(x)-8, float64(x)+9
	minY, maxY := float64(y)-6, float64(y)+7
	minZ, maxZ := float64(z)-8, float64(z)+9
	s.mu.RLock()
	var players []int32
	for _, other := range s.clients {
		if other.theWorld() != w || other.player.GameMode() == protocol.GameModeSpectator {
			continue
		}
		b := other.bodyBox()
		if b[3] > minX && b[0] < maxX && b[4] > minY && b[1] < maxY && b[5] > minZ && b[2] < maxZ {
			players = append(players, other.player.EntityID)
		}
	}
	s.mu.RUnlock()
	if len(players) == 0 {
		return
	}
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	for m := range s.herdOf(w).book {
		if m.Type != entity.TypeBee || m.TargetID != 0 {
			continue
		}
		if m.X+0.275 > minX && m.X-0.275 < maxX && m.Y+0.5 > minY && m.Y < maxY && m.Z+0.275 > minZ && m.Z-0.275 < maxZ {
			m.SetMetadataTargetID(players[s.levelRng.Intn(len(players))])
		}
	}
}

// hiveStay is a bee that reached its hive, with the tag of its body.
type hiveStay struct {
	w       *world.World
	x, y, z int32
	tag     nbt.Compound
}

// storeHiveBee puts the bee into the hive's list, at tick 0 of its stay.
// vanilla: BeehiveBlockEntity.addOccupant, Occupant.of
func (s *Server) storeHiveBee(stay hiveStay) {
	data := maps.Clone(stay.tag)
	for _, key := range hiveOccupantIgnored {
		delete(data, key)
	}
	minTicks := rules.HiveMinTicksNectarless
	if nbt.BoolField(data, "HasNectar") {
		minTicks = rules.HiveMinTicksNectar
	}
	occupant := nbt.Compound{
		"entity_data":       data,
		"ticks_in_hive":     nbt.Int(0),
		"min_ticks_in_hive": nbt.Int(minTicks),
	}
	tag, _ := stay.w.BlockEntityTag(stay.x, stay.y, stay.z)
	next := maps.Clone(tag)
	if next == nil {
		next = nbt.Compound{}
	}
	// vanilla: BeehiveBlockEntity.addOccupant
	if flower, ok := data["flower_pos"].(nbt.IntArray); ok && (next["flower_pos"] == nil || s.levelRng.Intn(2) == 0) {
		next["flower_pos"] = flower
	}
	list, _ := nbt.Get[nbt.List](next, "bees")
	next["bees"] = nbt.List{Elem: nbt.TypeCompound, Items: append(slices.Clone(list.Items), occupant)}
	stay.w.SetBlockEntityTag(stay.x, stay.y, stay.z, "minecraft:beehive", next)
	s.broadcastSoundID(stay.w, registry.SoundBlockBeehiveEnter, protocol.SoundCategoryBlock,
		float64(stay.x)+0.5, float64(stay.y)+0.5, float64(stay.z)+0.5, 1.0, 1.0)
	s.emptyHiveNearFire(stay.w, stay.x, stay.y, stay.z)
}

// emptyHiveNearFire frees every bee of a hive beside fire, regardless of
// time or weather; vanilla runs it whenever the block entity is marked
// changed. vanilla: BeehiveBlockEntity.setChanged, BeeReleaseStatus.EMERGENCY
func (s *Server) emptyHiveNearFire(w *world.World, x, y, z int32) {
	if !rules.HiveFireNearby(w, x, y, z) {
		return
	}
	if state := w.BlockAt(x, y, z); isHiveState(state) {
		s.releaseHiveBees(w, x, y, z, state, nil, true, true)
	}
}

// emptyHivesBesideFire frees the bees of every hive next to a fire block
// just set at the cell. vanilla: BeehiveBlock.updateShape
func (s *Server) emptyHivesBesideFire(w *world.World, x, y, z int32) {
	if registry.BlockOf(w.BlockAt(x, y, z)) != registry.BlockFire {
		return
	}
	for _, d := range [6][3]int32{{1, 0, 0}, {-1, 0, 0}, {0, 1, 0}, {0, -1, 0}, {0, 0, 1}, {0, 0, -1}} {
		if nx, ny, nz := x+d[0], y+d[1], z+d[2]; isHiveState(w.BlockAt(nx, ny, nz)) {
			s.releaseHiveBees(w, nx, ny, nz, w.BlockAt(nx, ny, nz), nil, true, true)
		}
	}
}

// hiveBlasters are the entities whose blast frees a hive's bees as it breaks.
// vanilla: BeehiveBlock.getDrops
var hiveBlasters = map[string]bool{
	"minecraft:tnt": true, "minecraft:creeper": true, "minecraft:wither_skull": true,
	"minecraft:wither": true, string(entity.MinecartTNT): true,
}

func isHiveState(state uint16) bool {
	return registry.BlockOf(state).InTag(registry.TagIDBeehives)
}

// cropGrow is a crop a bee grew one age.
type cropGrow struct {
	w       *world.World
	x, y, z int32
	state   uint16
}

// growBeeCrop sets the grown crop and sparkles over it.
// vanilla: Bee.BeeGrowCropGoal.tick
func (s *Server) growBeeCrop(g cropGrow) {
	s.broadcastLevelEvent(g.w, protocol.WorldEventBeeGrowth, g.x, g.y, g.z, 15)
	s.setBlockAndTell(g.w, g.x, g.y, g.z, g.state)
}

// enteredHive stores a bee in its hive if there is room, counting stays
// not yet stored this tick.
func (t *mobDimTick) enteredHive(fx *mobEffects, ev entity.MobEvent) {
	h, w := t.h, t.w
	// The bee leaves the world like a despawn; the store runs after the locks.
	ent, ok := h.book[ev.Mob]
	x, y, z := int32(ev.X), int32(ev.Y), int32(ev.Z)
	n, hive := w.BeehiveOccupants(x, y, z)
	// The stays of this tick are not stored yet; they count.
	for _, pending := range fx.hiveStays {
		if pending.w == w && pending.x == x && pending.y == y && pending.z == z {
			n++
		}
	}
	if ok && hive && n < rules.HiveMaxOccupants {
		// vanilla: BeehiveBlockEntity.addOccupant (bee.dropLeash)
		if holder := ent.mob.RemoveLeash(); holder != 0 {
			fx.leashDrops = append(fx.leashDrops, mobLeashDrop{w: w, mobID: ev.Mob.ID, holder: holder,
				x: ent.mob.X, y: ent.mob.Y, z: ent.mob.Z, laid: true})
		}
		fx.hiveStays = append(fx.hiveStays, hiveStay{w: w, x: x, y: y, z: z, tag: nbt.PlainCopy(mobTagIn(ent, h.book)).(nbt.Compound)})
		h.forget(ev.Mob)
		h.remove(ent.mob)
		fx.scoreDrops = append(fx.scoreDrops, goneMob{ent.uuid, plugin.RemoveDiscard})
		fx.release(ent)
		fx.remove(w, ev.Mob.ID)
	}
}

// dropHiveHarvest rolls the hive's harvest before its honey level resets.
// vanilla: BeehiveBlock.dropHoneycomb
func (s *Server) dropHiveHarvest(w *world.World, x, y, z int32, state uint16, tool protocol.ItemStack, interacting rules.LootEntity) {
	ctx := s.blockInteractLootContext(w, x, y, z, state, tool, interacting)
	for _, stack := range rules.RollLoot("minecraft:harvest/beehive", &ctx, s.lootRng) {
		s.spawnBlockDrop(w, x, y, z, stack)
	}
}
