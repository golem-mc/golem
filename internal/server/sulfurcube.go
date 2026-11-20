package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

func (c *client) feedSulfurCube(ent *mobEntity, hand int32, held protocol.ItemStack) (bool, error) {
	s := c.srv
	s.mobsMu.Lock()
	m := ent.mob
	dropped, ok := m.AbsorbSulfurBlock(held)
	x, y, z, silent := m.X, m.Y, m.Z, m.Silent
	dropY := y + m.BbHeight()
	after := bodyInteraction(m)
	s.mobsMu.Unlock()
	if !ok {
		return false, nil
	}
	if !dropped.Empty() {
		s.spawnItemAtFeet(ent.w, dropped, x, dropY, z)
	}
	if !silent {
		s.broadcastSoundID(ent.w, registry.SoundEntitySulfurCubeAbsorb, protocol.SoundCategoryNeutral, x, y, z, 1, 1)
	}
	c.useSwing = swingServer
	c.interactedWithEntityAdvancements(after, held.ItemID)
	if c.player.GameMode() != protocol.GameModeCreative {
		return true, c.consumeHeld(hand)
	}
	return true, nil
}

// igniteSulfurCube lights an explosive cube with flint and steel or a fire
// charge, spends the item and awards its use. Anything else leaves the click
// to the other branches.
// vanilla: SulfurCube.mobInteract
func (c *client) igniteSulfurCube(ent *mobEntity, hand int32, held protocol.ItemStack) (bool, error) {
	s := c.srv
	enabled := c.theWorld().TNTExplodes()
	s.mobsMu.Lock()
	can := ent.mob.SulfurCanExplode()
	primed := can && ent.mob.PrimeSulfurWithRule(false, enabled)
	s.mobsMu.Unlock()
	if !primed {
		if can && !enabled {
			c.send(protocol.IDPlaySystemChat, systemChatPayload(protocol.Text{Translate: "block.minecraft.tnt.disabled"}, true))
		}
		return false, nil
	}
	var err error
	if registry.Item(held.ItemID) == registry.ItemFlintAndSteel {
		c.wearHeld(hand, 1)
	} else if c.player.GameMode() != protocol.GameModeCreative {
		err = c.consumeHeld(hand)
	}
	c.awardItemStat(registry.StatTypeUsed, held.ItemID, 1)
	c.useSwing = swingServer
	return true, err
}

// mobContact is a cube's contact damage on a player, caused by the cube at x, y, z.
// vanilla: SulfurCube.applyContactDamage
type mobContact struct {
	w          *world.World
	victimID   int32
	amount     float64
	damageType registry.DamageType
	cubeID     int32
	x, y, z    float64
	killer     *rules.Killer
}

// contactOf is the contact damage ev names, caused by its cube.
func (t *mobDimTick) contactOf(ev entity.MobEvent) mobContact {
	killer := &rules.Killer{Name: mobTypeName(ev.Mob)}
	if ent, ok := t.h.book[ev.Mob]; ok {
		killer = t.s.mobKiller(ent)
	}
	return mobContact{w: t.w, victimID: ev.Player.EntityID, amount: ev.Damage, damageType: ev.DamageType,
		cubeID: ev.Mob.ID, x: ev.Mob.X, y: ev.Mob.Y, z: ev.Mob.Z, killer: killer}
}

// sulfurBurnPlayer lands a cube's contact burn on c, scaled by difficulty
// (sulfur_cube_hot scales when_caused_by_living_non_player). vanilla: Player.hurtServer
func (s *Server) sulfurBurnPlayer(c *client, hurt mobContact) {
	amount := rules.ScaledWithDifficulty(hurt.amount, hurt.w.Difficulty())
	if amount <= 0 {
		return
	}
	c.hurt(amount, hurt.damageType, &damageCause{entityID: hurt.cubeID, x: hurt.x, y: hurt.y, z: hurt.z, killer: hurt.killer, shove: true})
}

