package world

import (
	"slices"

	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/registry"
)

// treeDecorator is a TreeDecorator.
type treeDecorator interface{ place(c *decoratorCtx) }

// decoratorCtx is TreeDecorator.Context: the tree's positions sorted by y.
type decoratorCtx struct {
	t                   *treeBuild
	logs, leaves, roots []treePos
}

func (c *decoratorCtx) setBlock(p treePos, st uint16) { c.t.put(&c.t.decor, p, st) }

func (c *decoratorCtx) isAir(p treePos) bool { return c.t.isAir(p) }

// treeDirStep is Direction's step by name.
var treeDirStep = map[string]treePos{
	"down": {0, -1, 0}, "up": {0, 1, 0}, "north": {0, 0, -1}, "south": {0, 0, 1}, "west": {-1, 0, 0}, "east": {1, 0, 0},
}

var vineFaceProps = map[string]registry.Prop{
	"north": registry.PropNorth, "east": registry.PropEast, "south": registry.PropSouth, "west": registry.PropWest,
}

func (c *decoratorCtx) placeVine(p treePos, face string) {
	st, _ := registry.WithBool(registry.BlockVine.DefaultState(), vineFaceProps[face], true)
	c.setBlock(p, st)
}

// lowestTrunkOrRoot is the positions the ground decorators work from.
// vanilla: TreeFeature.getLowestTrunkOrRootOfTree
func (c *decoratorCtx) lowestTrunkOrRoot() []treePos {
	switch {
	case len(c.roots) == 0:
		return c.logs
	case len(c.logs) > 0 && c.roots[0][1] == c.logs[0][1]:
		return append(append([]treePos(nil), c.logs...), c.roots...)
	}
	return c.roots
}

// treeShuffledCopy is Util.shuffledCopy.
func treeShuffledCopy(in []treePos, rng *levelgen.WorldgenRandom) []treePos {
	out := append([]treePos(nil), in...)
	shuffle(out, rng)
	return out
}

// beehiveDeco is BeehiveDecorator.
type beehiveDeco struct{ probability float32 }

// beeSpawnDirs are the horizontal directions but north, the opposite of the
// nest's south facing.
var beeSpawnDirs = [3]int{1, 2, 3}

func (d beehiveDeco) place(c *decoratorCtx) {
	logs, leaves, rng := c.logs, c.leaves, c.t.rng
	if len(logs) == 0 || rng.NextFloat() >= d.probability {
		return
	}
	var hiveY int32
	if len(leaves) > 0 {
		hiveY = max(leaves[0][1]-1, logs[0][1]+1)
	} else {
		hiveY = min(logs[0][1]+1+rng.NextIntN(3), logs[len(logs)-1][1])
	}
	var places []treePos
	for _, p := range logs {
		if p[1] != hiveY {
			continue
		}
		for _, dir := range beeSpawnDirs {
			places = append(places, horizStep(p, dir, 1))
		}
	}
	if len(places) == 0 {
		return
	}
	shuffle(places, rng)
	for _, p := range places {
		if !c.isAir(p) || !c.isAir(horizStep(p, 2, 1)) {
			continue
		}
		st, _ := registry.WithProp(registry.BlockBeeNest.DefaultState(), registry.PropFacing, "south")
		c.setBlock(p, st)
		c.t.r.placeHive(p, 2+rng.NextIntN(2), rng)
		return
	}
}

// leaveVineDeco is LeaveVineDecorator.
type leaveVineDeco struct{ probability float32 }

func (d leaveVineDeco) place(c *decoratorCtx) {
	rng := c.t.rng
	for _, p := range c.leaves {
		for _, s := range [4]struct {
			dir  int
			face string
		}{{3, "east"}, {1, "west"}, {0, "south"}, {2, "north"}} {
			q := horizStep(p, s.dir, 1)
			if rng.NextFloat() < d.probability && c.isAir(q) {
				c.placeVine(q, s.face)
				q = at(q, treePos{0, -1, 0})
				for n := 4; c.isAir(q) && n > 0; n-- {
					c.placeVine(q, s.face)
					q = at(q, treePos{0, -1, 0})
				}
			}
		}
	}
}

// trunkVineDeco is TrunkVineDecorator.
type trunkVineDeco struct{}

func (trunkVineDeco) place(c *decoratorCtx) {
	rng := c.t.rng
	for _, p := range c.logs {
		for _, s := range [4]struct {
			dir  int
			face string
		}{{3, "east"}, {1, "west"}, {0, "south"}, {2, "north"}} {
			q := horizStep(p, s.dir, 1)
			if rng.NextIntN(3) > 0 && c.isAir(q) {
				c.placeVine(q, s.face)
			}
		}
	}
}

