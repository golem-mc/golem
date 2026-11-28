package server

import (
	"sync"

	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/nbt"
)

// entityOps is shared by every snapshot of a kind; no closures are allocated per snapshot.
type entityOps struct {
	tag  func(any) nbt.CompoundTag
	kill func(*Server, any)
	// pos is the Pos the kind's saved tag holds, nil when the kind has no
	// plain body position (or has not been checked against its tag writer).
	pos func(any) (x, y, z float64)
}

// withPos sets how a kind's saved Pos is read without building the tag.
func withPos[E any](ops entityOps, pos func(E) (x, y, z float64)) entityOps {
	ops.pos = func(ref any) (float64, float64, float64) { return pos(ref.(E)) }
	return ops
}

func entityOpsFor[E any, T nbt.CompoundTag](tag func(E) T, kill func(*Server, E)) entityOps {
	return entityOps{
		tag:  func(ref any) nbt.CompoundTag { return tag(ref.(E)) },
		kill: func(s *Server, ref any) { kill(s, ref.(E)) },
	}
}

// entityDataWriter calls saveWithoutId without first inserting id, which can grow the table.
func entityDataWriter[E any](write func(E, string) *nbt.OrderedCompound) func(E) *nbt.OrderedCompound {
	return func(ent E) *nbt.OrderedCompound { return write(ent, "") }
}

func (e *entitySnapshot) bind(mu *sync.Mutex, tags *nbt.StringSet, ops *entityOps) {
	e.mu, e.liveTags, e.ops = mu, tags, ops
	e.tags = tags.Values()
}

var (
	mobEntityOps        entityOps
	itemEntityOps       = withPos(entityOpsFor(entityDataWriter(itemTagAs), (*Server).killItem), func(it *itemEntity) (x, y, z float64) { return it.item.X, it.item.Y - entity.ItemHalfSize, it.item.Z })
	orbEntityOps        = entityOpsFor(entityDataWriter(orbTagAs), (*Server).killOrb)
	arrowEntityOps      = entityOpsFor(entityDataWriter(arrowTagAs), (*Server).killArrow)
	thrownEntityOps     = entityOpsFor(entityDataWriter(thrownTagAs), (*Server).killThrown)
	tntEntityOps        = entityOpsFor(entityDataWriter(tntTagAs), (*Server).killTNT)
	boatEntityOps       = entityOpsFor(entityDataWriter(boatTagAs), (*Server).killBoat)
	minecartEntityOps   = entityOpsFor(entityDataWriter(minecartTagAs), (*Server).killMinecart)
	fallingEntityOps    = entityOpsFor(entityDataWriter(fallingTagAs), (*Server).killFallingBlock)
	armorStandEntityOps = withPos(entityOpsFor(armorStandDataTag, (*Server).killArmorStand), func(a *armorStandEntity) (x, y, z float64) { return a.stand.X, a.stand.Y, a.stand.Z })
	mannequinEntityOps  = withPos(entityOpsFor(entityDataWriter(mannequinTagAs), (*Server).killMannequin), func(m *mannequinEntity) (x, y, z float64) { return m.mannequin.X, m.mannequin.Y, m.mannequin.Z })
	cloudEntityOps      = entityOpsFor(entityDataWriter(cloudTagAs), (*Server).killCloud)
	fangsEntityOps      = entityOpsFor(entityDataWriter(fangsTagAs), (*Server).killFangs)
	eyeEntityOps        = entityOpsFor(entityDataWriter(eyeTagAs), (*Server).killEye)
	boltEntityOps       = entityOpsFor(entityDataWriter(boltTagAs), (*Server).killBolt)
	fireworkEntityOps   = entityOpsFor(entityDataWriter(fireworkTagAs), (*Server).killFirework)
	bobberEntityOps     = entityOpsFor(entityDataWriter(bobberTagAs), (*Server).killBobber)
	knotEntityOps       = entityOpsFor(entityDataWriter(knotTagAs), (*Server).killKnot)
	paintingEntityOps   = entityOpsFor(entityDataWriter(paintingTagAs), (*Server).killPainting)
	frameEntityOps      = entityOpsFor(entityDataWriter(frameTagAs), (*Server).killFrame)
	endCrystalEntityOps = withPos(entityOpsFor(entityDataWriter(endCrystalTagAs), (*Server).killEndCrystal), func(c *endCrystalEntity) (x, y, z float64) { return c.crystal.X, c.crystal.Y, c.crystal.Z })
	fixtureEntityOps    = withPos(entityOpsFor(entityDataWriter(fixtureTagAs), (*Server).killFixture), func(f *fixtureEntity) (x, y, z float64) { return f.x, f.y, f.z })
)

func init() {
	// Dragon completion can destroy containers and resolve entity snapshots,
	// which read this table; assigning here breaks that initialization cycle.
	mobEntityOps = withPos(entityOpsFor(mobDataTag, (*Server).killMob), func(ent *mobEntity) (x, y, z float64) {
		m := ent.mob
		x, z = m.X, m.Z
		if v := m.Vehicle; v != nil {
			x, z = v.X, v.Z
		}
		return x, m.Y, z
	})
}
