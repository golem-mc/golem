package server

import (
	"reflect"
	"slices"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/effect"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/pluginruntime"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// deathKeeps is what a death decided to keep, read once by the respawn.
type deathKeeps struct{ inventory, level bool }

// takeDeathKeeps is the death's keep decision, the game rule when no death
// recorded one (a dead body that rejoined).
func (c *client) takeDeathKeeps() deathKeeps {
	if k := c.deathKeeps.Swap(nil); k != nil {
		return *k
	}
	rule := c.theWorld().BoolRule(world.RuleKeepInventory)
	return deathKeeps{inventory: rule, level: rule}
}

// pluginSource is the event's view of a hurt's cause: entity handles bound to
// the listener's host when the event fires (bindPluginEntity).
func (s *Server) pluginSource(w *world.World, damageType registry.DamageType, cause *damageCause) plugin.DamageSource {
	src := plugin.DamageSource{Type: damageType}
	if cause == nil {
		return src
	}
	handle := func(id int32) plugin.Entity {
		if e := s.relatedByID(w, id); e != nil {
			return pluginEntity{id: e.UUID()}
		}
		return nil
	}
	if !cause.posOnly {
		src.Causing = handle(cause.entityID)
		if cause.directID != 0 {
			src.Direct = handle(cause.directID - 1)
		} else {
			src.Direct = src.Causing
		}
		// A creeper or a primed TNT is gone before its blast hurts.
		if x := cause.exploder; x != nil {
			if src.Direct == nil {
				src.Direct = x.handle()
			}
			if src.Causing == nil && x.isCause {
				src.Causing = x.handle()
			}
		}
	}
	if cause.posOnly || cause.eventPos {
		src.Pos = &plugin.Vec3{X: cause.x, Y: cause.y, Z: cause.z}
	}
	return src
}

// pluginDamage fires EntityDamageEvent for a hurt of full (shield and helmet
// already taken). The event's Amount is what the hurt hurt-window lets
// through; the returned full is the amount to land, and ok is false when a
// listener cancelled the hurt or left nothing of it.
// vanilla: LivingEntity.hurtServer, actuallyHurt
func (c *client) pluginDamage(full float64, damageType registry.DamageType, cause *damageCause, effects []effect.Instance) (float64, bool) {
	v := c.player.Vitals()
	var window float64
	if v.InvulnerableTicks > 0 {
		window = v.LastHurt
	}
	if full <= window || full <= 0 {
		return full, true
	}
	e := &plugin.EntityDamageEvent{
		Entity: pluginPlayer{c: c},
		Source: c.srv.pluginSource(c.theWorld(), damageType, cause),
		Amount: float32(full - window),
	}
	// Final is the health the hit would take: armour, Resistance, protection
	// and then absorption, all previewed without wear.
	left := c.armourAbsorb(float64(e.Amount), damageType, damageIsDirect(cause), effects, causeWeapon(cause), true)
	e.Final = float32(max(left-v.Absorption, 0))
	c.srv.plugins.Fire(e)
	if e.Cancelled() || e.Amount <= 0 {
		return full, false
	}
	return float64(e.Amount) + window, true
}

// deathPlan is what a player's death does, as plugins left it.
type deathPlan struct {
	message                  protocol.Text
	showMessage              bool
	drops                    []protocol.ItemStack
	experience               int
	keepInventory, keepLevel bool
}

// deathPlan fires PlayerDeathEvent, before the drops spawn, and returns what
// the death does. With no listener it is the game rules' answer.
// vanilla: ServerPlayer.die, Player.dropEquipment
func (c *client) deathPlan(message protocol.Text, damageType registry.DamageType, cause *damageCause) deathPlan {
	rule := c.theWorld().BoolRule(world.RuleKeepInventory)
	plan := deathPlan{message: message, showMessage: c.theWorld().BoolRule(world.RuleShowDeathMsg), keepInventory: rule, keepLevel: rule}
	r := c.srv.plugins
	if r == nil || !r.Has(pluginruntime.PlayerDeath) {
		if !rule {
			plan.drops = c.deathDrops()
			plan.experience = c.deathExperience()
		}
		return plan
	}
	text, _ := pluginText(message)
	original := pluginruntime.CloneText(text)
	drops := c.deathDrops()
	xp := 0
	if !rule {
		xp = c.deathExperience()
	}
	e := &plugin.PlayerDeathEvent{
		Entity:        pluginPlayer{c: c},
		Source:        c.srv.pluginSource(c.theWorld(), damageType, cause),
		Drops:         make([]plugin.ItemStack, len(drops)),
		Experience:    xp,
		Player:        pluginPlayer{c: c},
		Message:       &text,
		KeepInventory: rule,
		KeepLevel:     rule,
	}
	for i, d := range drops {
		e.Drops[i] = pluginStack(d)
	}
	r.Fire(e)
	plan.keepInventory, plan.keepLevel = e.KeepInventory, e.KeepLevel
	switch {
	case e.Message == nil:
		plan.showMessage = false
	case !reflect.DeepEqual(*e.Message, original):
		plan.message = pluginProtocolText(*e.Message)
	}
	for _, d := range e.Drops {
		if stack, ok := pluginProtocolStack(d); ok && !stack.Empty() {
			plan.drops = append(plan.drops, stack)
		}
	}
	if plan.keepLevel {
		plan.experience = 0
	} else {
		plan.experience = max(e.Experience, 0)
	}
	return plan
}

// bindPluginEntity gives a handle built where no plugin was known the
// listener's host.
func bindPluginEntity(e plugin.Entity, host *pluginHost) plugin.Entity {
	switch h := e.(type) {
	case nil:
		return nil
	case pluginPlayer:
		h.host = host
		return h
	case pluginEntity:
		if body, ok := host.entityByUUID(uuid.UUID(h.id), ""); ok {
			return host.pluginEntityOf(body)
		}
		h.host = host
		return h
	case pluginRemoved:
		if body, ok := host.entityByUUID(h.id, ""); ok {
			return host.pluginEntityOf(body)
		}
		h.host = host
		return h
	}
	return e
}

// removedBody names a body that left the world before an event about it
// fired (a creeper that blew up, a primed TNT).
type removedBody struct {
	id  uuid.UUID
	typ registry.EntityType
	// isCause is whether the body is also the entity responsible for the
	// hurt it dealt, as a creeper is for its blast.
	isCause bool
}

func (r *removedBody) handle() plugin.Entity {
	return pluginRemoved{id: r.id, typ: r.typ}
}

// pluginRemoved is the handle of a removed body: invalid, but it still
// answers its entity type.
type pluginRemoved struct {
	pluginEntity
	typ registry.EntityType
}

func (r pluginRemoved) Type() registry.EntityType { return r.typ }

// bindPluginItem gives an item handle its host.
func bindPluginItem(e plugin.ItemEntity, host *pluginHost) plugin.ItemEntity {
	if h, ok := e.(pluginItemEntity); ok {
		h.host = host
		return h
	}
	return e
}

func bindPluginSource(src *plugin.DamageSource, host *pluginHost) {
	src.Causing = bindPluginEntity(src.Causing, host)
	src.Direct = bindPluginEntity(src.Direct, host)
}

// mobDeathNote is a mob's death recorded by the tick step for
// EntityDeathEvent: drops and xp index fx.deaths and fx.xpDrops, -1 for none.
type mobDeathNote struct {
	uuid       uuid.UUID
	w          *world.World
	x, y, z    float64
	damageType registry.DamageType
	causeID    int32
	drops, xp  int
}

// fireMobDeaths fires EntityDeathEvent for the step's deaths, before their
// drops spawn, and writes the listeners' drops and experience back.
// vanilla: LivingEntity.dropAllDeathLoot
func (s *Server) fireMobDeaths(fx *mobEffects) {
	for _, n := range fx.deathNotes {
		e := &plugin.EntityDeathEvent{
			Entity: pluginLivingHandle(n.uuid),
			Source: s.pluginSource(n.w, n.damageType, &damageCause{entityID: n.causeID}),
		}
		if n.drops >= 0 {
			for _, stack := range fx.deaths[n.drops].stacks {
				e.Drops = append(e.Drops, pluginStack(stack))
			}
		}
		if n.xp >= 0 {
			e.Experience = fx.xpDrops[n.xp].amount
		}
		s.plugins.Fire(e)
		if n.drops >= 0 {
			var kept []protocol.ItemStack
			for _, d := range e.Drops {
				if stack, ok := pluginProtocolStack(d); ok && !stack.Empty() {
					kept = append(kept, stack)
				}
			}
			fx.deaths[n.drops].stacks = kept
		}
		switch {
		case n.xp >= 0:
			fx.xpDrops[n.xp].amount = max(e.Experience, 0)
		case e.Experience > 0:
			fx.xpDrops = append(fx.xpDrops, mobXpDrop{w: n.w, amount: e.Experience, x: n.x, y: n.y, z: n.z})
		}
	}
}

// pluginLivingHandle is a handle with no host yet, for bindPluginLiving.
func pluginLivingHandle(id uuid.UUID) plugin.LivingEntity {
	base := pluginEntity{id: id}
	return pluginLivingEntity{pluginEntity: base, livingOps: livingOps{base}}
}

// bindPluginLiving is bindPluginEntity for a living handle: the live body's
// typed handle, else the bare one given the host.
func bindPluginLiving(e plugin.LivingEntity, host *pluginHost) plugin.LivingEntity {
	if h, ok := e.(pluginLivingEntity); ok {
		// The dying body is still listed for a direct UUID lookup.
		id := h.id
		if list := (worldScope{srv: host.srv}).Entities(command.EntityQuery{UUID: &id, IncludeDead: true}); len(list) > 0 {
			if b, ok := host.pluginEntityOf(list[0]).(plugin.LivingEntity); ok {
				return b
			}
		}
		h.host, h.e.host = host, host
		return h
	}
	if b, ok := bindPluginEntity(e, host).(plugin.LivingEntity); ok {
		return b
	}
	return e
}

// totemAllowed fires EntityResurrectEvent when a totem would save the body
// and reports whether it may; a cancelled one is not spent.
// vanilla: LivingEntity.checkTotemDeathProtection
func (c *client) totemAllowed(damageType registry.DamageType) bool {
	r := c.srv.plugins
	if r == nil || !r.Has(pluginruntime.EntityResurrect) {
		return true
	}
	var hand int32 = -1
	c.player.Inventory(func(inv *player.Inventory) {
		h, _ := rules.TotemHand(damageType, inv.Slots[rules.SlotOfHand(inv, protocol.HandMain)], inv.Slots[player.OffHandSlot])
		hand = int32(h)
	})
	if hand < 0 {
		return true
	}
	e := &plugin.EntityResurrectEvent{Entity: pluginPlayer{c: c}, Hand: plugin.MainHand}
	if hand == protocol.HandOff {
		e.Hand = plugin.OffHand
	}
	r.Fire(e)
	return !e.Cancelled()
}

// pluginVitalsTick fires FoodLevelChangeEvent and EntityRegainHealthEvent for
// what a tick of the food bar changed since before, and puts back or resizes
// what a listener refused. The tick has already run, so a cancelled change
// is undone here; its exhaustion and saturation stay spent.
// vanilla: FoodData.tick
func (c *client) pluginVitalsTick(before player.Vitals) {
	r := c.srv.plugins
	if r == nil || before.Dead {
		return
	}
	after := c.player.Vitals()
	if after.Dead {
		return
	}
	if r.Has(pluginruntime.FoodLevelChange) && after.Food != before.Food {
		e := &plugin.FoodLevelChangeEvent{Player: pluginPlayer{c: c}, Level: int(after.Food)}
		r.Fire(e)
		level := int32(max(min(e.Level, 20), 0))
		if e.Cancelled() {
			level = before.Food
		}
		c.player.UpdateVitals(func(v *player.Vitals) { v.Food = level })
	}
	if r.Has(pluginruntime.EntityAirChange) && after.AirSupply != before.AirSupply {
		e := &plugin.EntityAirChangeEvent{Entity: pluginPlayer{c: c}, Air: after.AirSupply}
		r.Fire(e)
		air := e.Air
		if e.Cancelled() {
			air = before.AirSupply
		}
		c.player.UpdateVitals(func(v *player.Vitals) { v.AirSupply = air })
	}
	if r.Has(pluginruntime.EntityRegainHealth) && after.Health > before.Health {
		reason := plugin.RegainNatural
		switch {
		case slices.ContainsFunc(c.player.Effects(), func(i effect.Instance) bool { return i.ID == registry.MobEffectRegeneration }):
			reason = plugin.RegainRegeneration
		case before.Food >= 20 && before.Saturation > 0:
			reason = plugin.RegainSatiated
		}
		e := &plugin.EntityRegainHealthEvent{Entity: pluginPlayer{c: c}, Amount: float32(after.Health - before.Health), Reason: reason}
		r.Fire(e)
		health := before.Health + float64(max(e.Amount, 0))
		if e.Cancelled() {
			health = before.Health
		}
		var maxHealth float64
		c.player.Attributes(func(a *player.Attributes) { maxHealth = a.Value(registry.AttributeMaxHealth) })
		c.player.UpdateVitals(func(v *player.Vitals) { v.Health = min(health, maxHealth) })
	}
}

// glideAllowed fires EntityToggleGlideEvent for a glide about to start or
// stop and reports whether it may; a cancelled start leaves the player
// grounded, a cancelled stop keeps the wings open.
// vanilla: Player.tryToStartFallFlying, stopFallFlying
func (c *client) glideAllowed(on bool) bool {
	r := c.srv.plugins
	if r == nil || !r.Has(pluginruntime.EntityToggleGlide) {
		return true
	}
	e := &plugin.EntityToggleGlideEvent{Entity: pluginPlayer{c: c}, On: on}
	r.Fire(e)
	return !e.Cancelled()
}

// mountAllowed fires EntityMountEvent, and VehicleEvent (enter) for a boat
// or cart, before the player boards the vehicle with this UUID.
// vanilla: Entity.startRiding
func (c *client) mountAllowed(vehicle uuid.UUID, isVehicle bool) bool {
	r := c.srv.plugins
	if r == nil {
		return true
	}
	if r.Has(pluginruntime.EntityMount) {
		e := &plugin.EntityMountEvent{Entity: pluginPlayer{c: c}, Vehicle: pluginEntity{id: vehicle}}
		r.Fire(e)
		if e.Cancelled() {
			return false
		}
	}
	return !isVehicle || c.fireVehicle(vehicle, plugin.VehicleEnter)
}

// dismountAllowed fires EntityDismountEvent, and VehicleEvent (exit) for a
// boat or cart, when the player's sneak would leave the vehicle they ride;
// cancelling keeps them seated.
// vanilla: ServerGamePacketListenerImpl.handlePlayerInput, Entity.removeVehicle
func (c *client) dismountAllowed() bool {
	r := c.srv.plugins
	if r == nil || !r.Has(pluginruntime.EntityDismount) && !r.Has(pluginruntime.Vehicle) {
		return true
	}
	var id uuid.UUID
	var isVehicle bool
	switch {
	case c.vehicle.Load() != nil:
		id, isVehicle = c.vehicle.Load().uuid, true
	case c.minecart.Load() != nil:
		id, isVehicle = c.minecart.Load().uuid, true
	case c.mount.Load() != nil:
		id = c.mount.Load().uuid
	case c.cushion.Load() != nil:
		id = c.cushion.Load().uuid
	default:
		return true
	}
	if r.Has(pluginruntime.EntityDismount) {
		e := &plugin.EntityDismountEvent{Entity: pluginPlayer{c: c}, Vehicle: pluginEntity{id: id}}
		r.Fire(e)
		if e.Cancelled() {
			return false
		}
	}
	return !isVehicle || c.fireVehicle(id, plugin.VehicleExit)
}

// fireVehicle fires VehicleEvent for the player and reports whether it
// stands.
func (c *client) fireVehicle(id uuid.UUID, action plugin.VehicleAction) bool {
	r := c.srv.plugins
	if !r.Has(pluginruntime.Vehicle) {
		return true
	}
	e := &plugin.VehicleEvent{Entity: pluginPlayer{c: c}, Action: action}
	e.Vehicle = pendingVehicle{id: id}
	r.Fire(e)
	return !e.Cancelled()
}

// sheepDyeAllowed fires SheepDyeWoolEvent for a dye that would take and
// returns the colour the sheep ends up with; ok is false when a listener
// cancelled it.
// vanilla: DyeItem.interactLivingEntity
func (c *client) sheepDyeAllowed(ent *mobEntity, colour string) (string, bool) {
	r := c.srv.plugins
	if r == nil || !r.Has(pluginruntime.SheepDyeWool) {
		return colour, true
	}
	s := c.srv
	s.mobsMu.Lock()
	m := ent.mob
	takes := m.Type == entity.TypeSheep && m.Alive() && !m.Sheared && m.Colour != colour
	s.mobsMu.Unlock()
	if !takes {
		return colour, true // Dye refuses it with nothing fired
	}
	e := &plugin.SheepDyeWoolEvent{Sheep: pendingMob{id: ent.uuid}, Player: pluginPlayer{c: c}, Color: plugin.DyeColor(entity.DyeOfColour(colour))}
	r.Fire(e)
	if e.Cancelled() {
		return "", false
	}
	if int(e.Color) >= len(registry.DyeNames) {
		return colour, true
	}
	return registry.DyeNames[e.Color], true
}

// fireExpBottle fires ExpBottleEvent for a bottle o' enchanting's burst and
// returns the experience the orbs carry.
// vanilla: ThrownExperienceBottle.onHit
func (s *Server) fireExpBottle(br thrownBreak) int {
	r := s.plugins
	if r == nil || !r.Has(pluginruntime.ExpBottle) {
		return br.xp
	}
	e := &plugin.ExpBottleEvent{Bottle: pendingProjectile{id: br.uuid}, Experience: br.xp}
	r.Fire(e)
	return max(e.Experience, 0)
}

// fireEggThrow fires PlayerEggThrowEvent for a player's thrown egg that broke
// and returns the chicks it hatches.
// vanilla: ThrownEgg.onHit
func (s *Server) fireEggThrow(br thrownBreak) int {
	r := s.plugins
	owner := s.clientByUUID(br.owner)
	if r == nil || !r.Has(pluginruntime.PlayerEggThrow) || owner == nil {
		return br.chicks
	}
	e := &plugin.PlayerEggThrowEvent{Player: pluginPlayer{c: owner}, Egg: (&removedBody{id: br.uuid, typ: registry.EntityTypeEgg}).handle(),
		Hatching: br.chicks > 0, Hatches: max(br.chicks, 1)}
	s.firePluginEvent(e)
	if !e.Hatching {
		return 0
	}
	return max(e.Hatches, 0)
}

// pendingMob, pendingProjectile and pendingVehicle stand for a handle no
// host has bound yet; bindPluginEvent swaps in the live body's typed handle.
type (
	pendingMob struct {
		mobHandle
		id uuid.UUID
	}
	pendingProjectile struct {
		projectileHandle
		id uuid.UUID
	}
	pendingVillager struct {
		plugin.Villager
		id uuid.UUID
	}
	pendingHanging struct {
		plugin.Hanging
		id uuid.UUID
	}
	pendingVehicle struct {
		vehicleHandle
		id uuid.UUID
	}
)

type (
	mobHandle        = plugin.Mob
	projectileHandle = plugin.Projectile
	vehicleHandle    = plugin.Vehicle
)

// bindPending is the live body's handle as T, else e unchanged.
func bindPending[T plugin.Entity](e T, id uuid.UUID, host *pluginHost) T {
	if b, ok := bindPluginEntity(pluginEntity{id: id}, host).(T); ok {
		return b
	}
	return e
}

// bindPluginVehicleEvents binds the handles of the events that carry typed
// pending ones.
func bindPluginVehicleEvents(event plugin.Event, host *pluginHost) {
	switch e := event.(type) {
	case *plugin.SheepDyeWoolEvent:
		if p, ok := e.Sheep.(pendingMob); ok {
			e.Sheep = bindPending(e.Sheep, p.id, host)
		}
		e.Player = bindPluginPlayer(e.Player, host)
	case *plugin.ProjectileLaunchEvent:
		if p, ok := e.Projectile.(pendingProjectile); ok {
			e.Projectile = bindPending(e.Projectile, p.id, host)
		}
	case *plugin.EntityPotionEffectEvent:
		e.Entity = bindPluginLiving(e.Entity, host)
	case *plugin.EntityInteractEvent:
		e.Entity = bindPluginEntity(e.Entity, host)
	case *plugin.EntitySpawnEvent:
		e.Entity = bindPluginEntity(e.Entity, host)
	case *plugin.EntityTargetEvent:
		if p, ok := e.Entity.(pendingMob); ok {
			e.Entity = bindPending(e.Entity, p.id, host)
		}
		if e.Target != nil {
			e.Target = bindPluginLiving(e.Target, host)
		}
	case *plugin.ProjectileHitEvent:
		if p, ok := e.Projectile.(pendingProjectile); ok {
			e.Projectile = bindPending(e.Projectile, p.id, host)
		}
		if l, ok := e.Hit.Entity.(plugin.LivingEntity); ok {
			e.Hit.Entity = bindPluginLiving(l, host)
		}
	case *plugin.PlayerEggThrowEvent:
		e.Player = bindPluginPlayer(e.Player, host)
		e.Egg = bindPluginEntity(e.Egg, host)
	case *plugin.ExpBottleEvent:
		if p, ok := e.Bottle.(pendingProjectile); ok {
			e.Bottle = bindPending(e.Bottle, p.id, host)
		}
	case *plugin.VehicleEvent:
		if p, ok := e.Vehicle.(pendingVehicle); ok {
			e.Vehicle = bindPending(e.Vehicle, p.id, host)
		}
		e.Entity = bindPluginEntity(e.Entity, host)
	case *plugin.VehicleMoveEvent:
		if p, ok := e.Vehicle.(pendingVehicle); ok {
			e.Vehicle = bindPending(e.Vehicle, p.id, host)
		}
	case *plugin.EntityMoveEvent:
		e.Entity = bindPluginLiving(e.Entity, host)
	case *plugin.EntityMountEvent:
		e.Entity, e.Vehicle = bindPluginEntity(e.Entity, host), bindPluginEntity(e.Vehicle, host)
	case *plugin.EntityDismountEvent:
		e.Entity, e.Vehicle = bindPluginEntity(e.Entity, host), bindPluginEntity(e.Vehicle, host)
	case *plugin.CreeperPowerEvent:
		if p, ok := e.Creeper.(pendingMob); ok {
			e.Creeper = bindPending(e.Creeper, p.id, host)
		}
	case *plugin.SlimeSplitEvent:
		if p, ok := e.Slime.(pendingMob); ok {
			// The dead cube is gone; a bare handle answers nothing.
			base := pluginEntity{host: host, id: p.id}
			e.Slime = pluginMobEntity{pluginEntity: base, livingOps: livingOps{base}, mobOps: mobOps{base}}
		}
	case *plugin.EntityTameEvent:
		if p, ok := e.Entity.(pendingTameable); ok {
			e.Entity = bindPending(e.Entity, p.id, host)
		}
		e.Owner = bindPluginPlayer(e.Owner, host)
	case *plugin.EntityToggleGlideEvent:
		e.Entity = bindPluginLiving(e.Entity, host)
	case *plugin.EntityEnterLoveModeEvent:
		if p, ok := e.Entity.(pendingBreedable); ok {
			e.Entity = bindPending(e.Entity, p.id, host)
		}
		e.Player = bindPluginPlayer(e.Player, host)
	case *plugin.EntityAirChangeEvent:
		e.Entity = bindPluginLiving(e.Entity, host)
	case *plugin.EntityShootBowEvent:
		e.Entity = bindPluginLiving(e.Entity, host)
	}
}

// loveAllowed fires EntityEnterLoveModeEvent for an animal the player's food
// just put in love and returns the love ticks it keeps; 0 when a listener
// cancelled it (the food is spent either way).
// vanilla: Animal.mobInteract, Animal.setInLove
func (c *client) loveAllowed(ent *mobEntity, ticks int) int {
	r := c.srv.plugins
	if r == nil || !r.Has(pluginruntime.EntityEnterLoveMode) {
		return ticks
	}
	e := &plugin.EntityEnterLoveModeEvent{Entity: pendingBreedable{id: ent.uuid}, Player: pluginPlayer{c: c}, Ticks: ticks}
	r.Fire(e)
	if e.Cancelled() {
		return 0
	}
	return max(e.Ticks, 0)
}

// shootBowAllowed fires EntityShootBowEvent for a full-enough draw before the
// ammunition is spent; Projectile is nil because the arrow does not exist
// until the event is done. It returns the force and whether the ammunition is
// used up, and ok is false when a listener cancelled the shot.
// vanilla: BowItem.releaseUsing
func (c *client) shootBowAllowed(bow protocol.ItemStack, force float64, consume bool) (float64, bool, bool) {
	r := c.srv.plugins
	if r == nil || !r.Has(pluginruntime.EntityShootBow) {
		return force, consume, true
	}
	e := &plugin.EntityShootBowEvent{Entity: pluginPlayer{c: c}, Bow: pluginStack(bow), Force: float32(force), Consume: consume}
	r.Fire(e)
	if e.Cancelled() {
		return force, consume, false
	}
	return float64(max(e.Force, 0)), e.Consume, true
}

type breedableHandle = plugin.Breedable

// pendingBreedable stands for an animal handle no host has bound yet.
type pendingBreedable struct {
	breedableHandle
	id uuid.UUID
}

// tameAllowed fires EntityTameEvent for a won taming roll and reports whether
// the animal may be tamed.
// vanilla: TamableAnimal.tame
func (c *client) tameAllowed(ent *mobEntity) bool {
	r := c.srv.plugins
	if r == nil || !r.Has(pluginruntime.EntityTame) {
		return true
	}
	e := &plugin.EntityTameEvent{Entity: pendingTameable{id: ent.uuid}, Owner: pluginPlayer{c: c}}
	r.Fire(e)
	return !e.Cancelled()
}

type tameableHandle = plugin.Tameable

// pendingTameable stands for a tameable handle no host has bound yet.
type pendingTameable struct {
	tameableHandle
	id uuid.UUID
}

// pluginEaten fires FoodLevelChangeEvent for the food a meal just gave, with
// the item eaten; a cancel puts the food and saturation back, a changed
// Level is what the bar holds.
// vanilla: FoodData.eat
func (c *client) pluginEaten(before player.Vitals, item protocol.ItemStack) {
	r := c.srv.plugins
	if r == nil || !r.Has(pluginruntime.FoodLevelChange) {
		return
	}
	after := c.player.Vitals()
	if after.Food == before.Food {
		return
	}
	e := &plugin.FoodLevelChangeEvent{Player: pluginPlayer{c: c}, Level: int(after.Food), Item: pluginStack(item)}
	r.Fire(e)
	level := int32(max(min(e.Level, 20), 0))
	c.player.UpdateVitals(func(v *player.Vitals) {
		if e.Cancelled() {
			v.Food, v.Saturation = before.Food, before.Saturation
			return
		}
		v.Food = level
		v.Saturation = min(v.Saturation, float64(level))
	})
}

// fireSlimeSplits fires SlimeSplitEvent once per dying cube and returns the
// splits that stay: none when cancelled, fewer or more (the last cube's
// place repeated) as Count says.
// vanilla: AbstractCubeMob.remove
func (s *Server) fireSlimeSplits(splits []cubeSplit) []cubeSplit {
	r := s.plugins
	if r == nil || !r.Has(pluginruntime.SlimeSplit) || len(splits) == 0 {
		return splits
	}
	var out []cubeSplit
	for i := 0; i < len(splits); {
		j := i
		for j < len(splits) && splits[j].from == splits[i].from {
			j++
		}
		group := splits[i:j]
		i = j
		e := &plugin.SlimeSplitEvent{Slime: pendingMob{id: group[0].from.uuid}, Count: len(group)}
		r.Fire(e)
		if e.Cancelled() {
			continue
		}
		for n := range max(e.Count, 0) {
			out = append(out, group[min(n, len(group)-1)])
		}
	}
	return out
}
