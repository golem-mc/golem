package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
)

// spinSession is a riptide spin in flight: ticks left and the last
// position, the old end of the box a tick sweeps.
type spinSession struct {
	ticks int
	last  player.Position
	// weapon is the spin's starting stack; the hit reads and wears it
	// whatever the hotbar shows. vanilla: Player.startAutoSpinAttack
	weapon protocol.ItemStack
}

// startSpinAttack starts a riptide spin with the worn trident in hand: the
// push along the look, the flag and pose other clients draw from, and the
// level's sound. The client moves the body after the grounded launch.
// vanilla: TridentItem.releaseUsing, Player.startAutoSpinAttack
func (c *client) startSpinAttack(hand int32, strength float64) {
	pos := c.player.Position()
	var weapon protocol.ItemStack
	c.player.Inventory(func(inv *player.Inventory) {
		slot := rules.SlotOfHand(inv, hand)
		weapon = inv.Slots[slot]
		weapon = weapon.WithStackIdentity(c.srv.spinStacks.Add(1))
		inv.Slots[slot] = weapon
	})
	w := c.theWorld()
	x, y, z := rules.SpinAttackPush(pos.Yaw, pos.Pitch, strength)
	c.addVelocity(w, x, y, z)
	c.removeEntitiesOnShoulder()
	if pos.OnGround {
		dims := c.dims(c.player.Appearance().Pose)
		hw := dims.Width / 2
		bounds := [6]float64{pos.X - hw, pos.Y, pos.Z - hw, pos.X + hw, pos.Y + dims.Height, pos.Z + hw}
		_, lift, _ := entity.ClipDisplacement(w, bounds, 0, rules.SpinAttackGroundLift, 0)
		pos.Y += lift
		pos.OnGround = false
		c.move(pos, false)
	}
	// The tick loop must not consume the first tick before the launch finishes.
	// vanilla: TridentItem.releaseUsing, LivingEntity.aiStep (beforeTravelBox)
	c.spinMu.Lock()
	c.spin = spinSession{ticks: rules.SpinAttackTicks, last: c.player.Position(),
		weapon: weapon}
	c.updateAppearance(func(a *player.Appearance) { a.SpinAttack = true })
	c.spinMu.Unlock()
	c.srv.broadcastSoundHolder(w, rules.TridentSound(weapon), protocol.SoundCategoryPlayer,
		pos.X, pos.Y, pos.Z, 1.0, 1.0)
}

// attackWeapon is a blow's stack: the spin's while it lasts, else the main
// hand's. vanilla: Player.getWeaponItem
func (c *client) attackWeapon() protocol.ItemStack {
	if c.player.Appearance().SpinAttack {
		c.spinMu.Lock()
		defer c.spinMu.Unlock()
		return c.spin.weapon
	}
	return c.heldItem(protocol.HandMain)
}

// endSpinAttack clears the spin and its flag.
func (c *client) endSpinAttack() {
	c.spinMu.Lock()
	c.spin = spinSession{}
	c.spinMu.Unlock()
	c.updateAppearance(func(a *player.Appearance) { a.SpinAttack = false })
}

// tickSpinAttack counts the spin down and hits the first living body the
// tick's swept box overlaps, ending the spin and bouncing back; a spin that
// overlaps no entity but runs into a wall ends without the bounce.
// vanilla: LivingEntity.aiStep, checkAutoSpinAttack
func (c *client) tickSpinAttack() {
	c.spinMu.Lock()
	if c.spin.ticks <= 0 {
		c.spinMu.Unlock()
		return
	}
	pos := c.player.Position()
	last := c.spin.last
	// The first tick's before-travel box follows the release's upward move
	// and the client's first step, not the pre-launch shoulder position.
	// vanilla: LivingEntity.aiStep (beforeTravelBox)
	if c.spin.ticks == rules.SpinAttackTicks {
		last = pos
	}
	c.spin.ticks--
	c.spin.last = pos
	left := c.spin.ticks
	c.spinMu.Unlock()

	dims := c.dims(protocol.PoseSpinAttack)
	hw := dims.Width / 2
	box := [6]float64{
		min(last.X, pos.X) - hw, min(last.Y, pos.Y), min(last.Z, pos.Z) - hw,
		max(last.X, pos.X) + hw, max(last.Y, pos.Y) + dims.Height, max(last.Z, pos.Z) + hw,
	}
	if c.attackSpinTarget(box) {
		v := c.player.Velocity()
		c.setVelocity(c.theWorld(), v.X*rules.SpinAttackBounce, v.Y*rules.SpinAttackBounce, v.Z*rules.SpinAttackBounce)
		left = 0
	} else if c.horizontalCollision.Load() && c.spinBoxEmpty(box) {
		left = 0
	}
	if left <= 0 {
		c.endSpinAttack()
	}
}

