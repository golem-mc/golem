package rules

import (
	"sync"

	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// POIKind is the point-of-interest type of a block: none, the bed, the
// bell, or the job site of one profession.
// vanilla: PoiTypes.bootstrap
type POIKind uint8

// The POI kinds; the job kinds follow PoiTypes.bootstrap's order.
const (
	POINone POIKind = iota
	POIHome
	POIMeeting
	POIArmorer
	POIButcher
	POICartographer
	POICleric
	POIFarmer
	POIFisherman
	POIFletcher
	POILeatherworker
	POILibrarian
	POIMason
	POIShepherd
	POIToolsmith
	POIWeaponsmith
)

// poiJobSites lists each job kind's profession (also its POI type).
// vanilla: PoiTypes.bootstrap, VillagerProfession
var poiJobSites = [...]struct {
	kind       POIKind
	profession string
}{
	{POIArmorer, "minecraft:armorer"},
	{POIButcher, "minecraft:butcher"},
	{POICartographer, "minecraft:cartographer"},
	{POICleric, "minecraft:cleric"},
	{POIFarmer, "minecraft:farmer"},
	{POIFisherman, "minecraft:fisherman"},
	{POIFletcher, "minecraft:fletcher"},
	{POILeatherworker, "minecraft:leatherworker"},
	{POILibrarian, "minecraft:librarian"},
	{POIMason, "minecraft:mason"},
	{POIShepherd, "minecraft:shepherd"},
	{POIToolsmith, "minecraft:toolsmith"},
	{POIWeaponsmith, "minecraft:weaponsmith"},
}

// JobSiteProfession is the profession a job kind's block gives, "" for any
// other kind.
func JobSiteProfession(k POIKind) string {
	for _, j := range poiJobSites {
		if j.kind == k {
			return j.profession
		}
	}
	return ""
}

// ProfessionJobSite is the job kind a profession holds, POINone for none and
// nitwit.
func ProfessionJobSite(profession string) POIKind {
	for _, j := range poiJobSites {
		if j.profession == profession {
			return j.kind
		}
	}
	return POINone
}

// POITypeOf is the registry id of a block state's point-of-interest type, ""
// for none.
// vanilla: PoiTypes.forState
func POITypeOf(state uint16) string { return registry.POITypeOf(state) }

var poiKinds = func() [registry.StateCount]POIKind {
	var kinds [registry.StateCount]POIKind
	byType := map[string]POIKind{"minecraft:home": POIHome, "minecraft:meeting": POIMeeting}
	for _, j := range poiJobSites {
		byType[j.profession] = j.kind
	}
	for state := range uint16(registry.StateCount) {
		kinds[state] = byType[registry.POITypeOf(state)]
	}
	return kinds
}()

// POIKindOf is the village point-of-interest kind of a block state.
// vanilla: PoiTypes.forState
func POIKindOf(state uint16) POIKind {
	if int(state) >= len(poiKinds) {
		return POINone
	}
	return poiKinds[state]
}

// IsVillagePOI reports whether the block state is a village point of
// interest: a job site, the bell or a bed's head.
// vanilla: PoiTypeTags.VILLAGE
func IsVillagePOI(state uint16) bool { return POIKindOf(state) != POINone }

// VillageScanTicks is how long a chunk's village points of interest are
// kept before the chunk is scanned again, unless the caller accepts older.
const VillageScanTicks = 400

// POI is the block position and kind of a point of interest.
type POI struct {
	X, Y, Z int
	Kind    POIKind
}

// POIChunks is the loaded terrain a VillageIndex scans.
type POIChunks interface {
	ChunkLoaded(pos world.ChunkPos) bool
	SurfaceHeight(x, z int32) int32
	MinY() int32
	MaxY() int32
	BlockAt(x, y, z int32) uint16
}

// scanStagger shortens a chunk's cache lifetime by up to half of maxAge,
// differently per chunk, so chunks first scanned on one tick expire on
// different ticks instead of all being rescanned in a single tick.
func scanStagger(pos world.ChunkPos, maxAge int64) int64 {
	half := maxAge / 2
	if half <= 0 {
		return 0
	}
	h := uint32(pos.X)*73856093 ^ uint32(pos.Z)*19349663
	return int64(h>>8) % half
}

type villageChunk struct {
	at   int64
	pois []POI
}

// VillageIndex lists the village points of interest of one dimension's
// loaded chunks, scanning a chunk when it is first asked for and again
// after VillageScanTicks. Every exported method locks mu, a leaf lock held
// only over world reads, so it is safe under the raid and mob locks.
// vanilla: PoiManager
type VillageIndex struct {
	mu       sync.Mutex
	chunks   POIChunks
	cached   map[world.ChunkPos]villageChunk
	occupied map[[3]int]struct{}
	claimed  map[[3]int]struct{} // block positions of the claimed POIs
}

// NewVillageIndex indexes the chunks.
func NewVillageIndex(chunks POIChunks) *VillageIndex {
	return &VillageIndex{chunks: chunks, cached: map[world.ChunkPos]villageChunk{}}
}

// InChunk lists the points of interest of a chunk, nil when it is not
// loaded. tick is the caller's tick counter.
func (v *VillageIndex) InChunk(pos world.ChunkPos, tick int64) []POI {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.inChunk(pos, tick, VillageScanTicks)
}

// inChunk is InChunk with a scan up to maxAge ticks old accepted.
func (v *VillageIndex) inChunk(pos world.ChunkPos, tick, maxAge int64) []POI {
	if c, ok := v.cached[pos]; ok && tick-c.at < maxAge-scanStagger(pos, maxAge) {
		return c.pois
	}
	w := v.chunks
	if !w.ChunkLoaded(pos) {
		return nil
	}
	var sections world.SectionMask
	indexed, hasIndex := w.(interface {
		POISections(world.ChunkPos) world.SectionMask
	})
	if hasIndex {
		sections = indexed.POISections(pos)
		if sections == 0 {
			v.cached[pos] = villageChunk{at: tick}
			return nil
		}
	}
	var pois []POI
	for x := pos.X * 16; x < pos.X*16+16; x++ {
		for z := pos.Z * 16; z < pos.Z*16+16; z++ {
			top := min(w.SurfaceHeight(x, z), w.MaxY()-1)
			for y := w.MinY(); y <= top; y++ {
				if hasIndex && !sections.Has(y) {
					y = min(y|15, top)
					continue
				}
				if k := POIKindOf(w.BlockAt(x, y, z)); k != POINone {
					pois = append(pois, POI{int(x), int(y), int(z), k})
				}
			}
		}
	}
	v.cached[pos] = villageChunk{at: tick, pois: pois}
	return pois
}

// Changed drops the cached scan of the chunk holding a block whose state
// changed from old to new, when its point-of-interest kind changed, so the
// next lookup sees it.
func (v *VillageIndex) Changed(x, y, z int, old, new uint16) {
	if POIKindOf(old) == POIKindOf(new) {
		return
	}
	v.mu.Lock()
	delete(v.cached, world.ChunkPos{X: int32(x >> 4), Z: int32(z >> 4)})
	v.mu.Unlock()
}

// SetOccupied replaces the occupied village sections from loaded villagers'
// ticket reservations. Stale memories of removed POIs do not create villages.
func (v *VillageIndex) SetOccupied(claims []POI) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.occupied == nil {
		v.occupied = make(map[[3]int]struct{})
	}
	clear(v.occupied)
	if v.claimed == nil {
		v.claimed = make(map[[3]int]struct{})
	}
	clear(v.claimed)
	for _, p := range claims {
		if v.chunks.ChunkLoaded(world.ChunkPos{X: int32(p.X >> 4), Z: int32(p.Z >> 4)}) &&
			IsVillagePOI(v.chunks.BlockAt(int32(p.X), int32(p.Y), int32(p.Z))) {
			v.occupied[[3]int{p.X >> 4, p.Y >> 4, p.Z >> 4}] = struct{}{}
			v.claimed[[3]int{p.X, p.Y, p.Z}] = struct{}{}
		}
	}
}

