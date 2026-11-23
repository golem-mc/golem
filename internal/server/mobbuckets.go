package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
)

// Mob buckets preserve the mob data between pickup and release.

const fishPickupSound = registry.SoundItemBucketFillFish

// bucketMob removes a mob and exchanges the held bucket for its saved form.
// vanilla: Bucketable.bucketMobPickup
func (c *client) bucketMob(ent *mobEntity, hand int32) error {
	s := c.srv
	s.mobsMu.Lock()
	m := ent.mob
	if !m.Alive() || m.Discarded() {
		s.mobsMu.Unlock()
		return nil
	}
	bucketName := entity.MobBucketItem(m.Type)
	tag := m.BucketTag()
	body := m.Body
	size, kind, silent := int32(m.SalmonSize), m.Type, m.Silent
	variant := int32(m.AxolotlVariant())
	tropicalVariant := m.TropicalVariant
	x, y, z := m.X, m.Y, m.Z
	name, _, named := mobCustomName(ent)
	m.Discard()
	s.mobsMu.Unlock()

	id, ok := registry.ItemID(bucketName)
	if !ok {
		return nil
	}
	bucket := protocol.ItemStack{ItemID: id, Count: 1}
	bucket.SetBucketEntityData(tag)
	if kind == entity.TypeSulfurCube && !body.Empty() {
		bucket.SetSulfurCubeContent(body)
	}
	// vanilla: Bucketable.saveDefaultDataToBucketTag copies the custom name
	if named {
		bucket.CustomName = &name
	}
	if kind == entity.TypeAxolotl {
		bucket.SetAxolotlVariant(variant)
	}
	if kind == entity.TypeTropicalFish {
		bucket.SetTropicalFishVariant(tropicalVariant)
	}
	if kind == entity.TypeSalmon {
		bucket.SetSalmonSize(size)
	}
	// vanilla: Entity.playSound
	if !silent {
		pickupSound := fishPickupSound
		if kind == entity.TypeTadpole {
			pickupSound = registry.SoundItemBucketFillTadpole
		}
		if kind == entity.TypeAxolotl {
			pickupSound = registry.SoundItemBucketFillAxolotl
		}
		if kind == entity.TypeSulfurCube {
			pickupSound = registry.SoundItemBucketFillSulfurCube
		}
		s.broadcastSoundID(ent.w, pickupSound, mobSoundCategory(kind), x, y, z, 1.0, 1.0)
	}
	c.filledResult(hand, bucket, false)
	c.filledBucketAdvancements(bucket)
	return nil
}

// releaseBucketMob spawns a bucket's mob with its saved data and reports whether
// the item was a mob bucket.
// vanilla: MobBucketItem.spawn
func (s *Server) releaseBucketMob(dim *dimension, bucket protocol.ItemStack, x, y, z float64) bool {
	kind, ok := entity.BucketMobKind(registry.Item(bucket.ItemID))
	if !ok {
		return false
	}
	var sound registry.Sound
	var volume, pitch float32
	s.spawnMobArriving(dim, kind, x, y, z, 0, nil, false, "", false, func(ent *mobEntity) {
		m := ent.mob
		m.Yaw = s.levelRng.Float32() * 360
		if m.Yaw >= 180 {
			m.Yaw -= 360
		}
		m.HeadYaw = m.Yaw
		if size, ok := bucket.SalmonSize(); ok && kind == entity.TypeSalmon && size >= 0 && size <= entity.SalmonLarge {
			m.SalmonSize = int(size)
		}
		if kind == entity.TypeTropicalFish {
			m.SetMetadataTropicalVariant(entity.NormalizeTropicalVariant(bucket.TropicalFishVariant(m.TropicalVariant)))
		}
		if bucket.CustomName != nil {
			ent.name = protocol.TextTag(*bucket.CustomName)
			m.Named = true
		}
		m.LoadBucketTag(bucket.BucketEntityData())
		if kind == entity.TypeSulfurCube {
			body, _ := bucket.SulfurCubeContent()
			m.SetEquipment(6, body)
			if !body.Empty() {
				m.BodyDropChance = entity.GuaranteedDropChance
			}
		}
		if v, ok := bucket.AxolotlVariant(); ok && kind == entity.TypeAxolotl {
			m.SetAxolotlVariant(int(v))
		}
		sound, volume, pitch = m.AmbientVoice()
	})
	s.broadcastVoiceID(dim.w, kind, false, x, y, z, sound, volume, pitch)
	return true
}

// emptySulfurCubeBucket releases a land mob without placing or replacing a block.
// vanilla: MobBucketItem.emptyContents (Fluids.EMPTY), BucketItem.use
func (c *client) emptySulfurCubeBucket(hand int32, held protocol.ItemStack) bool {
	w := c.theWorld()
	pos := c.player.Position()
	dx, dy, dz := entity.LookVector(pos.Yaw, pos.Pitch)
	reach := c.blockRange()
	hit, ok := w.ClipBlocks(pos.X, pos.Y+c.dims(c.player.Appearance().Pose).EyeHeight, pos.Z,
		dx*reach, dy*reach, dz*reach, nil)
	if !ok || !rules.MayUseItemAt(c.player.GameMode(), held, adventureTarget(w, hit.X, hit.Y, hit.Z, hit.State)) || !c.mayInteractAt(hit.X, hit.Z) {
		return false
	}
	offset := protocol.FaceOffset[hit.Face]
	x, y, z := hit.X+offset[0], hit.Y+offset[1], hit.Z+offset[2]
	c.srv.broadcastSoundIDExcept(w, c, registry.SoundItemBucketEmptySulfurCube, protocol.SoundCategoryNeutral,
		float64(x)+0.5, float64(y)+0.5, float64(z)+0.5, 1, 1)
	c.srv.releaseBucketMob(c.dimension(), held, float64(x)+0.5, float64(y), float64(z)+0.5)
	c.awardItemStat(registry.StatTypeUsed, held.ItemID, 1)
	result := held
	if c.player.GameMode() != protocol.GameModeCreative {
		result = protocol.ItemStack{ItemID: int32(registry.ItemBucket), Count: 1}
	}
	c.filledResult(hand, result, true)
	return true
}