// shooterHitSource is where a shooter stood and looked, used for breeze immunity and a filled cube
// whose shove it bends; nil for a shooter that is gone.
func shooterHitSource(ownerID int32, bodies *arrowBodies, victim *entity.Mob) *entity.HitSource {
	return ownerHitSource(victim, bodies.clients[ownerID], func() *entity.Mob { return bodies.mobs[ownerID] })
}

// ownerHitSource resolves a player owner first, else the mob that mobOf finds
// (called only then); nil unless the victim needs the source.
func ownerHitSource(victim *entity.Mob, player *client, mobOf func() *entity.Mob) *entity.HitSource {
	if !victim.SulfurCubeFilled() && victim.Type != entity.TypeBreeze {
		return nil
	}
	if player != nil {
		pos := player.player.Position()
		return &entity.HitSource{X: pos.X, Y: pos.Y, Z: pos.Z, Eye: pos.Y + player.dims(player.player.Appearance().Pose).EyeHeight,
			Yaw: float64(pos.Yaw), Pitch: float64(pos.Pitch)}
	}
	if shooter := mobOf(); shooter != nil {
		src := shooter.HitSource()
		return &src
	}
	return nil
}

// sulfurPowerCell includes the world so equal coordinates in different
// dimensions never share a signal.
type sulfurPowerCell struct {
	w       *world.World
	x, y, z int32
}

func sulfurCell(w *world.World, m *entity.Mob) sulfurPowerCell {
	return sulfurPowerCell{w: w, x: rules.BlockOf(m.X), y: rules.BlockOf(m.Y), z: rules.BlockOf(m.Z)}
}

// readHerdInputs snapshots cube cells and dolphin treasure requests under the
// mob lock, then resolves them with no lock held: physics may count bodies
// under the mob lock.
func (s *Server) readHerdInputs(in *mobTickInputs) {
	s.lockTick(&s.mobsMu)
	for _, dim := range s.allDims() {
		for _, m := range dim.mobs.herd {
			if m.SulfurCanExplode() {
				in.sulfurPower[sulfurCell(dim.w, m)] = false
			}
			if m.DolphinNeedsTreasure() {
				in.treasures = append(in.treasures, dolphinTreasureInput{w: dim.w, m: m, x: rules.BlockOf(m.X), y: rules.BlockOf(m.Y), z: rules.BlockOf(m.Z)})
			}
		}
	}
	s.unlockTick(&s.mobsMu)
	for cell := range in.sulfurPower {
		in.sulfurPower[cell] = s.physicsFor(cell.w).HasOwnOrNeighborSignal(cell.x, cell.y, cell.z)
	}
	s.resolveDolphinTreasure(in.treasures)
}

// dispenseSulfurBlock tries the cubes intersecting the dispenser's target cell.
// vanilla: SulfurCubeBlockDispenseItemBehavior.dispenseBlock
func (s *Server) dispenseSulfurBlock(w *world.World, stack protocol.ItemStack, d world.Dispense) bool {
	tx, ty, tz := faceOffset(d.X, d.Y, d.Z, int32(d.Dir))
	box := entity.NewAABB(float64(tx), float64(ty), float64(tz), float64(tx+1), float64(ty+1), float64(tz+1))
	s.mobsMu.Lock()
	for _, m := range s.herdOf(w).herd {
		if m.Type != entity.TypeSulfurCube {
			continue
		}
		if !box.Crosses(entity.NewAABB(m.BlastBox())) {
			continue
		}
		old, ok := m.AbsorbSulfurBlock(stack)
		if !ok {
			continue
		}
		x, y, z, silent := m.X, m.Y, m.Z, m.Silent
		dropY := y + m.BbHeight()
		s.mobsMu.Unlock()
		if !old.Empty() {
			s.spawnItemAtFeet(w, old, x, dropY, z)
		}
		if !silent {
			s.broadcastSoundID(w, registry.SoundEntitySulfurCubeAbsorb, protocol.SoundCategoryNeutral, x, y, z, 1, 1)
		}
		s.dispenseDefaultSound(w, d)
		s.dispenseAnimation(w, d)
		return true
	}
	s.mobsMu.Unlock()
	return false
}