// alterGroundDeco is AlterGroundDecorator.
type alterGroundDeco struct{ provider optState }

func (d alterGroundDeco) place(c *decoratorCtx) {
	ps := c.lowestTrunkOrRoot()
	if len(ps) == 0 {
		return
	}
	minY := ps[0][1]
	for _, p := range ps {
		if p[1] != minY {
			continue
		}
		d.circle(c, at(p, treePos{-1, 0, -1}))
		d.circle(c, at(p, treePos{2, 0, -1}))
		d.circle(c, at(p, treePos{-1, 0, 2}))
		d.circle(c, at(p, treePos{2, 0, 2}))
		for range 5 {
			placement := c.t.rng.NextIntN(64)
			xx, zz := placement%8, placement/8
			if xx != 0 && xx != 7 && zz != 0 && zz != 7 {
				continue
			}
			d.circle(c, at(p, treePos{-3 + xx, 0, -3 + zz}))
		}
	}
}

func (d alterGroundDeco) circle(c *decoratorCtx, p treePos) {
	for xx := int32(-2); xx <= 2; xx++ {
		for zz := int32(-2); zz <= 2; zz++ {
			if abs32(xx) == 2 && abs32(zz) == 2 {
				continue
			}
			d.blockAt(c, at(p, treePos{xx, 0, zz}))
		}
	}
}

func (d alterGroundDeco) blockAt(c *decoratorCtx, p treePos) {
	for dy := int32(2); dy >= -3; dy-- {
		cur := at(p, treePos{0, dy, 0})
		if st, ok := d.provider(c.t.r, cur); ok {
			c.setBlock(cur, st)
			return
		}
		if !c.isAir(cur) && dy < 0 {
			return
		}
	}
}

// placeOnGroundDeco is PlaceOnGroundDecorator.
type placeOnGroundDeco struct {
	tries, radius, height int32
	provider              stateProvider
}

func (d placeOnGroundDeco) place(c *decoratorCtx) {
	ps := c.lowestTrunkOrRoot()
	if len(ps) == 0 {
		return
	}
	rng := c.t.rng
	minY, minX, maxX, minZ, maxZ := ps[0][1], ps[0][0], ps[0][0], ps[0][2], ps[0][2]
	for _, p := range ps {
		if p[1] != minY {
			continue
		}
		minX, maxX = min(minX, p[0]), max(maxX, p[0])
		minZ, maxZ = min(minZ, p[2]), max(maxZ, p[2])
	}
	between := func(lo, hi int32) int32 { return lo + rng.NextIntN(hi-lo+1) }
	loX, hiX, loY, hiY, loZ, hiZ := minX-d.radius, maxX+d.radius, minY-d.height, minY+d.height, minZ-d.radius, maxZ+d.radius
	for range d.tries {
		p := treePos{between(loX, hiX), 0, 0}
		p[1] = between(loY, hiY)
		p[2] = between(loZ, hiZ)
		d.attempt(c, p)
	}
}

func (d placeOnGroundDeco) attempt(c *decoratorCtx, p treePos) {
	r := c.t.r
	up := at(p, treePos{0, 1, 0})
	above := r.get(up[0], up[1], up[2])
	if (c.isAir(up) || registry.BlockOf(above) == registry.BlockVine) &&
		registry.IsSolidRender(c.t.state(p)) && r.heightAt(func(s uint16) bool { return noLeavesBlockingByState[s] }, p[0], p[2]) <= up[1] {
		c.setBlock(up, d.provider(r, up))
	}
}

// attachedToLeavesDeco is AttachedToLeavesDecorator.
type attachedToLeavesDeco struct {
	probability       float32
	radiusXZ, radiusY int32
	provider          stateProvider
	requiredEmpty     int32
	directions        []treePos
}

func (d attachedToLeavesDeco) place(c *decoratorCtx) {
	rng := c.t.rng
	blacklist := map[treePos]struct{}{}
	for _, leaf := range treeShuffledCopy(c.leaves, rng) {
		dir := d.directions[rng.NextIntN(int32(len(d.directions)))]
		place := at(leaf, dir)
		if _, no := blacklist[place]; no || !(rng.NextFloat() < d.probability) {
			continue
		}
		free := true
		for i := int32(1); i <= d.requiredEmpty; i++ {
			if !c.isAir(at(leaf, treePos{dir[0] * i, dir[1] * i, dir[2] * i})) {
				free = false
				break
			}
		}
		if !free {
			continue
		}
		for x := place[0] - d.radiusXZ; x <= place[0]+d.radiusXZ; x++ {
			for y := place[1] - d.radiusY; y <= place[1]+d.radiusY; y++ {
				for z := place[2] - d.radiusXZ; z <= place[2]+d.radiusXZ; z++ {
					blacklist[treePos{x, y, z}] = struct{}{}
				}
			}
		}
		c.setBlock(place, d.provider(c.t.r, place))
	}
}