// spinBoxEmpty excludes only the spinner and spectators: a non-living body
// prevents the wall-collision branch too.
// vanilla: LivingEntity.checkAutoSpinAttack, EntityGetter.getEntities
func (c *client) spinBoxEmpty(box [6]float64) bool {
	ws := worldScope{srv: c.srv}
	for _, e := range ws.Entities(command.EntityQuery{Dimension: c.DimensionName(), Box: &box}) {
		if other, ok := e.(*client); ok && (other == c || other.player.GameMode() == protocol.GameModeSpectator) {
			continue
		}
		return false
	}
	return true
}

// attackSpinTarget hits the first living body overlapping box and reports
// whether it found one.
// vanilla: LivingEntity.checkAutoSpinAttack, Player.doAutoAttackOnTouch
func (c *client) attackSpinTarget(box [6]float64) bool {
	w := c.theWorld()
	if id, ok := c.srv.mobInBox(w, box); ok {
		c.attackMob(id)
		return true
	}
	if victim := c.srv.playerInBox(w, c, box); victim != nil {
		c.attackClient(victim)
		return true
	}
	if id, ok := c.srv.armorStandInBox(w, box); ok {
		c.attackArmorStand(id)
		return true
	}
	if id, ok := c.srv.mannequinInBox(w, box); ok {
		c.attackMannequin(id)
		return true
	}
	return false
}

// mannequinInBox is the entity ID of a live mannequin of w whose box overlaps box.
func (s *Server) mannequinInBox(w *world.World, box [6]float64) (int32, bool) {
	s.mannequinsMu.Lock()
	defer s.mannequinsMu.Unlock()
	for _, ent := range s.mannequins {
		if ent.w != w || ent.mannequin.Dead {
			continue
		}
		x0, y0, z0, x1, y1, z1 := ent.mannequin.BlastBox()
		if rules.BoxesIntersect([3]float64{box[0], box[1], box[2]}, [3]float64{box[3], box[4], box[5]},
			[3]float64{x0, y0, z0}, [3]float64{x1, y1, z1}) {
			return ent.mannequin.ID, true
		}
	}
	return 0, false
}

// armorStandInBox is the entity ID of a live non-marker armor stand of w
// whose box overlaps box.
func (s *Server) armorStandInBox(w *world.World, box [6]float64) (int32, bool) {
	s.armorStandsMu.Lock()
	defer s.armorStandsMu.Unlock()
	for _, ent := range s.armorStands {
		if ent.w != w || ent.stand.Dead || ent.stand.Marker {
			continue
		}
		x0, y0, z0, x1, y1, z1 := ent.stand.BlastBox()
		if rules.BoxesIntersect([3]float64{box[0], box[1], box[2]}, [3]float64{box[3], box[4], box[5]},
			[3]float64{x0, y0, z0}, [3]float64{x1, y1, z1}) {
			return ent.stand.ID, true
		}
	}
	return 0, false
}

// mobInBox is the entity ID of a live mob of the world whose box overlaps box.
func (s *Server) mobInBox(w *world.World, box [6]float64) (int32, bool) {
	s.mobsMu.Lock()
	defer s.mobsMu.Unlock()
	h := s.herdOf(w)
	for _, m := range h.herd {
		if !m.Alive() {
			continue
		}
		x0, y0, z0, x1, y1, z1 := m.BlastBox()
		if rules.BoxesIntersect([3]float64{box[0], box[1], box[2]}, [3]float64{box[3], box[4], box[5]},
			[3]float64{x0, y0, z0}, [3]float64{x1, y1, z1}) {
			return m.ID, true
		}
	}
	return 0, false
}

// playerInBox is a living non-spectator player of w other than from whose
// box overlaps box.
func (s *Server) playerInBox(w *world.World, from *client, box [6]float64) *client {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, o := range s.clients {
		if o == from || o.theWorld() != w || o.player.Vitals().Dead ||
			o.player.GameMode() == protocol.GameModeSpectator {
			continue
		}
		x0, y0, z0, x1, y1, z1 := o.Box()
		if rules.BoxesIntersect([3]float64{box[0], box[1], box[2]}, [3]float64{box[3], box[4], box[5]},
			[3]float64{x0, y0, z0}, [3]float64{x1, y1, z1}) {
			return o
		}
	}
	return nil
}
