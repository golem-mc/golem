package server

import (
	"github.com/golem-mc/golem/plugin"
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

// shoulderRespawnLift is where a dropped shoulder entity reappears above the
// player's feet. vanilla: ServerPlayer.respawnEntityOnShoulder (0.7f)
const shoulderRespawnLift = float64(float32(0.7))

// shoulderSettle is the ticks a landed entity stays before a trigger drops it.
// vanilla: ServerPlayer.removeEntitiesOnShoulder
const shoulderSettle = 20

// mobShoulder is a parrot saved onto a player's shoulder: its compound, taken
// under the mob lock, and the client it perches on.
type mobShoulder struct {
	w   *world.World
	c   *client
	tag nbt.CompoundTag
}

func hasShoulder(tag nbt.CompoundTag) bool {
	return tag != nil && len(tag.Entries()) > 0
}

// shoulderSlots is how many of the player's shoulders are free.
func shoulderSlots(sh player.Shoulders) int {
	n := 0
	if !hasShoulder(sh.Left) {
		n++
	}
	if !hasShoulder(sh.Right) {
		n++
	}
	return n
}

// landedOnShoulder queues a parrot's landing, unless an earlier one this tick
// took the last free side; the parrot leaves the world like a despawn.
func (t *mobDimTick) landedOnShoulder(fx *mobEffects, in *mobTickInputs, ev entity.MobEvent) {
	c := in.clientByEntityID[ev.Player.EntityID]
	ent, ok := t.h.book[ev.Mob]
	if c == nil || !ok || ev.Mob.Discarded() {
		return
	}
	taken := 0
	for _, l := range fx.shoulders {
		if l.c == c {
			taken++
		}
	}
	if taken >= ev.Player.ShoulderSlots {
		return
	}
	// vanilla: ShoulderRidingEntity.setEntityOnShoulder appends id after saveWithoutId.
	tag := mobDataTag(ent)
	tag.Set("id", nbt.String(ent.mob.Type.Name()))
	fx.shoulders = append(fx.shoulders, mobShoulder{w: t.w, c: c, tag: tag})
	t.h.forget(ev.Mob)
	t.h.remove(ev.Mob)
	fx.scoreDrops = append(fx.scoreDrops, goneMob{ent.uuid, plugin.RemoveDiscard})
	fx.release(ent)
	fx.remove(t.w, ev.Mob.ID)
}

// perch puts a landed parrot on a free shoulder; with none left it drops
// back into the world.
// vanilla: ServerPlayer.setEntityOnShoulder
func (s *Server) perch(l mobShoulder) {
	c := l.c
	sh := c.player.Shoulders()
	switch {
	case !hasShoulder(sh.Left):
		sh.Left = l.tag
	case !hasShoulder(sh.Right):
		sh.Right = l.tag
	default:
		s.respawnShoulder(c, l.tag)
		return
	}
	sh.SatAt = c.shoulderTime()
	c.player.SetShoulders(sh)
	s.broadcastWorld(c.theWorld(), protocol.IDPlayEntityMetadata, shoulderMetadata(c.player))
}

func (c *client) shoulderTime() int64 {
	age, _ := c.theWorld().Time()
	return age
}

// shoulderAmbient rolls each shoulder's voice before the drop check.
// vanilla: ServerPlayer.playShoulderEntityAmbientSound
func (c *client) shoulderAmbient(sh player.Shoulders) {
	for _, shoulder := range [2]nbt.CompoundTag{sh.Left, sh.Right} {
		if !hasShoulder(shoulder) {
			continue
		}
		tag := shoulder.Entries()
		if nbt.BoolField(tag, "Silent") {
			continue
		}
		rng := c.ownDice()
		if rng.Intn(200) != 0 || tag["id"] != nbt.String(registry.EntityTypeParrot.String()) {
			continue
		}
		pos := c.player.Position()
		sound, imitated := c.shoulderImitation(pos)
		volume := float32(0.7) // vanilla: Parrot.imitateNearbyMobs
		if !imitated {
			sound = entity.ParrotAmbientSound(rng, c.theWorld().Difficulty() == rules.DifficultyPeaceful)
			volume = 1
		}
		if sound != registry.SoundNone {
			// vanilla: Parrot.getPitch
			pitchDice := rng
			if imitated {
				pitchDice = c.srv.envRng
			}
			pitch := (pitchDice.Float32()-pitchDice.Float32())*0.2 + 1
			c.srv.broadcastSoundID(c.theWorld(), sound, protocol.SoundCategoryPlayer, pos.X, pos.Y, pos.Z, volume, pitch)
		}
	}
}

// shoulderImitation picks a nearby imitable mob, even when that mob is silent.
// vanilla: Parrot.imitateNearbyMobs
func (c *client) shoulderImitation(pos player.Position) (registry.Sound, bool) {
	rng := c.srv.envRng
	if c.player.Vitals().Dead || rng.Intn(2) != 0 {
		return registry.SoundNone, false
	}
	dims := c.dims(c.player.Appearance().Pose)
	half := dims.Width / 2
	// The herd scan waits until both random rolls pass.
	near := c.srv.mobsIn(c.theWorld(), pos.X-half-20, pos.Y-20, pos.Z-half-20,
		pos.X+half+20, pos.Y+dims.Height+20, pos.Z+half+20)
	c.srv.mobsMu.Lock()
	defer c.srv.mobsMu.Unlock()
	near = slices.DeleteFunc(near, func(ent *mobEntity) bool {
		_, ok := entity.ParrotImitationSound(ent.mob.Type)
		return !ok
	})
	if len(near) == 0 {
		return registry.SoundNone, false
	}
	m := near[rng.Intn(len(near))].mob
	if m.Silent {
		return registry.SoundNone, false
	}
	return entity.ParrotImitationSound(m.Type)
}

// removeEntitiesOnShoulder drops what sits on the shoulders back into the
// world once it has sat a second.
// vanilla: ServerPlayer.removeEntitiesOnShoulder
func (c *client) removeEntitiesOnShoulder() {
	sh := c.player.Shoulders()
	if !hasShoulder(sh.Left) && !hasShoulder(sh.Right) || sh.SatAt+shoulderSettle >= c.shoulderTime() {
		return
	}
	c.player.SetShoulders(player.Shoulders{SatAt: sh.SatAt})
	c.srv.respawnShoulder(c, sh.Left)
	c.srv.respawnShoulder(c, sh.Right)
	c.srv.broadcastWorld(c.theWorld(), protocol.IDPlayEntityMetadata, shoulderMetadata(c.player))
}

// respawnShoulder adds a saved shoulder entity back above its owner.
// vanilla: ServerPlayer.respawnEntityOnShoulder
func (s *Server) respawnShoulder(c *client, tag nbt.CompoundTag) {
	if !hasShoulder(tag) {
		return
	}
	ent := mobOfTag(s, c.theWorld(), nbt.Copy(tag).(nbt.CompoundTag).Entries())
	if ent == nil {
		return
	}
	pos := c.player.Position()
	m := ent.mob
	m.SetMetadataTamableTame(true)
	m.SetMetadataTamableOwner(c.player.UUID)
	m.X, m.Y, m.Z = pos.X, pos.Y+shoulderRespawnLift, pos.Z
	s.addMobCore(true, ent)
}

// perchState reports whether the player is in water or powder snow, which
// keep a parrot off the shoulders.
func (c *client) perchState(pos player.Position) (inWater, inSnow bool) {
	dims := c.dims(c.player.Appearance().Pose)
	w := c.theWorld()
	inWater, _, _ = entity.BoxInWater(w, pos.X, pos.Y, pos.Z, dims.Width, dims.Height, pos.Y+dims.EyeHeight)
	half := dims.Width / 2
	minX, maxX := rules.CellRange(pos.X-half, pos.X+half)
	minY, maxY := rules.CellRange(pos.Y, pos.Y+dims.Height)
	minZ, maxZ := rules.CellRange(pos.Z-half, pos.Z+half)
	for y := minY; y <= maxY; y++ {
		for z := minZ; z <= maxZ; z++ {
			for x := minX; x <= maxX; x++ {
				if rules.IsPowderSnow(w.BlockAt(x, y, z)) {
					return inWater, true
				}
			}
		}
	}
	return inWater, false
}

// tamedParrotOwners is the owners of the tamed parrots in the herds, nil when
// there are none.
func (s *Server) tamedParrotOwners() map[uuid.UUID]bool {
	var owners map[uuid.UUID]bool
	s.mobsMu.Lock()
	for _, dim := range s.allDims() {
		for _, m := range dim.mobs.herd {
			if m.Type == entity.TypeParrot && m.Tamable.Tame {
				if owners == nil {
					owners = make(map[uuid.UUID]bool)
				}
				owners[m.Tamable.Owner] = true
			}
		}
	}
	s.mobsMu.Unlock()
	return owners
}

// shoulderSnapshot fills the facts a parrot lands by, for the owner of a
// tamed parrot only: no other goal reads them. Water and snow are read last,
// and only for a grounded, walking player with a free shoulder, since the goal
// checks them after those facts.
func (c *client) shoulderSnapshot(snap *entity.Player, pos player.Position, ownsParrot bool) {
	if !ownsParrot {
		return
	}
	snap.Flying, snap.OnGround = c.flying.Load(), pos.OnGround
	snap.ShoulderSlots = shoulderSlots(c.player.Shoulders())
	if snap.OnGround && !snap.Flying && snap.ShoulderSlots > 0 {
		snap.InWater, snap.InPowderSnow = c.perchState(pos)
	}
}

// shouldersFromTag reads the saved shoulder entities.
// vanilla: ServerPlayer.readAdditionalSaveData
func shouldersFromTag(root nbt.Compound) player.Shoulders {
	left, _ := nbt.Get[nbt.CompoundTag](root, "ShoulderEntityLeft")
	right, _ := nbt.Get[nbt.CompoundTag](root, "ShoulderEntityRight")
	return player.Shoulders{Left: left, Right: right}
}

// storeShoulders writes the shoulder entities, none when free.
func storeShoulders(state nbt.Compound, sh player.Shoulders) {
	for key, tag := range map[string]nbt.CompoundTag{"ShoulderEntityLeft": sh.Left, "ShoulderEntityRight": sh.Right} {
		if !hasShoulder(tag) {
			delete(state, key)
		} else {
			state[key] = tag
		}
	}
}
