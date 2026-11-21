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

// restoreSaved puts a body back in its kind's list and chunk index.
func restoreSaved[B, S any](s *Server, k *trackedKind[B, S], body B) {
	mu := k.lock(s)
	mu.Lock()
	list := k.list(s)
	*list = append(*list, body)
	k.file(s, body)
	mu.Unlock()
}

// sweepSaved keeps the bodies keep accepts and calls drop on each other.
// keep runs under the kind's lock and records any departure before returning
// false. The slice header is also replaced under that lock.
func sweepSaved[E any](list []E, keep func(E) bool, drop func(E)) []E {
	alive := list[:0]
	for _, body := range list {
		if keep(body) {
			alive = append(alive, body)
		} else {
			drop(body)
		}
	}
	clear(list[len(alive):])
	return alive
}

// sweepKind makes a per-kind sweep: under the kind's lock it keeps the bodies
// whose world keep accepts.
func sweepKind[B, S any](k *trackedKind[B, S]) func(*Server, func(*world.World) bool) {
	return func(s *Server, keep func(*world.World) bool) {
		m := k.lock(s)
		m.Lock()
		defer m.Unlock()
		l := k.list(s)
		*l = sweepSaved(*l, func(b B) bool { return keep(k.world(b)) }, k.unfile)
	}
}

// entitySweeps has one sweep for every list of world-bound entities the
// server keeps; a new kind is added here to be dropped with its world. Kinds
// sharing a lock sweep one after another, each under it.
var entitySweeps = []func(*Server, func(*world.World) bool){
	sweepKind(&itemTracker),
	sweepKind(&arrowTracker),
	sweepKind(&thrownTracker),
	sweepKind(&cloudTracker),
	sweepKind(&fireworkTracker),
	sweepKind(&eyeTracker),
	sweepKind(&orbTracker),
	sweepKind(&tntTracker),
	sweepKind(&fallingTracker),
	sweepKind(&paintingTracker),
	sweepKind(&frameTracker),
	sweepKind(&boatTracker),
	sweepKind(&minecartTracker),
	sweepKind(&standTracker),
	sweepKind(&endCrystalTracker),
	sweepKind(&mannequinTracker),
	sweepKind(&fixtureTracker),
	sweepKind(&fangsTracker),
	sweepKind(&boltTracker),
	sweepKind(&bobberTracker),
	sweepKind(&knotTracker),
}
