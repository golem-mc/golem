package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/world"
)

// projectileTargetSnapshots holds pickable bodies read before a flight takes its locks.
type projectileTargetSnapshots struct {
	paintings   paintingSnapshot
	frames      map[*world.World][]entitySliver
	projectiles map[*world.World][]entity.ArrowTarget
	crystals    map[*world.World][]entity.ArrowTarget
	cushions    map[*world.World][]entity.ArrowTarget
	mannequins  mannequinTargetSnapshot
	stands      map[*world.World][]entity.ArrowTarget
}

func (s *Server) readProjectileTargetSnapshots() projectileTargetSnapshots {
	in := projectileTargetSnapshots{
		crystals:   s.endCrystalSnapshots(),
		cushions:   s.cushionSnapshots(),
		stands:     s.armorStandSnapshots(),
		mannequins: s.mannequinTargets(),
	}
	s.lockTick(&s.paintingsMu)
	in.paintings = s.snapshotPaintingsLocked()
	s.unlockTick(&s.paintingsMu)
	s.lockTick(&s.framesMu)
	in.frames = s.snapshotFramesLocked()
	s.unlockTick(&s.framesMu)
	s.lockTick(&s.arrowsMu)
	in.projectiles = s.snapshotProjectileTargetsLocked()
	s.unlockTick(&s.arrowsMu)
	return in
}

func (in *projectileTargetSnapshots) appendHangings(dst []entity.ArrowTarget, w *world.World) []entity.ArrowTarget {
	for _, p := range in.paintings.byWorld[w] {
		dst = append(dst, entity.ArrowTarget{EntityID: p.id, Type: protocol.EntityTypePainting,
			MinX: p.minX, MinY: p.minY, MinZ: p.minZ, MaxX: p.maxX, MaxY: p.maxY, MaxZ: p.maxZ})
	}
	for _, f := range in.frames[w] {
		dst = append(dst, entity.ArrowTarget{EntityID: f.id, Type: f.typ,
			MinX: f.minX, MinY: f.minY, MinZ: f.minZ, MaxX: f.maxX, MaxY: f.maxY, MaxZ: f.maxZ})
	}
	return dst
}
