package server

import (
	"sync"

	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/google/uuid"
)

// uuidScan says what a scan does with each live UUID: add it to into or,
// with into nil, stop at want.
type uuidScan struct {
	into  map[uuid.UUID]bool
	want  uuid.UUID
	found bool
}

// stop takes one live UUID and reports whether the scan is over.
func (sc *uuidScan) stop(id uuid.UUID) bool {
	if sc.into != nil {
		sc.into[id] = true
		return false
	}
	sc.found = id == sc.want
	return sc.found
}

// scanSavedUUIDs feeds each live body's UUID to sc; it reports false once sc
// found its UUID. The caller holds the kind's lock. It takes no lock itself
// so that it inlines, and identity with it, into the mob tick's per-body loop.
func scanSavedUUIDs[E any](list []E, identity func(E) (uuid.UUID, bool), sc *uuidScan) bool {
	for _, body := range list {
		if id, live := identity(body); live && sc.stop(id) {
			return false
		}
	}
	return true
}

// locked runs scan under mu.
func locked(mu *sync.Mutex, scan func() bool) bool {
	mu.Lock()
	defer mu.Unlock()
	return scan()
}

func collectSavedTags[E any](out map[*world.World]map[world.ChunkPos][]nbt.Tag, list []E,
	project func(E) (*world.World, float64, float64, nbt.CompoundTag)) {
	for _, body := range list {
		w, x, z, tag := project(body)
		if tag == nil {
			continue
		}
		if out[w] == nil {
			out[w] = make(map[world.ChunkPos][]nbt.Tag)
		}
		pos := world.ChunkPos{X: world.ChunkOf(x), Z: world.ChunkOf(z)}
		out[w][pos] = append(out[w][pos], tag)
	}
}

// list is a pointer so its header is read and replaced under mu.
func restoreSaved[E any](mu *sync.Mutex, list *[]E, body E) {
	mu.Lock()
	*list = append(*list, body)
	mu.Unlock()
}

// keep runs under the kind's lock and records any departure before returning false.
// The slice header is also replaced under that lock.
func sweepSaved[E any](list []E, keep func(E) bool) []E {
	alive := list[:0]
	for _, body := range list {
		if keep(body) {
			alive = append(alive, body)
		}
	}
	return alive
}

// sweepKind makes a per-kind sweep: under the kind's lock it keeps the bodies
// whose world keep accepts.
func sweepKind[E any](mu func(*Server) *sync.Mutex, list func(*Server) *[]E, worldOf func(E) *world.World) func(*Server, func(*world.World) bool) {
	return func(s *Server, keep func(*world.World) bool) {
		m := mu(s)
		m.Lock()
		defer m.Unlock()
		l := list(s)
		*l = sweepSaved(*l, func(e E) bool { return keep(worldOf(e)) })
	}
}

// entitySweeps has one sweep for every list of world-bound entities the
// server keeps; a new kind is added here to be dropped with its world. Kinds
// sharing a lock sweep one after another, each under it.
var entitySweeps = []func(*Server, func(*world.World) bool){
	sweepKind(func(s *Server) *sync.Mutex { return &s.itemsMu }, func(s *Server) *[]*itemEntity { return &s.items }, func(e *itemEntity) *world.World { return e.w }),
	sweepKind(func(s *Server) *sync.Mutex { return &s.arrowsMu }, func(s *Server) *[]*arrowEntity { return &s.arrows }, func(e *arrowEntity) *world.World { return e.w }),
	sweepKind(func(s *Server) *sync.Mutex { return &s.arrowsMu }, func(s *Server) *[]*thrownEntity { return &s.thrown }, func(e *thrownEntity) *world.World { return e.w }),
	sweepKind(func(s *Server) *sync.Mutex { return &s.arrowsMu }, func(s *Server) *[]*cloudEntity { return &s.clouds }, func(e *cloudEntity) *world.World { return e.w }),
	sweepKind(func(s *Server) *sync.Mutex { return &s.fireworksMu }, func(s *Server) *[]*fireworkEntity { return &s.fireworks }, func(e *fireworkEntity) *world.World { return e.w }),
	sweepKind(func(s *Server) *sync.Mutex { return &s.eyesMu }, func(s *Server) *[]*eyeEntity { return &s.eyes }, func(e *eyeEntity) *world.World { return e.w }),
	sweepKind(func(s *Server) *sync.Mutex { return &s.orbsMu }, func(s *Server) *[]*orbEntity { return &s.orbs }, func(e *orbEntity) *world.World { return e.w }),
	sweepKind(func(s *Server) *sync.Mutex { return &s.tntMu }, func(s *Server) *[]*tntEntity { return &s.tnts }, func(e *tntEntity) *world.World { return e.w }),
	sweepKind(func(s *Server) *sync.Mutex { return &s.fallingMu }, func(s *Server) *[]*fallingEntity { return &s.fallings }, func(e *fallingEntity) *world.World { return e.w }),
	sweepKind(func(s *Server) *sync.Mutex { return &s.paintingsMu }, func(s *Server) *[]*paintingEntity { return &s.paintings }, func(e *paintingEntity) *world.World { return e.w }),
	sweepKind(func(s *Server) *sync.Mutex { return &s.framesMu }, func(s *Server) *[]*frameEntity { return &s.frames }, func(e *frameEntity) *world.World { return e.w }),
	sweepKind(func(s *Server) *sync.Mutex { return &s.boatsMu }, func(s *Server) *[]*boatEntity { return &s.boats }, func(e *boatEntity) *world.World { return e.w }),
	sweepKind(func(s *Server) *sync.Mutex { return &s.minecartsMu }, func(s *Server) *[]*minecartEntity { return &s.minecarts }, func(e *minecartEntity) *world.World { return e.w }),
	sweepKind(func(s *Server) *sync.Mutex { return &s.armorStandsMu }, func(s *Server) *[]*armorStandEntity { return &s.armorStands }, func(e *armorStandEntity) *world.World { return e.w }),
	sweepKind(func(s *Server) *sync.Mutex { return &s.endCrystalsMu }, func(s *Server) *[]*endCrystalEntity { return &s.endCrystals }, func(e *endCrystalEntity) *world.World { return e.w }),
	sweepKind(func(s *Server) *sync.Mutex { return &s.mannequinsMu }, func(s *Server) *[]*mannequinEntity { return &s.mannequins }, func(e *mannequinEntity) *world.World { return e.w }),
	sweepKind(func(s *Server) *sync.Mutex { return &s.fixturesMu }, func(s *Server) *[]*fixtureEntity { return &s.fixtures }, func(e *fixtureEntity) *world.World { return e.w }),
	sweepKind(func(s *Server) *sync.Mutex { return &s.fangs.mu }, func(s *Server) *[]*fangsEntity { return &s.fangs.list }, func(e *fangsEntity) *world.World { return e.w }),
	sweepKind(func(s *Server) *sync.Mutex { return &s.boltMu }, func(s *Server) *[]*boltEntity { return &s.bolts }, func(e *boltEntity) *world.World { return e.w }),
	sweepKind(func(s *Server) *sync.Mutex { return &s.bobbersMu }, func(s *Server) *[]*bobberEntity { return &s.bobbers }, func(e *bobberEntity) *world.World { return e.w }),
	sweepKind(func(s *Server) *sync.Mutex { return &s.knotsMu }, func(s *Server) *[]*knotEntity { return &s.knots }, func(e *knotEntity) *world.World { return e.w }),
}
