package rules

import "github.com/golem-mc/golem/registry"

// The wither's pattern around a placed wither skeleton skull: a T of soul
// sand or soul soil, three skulls over its bar. What a match does is
// internal/server/wither.go's.
// vanilla: WitherSkullBlock

// WitherSpawn is a match: every cell the pattern fills in the order
// clearPatternBlocks walks them, the cell the wither stands in, and whether
// the match looks along the X axis, which is the yaw 0 of the spawn (a T
// along Z).
type WitherSpawn struct {
	Clear     [][3]int32
	Feet      [3]int32
	ForwardsX bool
}

var (
	witherSkulls = registry.BlocksOf(func(b registry.Block) bool {
		return b == registry.BlockWitherSkeletonSkull || b == registry.BlockWitherSkeletonWallSkull
	})
	witherBases = registry.BlocksOf(func(b registry.Block) bool {
		return b.InTag(registry.BlockTagOf("minecraft:wither_summon_base_blocks"))
	})
)

// witherRows is the pattern's three rows of three, top first: 's' a skull,
// '#' a base block, '~' air, ' ' anything.
// vanilla: WitherSkullBlock.getOrCreateWitherFull, getOrCreateWitherBase
var (
	witherFull = [3]string{"sss", "###", "~#~"}
	witherBase = [3]string{"   ", "###", "~#~"}
)

// WitherSpawnOf reads the wither a skull just placed at (x, y, z) builds.
func WitherSpawnOf(w BlockReader, x, y, z int32) (WitherSpawn, bool) {
	return findWither(w, x, y, z, witherFull)
}

// WitherBaseAt reports whether the T alone stands at (x, y, z) in any
// orientation, which a dispenser asks before it places a skull.
// vanilla: WitherSkullBlock.canSpawnMob
func WitherBaseAt(w BlockReader, x, y, z int32) bool {
	_, ok := findWither(w, x, y, z, witherBase)
	return ok
}

// patternDirs is Direction.values(): down, up, north, south, west, east.
var patternDirs = [6][3]int32{{0, -1, 0}, {0, 1, 0}, {0, 0, -1}, {0, 0, 1}, {-1, 0, 0}, {1, 0, 0}}

// findWither matches rows and rebuilds what the wither needs from the match.
func findWither(w BlockReader, x, y, z int32, rows [3]string) (WitherSpawn, bool) {
	origin, fwd, up, ok := findRows(w, x, y, z, rows[:], func(ch byte, state uint16) bool {
		block := registry.BlockOf(state)
		switch ch {
		case 's':
			return witherSkulls[block]
		case '#':
			return witherBases[block]
		}
		return true
	})
	if !ok {
		return WitherSpawn{}, false
	}
	var clear [][3]int32
	for col := range int32(3) {
		for row := range int32(3) {
			clear = append(clear, patternCell(origin, fwd, up, col, row))
		}
	}
	return WitherSpawn{Clear: clear, Feet: patternCell(origin, fwd, up, 1, 2), ForwardsX: fwd[0] != 0}, true
}

// patternCell is the cell of column col, row row: origin + right*col - up*row,
// right = forwards x up.
// vanilla: BlockPattern.translateAndRotate
func patternCell(origin, fwd, up [3]int32, col, row int32) [3]int32 {
	right := [3]int32{fwd[1]*up[2] - fwd[2]*up[1], fwd[2]*up[0] - fwd[0]*up[2], fwd[0]*up[1] - fwd[1]*up[0]}
	return [3]int32{origin[0] - up[0]*row + right[0]*col, origin[1] - up[1]*row + right[1]*col, origin[2] - up[2]*row + right[2]*col}
}

// findRows is BlockPattern.find: every origin in the cube from (x, y, z) as
// wide as the rows are wide or tall, x fastest, and every
// forwards and up pair that is not parallel. A cell is tested by ok with its
// row character and state; ' ' cells are not tested, '~' cells must be air
// before ok is asked.
// vanilla: BlockPattern.find, BlockPattern.matches
func findRows(w BlockReader, x, y, z int32, rows []string, ok func(ch byte, state uint16) bool) (origin, fwd, up [3]int32, found bool) {
	dist := int32(max(len(rows[0]), len(rows)))
	for dz := range dist {
		for dy := range dist {
			for dx := range dist {
				o := [3]int32{x + dx, y + dy, z + dz}
				for _, f := range patternDirs {
					for _, u := range patternDirs {
						if f == u || f == [3]int32{-u[0], -u[1], -u[2]} {
							continue
						}
						if rowsMatch(w, o, f, u, rows, ok) {
							return o, f, u, true
						}
					}
				}
			}
		}
	}
	return origin, fwd, up, false
}

func rowsMatch(w BlockReader, o, f, u [3]int32, rows []string, ok func(ch byte, state uint16) bool) bool {
	for ri, row := range rows {
		for c := range len(row) {
			if row[c] == ' ' {
				continue
			}
			p := patternCell(o, f, u, int32(c), int32(ri))
			state := w.BlockAt(p[0], p[1], p[2])
			if row[c] == '~' && !isAirState(state) || !ok(row[c], state) {
				return false
			}
		}
	}
	return true
}
