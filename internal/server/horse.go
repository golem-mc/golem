package server

import (
	"math/rand/v2"
	"slices"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// The wire number of the saddle's equipment slot.
// vanilla: EquipmentSlot.SADDLE
const saddleEquipmentSlot = 7

// saddlePayload is the Set Equipment packet of a mob's saddle slot (a copper golem's antenna).
func saddlePayload(m *entity.Mob) []byte {
	var b protocol.Buffer
	b.PutVarInt(m.ID)
	b.PutU8(saddleEquipmentSlot)
	b.PutSlot(m.SaddleSlot())
	return b.Bytes()
}

// ridingMob reports whether the player sits on a mob.
func (c *client) ridingMob() bool { return c.mount.Load() != nil }

// isPassenger reports whether the player rides a boat, minecart, mob or
// cushion. vanilla: Entity.isPassenger
func (c *client) isPassenger() bool {
	return c.ridingBoat() || c.ridingMinecart() || c.ridingMob() || c.ridingCushion()
}

// horseSeat is where the feet of the mob's passenger at index stand.
func horseSeat(m *entity.Mob, index int) player.Position {
	x, y, z := entity.PlayerSeat(m, index)
	return player.Position{X: x, Y: y, Z: z, Yaw: m.Yaw}
}

type equineVoice uint8

const (
	horseAngry equineVoice = iota
	horseEat
	horseJump
	horseChest
)

var equineVoices = [registry.EntityTypeCount][4]protocol.SoundHolder{
	entity.TypeDonkey:      {registeredSoundHolder(registry.SoundEntityDonkeyAngry), registeredSoundHolder(registry.SoundEntityDonkeyEat), registeredSoundHolder(registry.SoundEntityDonkeyJump), registeredSoundHolder(registry.SoundEntityDonkeyChest)},
	entity.TypeMule:        {registeredSoundHolder(registry.SoundEntityMuleAngry), registeredSoundHolder(registry.SoundEntityMuleEat), registeredSoundHolder(registry.SoundEntityMuleJump), registeredSoundHolder(registry.SoundEntityMuleChest)},
	entity.TypeLlama:       {registeredSoundHolder(registry.SoundEntityLlamaAngry), registeredSoundHolder(registry.SoundEntityLlamaEat), protocol.SoundHolder{Name: "minecraft:entity.llama." + "jump"}, registeredSoundHolder(registry.SoundEntityLlamaChest)},
	entity.TypeTraderLlama: {registeredSoundHolder(registry.SoundEntityLlamaAngry), registeredSoundHolder(registry.SoundEntityLlamaEat), protocol.SoundHolder{Name: "minecraft:entity.llama." + "jump"}, registeredSoundHolder(registry.SoundEntityLlamaChest)},
	entity.TypeCamel:       {protocol.SoundHolder{Name: "minecraft:entity.camel." + "angry"}, registeredSoundHolder(registry.SoundEntityCamelEat), protocol.SoundHolder{Name: "minecraft:entity.camel." + "jump"}, protocol.SoundHolder{Name: "minecraft:entity.camel." + "chest"}},
	entity.TypeCamelHusk:   {protocol.SoundHolder{Name: "minecraft:entity.camel_husk." + "angry"}, registeredSoundHolder(registry.SoundEntityCamelHuskEat), protocol.SoundHolder{Name: "minecraft:entity.camel_husk." + "jump"}, protocol.SoundHolder{Name: "minecraft:entity.camel_husk." + "chest"}},
	entity.TypeZombieHorse: {registeredSoundHolder(registry.SoundEntityZombieHorseAngry), registeredSoundHolder(registry.SoundEntityZombieHorseEat), registeredSoundHolder(registry.SoundEntityHorseJump), protocol.SoundHolder{Name: "minecraft:entity.horse." + "chest"}},
	entity.TypeHorse:       {registeredSoundHolder(registry.SoundEntityHorseAngry), registeredSoundHolder(registry.SoundEntityHorseEat), registeredSoundHolder(registry.SoundEntityHorseJump), protocol.SoundHolder{Name: "minecraft:entity.horse." + "chest"}},
}

// equineSound selects the kind's action voice; skeleton horses have no angry voice.
// vanilla: Donkey.getAngrySound, Mule.getEatingSound, Mule.playJumpSound
func equineSound(m *entity.Mob, event equineVoice) protocol.SoundHolder {
	kind := m.Type
	switch kind {
	case entity.TypeCamel, entity.TypeCamelHusk, entity.TypeDonkey, entity.TypeMule, entity.TypeLlama, entity.TypeTraderLlama:
		return equineVoices[kind][event]
	case entity.TypeSkeletonHorse:
		if event == horseAngry {
			return protocol.SoundHolder{ID: -1}
		}
		// vanilla: SkeletonHorse.playJumpSound
		if event == horseJump && m.InWater() {
			return registeredSoundHolder(registry.SoundEntitySkeletonHorseJumpWater)
		}
	case entity.TypeZombieHorse:
		if event == horseAngry || event == horseEat {
			return equineVoices[kind][event]
		}
	}
	if m.IsBaby() {
		switch event {
		case horseAngry:
			return registeredSoundHolder(registry.SoundEntityBabyHorseAngry)
		case horseEat:
			return registeredSoundHolder(registry.SoundEntityBabyHorseEat)
		}
	}
	return equineVoices[entity.TypeHorse][event]
}

// useOnHorse is Horse.mobInteract for a click no lead or name tag took:
// food goes to the next part, an untamed horse rears at any item, a tame
// one takes a saddle or armour, a sneaking click opens its screen, and a
// bare click mounts.
// vanilla: Horse.mobInteract, AbstractHorse.mobInteract, Equippable.equipOnTarget
func (c *client) useOnHorse(ent *mobEntity, hand int32, held protocol.ItemStack, sneaking bool) (bool, error) {
	s := c.srv
	if ent.mob.Type == entity.TypeZombieHorse {
		// vanilla: ZombieHorse.interact
		s.mobsMu.Lock()
		ent.mob.Persistence = true
		s.mobsMu.Unlock()
	}
	s.mobsMu.Lock()
	m := ent.mob
	if m.Type == entity.TypeCamelHusk {
		// vanilla: CamelHusk.interact
		m.Persistence = true
	}
	click, mad := m.UseItem(held, sneaking)
	var sends []mobSend
	if mad {
		if send, ok := mobVoiceSend(ent.w, m, equineSound(m, horseAngry), m.SoundVolume(), m.VoicePitch()); ok {
			sends = append(sends, send)
		}
	}
	x, y, z, silent := m.X, m.Y, m.Z, m.Silent
	s.mobsMu.Unlock()
	s.sendMobSends(sends)
	switch click {
	case entity.HorseClickChest:
		if !silent {
			s.broadcastSoundHolder(ent.w, equineSound(m, horseChest), protocol.SoundCategoryNeutral, x, y, z, 1, 1+(rand.Float32()-rand.Float32())*0.2)
		}
		return true, c.consumeHeldOutsideCreative(hand)
	case entity.HorseClickSaddle:
		saddle := registry.SoundEntityHorseSaddle
		if entity.IsCamel(m.Type) {
			saddle = m.CamelSound(entity.CamelSaddle)
		}
		s.broadcastSoundID(ent.w, saddle, protocol.SoundCategoryNeutral, x, y, z, 1, 1)
		return true, c.consumeHeldOutsideCreative(hand)
	case entity.HorseClickArmor:
		// vanilla: AbstractHorse.equipBodyArmor, LivingEntity.onEquipItem
		if item, ok := registry.StackEquippable(held); ok && !silent {
			s.broadcastSoundHolder(ent.w, item.EquipSound, protocol.SoundCategoryNeutral, x, y, z, 1, 1)
		}
		return true, c.consumeHeldOutsideCreative(hand)
	case entity.HorseClickInventory:
		return true, c.openMountScreen(ent)
	case entity.HorseClickMount:
		if c.equipOnInteract(ent, hand, held) {
			return true, nil
		}
		s.mountHorse(c, ent)
		return true, nil
	case entity.HorseClickFeed:
		return c.feedHorse(ent, hand, held)
	case entity.HorseClickRear:
		return true, nil
	}
	return false, nil
}

// feedHorse feeds the horse the held food: a meal that does anything is
// spent outside creative, shows the hearts if love began, plays the eating
// sound, posts the eat event and swings the arm (SUCCESS_SERVER); one that
// does nothing passes.
// vanilla: AbstractHorse.fedFood, AbstractHorse.handleEating, AbstractHorse.eating
func (c *client) feedHorse(ent *mobEntity, hand int32, held protocol.ItemStack) (bool, error) {
	s := c.srv
	s.mobsMu.Lock()
	m := ent.mob
	ate, loved := m.FeedHorse(ent.w, held.ItemID)
	if loved {
		ent.loveCause = c.player.UUID
	}
	sound, pitch := protocol.SoundHolder{ID: -1}, float32(0)
	if ate && !m.Silent {
		sound = equineSound(m, horseEat)
		pitch = 1 + (m.Rand.Float32()-m.Rand.Float32())*0.2
	}
	x, y, z := m.X, m.Y, m.Z
	s.mobsMu.Unlock()
	if !ate {
		return false, nil
	}
	if loved {
		s.broadcastWorld(ent.w, protocol.IDPlayEntityEvent, entityEventPayload(m.ID, protocol.EntityStatusLoveHearts))
	}
	if sound.ID >= 0 || sound.Name != "" {
		s.broadcastSoundHolder(ent.w, sound, protocol.SoundCategoryNeutral, x, y, z, 1, pitch)
	}
	s.gameEvent(ent.w, rules.GameEventEat, x, y, z, rules.GameEventContext{Mob: ent.uuid})
	if err := c.consumeHeldOutsideCreative(hand); err != nil {
		return true, err
	}
	c.useSwing = swingServer
	return true, nil
}

// consumeHeldOutsideCreative is ItemStack.consume's creative exemption.
// vanilla: ItemStack.consume
func (c *client) consumeHeldOutsideCreative(hand int32) error {
	if c.player.GameMode() == protocol.GameModeCreative {
		return nil
	}
	return c.consumeHeld(hand)
}

// mountHorse seats the player on the horse.
// vanilla: AbstractHorse.doPlayerRide, Entity.startRiding
func (s *Server) mountHorse(c *client, ent *mobEntity) bool {
	return s.seatPlayerOnMob(c, ent, false)
}

// seatPlayerOnMob seats the player on the mob. force (/ride) skips the
// horse and capacity checks, so a mob may carry several players; the first
// steers. A happy ghast seats up to four.
// vanilla: Entity.startRiding
func (s *Server) seatPlayerOnMob(c *client, ent *mobEntity, force bool) bool {
	if c.isPassenger() || c.player.Vitals().Dead || !force && !c.mountAllowed(ent.uuid, false) {
		return false
	}
	s.mobsMu.Lock()
	m := ent.mob
	if !m.Alive() ||
		!force && (!m.CanBeRidden() || ent.vehicle != nil || ent.cart != nil || m.Vehicle != nil) {
		s.mobsMu.Unlock()
		return false
	}
	if m.Horse.Rider == uuid.Nil {
		m.Board(c.player.UUID)
		m.Horse.RiderID = c.player.EntityID
		ent.rider = c
		ent.firstX, ent.firstY, ent.firstZ = m.X, m.Y, m.Z
	} else {
		ent.extra = append(ent.extra, c)
		m.Horse.ExtraRiderIDs = append(m.Horse.ExtraRiderIDs, c.player.EntityID)
	}
	c.mount.Store(ent)
	sound := m.PlayerBoarded()
	ids := m.PassengerIDs()
	seat := horseSeat(m, slices.Index(ids, c.player.EntityID))
	w, x, y, z, riders := ent.w, m.X, m.Y, m.Z, ent.playersAboard()
	active := m.AllEffects()
	s.mobsMu.Unlock()

	// vanilla: ServerPlayer.startRiding, PlayerList.sendActiveEffects
	for _, in := range active {
		c.send(protocol.IDPlayUpdateMobEffect, updateMobEffectPayload(m.ID, in, false))
	}

	if sound != registry.SoundNone {
		s.broadcastSoundID(w, sound, protocol.SoundCategoryNeutral, x, y, z, 1, 1)
	}
	s.gameEvent(w, rules.GameEventEntityMount, x, y, z, c.gameEventContext(0))
	s.broadcastWorld(w, protocol.IDPlaySetPassengers, setPassengersPayload(m.ID, ids))
	pos := c.player.Position()
	seat.Pitch, seat.OnGround = pos.Pitch, false
	c.move(seat, false)
	startedRidingAdvancements(riders)
	return true
}

// dismountMob unseats the player and, with step, places them beside the
// mount and tells their client; dimension change, leaving and death skip the
// step. vanilla: Entity.stopRiding, Entity.getDismountLocationForPassenger
func (c *client) dismountMob(step bool) {
	ent := c.mount.Load()
	if ent == nil {
		return
	}
	s := c.srv
	s.mobsMu.Lock()
	if c.mount.Load() != ent {
		s.mobsMu.Unlock()
		return
	}
	m := ent.mob
	if ent.rider == c {
		ent.rider = nil
		m.Unboard()
	} else if i := slices.Index(ent.extra, c); i >= 0 {
		ent.extra = slices.Delete(ent.extra, i, i+1)
		m.Horse.ExtraRiderIDs = slices.DeleteFunc(m.Horse.ExtraRiderIDs, func(id int32) bool { return id == c.player.EntityID })
	}
	// The next player takes the steering.
	// vanilla: Entity.getControllingPassenger is the first passenger
	if ent.rider == nil && len(ent.extra) > 0 {
		next := ent.extra[0]
		ent.extra = slices.Delete(ent.extra, 0, 1)
		m.Horse.ExtraRiderIDs = slices.DeleteFunc(m.Horse.ExtraRiderIDs, func(id int32) bool { return id == next.player.EntityID })
		m.Board(next.player.UUID)
		m.Horse.RiderID = next.player.EntityID
		ent.rider = next
		ent.firstX, ent.firstY, ent.firstZ = m.X, m.Y, m.Z
	}
	c.mount.Store(nil)
	sound := m.PlayerLeft()
	ids := m.PassengerIDs()
	w, x, y, z := ent.w, m.X, m.Y, m.Z
	var spot player.Position
	yaw := c.player.Position().Yaw
	if step {
		d := c.dims(c.player.Appearance().Pose)
		spot = entity.PlayerDismountSpot(w, m, yaw, d.Width, d.Height)
	}
	id := m.ID
	active := m.AllEffects()
	s.mobsMu.Unlock()

	// vanilla: ServerPlayer.removeVehicle
	for _, in := range active {
		c.send(protocol.IDPlayRemoveMobEffect, removeMobEffectPayload(id, in.ID))
	}

	if sound != registry.SoundNone {
		s.broadcastSoundID(w, sound, protocol.SoundCategoryNeutral, x, y, z, 1, 1)
	}
	s.gameEvent(w, rules.GameEventEntityDismount, x, y, z, c.gameEventContext(0))
	s.broadcastWorld(w, protocol.IDPlaySetPassengers, setPassengersPayload(id, ids))
	if step {
		c.stepOffBoat(spot)
	}
}

// moveHorse moves a steered horse to its rider's report, unless it leaps
// over ten blocks from the tick's first position or enters a new collision,
// in which case the horse's position is sent back. Landing past the safe
// fall hurts the horse.
// vanilla: ServerGamePacketListenerImpl.handleMoveVehicle
func (s *Server) moveHorse(rider *client, ent *mobEntity, x, y, z float64, yaw, pitch float32, onGround bool) {
	stick := rider.holdsSteeringStick(ent.mob.Type)
	s.mobsMu.Lock()
	m := ent.mob
	m.Horse.StickHeld = stick
	if !m.Alive() || ent.rider != rider || !m.Steered() {
		s.mobsMu.Unlock()
		return
	}
	if m.RejectsMove(ent.w, ent.firstX, ent.firstY, ent.firstZ, x, y, z) {
		back := player.Position{X: m.X, Y: m.Y, Z: m.Z, Yaw: m.Yaw, Pitch: m.Pitch}
		s.mobsMu.Unlock()
		rider.send(protocol.IDPlayMoveVehicle, vehicleMovePayload(back))
		return
	}
	fromX, fromY, fromZ := m.X, m.Y, m.Z
	m.SetPos(x, y, z)
	m.Yaw, m.Pitch, m.HeadYaw = yaw, pitch, yaw
	m.OnGround = onGround
	m.Impulse = true
	landed, damage, hurt, soft, effect := m.RideLanding(ent.w, fromX, fromY, fromZ, onGround)
	sends := s.horseLandedSends(ent.w, m, landed, damage, hurt, soft, effect.DamageType)
	for _, ev := range m.RiddenStepSounds(ent.w, fromX, fromY, fromZ) {
		if send, ok := mobVoiceIDSend(ent.w, m, ev.SoundID, ev.Volume, ev.Pitch); ok {
			sends = append(sends, send)
		}
	}
	seat := horseSeat(m, 0)
	s.mobsMu.Unlock()
	s.sendMobSends(sends)
	if damage > 0 {
		rider.fallOnto(landed, effect)
	}

	seat.Pitch, seat.OnGround = rider.player.Position().Pitch, onGround
	rider.move(seat, false)
}

// horseJump handles the Player Command starting a ridden horse's jump: the
// client already leapt, so the server rears the horse and plays the jump.
// vanilla: ServerGamePacketListenerImpl.handlePlayerCommand, AbstractHorse.handleStartJump
func (c *client) horseJump(power int32) {
	ent := c.mount.Load()
	if ent == nil {
		return
	}
	s := c.srv
	s.mobsMu.Lock()
	m := ent.mob
	jumped := false
	if ent.rider == c && entity.IsNautilus(m.Type) {
		jumped = m.NautilusJump(power)
	} else if ent.rider == c && entity.IsCamel(m.Type) {
		jumped = m.CamelJump(power)
	} else if ent.rider == c && m.Horse.Controlled() {
		jumped = m.RidingJump(power)
	}
	if !jumped {
		s.mobsMu.Unlock()
		return
	}
	sound := equineSound(m, horseJump)
	volume, pitch := float32(0.4), float32(1)
	if entity.IsNautilus(m.Type) {
		sound = registeredSoundHolder(m.NautilusSound(entity.NautilusDash))
		volume, pitch = 1, m.VoicePitch()
	}
	if entity.IsCamel(m.Type) {
		// vanilla: Camel.handleStartJump, makeSound
		sound = registeredSoundHolder(m.CamelSound(entity.CamelDash))
		volume, pitch = m.SoundVolume(), m.VoicePitch()
	}
	send, ok := mobVoiceSend(ent.w, m, sound, volume, pitch)
	dashed := entity.IsNautilus(m.Type) || entity.IsCamel(m.Type)
	x, y, z, mobID := m.X, m.Y, m.Z, ent.uuid
	s.mobsMu.Unlock()
	if dashed {
		// vanilla: Camel.handleStartJump, AbstractNautilus.handleStartJump
		s.gameEvent(ent.w, rules.GameEventEntityAction, x, y, z, rules.GameEventContext{Mob: mobID})
	}
	if ok {
		s.sendMobSends([]mobSend{send})
	}
}

// steerCamel hands the controlling rider's forward key to the camel it rides.
// vanilla: Player.zza, Camel.tickRidden
func (c *client) steerCamel(forward bool) {
	ent := c.mount.Load()
	if ent == nil || !entity.IsCamel(ent.mob.Type) {
		return
	}
	c.srv.mobsMu.Lock()
	if ent.rider == c {
		ent.mob.SetCamelForward(forward)
	}
	c.srv.mobsMu.Unlock()
}

// tickMounts ends seats on dead or vanished mounts and carries riders on
// mounts the server moves. vanilla: Entity.positionRider
func (s *Server) tickMounts() {
	s.mu.RLock()
	var riders []*client
	for _, c := range s.clients {
		if c.ridingMob() {
			riders = append(riders, c)
		}
	}
	s.mu.RUnlock()
	for _, c := range riders {
		ent := c.mount.Load()
		if ent == nil {
			continue
		}
		stick := c.holdsSteeringStick(ent.mob.Type)
		s.lockTick(&s.mobsMu)
		if ent.converting {
			s.unlockTick(&s.mobsMu)
			continue
		}
		if ent.rider == c {
			ent.mob.Horse.StickHeld = stick
		}
		h := s.herdOf(ent.w)
		// A dead mob keeps its riders for the corpse's twenty ticks.
		// vanilla: Entity.setRemoved stops the riding
		removed := h.book[ent.mob] != ent && !ent.dying
		gone := removed || c.theWorld() != ent.w
		// The steering rider's own reports carry them.
		steered := ent.rider == c && ent.mob.Steered()
		// The tick's first good position, which next reports are measured from.
		// vanilla: ServerGamePacketListenerImpl.tick, vehicleFirstGoodX
		ent.firstX, ent.firstY, ent.firstZ = ent.mob.X, ent.mob.Y, ent.mob.Z
		seat := horseSeat(ent.mob, slices.Index(ent.mob.PassengerIDs(), c.player.EntityID))
		s.unlockTick(&s.mobsMu)
		if gone {
			// A removed vehicle leaves the rider where the seat held them.
			// vanilla: LivingEntity.dismountVehicle
			c.dismountMob(!removed)
			continue
		}
		if entity.IsNautilus(ent.mob.Type) {
			c.nautilusRiderEffect()
		}
		if !steered {
			seat.Pitch = c.player.Position().Pitch
			c.move(seat, false)
		}
	}
}

// finishHorseEvents carries out a tick's horse events: hearts or smoke,
// the angry sound, the rider thrown.
func (s *Server) finishHorseEvents(events []horseNews) {
	for _, ev := range events {
		status := uint8(protocol.EntityEventHorseTamed)
		if ev.threw {
			status = protocol.EntityEventHorseTameFailed
		}
		s.broadcastWorld(ev.w, protocol.IDPlayEntityEvent, entityEventPayload(ev.id, status))
		if ev.tamer != nil {
			ev.tamer.tameAnimalAdvancements(ev.animal, "", s.mobLootByEntityID(ev.w, ev.id))
		}
		if ev.threw && ev.rider != nil {
			ev.rider.dismountMob(true)
		}
		if ev.sound != nil {
			s.sendMobSends([]mobSend{*ev.sound})
		}
	}
}

// horseNews is a tick's horse event, told after the mob lock.
type horseNews struct {
	w     *world.World
	id    int32
	threw bool
	rider *client
	sound *mobSend
	// tamer is the rider of the tamed horse, nil for none; animal is its type
	// name.
	tamer  *client
	animal string
}

// horseLandedSends builds a horse's landing sounds and accepted hurt feedback.
// vanilla: AbstractHorse.causeFallDamage
func (s *Server) horseLandedSends(w *world.World, m *entity.Mob, landed, damage float64, hurt, soft bool, damageType registry.DamageType) []mobSend {
	var sends []mobSend
	// vanilla: Llama.causeFallDamage plays no land sound
	if landed > 1 && !entity.IsLlama(m.Type) && !entity.IsNautilus(m.Type) {
		name := registry.SoundEntityHorseLand
		if m.IsBaby() {
			name = registry.SoundEntityBabyHorseLand
		}
		if send, ok := mobVoiceIDSend(w, m, name, 0.4, 1.0); ok {
			sends = append(sends, send)
		}
	}
	// A refused hurt still plays the landing block's fall sound.
	if damage <= 0 {
		return sends
	}
	if fall, volume, pitch, ok := blockFallSound(w, m.X, m.Y, m.Z); ok {
		if send, ok := mobVoiceIDSend(w, m, fall, volume, pitch); ok {
			sends = append(sends, send)
		}
	}
	if hurt && !soft {
		sends = append(sends, mobHurtTold(w, m, damageType)...)
	}
	return sends
}

// fallOnto hurts a rider by the fall their mount landed, against the
// rider's own fall attributes. vanilla: Entity.propagateFallToPassengers
func (c *client) fallOnto(landed float64, effect rules.FallEffect) {
	var safeFall, fallMultiplier float64
	c.player.Attributes(func(a *player.Attributes) {
		safeFall = a.Value(registry.AttributeSafeFallDistance)
		fallMultiplier = a.Value(registry.AttributeFallDamageMultiplier)
	})
	if d := rules.FallDamage(landed*effect.Scale+effect.Offset, safeFall, effect.Modifier*fallMultiplier); d > 0 {
		c.hurt(d, effect.DamageType, nil)
	}
}

// horseEvent queues a taming or a thrown rider, unseating them under the
// mob lock.
func (t *mobDimTick) horseEvent(fx *mobEffects, ev entity.MobEvent) {
	h, w := t.h, t.w
	news := horseNews{w: w, id: ev.Mob.ID, threw: ev.Kind == entity.MobHorseThrewRider}
	if ent, ok := h.book[ev.Mob]; ok && !news.threw {
		news.tamer, news.animal = ent.rider, ev.Mob.Type.Name()
	}
	if ent, ok := h.book[ev.Mob]; ok && news.threw {
		news.rider, ent.rider = ent.rider, nil
		if send, ok := mobVoiceSend(w, ev.Mob, equineSound(ev.Mob, horseAngry), ev.Mob.SoundVolume(), ev.Mob.VoicePitch()); ok {
			news.sound = &send
		}
	}
	fx.horseEvents = append(fx.horseEvents, news)
}

// trapSprung queues the skeleton horse's lightning trap.
func (t *mobDimTick) trapSprung(fx *mobEffects, ev entity.MobEvent) {
	if ent, ok := t.h.book[ev.Mob]; ok {
		fx.traps = append(fx.traps, skeletonTrap{w: t.w, horse: ent})
	}
}

// isMobTag reports whether an entity id names a kind this server simulates
// as a mob.
func isMobTag(id nbt.String) bool { return entity.KnownMobKind(entity.MobTypeOf(string(id))) }

// raiseSavedMob returns the mob of a saved compound's identity standing in
// the world, else stands up the compound with the mob passengers it nests,
// skipping any already live, and joins each one it stands up to its Team;
// nil when it cannot be read.
// vanilla: EntityType.loadEntityRecursive, ServerLevel.addWithUUID
func (s *Server) raiseSavedMob(w *world.World, tag nbt.Compound) *mobEntity {
	identity, ok := entityUUIDOfTag(tag)
	if !ok {
		return nil
	}
	h := s.herdOf(w)
	s.mobsMu.Lock()
	for _, live := range h.book {
		if live.uuid == identity && !live.mob.Discarded() {
			s.mobsMu.Unlock()
			return live
		}
	}
	s.mobsMu.Unlock()
	root := mobOfTag(s, w, tag)
	if root == nil {
		return nil
	}
	var woken wakeResult
	woken.collectTeam(tag)
	for _, ent := range s.standWithRiders(h, root, tag, &woken) {
		s.addMob(ent)
	}
	for _, t := range woken.teams {
		s.joinTagTeam(t)
	}
	return root
}

// standWithRiders stands up the mob riders tag nests under root, skipping
// any whose UUID a live mob has, and links the rides; it returns root and
// its riders, root first, for the caller to add together. A non-nil teams
// collects the riders' Team keys.
// vanilla: EntityType.loadEntityRecursive
func (s *Server) standWithRiders(h *mobHerd, root *mobEntity, tag nbt.Compound, teams *wakeResult) []*mobEntity {
	root.pendingPassengers = passengersOfTag(tag)
	list, _ := nbt.Get[nbt.List](tag, "Passengers")
	if len(list.Items) == 0 {
		return []*mobEntity{root}
	}
	known := map[uuid.UUID]bool{root.uuid: true}
	s.mobsMu.Lock()
	for _, live := range h.book {
		if !live.mob.Discarded() {
			known[live.uuid] = true
		}
	}
	s.mobsMu.Unlock()
	if teams != nil {
		for _, item := range list.Items {
			if c, ok := nbt.CompoundView(item); ok {
				teams.collectTeams(c, known)
			}
		}
	}
	mobs := append([]*mobEntity{root}, standPassengers(h, s, root, tag, known)...)
	linkMobRides(mobs)
	return mobs
}

// seatOnSavedMob seats a joining player on the live mob of the identity, as
// a saved ride's Attach names it.
// vanilla: ServerPlayer.loadAndSpawnParentVehicle
func (s *Server) seatOnSavedMob(c *client, w *world.World, identity uuid.UUID) {
	var seat *mobEntity
	s.mobsMu.Lock()
	for _, live := range s.herdOf(w).book {
		if live.uuid == identity && !live.mob.Discarded() {
			seat = live
			break
		}
	}
	s.mobsMu.Unlock()
	if seat != nil {
		s.seatPlayerOnMob(c, seat, true)
	}
}

// mountDismountsUnderwater reports whether the player's mount throws its
// riders off once their eyes are under water, whatever the game mode.
// vanilla: Entity.dismountsUnderwater
func (c *client) mountDismountsUnderwater() bool {
	ent := c.mount.Load()
	if ent == nil {
		return false
	}
	c.srv.mobsMu.Lock()
	name := ent.mob.Type.Name()
	c.srv.mobsMu.Unlock()
	id, ok := registry.EntityTypeID(name)
	return ok && registry.EntityTypeInTag(id, "minecraft:dismounts_underwater")
}