// attachedToLogsDeco is AttachedToLogsDecorator.
type attachedToLogsDeco struct {
	probability float32
	provider    stateProvider
	directions  []treePos
}

func (d attachedToLogsDeco) place(c *decoratorCtx) {
	rng := c.t.rng
	for _, log := range treeShuffledCopy(c.logs, rng) {
		place := at(log, d.directions[rng.NextIntN(int32(len(d.directions)))])
		if !(rng.NextFloat() <= d.probability) || !c.isAir(place) {
			continue
		}
		c.setBlock(place, d.provider(c.t.r, place))
	}
}

// cocoaDeco is CocoaDecorator.
type cocoaDeco struct{ probability float32 }

func (d cocoaDeco) place(c *decoratorCtx) {
	rng := c.t.rng
	if rng.NextFloat() >= d.probability || len(c.logs) == 0 {
		return
	}
	treeY := c.logs[0][1]
	for _, p := range c.logs {
		if p[1]-treeY > 2 {
			continue
		}
		for dir := range 4 {
			q := horizStep(p, (dir+2)%4, 1)
			if !(rng.NextFloat() <= 0.25) || !c.isAir(q) {
				continue
			}
			st, _ := registry.WithInt(registry.BlockCocoa.DefaultState(), registry.PropAge, int(rng.NextIntN(3)))
			st, _ = registry.WithProp(st, registry.PropFacing, [4]string{"north", "east", "south", "west"}[dir])
			c.setBlock(q, st)
		}
	}
}

func parseDecorator(o jobj) treeDecorator {
	dirs := func(k string) []treePos {
		var out []treePos
		for _, n := range o.arr(k) {
			out = append(out, treeDirStep[n.(string)])
		}
		return out
	}
	switch o.str("type") {
	case "minecraft:beehive":
		return beehiveDeco{float32(o.num("probability"))}
	case "minecraft:leave_vine":
		return leaveVineDeco{float32(o.num("probability"))}
	case "minecraft:trunk_vine":
		return trunkVineDeco{}
	case "minecraft:alter_ground":
		return alterGroundDeco{compileOptState(o["provider"])}
	case "minecraft:place_on_ground":
		return placeOnGroundDeco{o.integerOr("tries", 128), o.integerOr("radius", 2), o.integerOr("height", 1), compileState(o["block_state_provider"])}
	case "minecraft:attached_to_leaves":
		return attachedToLeavesDeco{float32(o.num("probability")), o.integer("exclusion_radius_xz"), o.integer("exclusion_radius_y"),
			compileState(o["block_provider"]), o.integer("required_empty_blocks"), dirs("directions")}
	case "minecraft:attached_to_logs":
		return attachedToLogsDeco{float32(o.num("probability")), compileState(o["block_provider"]), dirs("directions")}
	case "minecraft:cocoa":
		return cocoaDeco{float32(o.num("probability"))}
	case "minecraft:pale_moss":
		return paleMossDeco{float32(o.num("leaves_probability")), float32(o.num("trunk_probability")),
			float32(o.num("ground_probability")), compileConfigured("minecraft:pale_moss_patch")}
	case "minecraft:creaking_heart":
		return creakingHeartDeco{float32(o.num("probability"))}
	case "minecraft:shelf_mushroom":
		return shelfMushroomDeco{float32(o.num("probability"))}
	}
	return nil
}

// shelfMushroomDeco is ShelfMushroomDecorator.
type shelfMushroomDeco struct{ probability float32 }

var horizontalDirNames = [4]string{"north", "east", "south", "west"}

