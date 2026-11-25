package server

import (
	"fmt"
	"math"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/chatsecurity"
	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/effect"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// Actions of the Player Info Update packet, which is both the tab list and the
// profile - the skin above all - a client draws another player with.
const (
	actionAddPlayer      = 0x01
	actionInitializeChat = 0x02
	actionUpdateGameMode = 0x04
	actionUpdateListed   = 0x08
	actionUpdateLatency  = 0x10
	actionUpdateHat      = 0x80

	// skinPartHat is the hat bit of the skin parts byte.
	// vanilla: PlayerModelPart.HAT
	skinPartHat = 0x40
)

// putProperties writes the list of profile properties carried by Login Success
// and Player Info Update: each one a name, a value, and an optional signature.
func putProperties(b *protocol.Buffer, props []player.Property) {
	b.PutVarInt(int32(len(props)))
	for _, prop := range props {
		b.PutString(prop.Name)
		b.PutString(prop.Value)
		b.PutBool(prop.Signature != "")
		if prop.Signature != "" {
			b.PutString(prop.Signature)
		}
	}
}

type playerInfoEntry struct {
	player  *player.Player
	session *chatsecurity.Session
}

// playerInfo builds a Player Info Update that adds players to the tab list.
//
// It carries the whole profile, because the client has no other source for it:
// a player it has not been told about is drawn with a default skin and cannot
// be tab-completed, whether or not their entity has been spawned.
func playerInfo(players ...playerInfoEntry) []byte {
	var b protocol.Buffer
	b.PutU8(actionAddPlayer | actionInitializeChat | actionUpdateGameMode | actionUpdateListed | actionUpdateLatency | actionUpdateHat)
	b.PutVarInt(int32(len(players)))

	for _, entry := range players {
		p := entry.player
		b.PutUUID(p.UUID)

		// Add Player: the profile, exactly as it was sent in Login Success.
		b.PutString(p.Name)
		putProperties(&b, p.Properties)
		putChatSession(&b, entry.session)
		b.PutVarInt(p.GameMode())                            // Update Game Mode
		b.PutBool(true)                                      // Update Listed
		b.PutVarInt(p.Latency())                             // Update Latency
		b.PutBool(p.Appearance().SkinParts&skinPartHat != 0) // Update Hat
	}
	return b.Bytes()
}

// playerInfoLatency builds the Player Info Update that refreshes the latency
// of the given players.
func playerInfoLatency(players []*player.Player) []byte {
	var b protocol.Buffer
	b.PutU8(actionUpdateLatency)
	b.PutVarInt(int32(len(players)))
	for _, p := range players {
		b.PutUUID(p.UUID)
		b.PutVarInt(p.Latency())
	}
	return b.Bytes()
}

// playerInfoHat builds the Player Info Update that changes only a hat layer.
func playerInfoHat(p *player.Player) []byte {
	var b protocol.Buffer
	b.PutU8(actionUpdateHat)
	b.PutVarInt(1)
	b.PutUUID(p.UUID)
	b.PutBool(p.Appearance().SkinParts&skinPartHat != 0)
	return b.Bytes()
}

// playerInfoGameMode builds the Player Info Update that changes only a game
// mode, which is what a /gamemode broadcast is: the same action bit the join
// packet carries for the tab list, with nothing else changing.
func playerInfoGameMode(p *player.Player) []byte {
	var b protocol.Buffer
	b.PutU8(actionUpdateGameMode)
	b.PutVarInt(1)
	b.PutUUID(p.UUID)
	b.PutVarInt(p.GameMode())
	return b.Bytes()
}

// playerInfoRemove builds the packet taking a player off the tab list.
func playerInfoRemove(p *player.Player) []byte {
	var b protocol.Buffer
	b.PutVarInt(1)
	b.PutUUID(p.UUID)
	return b.Bytes()
}

// spawnPlayer builds the packet that puts a player's body into the world, where
// the others can see it.
func spawnPlayer(p *player.Player) []byte {
	pos := p.Position()

	var b protocol.Buffer
	b.PutVarInt(p.EntityID)
	b.PutUUID(p.UUID)
	b.PutVarInt(protocol.EntityTypePlayer)
	b.PutF64(pos.X)
	b.PutF64(pos.Y)
	b.PutF64(pos.Z)
	b.PutLpVec3(0, 0, 0) // velocity, which the client works out from the moves we send
	b.PutAngle(pos.Pitch)
	b.PutAngle(pos.Yaw)
	// Head yaw, which for a player is where they are actually looking; the yaw
	// above is the direction their body faces.
	b.PutAngle(pos.Yaw)
	b.PutVarInt(0) // no type-specific data
	return b.Bytes()
}

// spawnPlayerAt builds a player's spawn from a movement baseline: the body
// arrives where the relative steps that follow are measured from, so a pair
// made while a step is in flight is not sent a place the step then doubles.
func spawnPlayerAt(p *player.Player, t trackedPosition) []byte {
	var b protocol.Buffer
	b.PutVarInt(p.EntityID)
	b.PutUUID(p.UUID)
	b.PutVarInt(protocol.EntityTypePlayer)
	b.PutF64(float64(t.x) / positionStep)
	b.PutF64(float64(t.y) / positionStep)
	b.PutF64(float64(t.z) / positionStep)
	b.PutLpVec3(0, 0, 0)
	b.PutU8(t.pitch)
	b.PutU8(t.yaw)
	b.PutU8(t.yaw)
	b.PutVarInt(0)
	return b.Bytes()
}

// appearanceFlags is the shared flags byte a player's state carries at
// metadata index 0 apart from the on-fire bit: the sneak that hides the
// name tag, the sprint, the swim and the elytra's glide. The on-fire bit
// is the environment's to add, and joins them in the same byte wherever
// the byte is sent.
func appearanceFlags(look player.Appearance) uint8 {
	var flags uint8
	if look.Sneaking {
		flags |= protocol.EntityFlagSneaking
	}
	if look.Sprinting {
		flags |= protocol.EntityFlagSprinting
	}
	if look.Swimming {
		flags |= protocol.EntityFlagSwimming
	}
	if look.FallFlying {
		flags |= protocol.EntityFlagFlying
	}
	return flags
}

// playerFlags is the whole shared flags byte: the appearance bits above, the
// on-fire bit and the invisibility and glowing the effects carry. Every sender
// builds the byte here, so the bit that changed rides out with the bits that
// did not rather than clearing them.
func playerFlags(p *player.Player, fire bool) uint8 {
	effects := p.Effects()
	return playerFlagsFrom(p, fire, rules.InvisibleToOthers(p.GameMode(), effects), rules.Glowing(effects))
}

// playerFlagsFrom is playerFlags with the effect bits already worked out, so
// a caller holding them does not read the player's effects a second time.
func playerFlagsFrom(p *player.Player, fire, invisible, glowing bool) uint8 {
	look := p.Appearance()
	flags := appearanceFlags(look) | onFireBit(fire)
	if invisible {
		flags |= protocol.EntityFlagInvisible
	}
	if glowing || look.Glowing {
		flags |= protocol.EntityFlagGlowing
	}
	return flags
}

// playerMetadata builds the packet describing what a player is doing: the
// state that lasts and has to be true of them from the moment they are drawn,
// rather than the one-off animations.
//
// Sneaking is two fields. The flag hides the name tag and is what the vanilla
// server sets from the sneak key; the pose is what actually makes the client
// draw a crouching player, and what shrinks their hitbox. Sprinting is one flag,
// and the particles at the player's feet are the client's own doing. Swimming
// is one flag and a pose, set only while the player is sprinting in water.
//
// The pose itself is the one the server's own pose logic derived last —
// vanilla's updatePlayerPose, the pose the body's state asks for forced down
// to whatever fits where the body stands — which is what keeps a body that
// crawled under a block drawn crawling on everyone's screen, and what puts
// the sneak and the swim poses in the priority vanilla's decision puts them
// in rather than the order the flags happen to change in. A sleeper's pose
// is the sleeping one whatever the tracked pose says, set once for the
// whole sleep.
//
// Eating is the hand state a living entity carries: the flag that makes a
// client draw the held item being used, bite by bite. It is sent only while a
// use is in progress, the field's default - a hand doing nothing - being
// what an absence of it says; the off-hand bit follows the hand the use is
// held in.
//
// Sleeping is the pose the bed's own state draws and the optional position
// naming the bed the body lies in. The position is what a sleeper's own
// client moves their body into the bed on, so it rides with the rest to
// everyone the sleeper included; while the body is not asleep the entry is
// the field's empty default, which a spawn packet leaves out like every
// other default.
//
// The environment's two entries ride the same packet when they are worth
// telling: the on-fire bit merged into the flags, and the air a body burns
// through under water, which the client draws as the bubbles' countdown.
// Vanilla's own spawn packet carries exactly the non-default values, so the
// air is left out while it stands at the full breath a fresh body has. Health
// is the other entry that leaves its default: zero on a body that died, which
// is what draws it fallen over. includeHealth is false for the joining
// client's own metadata, whose bar comes from Set Health: an own body drawn
// dead before its chunk is there would let the client skip waiting for that
// chunk and fall through it.
func playerMetadata(p *player.Player, includeHealth bool) []byte {
	look := p.Appearance()
	v := p.Vitals()
	sleep := p.Sleep()

	flags := playerFlags(p, rules.OnFire(&v))
	pose := look.Pose
	if sleep.Lying {
		pose = protocol.PoseSleeping
	}

	var b protocol.Buffer
	b.PutVarInt(p.EntityID)
	b.PutMetadata(protocol.MetadataIndexEntityFlags, protocol.MetadataByte)
	b.PutU8(flags)
	customName{wire: look.Name, visible: look.NameVisible}.putMetadata(&b, customName{})
	if look.Silent {
		b.PutMetadata(protocol.MetadataIndexSilent, protocol.MetadataBoolean)
		b.PutBool(true)
	}
	if look.NoGravity {
		b.PutMetadata(protocol.MetadataIndexNoGravity, protocol.MetadataBoolean)
		b.PutBool(true)
	}
	if v.AirSupply != rules.MaxAirSupply {
		b.PutMetadata(protocol.MetadataIndexAir, protocol.MetadataVarInt)
		b.PutVarInt(int32(v.AirSupply))
	}
	b.PutMetadata(protocol.MetadataIndexPose, protocol.MetadataPose)
	b.PutVarInt(pose)
	if v.TicksFrozen != 0 {
		// The frost overlay's countdown; nought is the client's own
		// default, so it is left out like every other default.
		b.PutMetadata(protocol.MetadataIndexTicksFrozen, protocol.MetadataVarInt)
		b.PutVarInt(int32(v.TicksFrozen))
	}
	if v.ArrowCount != 0 {
		b.PutMetadata(protocol.MetadataIndexArrowCount, protocol.MetadataVarInt)
		b.PutVarInt(int32(v.ArrowCount))
	}
	if v.StingerCount != 0 {
		b.PutMetadata(protocol.MetadataIndexStingerCount, protocol.MetadataVarInt)
		b.PutVarInt(int32(v.StingerCount))
	}
	if sleep.Lying {
		// The bed the body lies in: the optional position, present.
		b.PutMetadata(protocol.MetadataIndexSleepingPos, protocol.MetadataOptionalPosition)
		b.PutBool(true)
		b.PutPosition(sleep.X, sleep.Y, sleep.Z)
	}
	if state := handStateOf(look); state != 0 {
		b.PutMetadata(protocol.MetadataIndexHandState, protocol.MetadataByte)
		b.PutU8(state)
	}
	if v.Dead && includeHealth {
		// The bar emptied on a body the client would otherwise draw standing
		// alive: a pairing shown to a watcher arriving after the death needs
		// it as much as the death's own broadcast does.
		b.PutMetadata(protocol.MetadataIndexLivingHealth, protocol.MetadataFloat)
		b.PutF32(0)
	}
	// Always sent: a watcher keeps the arm it last heard, so a switch back to
	// right has to be told too. vanilla: ServerPlayer.updateOptions
	mainHand := int32(protocol.MainHandRight)
	if look.LeftHanded {
		mainHand = protocol.MainHandLeft
	}
	b.PutMetadata(protocol.MetadataIndexMainHand, protocol.MetadataHumanoidArm)
	b.PutVarInt(mainHand)
	b.PutMetadata(protocol.MetadataIndexSkinParts, protocol.MetadataByte)
	b.PutU8(look.SkinParts)
	body := bodyNow(p, bodyShown{})
	putBodyMetadata(&b, body, len(body.particles) > 0, body.ambient, body.absorption != 0)
	if v.Score != 0 {
		b.PutMetadata(protocol.MetadataIndexScore, protocol.MetadataVarInt)
		b.PutVarInt(v.Score)
	}
	putShoulderMetadata(&b, p.Shoulders(), false)
	b.EndMetadata()
	return b.Bytes()
}

// putShoulderMetadata writes the perched parrots' variants; the client's
// default is none, so all=false leaves a free side out.
// vanilla: Player.DATA_SHOULDER_PARROT_LEFT, DATA_SHOULDER_PARROT_RIGHT
func putShoulderMetadata(b *protocol.Buffer, sh player.Shoulders, all bool) {
	for _, side := range []struct {
		index uint8
		tag   nbt.CompoundTag
	}{{protocol.MetadataIndexShoulderLeft, sh.Left}, {protocol.MetadataIndexShoulderRight, sh.Right}} {
		variant, ok := shoulderParrotVariant(side.tag)
		if !ok && !all {
			continue
		}
		b.PutMetadata(side.index, protocol.MetadataOptionalVarInt)
		if ok {
			b.PutVarInt(variant + 1)
		} else {
			b.PutVarInt(0)
		}
	}
}

// shoulderMetadata builds the Set Entity Metadata carrying both shoulders.
func shoulderMetadata(p *player.Player) []byte {
	var b protocol.Buffer
	b.PutVarInt(p.EntityID)
	putShoulderMetadata(&b, p.Shoulders(), true)
	b.EndMetadata()
	return b.Bytes()
}

// shoulderParrotVariant is the variant a perched parrot's compound carries.
// vanilla: ServerPlayer.extractParrotVariant
func shoulderParrotVariant(shoulder nbt.CompoundTag) (int32, bool) {
	if !hasShoulder(shoulder) {
		return 0, false
	}
	tag := shoulder.Entries()
	if id, _ := nbt.Get[nbt.String](tag, "id"); string(id) != entity.TypeParrot.Name() {
		return 0, false
	}
	v, _ := nbt.IntValue(tag["Variant"])
	return max(0, min(v, 4)), true
}

// scoreMetadata builds the Set Entity Metadata carrying only a player's score.
func scoreMetadata(p *player.Player) []byte {
	var b protocol.Buffer
	b.PutVarInt(p.EntityID)
	b.PutMetadata(protocol.MetadataIndexScore, protocol.MetadataVarInt)
	b.PutVarInt(p.Vitals().Score)
	b.EndMetadata()
	return b.Bytes()
}

// burningMetadata builds the Set Entity Metadata carrying the entries a
// burning, breathing or freezing body changes: the shared flags with the
// on-fire bit merged into what the player is otherwise doing, the air the
// client's bubbles count down, and the frozen ticks the frost overlay is
// drawn by. Only the entries the caller says changed go out, the way
// vanilla's dirty-set packets carry only what moved.
func burningMetadata(p *player.Player, flagsChanged, fire bool, airChanged bool, air int, frozenChanged bool, frozen int, arrowsChanged bool, arrows int, stingersChanged bool, stingers int) []byte {
	var b protocol.Buffer
	b.PutVarInt(p.EntityID)
	if flagsChanged {
		b.PutMetadata(protocol.MetadataIndexEntityFlags, protocol.MetadataByte)
		b.PutU8(playerFlags(p, fire))
	}
	if airChanged {
		b.PutMetadata(protocol.MetadataIndexAir, protocol.MetadataVarInt)
		b.PutVarInt(int32(air))
	}
	if frozenChanged {
		b.PutMetadata(protocol.MetadataIndexTicksFrozen, protocol.MetadataVarInt)
		b.PutVarInt(int32(frozen))
	}
	if arrowsChanged {
		b.PutMetadata(protocol.MetadataIndexArrowCount, protocol.MetadataVarInt)
		b.PutVarInt(int32(arrows))
	}
	if stingersChanged {
		b.PutMetadata(protocol.MetadataIndexStingerCount, protocol.MetadataVarInt)
		b.PutVarInt(int32(stingers))
	}
	b.EndMetadata()
	return b.Bytes()
}

// onFireBit is the on-fire bit a flags byte carries for the burning state
// given, or nothing for a body that is not burning.
func onFireBit(fire bool) uint8 {
	if fire {
		return protocol.EntityFlagOnFire
	}
	return 0
}

// swingArm builds the packet that plays a player's arm swing on everyone else's
// screen, which is what punching and mining look like from the outside.
func swingArm(p *player.Player, hand int32) []byte {
	return swingAnimationPayload(p.EntityID, hand)
}

// swingAttackArm is swingArm with the animation the held main-hand item
// attacks with: a spear stabs where a fist whacks.
// vanilla: ServerGamePacketListenerImpl.handlePunch, PiercingWeapon.attack
func swingAttackArm(p *player.Player, held protocol.ItemStack) []byte {
	return swingAnimationWith(p.EntityID, protocol.HandMain, registry.StackAttackAnimation(held))
}

// swingAnimationPayload writes a default whack with a six-tick duration.
func swingAnimationPayload(entityID, hand int32) []byte {
	return swingAnimationWith(entityID, hand, registry.DefaultSwingAnimation)
}

// swingAnimationWith writes the swing of one animation.
// vanilla: ClientboundSwingAnimationPacket
func swingAnimationWith(entityID, hand int32, anim registry.SwingAnimation) []byte {
	var b protocol.Buffer
	b.PutVarInt(entityID)
	b.PutVarInt(hand)
	b.PutVarInt(anim.Type)
	b.PutVarInt(anim.Duration)
	return b.Bytes()
}

// entityAnimationPayload builds the Animate packet carrying one animation for
// one entity, which is what the arm swings ride on as much as the leave-bed
// wake a sleeping body ends its night on.
func entityAnimationPayload(entityID int32, animation uint8) []byte {
	var b protocol.Buffer
	b.PutVarInt(entityID)
	b.PutU8(animation)
	return b.Bytes()
}

// playerEquipment builds the Set Equipment packet, which tells a client what
// another player is holding and wearing. The six slots every player shows
// (main hand, off hand, feet, legs, chest, head) are sent whether or not they
// hold anything, so that taking something off reaches the others; body and
// saddle follow only when filled.
//
// The slot byte carries both the equipment slot (its low three bits) and a
// continuation bit (the top one), which is set on every entry but the last.
func playerEquipment(p *player.Player) []byte {
	var stacks [8]protocol.ItemStack
	p.Inventory(func(inv *player.Inventory) {
		slots := [...]int{
			player.HotbarSlot + int(inv.Selected), // main hand
			player.OffHandSlot,                    // off hand
			player.ArmorSlot + 3,                  // feet
			player.ArmorSlot + 2,                  // legs
			player.ArmorSlot + 1,                  // chest
			player.ArmorSlot,                      // head
		}
		for i, s := range slots {
			stacks[i] = inv.Slots[s]
		}
		stacks[6], stacks[7] = inv.Body, inv.Saddle // EquipmentSlot.BODY, SADDLE
	})

	// The body and saddle entries ride along only when filled: vanilla sends
	// just the non-empty slots, and these are empty for nearly every player.
	// vanilla: ServerEntity.sendPairingData
	var shown []int
	for i, st := range stacks {
		if i < 6 || !st.Empty() {
			shown = append(shown, i)
		}
	}
	var b protocol.Buffer
	b.PutVarInt(p.EntityID)
	for n, i := range shown {
		entry := uint8(i)
		if n < len(shown)-1 {
			entry |= 0x80
		}
		b.PutU8(entry)
		b.PutSlot(stacks[i])
	}
	return b.Bytes()
}

// attributesPayload builds an Update Attributes packet: each attribute's
// base and the modifiers on it, from which the client works out the value.
func attributesPayload(entityID int32, attrs []player.AttributeInstance) []byte {
	var b protocol.Buffer
	b.PutVarInt(entityID)
	b.PutVarInt(int32(len(attrs)))
	for _, a := range attrs {
		b.PutVarInt(a.Attribute.ID)
		b.PutF64(a.Base)
		b.PutVarInt(int32(len(a.Modifiers)))
		for _, m := range a.Modifiers {
			b.PutString(m.ID)
			b.PutF64(m.Amount)
			b.PutVarInt(int32(m.Operation))
		}
	}
	return b.Bytes()
}

// playerAttributes is the Update Attributes a client that is shown the
// player gets: every syncable attribute the player has.
// vanilla: ServerEntity.sendPairingData
func playerAttributes(p *player.Player) []byte {
	var attrs []player.AttributeInstance
	p.Attributes(func(a *player.Attributes) { attrs = a.Syncable() })
	return attributesPayload(p.EntityID, attrs)
}

// refreshAttributes puts on and takes off the modifiers the player's
// equipment, state and active effects call for, leaving the changes to be
// sent.
func (c *client) refreshAttributes(effects []effect.Instance) {
	creative := c.player.GameMode() == protocol.GameModeCreative
	look := c.player.Appearance()
	crouching := look.Pose == protocol.PoseSneaking
	v := c.player.Vitals()
	// The frost slow is the only modifier that reads the ground, and only a
	// body carrying frozen ticks can wear it; every other tick skips the
	// world lookup.
	var underfootAir bool
	if v.TicksFrozen > 0 {
		pos := c.player.Position()
		dims := c.dims(c.player.Appearance().Pose)
		underfootAir = rules.FrostSlowAir(c.theWorld(), pos.X, pos.Y, pos.Z, dims.Width/2, pos.OnGround)
	}
	c.player.AttributesWithEquipment(func(a *player.Attributes, now player.Equipment) {
		rules.UpdateEquipmentAttributes(a, now)
		rules.UpdateStateAttributes(a, creative, look.Sprinting, crouching)
		rules.UpdateFreezeAttributes(a, v.TicksFrozen, underfootAir)
		rules.UpdateEffectAttributes(a, effects)
	})
}

// introduceAttributes writes the player their whole attribute map, with the
// equipment's modifiers on: what a new body on the client starts from.
func (c *client) introduceAttributes() error {
	c.refreshAttributes(c.player.Effects())
	var attrs []player.AttributeInstance
	c.player.Attributes(func(a *player.Attributes) { attrs = a.Introduce() })
	return c.writePacket(protocol.IDPlayUpdateAttributes, "update attributes", attributesPayload(c.player.EntityID, attrs))
}

// tickAttributes refreshes the player's attributes and sends the ones that
// changed to the player and to everyone who can see them.
// vanilla: LivingEntity.detectEquipmentUpdates, ServerEntity.sendDirtyEntityData
func (c *client) tickAttributes() {
	c.refreshAttributes(c.player.Effects())
	c.tickPackLocated()
	var dirty []player.AttributeInstance
	c.player.Attributes(func(a *player.Attributes) { dirty = a.TakeDirty() })
	if len(dirty) > 0 {
		c.srv.broadcastWorld(c.theWorld(), protocol.IDPlayUpdateAttributes, attributesPayload(c.player.EntityID, dirty))
	}
}

// removeEntity builds the packet that takes a player's body back out again.
func removeEntity(p *player.Player) []byte {
	var b protocol.Buffer
	b.PutVarInt(1)
	b.PutVarInt(p.EntityID)
	return b.Bytes()
}

// bodyVisibleTo reports whether viewer is shown body's body at all, whatever
// the range between them.
func bodyVisibleTo(body, viewer *client) bool {
	return rules.SpectatorBodyVisible(
		viewer.player.GameMode() == protocol.GameModeSpectator,
		body.player.GameMode() == protocol.GameModeSpectator,
		body.camera.Load() == nil) && !body.bodyGone.Load() && !viewer.hides(body.player.EntityID)
}

// bodyVisibleToSnap is bodyVisibleTo over the tracking pass's snapshots.
func bodyVisibleToSnap(body, viewer *pairedPlayer) bool {
	return rules.SpectatorBodyVisible(viewer.spectator, body.spectator, body.onOwnCamera) && !body.gone && !viewer.c.hides(body.c.player.EntityID)
}

// tickDeadBodies counts the ticks each dead body has lain; the one whose
// corpseTicks are up leaves everyone who holds it, after the death poof, and
// fires its death effects.
// vanilla: LivingEntity.tickDeath
func (s *Server) tickDeadBodies() {
	s.mu.RLock()
	clients := s.clientsLocked()
	between := s.corpseExpirySeam
	s.mu.RUnlock()
	for _, c := range clients {
		if !c.joined() || !c.player.Vitals().Dead || c.holdsDropped.Load() {
			continue
		}
		// A dead join counts only once its first chunk fill is out.
		if c.deadHealthPending.Load() {
			continue
		}
		// Dead is rechecked under the vitals lock the respawn resets the body
		// under, so a tick that raced the respawn cannot count on the new body.
		c.trackMu.Lock()
		var ticks int32
		if !c.traveling.Load() {
			c.player.UpdateBody(func(v *player.Vitals, _ *effect.Set, _ *player.Attributes) {
				if v.Dead {
					ticks = c.deathTicks.Add(1)
				}
			})
		}
		c.trackMu.Unlock()
		if ticks < corpseTicks {
			continue
		}
		if between != nil {
			between()
		}
		c.expireCorpse()
	}
}

// expireCorpse removes the body and takes its effects before a respawn can
// replace it under the same entity ID.
func (c *client) expireCorpse() {
	c.trackMu.Lock()
	if c.traveling.Load() || c.deathTicks.Load() < corpseTicks {
		c.trackMu.Unlock()
		return
	}
	wasGone := c.bodyGone.Swap(true)
	w := c.theWorld()
	for _, viewer := range c.seenBy {
		viewer.sendIn(w, protocol.IDPlayEntityEvent,
			entityEventPayload(c.player.EntityID, protocol.EntityStatusDeathPoof))
		viewer.sendIn(w, protocol.IDPlayRemoveEntities, removeEntity(c.player))
	}
	clear(c.seenBy)
	var trigger mobEffectTrigger
	if !wasGone {
		trigger = c.takeDeathEffects()
	}
	c.trackMu.Unlock()

	// Effects need the corpse's chunk and take the mob and world locks.
	c.srv.fireMobEffectTrigger(trigger)
	c.trackMu.Lock()
	drop := !c.traveling.Load() && c.deathTicks.Load() >= corpseTicks
	if drop {
		c.holdsDropped.Store(true)
	}
	c.trackMu.Unlock()
	if drop {
		c.sendChunksAsync()
	}
}

// showPlayer queues the packets that make a player appear for this client. The
// profile goes first: the client draws an entity with whatever it knows about
// that player at the moment it spawns. The metadata follows it, so that someone
// who was already sneaking is not drawn standing up until they next move, and
// the attributes and equipment after, vanilla's sendPairingData order, the
// equipment last so that a player joining in armour is drawn wearing it rather
// than having it pop in. The body is w's (see sendIn); the profile is the
// whole server's.
func (c *client) showPlayer(p *player.Player, w *world.World, withBody bool) {
	c.send(protocol.IDPlayPlayerInfoUpdate, playerInfo(playerInfoEntry{player: p}))
	if withBody {
		c.sendAllIn(w, playerPairing(p, spawnPlayer(p)))
	}
}

// playerPairing is the packets that put a player's body into a viewer's
// world: the spawn and what follows it, without the profile, which the tab
// list is the whole server's and so never falls away with the body. spawn is
// the body's spawn, built by the caller from the position that fits its
// record: the live position at the join, the movement baseline in the
// tracker.
// vanilla: ServerEntity.sendPairingData
func playerPairing(p *player.Player, spawn []byte) []outbound {
	return bundle(pairing(
		outbound{id: protocol.IDPlaySpawnEntity, data: spawn},
		outbound{id: protocol.IDPlayEntityMetadata, data: playerMetadata(p, true)},
		outbound{id: protocol.IDPlayUpdateAttributes, data: playerAttributes(p)},
		outbound{id: protocol.IDPlayEntityEquipment, data: playerEquipment(p)},
	))
}

// broadcastEquipment tells the players in this player's dimension what this
// player is now holding and wearing. The player it is about is not told: a
// client draws its own equipment from its own inventory, and would fight the
// server over it.
func (c *client) broadcastEquipment() {
	c.srv.broadcastWorldExcept(c.theWorld(), c, protocol.IDPlayEntityEquipment, playerEquipment(c.player))
}

// updateAppearance applies a change to what this player looks like and tells
// the players in their dimension about it, unless it changed nothing. Their
// own client is told too, rather than only the others: a client draws its own
// player from the metadata of its own entity, so a player who switches off
// their cape in the options menu would otherwise go on seeing it until they
// rejoined. The vanilla server sends entity data the same way round.
//
// The pose is re-derived after whatever the change was — the pose follows
// the sneak, the sprint, the swim and the flight the same change carried —
// so the metadata a change goes out in carries the pose the vanilla server's
// per-tick decision would have settled on by the time it broadcast: a sprint
// begun in water is drawn swimming, not standing for a moment first.
//
// Metadata is absolute rather than a difference from what went before, so a
// client that misses one is put right by the next - and an entry left out
// says nothing at all. That is why a use coming down is its own packet: the
// hand state a meal or a bow held up is zeroed in so many words, the way the
// vanilla server's dirty-data update carries the byte it changed, or a
// client would go on drawing the bite that was already swallowed.
func (c *client) updateAppearance(change func(*player.Appearance)) {
	// Posted outside lookMu: a vibration reaches the world's lock.
	switch item, edge := c.changeAppearance(change); edge {
	case useStarted: // vanilla: LivingEntity.startUsingItem
		c.useVibration(item, rules.GameEventItemInteractStart)
	case useEnded: // vanilla: LivingEntity.stopUsingItem
		c.useVibration(item, rules.GameEventItemInteractFinish)
	}
}

// useEdge is what a change of appearance did to an item use.
type useEdge uint8

const (
	useUnchanged useEdge = iota
	useStarted
	// useEnded is a use whose hand dropped with its stack unspent.
	useEnded
)

// changeAppearance is updateAppearance under lookMu. item is the item of the
// use edge started or ended.
func (c *client) changeAppearance(change func(*player.Appearance)) (item protocol.ItemStack, edge useEdge) {
	c.lookMu.Lock()
	defer c.lookMu.Unlock()
	was := c.player.Appearance()
	look := was
	change(&look)
	c.refreshPose(&look)
	if look == was {
		return protocol.ItemStack{}, useUnchanged
	}
	switch {
	case look.Eating && !was.Eating:
		c.player.Inventory(func(inv *player.Inventory) {
			c.useItem = inv.Slots[rules.SlotOfHand(inv, look.EatingHand)]
		})
		c.useSpent = false
		edge = useStarted
	case was.Eating && !look.Eating && !c.useSpent:
		edge = useEnded
	}
	c.player.SetAppearance(look)
	if (was.SkinParts^look.SkinParts)&skinPartHat != 0 {
		// The tab list is the whole server's, the sender's own entry included.
		// vanilla: ServerGamePacketListenerImpl.handleClientInformation
		c.srv.broadcast(protocol.IDPlayPlayerInfoUpdate, playerInfoHat(c.player))
	}
	c.srv.broadcastWorld(c.theWorld(), protocol.IDPlayEntityMetadata, playerMetadata(c.player, true))
	if (was.Eating && !look.Eating) || (was.SpinAttack && !look.SpinAttack) {
		c.srv.broadcastWorld(c.theWorld(), protocol.IDPlayEntityMetadata,
			handStateMetadata(c.player, handStateOf(look)))
	}
	return c.useItem, edge
}

// spendUse marks the running use's stack as used up, so its end posts no
// vibration. vanilla: LivingEntity.stopUsingItem on an emptied useItem
func (c *client) spendUse() {
	c.lookMu.Lock()
	c.useSpent = true
	c.lookMu.Unlock()
}

// useVibration posts event, an item_interact one, at the player for a use
// of stack, unless its use_effects turns interact_vibrations off.
// vanilla: ItemStack.causeUseVibration
func (c *client) useVibration(item protocol.ItemStack, event rules.GameEvent) {
	if !registry.StackInteractVibrations(item) {
		return
	}
	pos := c.player.Position()
	c.gameEventAt(event, pos.X, pos.Y, pos.Z, 0)
}

// refreshPose works out the pose the body is drawn in, vanilla's
// Player.updatePlayerPose: the pose the body's state asks for, forced down
// to whatever fits where the body stands. The fit test is the pose's box
// built around where the body is and shaved by the millionth vanilla's own
// test shaves, left uncollided by every block around it — the entities half
// of vanilla's noCollision not modelled here, bodies never pushing one
// another — and when even the swimming pose's box does not fit, the pose is
// left exactly as it stands, the early return vanilla runs first.
//
// The caller gets the appearance handed to it rather than read off the
// player, so that the pose a change of input goes out with is the one
// derived from the inputs the change carried, in the same packet.
func (c *client) refreshPose(look *player.Appearance) {
	sleep := c.player.Sleep()
	// The glide is synced here so that every appearance the server derives
	// carries the flag the client's own answer left, the same way the pose
	// is: a change with the wings already open goes out with the bit set.
	look.FallFlying = c.fallFlying.Load()
	desired := rules.DesiredPose(sleep.Lying, look.Swimming, look.FallFlying, look.SpinAttack,
		look.Sneaking, c.flying.Load())
	free := c.player.GameMode() == protocol.GameModeSpectator || c.isPassenger()
	if pose, ok := rules.PlayerPose(desired, free, c.poseFits); ok {
		look.Pose = pose
	}
}

// dims is the player's body shape in the pose, scaled by
// minecraft:scale except when sleeping. It takes the player's lock.
// vanilla: LivingEntity.getDimensions
func (c *client) dims(pose rules.Pose) rules.PlayerDimensions {
	d := rules.PlayerDimensionsOf(pose)
	if pose == protocol.PoseSleeping {
		return d
	}
	var scale float64
	c.player.Attributes(func(a *player.Attributes) {
		scale = a.Value(registry.AttributeScale)
	})
	// LivingEntity.getScale narrows the attribute to a float.
	return d.Scaled(float64(float32(scale)))
}

// sized gives the mob snapshot the box of the player's pose and scale.
func (c *client) sized(p entity.Player) entity.Player {
	d := c.dims(c.player.Appearance().Pose)
	p.Width, p.Height, p.EyeHeight = d.Width, d.Height, d.EyeHeight
	return p
}

// projectileTarget is the player as a projectile's target at pos: the box of
// its pose and scale.
func (c *client) projectileTarget(pos player.Position, unhittable bool) entity.ArrowTarget {
	d := c.dims(c.player.Appearance().Pose)
	return entity.SizedPlayerTarget(c.player.EntityID, pos.X, pos.Y, pos.Z, d.Width, d.Height, d.EyeHeight, unhittable)
}

// poseFits is vanilla's canPlayerFitWithinBlocksAndEntitiesWhen: whether a
// body of the pose's shape stands free where this player stands. The box is
// the pose's dimensions built around the reported position and shaved by the
// millionth vanilla's own box is shaved by, so resting against a wall is
// standing free of it; the world's word on the blocks is the whole of the
// answer this server has, the entities half of the vanilla question having
// nothing to push against.
func (c *client) poseFits(pose rules.Pose) bool {
	dims := c.dims(pose)
	pos := c.player.Position()
	const shave = 1e-7
	return !c.theWorld().BoxCollides(
		pos.X-dims.Width/2+shave, pos.Y+shave, pos.Z-dims.Width/2+shave,
		pos.X+dims.Width/2-shave, pos.Y+dims.Height-shave, pos.Z+dims.Width/2-shave)
}

// inRangeOfGround reports whether the player's body is free of blocks and a
// block's collision lies less than one block under the feet; a top exactly
// one below only touches the slab and does not count. Entity colliders are
// left out, as in poseFits.
// vanilla: ServerPlayerGameMode.isInRangeOfGround
func (c *client) inRangeOfGround() bool {
	if !c.poseFits(c.player.Appearance().Pose) {
		return false
	}
	w := rules.PlayerDimensionsOf(c.player.Appearance().Pose).Width
	pos := c.player.Position()
	const shave = 1e-7
	return c.theWorld().BoxCollides(
		pos.X-w/2+shave, pos.Y-1+shave, pos.Z-w/2+shave,
		pos.X+w/2-shave, pos.Y-shave, pos.Z+w/2-shave)
}

// handStateOf is the hand-state byte of an appearance: the hand active, the
// off-hand bit when the use is held there, and the riptide spin.
func handStateOf(look player.Appearance) uint8 {
	var state uint8
	if look.Eating {
		state = protocol.EntityFlagHandActive
		if look.EatingHand == protocol.HandOff {
			state |= protocol.EntityFlagActiveHandOff
		}
	}
	if look.SpinAttack {
		state |= protocol.EntityFlagSpinAttack
	}
	return state
}

// handStateMetadata builds the Set Entity Metadata carrying only a player's
// hand-state byte, which is how a hand raised by a use is put back down on
// the clients that were told it was up.
func handStateMetadata(p *player.Player, state uint8) []byte {
	var b protocol.Buffer
	b.PutVarInt(p.EntityID)
	b.PutMetadata(protocol.MetadataIndexHandState, protocol.MetadataByte)
	b.PutU8(state)
	b.EndMetadata()
	return b.Bytes()
}

// sleepEndMetadata builds the Set Entity Metadata that stands a sleeper back
// up: the pose their appearance now says they hold, and the sleeping position
// emptied in so many words. The empty entry is the point of the packet —
// metadata is absolute, and an entry left out says nothing at all, so without
// it every client would go on holding the body in the bed the last packet put
// it in.
func sleepEndMetadata(p *player.Player) []byte {
	look := p.Appearance()
	pose := look.Pose
	var b protocol.Buffer
	b.PutVarInt(p.EntityID)
	b.PutMetadata(protocol.MetadataIndexPose, protocol.MetadataPose)
	b.PutVarInt(pose)
	b.PutMetadata(protocol.MetadataIndexSleepingPos, protocol.MetadataOptionalPosition)
	b.PutBool(false)
	b.EndMetadata()
	return b.Bytes()
}

// swimmingNow reports whether the player should be swimming given their
// current appearance, matching the vanilla server's Player.updateSwimming:
//
//   - A flying player never swims: creative flight is a top-level guard that
//     short-circuits everything else, which is the fix for the vanilla bug where
//     sprinting underwater while flying put the player into the swim pose.
//   - A passenger never swims: a body in a seat is where its vehicle keeps it.
//   - Swimming starts when the player is sprinting, their eyes are underwater
//     and the feet block holds water.
//   - Once swimming, it continues while they are sprinting and any of their body
//     is in water: isInWater tests the whole bounding box, so the centre may
//     stand beside the water the box still reaches.
//   - Stopping sprinting stops swimming immediately, regardless of the water.
//
// A player merely standing in water is not swimming: the vanilla server sets
// the flag only while sprinting, and so does this.
//
// The eyes the start test looks out of sit where the body's current pose
// puts them — vanilla's getEyeY reads the pose's own eye height — so a body
// already drawn in the crawl's low pose starts its swim from four tenths up,
// the way the vanilla one does.
func (c *client) swimmingNow(look player.Appearance) bool {
	if c.flying.Load() {
		return false
	}
	if !look.Sprinting {
		return false
	}
	if c.isPassenger() {
		return false
	}
	w := c.theWorld()
	dims := c.dims(look.Pose)
	pos := c.player.Position()
	inWater, eyesIn, _ := entity.BoxInWater(w, pos.X, pos.Y, pos.Z,
		dims.Width, dims.Height, pos.Y+dims.EyeHeight)
	if look.Swimming {
		// Continue: only the body needs to be in water, not the eyes, which
		// is what keeps a player swimming as they break the surface instead
		// of flickering between the two states.
		return inWater
	}
	// Start: the eyes have to be underwater and the feet block's fluid a
	// water one, vanilla's isUnderWater and
	// getFluidState(blockPosition).is(WATER).
	return eyesIn && rules.IsWater(w.BlockAt(
		int32(math.Floor(pos.X)), int32(math.Floor(pos.Y)), int32(math.Floor(pos.Z))))
}

// updateSwimming re-evaluates the swimming state from where the player is and
// broadcasts the change, if there was one. The vanilla server does this every
// tick; this server has no tick loop, so it is done wherever a position is
// recorded, wherever sprinting changes, and wherever flying changes, which is
// often enough that a player entering or leaving water is seen to do so without
// a noticeable gap.
//
// Going through updateAppearance keeps the no-op case quiet: a position packet
// that leaves the player in the same state sends nothing, the way a repeated
// sneak key does not either.
func (c *client) updateSwimming() {
	c.updateAppearance(func(a *player.Appearance) {
		a.Swimming = c.swimmingNow(*a)
	})
}

// hidePlayer queues the packets that take a player away again.
func (c *client) hidePlayer(p *player.Player) {
	c.send(protocol.IDPlayRemoveEntities, removeEntity(p))
	c.send(protocol.IDPlayPlayerInfoRemove, playerInfoRemove(p))
}

// trackedPosition is where the other players have been told a player is. The
// coordinates are kept in the 1/4096 of a block that the movement packets
// measure a step in, and the angles as the byte they are sent as, so that what
// is sent is the difference between two values the client has, and rounding
// cannot accumulate into a drift between where a player is and where everyone
// else sees them.
type trackedPosition struct {
	x, y, z    int64
	yaw, pitch uint8
	onGround   bool
	known      bool // false until the player has been spawned for anyone
}

// track converts a position into what the other players are told about it.
func track(pos player.Position) trackedPosition {
	return trackedPosition{
		x:        encodeStep(pos.X),
		y:        encodeStep(pos.Y),
		z:        encodeStep(pos.Z),
		yaw:      encodeAngle(pos.Yaw),
		pitch:    encodeAngle(pos.Pitch),
		onGround: pos.OnGround,
		known:    true,
	}
}

// clampBodyPitch keeps a stored pitch between straight down and straight up,
// the range vanilla clamps a body's rotation to.
// vanilla: Entity.setXRot
func clampBodyPitch(pitch float32) float32 {
	return min(max(pitch, -90), 90)
}

// snapLook is the snap the vanilla server gives a rotation it is about to
// keep: the yaw folded into [-180, 180), the pitch between straight down and
// straight up.
// vanilla: Mth.wrapDegrees, Entity.absSnapRotationTo, Entity.setXRot
func snapLook(yaw, pitch float32) (float32, float32) {
	return command.WrapDegrees(yaw), clampBodyPitch(command.WrapDegrees(pitch))
}

// absSnapLook is the snap a teleport's angles get at the client's accept:
// each taken mod 360 with the sign kept, which leaves a yaw of 200 at 200
// where the move path's wrap would fold it, and the pitch then clamped.
// vanilla: Entity.absSnapTo, Entity.setRot
func absSnapLook(yaw, pitch float32) (float32, float32) {
	return float32(math.Mod(float64(yaw), 360)), clampBodyPitch(float32(math.Mod(float64(pitch), 360)))
}

// positionStep is the 1/4096 of a block a movement packet measures a step in.
const positionStep = 4096

// encodeStep converts a coordinate into the 1/4096 of a block that a movement
// packet measures a step in.
func encodeStep(coordinate float64) int64 {
	return int64(math.Round(coordinate * positionStep))
}

// encodeAngle converts degrees into the 1/256 of a full turn an angle is sent
// as, matching protocol.Buffer.PutAngle.
func encodeAngle(degrees float32) uint8 {
	return uint8(int32(math.Floor(math.Mod(float64(degrees), 360) * 256 / 360)))
}

// maxStep is the furthest a single relative move can carry a player, in the
// units a step is measured in: the deltas are signed 16 bit numbers, so a
// step of 8 blocks or more has to be sent as a teleport instead.
const maxStep = math.MaxInt16

// The numbers the entity movement schedule runs on, the vanilla server's
// own ServerEntity ones, read out of the server jar's bytecode:
//
//   - a position update is only worth sending once the entity has moved
//     far enough since the last one for the client to see it — the distance
//     squared compared against 7.62939453125e-6 blocks squared, two to the
//     minus seventeen, which in the 1/4096-of-a-block units the packets
//     measure a step in is one hundred and twenty-eight squared units;
//   - an entity that has gone four hundred movement ticks without an
//     absolute one is put back on record with a full Teleport Entity, so
//     nothing a dropped or lost packet did can drift it;
//   - and every sixtieth tick a position update goes out whether the
//     entity moved or not, carrying the on-ground flag round again.
const (
	movedThresholdSq    = 128
	teleportResyncAfter = 400
	forcedPosPeriod     = 60
)

// inTrackingRange reports whether a player standing at (px, pz) is shown an
// entity standing at (ex, ez), the entity's kind seen from rangeChunks away
// scaled by pct percent (entity-broadcast-range-percentage) and the player's
// chunks streamed radius chunks around them. Vanilla's
// ChunkMap$TrackedEntity.updatePlayer asks two things: whether the
// horizontal distance between the two is within min(the scaled range in
// blocks, the player's view distance in blocks) — a circle, not a square,
// the two axes squared and summed — and whether the entity's chunk is one
// the player is actually being sent. The chunk half is answered here by the
// streamed square's radius rather than by reading the client's own chunk
// set, which is the chunk sender's bookkeeping under its own lock; the
// sender keeps the square filled out to the same radius, and lags a moment
// behind a fast walk.
// vanilla: ChunkMap$TrackedEntity.getEffectiveRange, DedicatedServer.getScaledTrackingDistance
func inTrackingRange(px, pz, ex, ez float64, rangeChunks, pct int, radius int32) bool {
	limit := trackingRangeBlocks(rangeChunks, pct, radius)
	dx, dz := px-ex, pz-ez
	if dx*dx+dz*dz > float64(limit*limit) {
		return false
	}
	entityChunk := world.ChunkPos{X: world.ChunkOf(ex), Z: world.ChunkOf(ez)}
	playerChunk := world.ChunkPos{X: world.ChunkOf(px), Z: world.ChunkOf(pz)}
	return world.InViewOf(entityChunk, playerChunk, radius)
}

func trackingRangeBlocks(rangeChunks, pct int, radius int32) int {
	// Integer division after the multiply, as vanilla's int arithmetic.
	return min(int(radius)*16, pct*rangeChunks*16/100)
}

// pairing is what shows one body to a client, vanilla
// ServerEntity.sendPairingData: the spawn, then what follows it. A nil
// payload is one the body does not carry, and is left out.
func pairing(packets ...outbound) []outbound {
	out := packets[:0]
	for _, p := range packets {
		if p.data != nil {
			out = append(out, p)
		}
	}
	return out
}

// spawnAndMetadata is the pairing of a body whose only follow-up is its
// metadata.
func spawnAndMetadata(spawn, metadata []byte) []outbound {
	return bundle(pairing(outbound{id: protocol.IDPlaySpawnEntity, data: spawn}, outbound{id: protocol.IDPlayEntityMetadata, data: metadata}))
}

// bundle puts packets between two Bundle Delimiters, so the client applies
// them on one tick.
// vanilla: ServerEntity.addPairing, ClientboundBundlePacket
func bundle(packets []outbound) []outbound {
	out := make([]outbound, 0, len(packets)+2)
	out = append(out, outbound{id: protocol.IDPlayBundleDelimiter})
	out = append(out, packets...)
	return append(out, outbound{id: protocol.IDPlayBundleDelimiter})
}

// showTo is a client a tick's pairing or removal goes to, with the world
// the tick decided it in.
type showTo struct {
	c *client
	w *world.World
}

// send queues a packet for t's client unless it has left t's world.
func (t showTo) send(packetID int32, data []byte) { t.c.sendIn(t.w, packetID, data) }

// writeAll writes packets straight to the connection, in order, for the
// join's and a dimension arrival's send-offs.
func (c *client) writeAll(packets []outbound) error {
	if err := c.conn.WritePacketsCached(len(packets), func(i int) (int32, []byte, *protocol.CachedPacket) {
		return packets[i].id, packets[i].data, packets[i].shared
	}); err != nil {
		return fmt.Errorf("write send-off: %w", err)
	}
	return nil
}

// forgetObserver drops a player from every kind's per-observer records and
// from the players' pairing records: on leaving, and on a dimension change,
// whose Respawn clears the client's world.
func (s *Server) forgetObserver(c *client) {
	for _, k := range observerTrackers() {
		k.forget(s, c)
	}
	s.forgetBodies(c)
}

// observerRemovals returns the ids the observer's tracking records say it
// holds: every kind's entries and the player bodies paired with it. A
// same-dimension Respawn leaves the client's world standing, so these have
// to be sent back out as Remove Entities before it; a dimension change's
// new world takes them with it and needs none.
func (s *Server) observerRemovals(c *client) []int32 {
	id := c.player.UUID
	var ids []int32
	for _, k := range observerTrackers() {
		ids = k.held(s, c, ids)
	}
	s.mu.RLock()
	for _, other := range s.clients {
		if other == c {
			continue
		}
		other.trackMu.Lock()
		held := other.seenBy[id] == c
		other.trackMu.Unlock()
		if held {
			ids = append(ids, other.player.EntityID)
		}
	}
	s.mu.RUnlock()
	return ids
}

// forgetBodies takes a player out of every client's record of the bodies
// they hold, and empties their own, beside the packets that do the same to
// the clients: a departure's Remove Entities and Player Info Remove, and a
// dimension change's Respawn, which clears the whole world the client
// had — its bodies along with its chunks.
func (s *Server) forgetBodies(traveler *client) {
	s.mu.RLock()
	for _, other := range s.clients {
		other.trackMu.Lock()
		delete(other.seenBy, traveler.player.UUID)
		other.trackMu.Unlock()
	}
	s.mu.RUnlock()
	traveler.trackMu.Lock()
	clear(traveler.seenBy)
	traveler.trackMu.Unlock()
}

// recordBody notes every player in w but the arrived as holding the arrived's
// body, beside the spawn broadcast the caller has just made to the whole
// dimension — a respawn's or a dimension arrival's. What the clients were
// sent is what the record says; the tick's settle takes the out-of-range
// holders back out.
func (s *Server) recordBody(w *world.World, arrived *client) {
	s.mu.RLock()
	arrived.trackMu.Lock()
	for _, other := range s.clients {
		if other != arrived && other.theWorld() == w && bodyVisibleTo(arrived, other) {
			arrived.seenBy[other.player.UUID] = other
		}
	}
	arrived.trackMu.Unlock()
	s.mu.RUnlock()
}

// pairedPlayer is one player as the tracking pass reads them, once a tick:
// who they are and where they stand, and the chunks of world their sight
// reaches from.
type pairedPlayer struct {
	c      *client
	w      *world.World
	x, z   float64
	radius int32

	// Visibility inputs, read once so the pair loop takes no per-pair lock;
	// the settle phase re-checks what it acts on under trackMu.
	spectator   bool
	onOwnCamera bool
	gone        bool
	wonGame     bool
	joined      bool
}

// tickPlayerTracking settles which players each client holds the body of,
// against the tracking range the player type carries and each viewer's own
// sight, vanilla's ChunkMap$TrackedEntity.updatePlayer: a pair drifted out
// of range is unmade — the body alone leaves; a tab-list entry falls away
// only with the player — and a pair back in is remade from the whole spawn.
// The clients' own chunks ride the same range, and drop the bodies in them
// as they unload, so without the resend a teleport far and back leaves both
// sides looking straight through each other.
func (s *Server) tickPlayerTracking() {
	s.mu.RLock()
	present := make([]pairedPlayer, 0, len(s.clients))
	for _, c := range s.clients {
		pos := c.player.Position()
		present = append(present, pairedPlayer{
			c: c, w: c.theWorld(), x: pos.X, z: pos.Z, radius: c.chunkRadius(),
			spectator:   c.player.GameMode() == protocol.GameModeSpectator,
			onOwnCamera: c.camera.Load() == nil,
			gone:        c.bodyGone.Load(),
			wonGame:     c.wonGame.Load(),
			joined:      c.joined(),
		})
	}
	s.mu.RUnlock()

	s.mu.RLock()
	trackSeam := s.playerTrackSeam
	s.mu.RUnlock()
	if trackSeam != nil {
		trackSeam()
	}

	type settle struct {
		viewer *client
		body   *client
		w      *world.World
		pair   bool
	}
	var settles []settle
	pairing := false
	for i := range present {
		present[i].c.trackIdx = i
	}
	told := make([]bool, len(present))
	for j := range present {
		body := &present[j]
		// A player held on the end credits was removed from the
		// dimension's sight when the credits went on; the pass leaves
		// their body out until the credits' respawn moves them home.
		if body.wonGame {
			continue
		}
		// The record that changes hands is under the mover's lock, which
		// forgetBodies and move() take too.
		body.c.trackMu.Lock()
		clear(told)
		stale := false
		for id, holder := range body.c.seenBy {
			// A record left by a reconnect names the old connection, so
			// only a holder still present under its own UUID counts.
			if i := holder.trackIdx; i < len(present) && present[i].c == holder && holder.player.UUID == id {
				told[i] = true
			} else {
				stale = true
			}
		}
		for i := range present {
			viewer := &present[i]
			// A viewer still in its join is left to the join: sendPlayerInfo
			// sends its whole view of the others, once, and the tick pairing
			// it in between would send the same bodies a second time.
			if !viewer.joined || i == j || viewer.w != body.w {
				continue
			}
			seen := inTrackingRange(viewer.x, viewer.z, body.x, body.z,
				entity.PlayerTrackingRange, int(s.entityBroadcastRange.Load()), viewer.radius) && bodyVisibleToSnap(body, viewer)
			if stale && !told[i] {
				if held, ok := body.c.seenBy[viewer.c.player.UUID]; ok && held != viewer.c {
					delete(body.c.seenBy, viewer.c.player.UUID)
				}
			}
			switch {
			case seen && !told[i]:
				body.c.seenBy[viewer.c.player.UUID] = viewer.c
				settles = append(settles, settle{viewer: viewer.c, body: body.c, w: viewer.w, pair: true})
				pairing = true
			case !seen && told[i]:
				delete(body.c.seenBy, viewer.c.player.UUID)
				settles = append(settles, settle{viewer: viewer.c, body: body.c, w: viewer.w})
			}
		}
		body.c.trackMu.Unlock()
	}

	s.mu.RLock()
	sendSeam := s.playerSendSeam
	s.mu.RUnlock()
	if sendSeam != nil && pairing {
		sendSeam()
	}

	for _, st := range settles {
		if !st.pair {
			// The record is re-checked under the lock: an arrival or a
			// respawn in between records the body again, and the removal
			// would take out what the arrival just put back.
			st.body.trackMu.Lock()
			if _, held := st.body.seenBy[st.viewer.player.UUID]; held {
				st.body.trackMu.Unlock()
				continue
			}
			st.viewer.sendAllIn(st.w, []outbound{{id: protocol.IDPlayRemoveEntities, data: removeEntity(st.body.player)}})
			st.body.trackMu.Unlock()
			continue
		}
		// The spawn is built from the mover's movement baseline under its
		// lock, in order with move(): a step that landed since the decision
		// is folded in here rather than doubled by a spawn the client then
		// steps away from, and a crossing leaves the body to the next pass.
		// The send rides the same lock so a step cannot slip between the
		// baseline read and the queue.
		st.body.trackMu.Lock()
		if st.body.theWorld() != st.w || st.viewer.theWorld() != st.w ||
			st.body.traveling.Load() || st.viewer.traveling.Load() {
			delete(st.body.seenBy, st.viewer.player.UUID)
			st.body.trackMu.Unlock()
			continue
		}
		if packets := st.body.movementPackets(moveTick{teleported: true}); len(packets) != 0 {
			// A watcher newly in range folds any step the existing holders
			// have not been told into its spawn; the others cannot lose the
			// same step when the shared baseline moves with it.
			for _, holder := range st.body.seenBy {
				for _, packet := range packets {
					holder.sendIn(st.w, packet.id, packet.data)
				}
			}
		}
		st.body.tracked = track(st.body.player.Position())
		st.body.seenBy[st.viewer.player.UUID] = st.viewer
		packets := playerPairing(st.body.player, spawnPlayerAt(st.body.player, st.body.tracked))
		st.viewer.sendAllIn(st.w, packets)
		st.body.trackMu.Unlock()
	}

	// Only the tick goroutine touches the schedule, so the off ticks take no lock.
	var movements sendList
	for _, body := range present {
		mt := body.c.movement.step(entity.PlayerUpdateInterval, nil)
		if !mt.gate {
			continue
		}
		body.c.trackMu.Lock()
		if packets := body.c.movementPackets(mt); len(packets) != 0 {
			for _, viewer := range body.c.seenBy {
				for _, packet := range packets {
					movements.add(viewer, packet)
				}
			}
		}
		body.c.trackMu.Unlock()
	}
	movements.send()
}

// move records where a player has got to and tells everyone who holds the
// body, through sendMovement. own says the step is the player's own doing: a
// rider's step is the boat's report carrying them, whose survival — the
// fall's damage, the trampling — is the boat's to take, not theirs.
//
// A dead player's steps go unrecorded: their body stays where it fell until
// they respawn, which is what the others saw happen to it. A player held on
// the end credits moves nothing either — vanilla's handleMovePlayer refuses
// their steps while wonGame is set.
//
// A sleeping body's steps go unrecorded besides: the client reports the drift
// of its own display of the body it has lain in the bed, which the server
// leaves where the sleep put it — the vanilla server ignores the movement of
// a sleeper for the same reason. A body that has got away from its bed by
// more than a block of it is put back, the resync telling the client where
// the server says its body stands.
//
// A move is dropped while a teleport waits on the client's accept, and the
// check and the write are under c.tp.mu so a teleport cannot land between
// them.
func (c *client) move(pos player.Position, own bool) {
	c.moveStep(pos, own, false)
}

// moveReported is move for the step a client's own move packet reports,
// which the move checks may refuse (movechecks.go).
func (c *client) moveReported(pos player.Position) {
	pos, ok := c.pluginMove(pos)
	if !ok {
		return
	}
	c.moveStep(pos, true, true)
}

// moveStep is move with the move checks run on the step where checked.
func (c *client) moveStep(pos player.Position, own, checked bool) {
	c.tp.mu.Lock()
	was, moved := c.moveHeldChecked(pos, own, checked)
	fall := c.tp.fall
	c.tp.fall = nil
	c.tp.mu.Unlock()
	if fall != nil {
		c.trackMovement(fall[0], fall[1])
	}
	if moved && own {
		c.trackMovement(was, pos)
	}
}

// moveHeld is move's body with c.tp.mu already held, so a caller that has
// to check something else before the step — a cart's seat write checking
// its rider still holds it — can keep the check and the step in one
// critical section. moved is false where the step was dropped, and was
// is the value move's own survival side reads. A pending
// teleport drops only the player's own move, own true; a placement the
// server drives itself — a camera's snap, a seat's carry — is not from the
// position the client last saw, so the wait does not hold it back.
func (c *client) moveHeld(pos player.Position, own bool) (was player.Position, moved bool) {
	return c.moveHeldChecked(pos, own, false)
}

// moveHeldChecked is moveHeld with the move checks run where checked, after
// the teleport wait and the sleeper's hold have had their say.
// vanilla: ServerGamePacketListenerImpl.handleMovePlayer
func (c *client) moveHeldChecked(pos player.Position, own, checked bool) (was player.Position, moved bool) {
	if own && c.ignoreMove(pos) || c.player.Vitals().Dead || c.wonGame.Load() {
		return player.Position{}, false
	}
	if sl := c.player.Sleep(); sl.Lying {
		p := c.player.Position()
		dx, dy, dz := p.X-pos.X, p.Y-pos.Y, p.Z-pos.Z
		if dx*dx+dy*dy+dz*dz > 1 {
			c.send(protocol.IDPlaySyncPosition, c.awaitTeleport(command.Move{Pos: p}))
		}
		return player.Position{}, false
	}
	if checked && c.refuseStep(pos) {
		return player.Position{}, false
	}
	was = c.player.Position()
	// vanilla: ServerGamePacketListenerImpl.handlePlayerPositionChange, Entity.restituteMovementAfterCollisions
	if own {
		if at := c.bouncedAt.Swap(0); at != 0 && c.srv.ticksRun.Load() >= at {
			was.OnGround = false
		}
	}
	movement := player.Velocity{X: pos.X - was.X, Y: pos.Y - was.Y, Z: pos.Z - was.Z}
	if own {
		c.priorDY.Store(math.Float64bits(c.player.KnownMovement().Y))
	}
	c.player.SetKnownMovement(movement)
	if own && checked {
		c.noteMovement(movement.X, movement.Y, movement.Z)
	}

	c.noteFloating(was, pos)
	c.recordMovement(pos)
	return was, true
}

// sendMovement records the player at pos and gives the viewers that hold the
// body the movement packets they are owed at once. Teleports and teleport-like
// commands use it; ordinary steps wait for tickPlayerTracking. c.tp.mu is held,
// so a teleport cannot land between the position and the telling.
func (c *client) sendMovement(pos player.Position) {
	dim := c.dimension()
	c.player.SetPosition(pos)
	c.bouncedAt.Store(0)
	if c.moveSeam != nil {
		c.moveSeam()
	}
	c.trackMu.Lock()
	for _, p := range c.movementPackets(moveTick{teleported: true}) {
		for _, r := range c.seenBy {
			r.sendIn(dim.w, p.id, p.data)
		}
	}
	c.trackMu.Unlock()
}

// recordMovement is sendMovement for a step a client reported.
func (c *client) recordMovement(pos player.Position) {
	dim := c.dimension()
	c.player.SetPosition(pos)
	if c.moveSeam != nil {
		c.moveSeam()
	}
	c.trackMu.Lock()
	now := track(pos)
	if c.tracked.known && !inOneStep(now.x-c.tracked.x, now.y-c.tracked.y, now.z-c.tracked.z) {
		for _, p := range c.movementPackets(moveTick{teleported: true}) {
			for _, r := range c.seenBy {
				r.sendIn(dim.w, p.id, p.data)
			}
		}
	}
	c.trackMu.Unlock()
}

// movementPackets works out what the others must be told about the player now,
// from the baseline they were last told, and records that they were told it.
// The schedule's caller holds trackMu, so a move the client reports in between
// records the next packet's baseline and does not double this one.
func (c *client) movementPackets(mt moveTick) []outbound {
	was := c.tracked
	if !was.known {
		// Nobody has been told where this player is yet, which is the case
		// until they have been registered.
		return nil
	}
	pos := c.player.Position()
	abs := bodyPose{x: pos.X, y: pos.Y, z: pos.Z, yaw: pos.Yaw, pitch: pos.Pitch}
	now, packet, ok := moveDiff(c.player.EntityID, was, track(pos), abs, mt, 0)
	if !ok {
		return nil
	}
	c.tracked = now
	packets := []outbound{packet}

	// Turning the body is not enough to turn the head: without this the other
	// clients draw a player whose head stays where it was looking when they
	// last moved.
	if now.yaw != was.yaw || now.pitch != was.pitch {
		var head protocol.Buffer
		head.PutVarInt(c.player.EntityID)
		head.PutU8(now.yaw)
		packets = append(packets, outbound{id: protocol.IDPlayHeadRotation, data: head.Bytes()})
	}
	return packets
}

// The longest Teleport Entity and relative move payloads: an entity ID of at
// most five bytes, then the position and velocity doubles, the two rotation
// floats and on-ground; or three shorts, two angle bytes and on-ground.
const (
	teleportEntityLen    = 5 + 1 + 3*8 + 2*4 + 1
	entityPositionRotLen = 5 + 5 + 3*2 + 2
)

// inOneStep reports whether a movement fits in the relative packets, whose
// deltas are signed 16 bit numbers.
func inOneStep(dx, dy, dz int64) bool {
	return dx >= -maxStep && dx <= maxStep &&
		dy >= -maxStep && dy <= maxStep &&
		dz >= -maxStep && dz <= maxStep
}

// sendPlayerInfo puts the players already in the world on this client's tab
// list, this one first and the others after, and spawns the bodies of those
// in the same dimension — the tab list is the whole server's, but a body is
// only where its player is. It is the other half of what addPlayer did for
// everyone else, and is sent once the world is there to put the bodies in.
func (c *client) sendPlayerInfo(others []*client) error {
	// One packet for the whole tab list, the way the vanilla server sends
	// it, with the joining player first: the client learns of itself before
	// it learns of anyone else.
	c.srv.chatOrderMu.Lock()
	everyone := make([]playerInfoEntry, 0, len(others)+1)
	everyone = append(everyone, playerInfoEntry{c.player, c.chatSession})
	for _, other := range others {
		everyone = append(everyone, playerInfoEntry{other.player, other.chatSession})
	}
	c.srv.chatOrderMu.Unlock()
	// Updates queued since registration stay in the mailbox until join ends,
	// so they follow this snapshot even if its network write stalls.
	if err := c.writePacket(protocol.IDPlayPlayerInfoUpdate, "player info update", playerInfo(everyone...)); err != nil {
		return err
	}
	if c.srv.plugins != nil {
		for _, other := range others {
			if other.pluginListName.Load() != nil {
				c.send(protocol.IDPlayPlayerInfoUpdate, other.pluginListNamePacket())
			}
		}
	}

	// Only the others in this dimension get a body: the client draws the
	// player it is controlling itself, and spawning that one has strange
	// consequences — and a player in another dimension is only a name on a
	// list, not something to stand next to.
	c.srv.mu.RLock()
	joinSeam := c.srv.playerJoinSeam
	bodiesSeam := c.srv.playerJoinBodiesSeam
	c.srv.mu.RUnlock()
	if bodiesSeam != nil {
		bodiesSeam()
	}
	for _, other := range others {
		if other.dimension() != c.dimension() {
			continue
		}
		p := other.player
		// The spawn comes from the body's movement baseline under its lock,
		// in order with move(): a step that lands while this is being sent
		// is queued behind the spawn rather than lost, and the record is
		// set in the same hold so that step's own send knows the viewer.
		// The record may already hold this viewer: a crossing that arrived
		// here meanwhile broadcast the body and queued its spawn, which
		// flushes at release. A direct spawn beside it would spawn twice.
		if !bodyVisibleTo(other, c) {
			continue
		}
		other.trackMu.Lock()
		held := other.seenBy[c.player.UUID] == c
		other.seenBy[c.player.UUID] = c
		spawn := spawnPlayerAt(p, other.tracked)
		other.trackMu.Unlock()
		if held {
			continue
		}
		if joinSeam != nil {
			joinSeam()
		}
		if err := c.writeAll(playerPairing(p, spawn)); err != nil {
			return err
		}
	}

	// The client does need to be told what its own player looks like, though,
	// even though it made that body itself: the skin layers it asked for are a
	// setting the server holds, and without them back the player sees
	// themselves in third person without a cape or a jacket. The vanilla
	// server sends this at the same point.
	if err := c.writePacket(protocol.IDPlayEntityMetadata, "entity metadata", playerMetadata(c.player, false)); err != nil {
		return err
	}
	return c.introduceAttributes()
}
