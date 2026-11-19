package server

import (
	"sync"

	"github.com/golem-mc/golem/nbt"
)

// entityOps is shared by every snapshot of a kind; no closures are allocated per snapshot.
type entityOps struct {
	tag  func(any) nbt.CompoundTag
	kill func(*Server, any)
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
	itemEntityOps       = entityOpsFor(entityDataWriter(itemTagAs), (*Server).killItem)
	orbEntityOps        = entityOpsFor(entityDataWriter(orbTagAs), (*Server).killOrb)
	arrowEntityOps      = entityOpsFor(entityDataWriter(arrowTagAs), (*Server).killArrow)
	thrownEntityOps     = entityOpsFor(entityDataWriter(thrownTagAs), (*Server).killThrown)
	tntEntityOps        = entityOpsFor(entityDataWriter(tntTagAs), (*Server).killTNT)
	boatEntityOps       = entityOpsFor(entityDataWriter(boatTagAs), (*Server).killBoat)
	minecartEntityOps   = entityOpsFor(entityDataWriter(minecartTagAs), (*Server).killMinecart)
	fallingEntityOps    = entityOpsFor(entityDataWriter(fallingTagAs), (*Server).killFallingBlock)
	armorStandEntityOps = entityOpsFor(armorStandDataTag, (*Server).killArmorStand)
	mannequinEntityOps  = entityOpsFor(entityDataWriter(mannequinTagAs), (*Server).killMannequin)
	cloudEntityOps      = entityOpsFor(entityDataWriter(cloudTagAs), (*Server).killCloud)
	fangsEntityOps      = entityOpsFor(entityDataWriter(fangsTagAs), (*Server).killFangs)
	eyeEntityOps        = entityOpsFor(entityDataWriter(eyeTagAs), (*Server).killEye)
	boltEntityOps       = entityOpsFor(entityDataWriter(boltTagAs), (*Server).killBolt)
	fireworkEntityOps   = entityOpsFor(entityDataWriter(fireworkTagAs), (*Server).killFirework)
	bobberEntityOps     = entityOpsFor(entityDataWriter(bobberTagAs), (*Server).killBobber)
	knotEntityOps       = entityOpsFor(entityDataWriter(knotTagAs), (*Server).killKnot)
	paintingEntityOps   = entityOpsFor(entityDataWriter(paintingTagAs), (*Server).killPainting)
	frameEntityOps      = entityOpsFor(entityDataWriter(frameTagAs), (*Server).killFrame)
	endCrystalEntityOps = entityOpsFor(entityDataWriter(endCrystalTagAs), (*Server).killEndCrystal)
	fixtureEntityOps    = entityOpsFor(entityDataWriter(fixtureTagAs), (*Server).killFixture)
)

func init() {
	// Dragon completion can destroy containers and resolve entity snapshots,
	// which read this table; assigning here breaks that initialization cycle.
	mobEntityOps = entityOpsFor(mobDataTag, (*Server).killMob)
}