func (d shelfMushroomDeco) place(c *decoratorCtx) {
	rng, logs := c.t.rng, c.logs
	if rng.NextFloat() >= d.probability || len(logs) == 0 {
		return
	}
	first, last := logs[0], logs[len(logs)-1]
	isMushroom := func(p treePos) bool { return registry.BlockOf(c.t.state(p)) == registry.BlockShelfMushroom }
	free := func(p treePos) bool {
		if !registry.BlockOf(c.t.state(p)).IsReplaceable() || isWater(c.t.state(p)) {
			return false
		}
		for dir := range 4 {
			if isWater(c.t.state(horizStep(p, dir, 1))) {
				return false
			}
		}
		return true
	}
	beside := func(p treePos) bool {
		for dir := range 4 {
			if isMushroom(horizStep(p, dir, 1)) {
				return true
			}
		}
		return false
	}
	put := func(p treePos, dir int) {
		st, _ := registry.WithInt(registry.BlockShelfMushroom.DefaultState(), registry.PropAge, int(rng.NextIntN(2)))
		st, _ = registry.WithProp(st, registry.PropFacing, horizontalDirNames[dir])
		c.setBlock(p, st)
	}
	if first[1] == last[1] { // a fallen log: both sides across its axis
		dirs := [2]int{1, 3}
		if first[0] != last[0] {
			dirs = [2]int{0, 2}
		}
		for _, log := range logs {
			for _, dir := range dirs {
				if rng.NextFloat() > 0.25 {
					continue
				}
				if p := horizStep(log, dir, 1); free(p) && !beside(p) && !beside(log) {
					put(p, dir)
				}
			}
		}
		return
	}
	dir := randomHorizontal(rng)
	for _, log := range logs {
		if dy := log[1] - first[1]; dy < 1 || dy > 4 {
			continue
		}
		for _, dir := range [2]int{dir, (dir + 1) % 4} {
			if rng.NextFloat() > 0.25 {
				continue
			}
			if p := horizStep(log, dir, 1); free(p) && !isMushroom(at(p, treePos{0, -1, 0})) {
				put(p, dir)
				break
			}
		}
	}
}

// placeHive stores the bees of a bee nest placed at p: one occupant per draw of
// ticks_in_hive.
// vanilla: BeehiveBlockEntity.Occupant.create
func (r *decorRegion) placeHive(p treePos, bees int32, rng *levelgen.WorldgenRandom) {
	packed := int64(bees) << 30
	for i := range bees {
		packed |= int64(rng.NextIntN(599)) << (10 * i)
	}
	pr := r.protoAt(p[0], p[2])
	pr.entities = append(pr.entities, protoEntity{hive: true, x: p[0] & 15, y: p[1] - r.minY, z: p[2] & 15, lootSeed: packed})
}

// paleMossDeco is PaleMossDecorator.
type paleMossDeco struct {
	leaves, trunk, ground float32
	patch                 func(*decorRegion, treePos) bool
}

func (d paleMossDeco) place(c *decoratorCtx) {
	rng := c.t.rng
	logs := treeShuffledCopy(c.logs, rng)
	if len(logs) == 0 {
		return
	}
	origin := logs[0]
	for _, p := range logs[1:] {
		if p[1] < origin[1] {
			origin = p
		}
	}
	if rng.NextFloat() < d.ground {
		d.patch(c.t.r, at(origin, treePos{0, 1, 0}))
	}
	for _, p := range c.logs {
		if down := at(p, treePos{0, -1, 0}); rng.NextFloat() < d.trunk && c.isAir(down) {
			c.addMossHanger(down)
		}
	}
	for _, p := range c.leaves {
		if down := at(p, treePos{0, -1, 0}); rng.NextFloat() < d.leaves && c.isAir(down) {
			c.addMossHanger(down)
		}
	}
}

// addMossHanger places a pale hanging moss column, 1 in 2 per block to end.
func (c *decoratorCtx) addMossHanger(p treePos) {
	moss := func(tip bool) uint16 {
		st, _ := registry.WithBool(registry.BlockPaleHangingMoss.DefaultState(), registry.PropTip, tip)
		return st
	}
	for c.isAir(at(p, treePos{0, -1, 0})) && c.t.rng.NextFloat() >= 0.5 {
		c.setBlock(p, moss(false))
		p = at(p, treePos{0, -1, 0})
	}
	c.setBlock(p, moss(true))
}

// creakingHeartDeco is CreakingHeartDecorator.
type creakingHeartDeco struct{ probability float32 }

func (d creakingHeartDeco) place(c *decoratorCtx) {
	rng := c.t.rng
	if len(c.logs) == 0 || rng.NextFloat() >= d.probability {
		return
	}
	for _, p := range treeShuffledCopy(c.logs, rng) {
		if !slices.ContainsFunc(dirDelta[:], func(s [3]int32) bool { return !c.t.is(at(p, s), treeLog) }) {
			st, _ := registry.WithProp(registry.BlockCreakingHeart.DefaultState(), registry.PropCreakingHeartState, "dormant")
			st, _ = registry.WithBool(st, registry.PropNatural, true)
			c.setBlock(p, st)
			return
		}
	}
}