// SectionsToVillage is the Chebyshev section distance to an occupied village
// section, capped at limit+1. It does not scan terrain or age the POI cache.
// vanilla: PoiManager.sectionsToVillage
func (v *VillageIndex) SectionsToVillage(x, y, z, limit int) int {
	v.mu.Lock()
	defer v.mu.Unlock()
	cx, cy, cz := x>>4, y>>4, z>>4
	best := limit + 1
	// Fewer occupied sections than probes: walk them instead.
	if side := 2*limit + 1; len(v.occupied) < side*side*side {
		for k := range v.occupied {
			best = min(best, max(abs(k[0]-cx), abs(k[1]-cy), abs(k[2]-cz)))
		}
		return best
	}
	// Probe only the bounded neighborhood; distant occupied sections add no work.
	for dx := -limit; dx <= limit; dx++ {
		for dy := -limit; dy <= limit; dy++ {
			for dz := -limit; dz <= limit; dz++ {
				d := max(abs(dx), abs(dy), abs(dz))
				if d >= best {
					continue
				}
				if _, ok := v.occupied[[3]int{cx + dx, cy + dy, cz + dz}]; ok {
					best = d
					if best == 0 {
						return best
					}
				}
			}
		}
	}
	return best
}

// Claimed reports whether a loaded villager holds the POI at the block.
func (v *VillageIndex) Claimed(x, y, z int) bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	_, ok := v.claimed[[3]int{x, y, z}]
	return ok
}

// OccupiedAround lists the claimed village points of interest within radius
// blocks of a block, in no particular order.
// vanilla: PoiManager.getInRange with Occupancy.IS_OCCUPIED
func (v *VillageIndex) OccupiedAround(x, y, z, radius int) []POI {
	v.mu.Lock()
	defer v.mu.Unlock()
	var out []POI
	for k := range v.claimed {
		dx, dy, dz := k[0]-x, k[1]-y, k[2]-z
		if dx*dx+dy*dy+dz*dz <= radius*radius {
			out = append(out, POI{X: k[0], Y: k[1], Z: k[2]})
		}
	}
	return out
}

// Around lists the points of interest within radius blocks of a block, from
// scans up to maxAge ticks old.
// vanilla: PoiManager.getInRange
func (v *VillageIndex) Around(x, y, z, radius int, tick, maxAge int64) []POI {
	v.mu.Lock()
	defer v.mu.Unlock()
	var out []POI
	for cx := (x - radius) >> 4; cx <= (x+radius)>>4; cx++ {
		for cz := (z - radius) >> 4; cz <= (z+radius)>>4; cz++ {
			for _, p := range v.inChunk(world.ChunkPos{X: int32(cx), Z: int32(cz)}, tick, maxAge) {
				dx, dy, dz := p.X-x, p.Y-y, p.Z-z
				if dx*dx+dy*dy+dz*dz <= radius*radius {
					out = append(out, p)
				}
			}
		}
	}
	return out
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
