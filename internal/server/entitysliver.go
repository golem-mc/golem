package server

// entitySliver is a hanging body's shape for one projectile tick, copied
// under its kind's lock, so the sweep and the delayed hurt (matched by ID)
// never hold the lock a /tp moving the body takes. Frames carry their
// projected entity type and fixed flag.
type entitySliver struct {
	id                                 int32
	typ                                int32
	fixed                              bool
	minX, minY, minZ, maxX, maxY, maxZ float64
}
