package world

import (
	"github.com/golem-mc/golem/registry"
)

// treePos is a block position, x, y, z.
type treePos = [3]int32

type uniformBounds struct{ lo, hi int32 }

func parseUniformBounds(v any) uniformBounds {
	o := jobjOf(v)
	return uniformBounds{o.integer("min_inclusive"), o.integer("max_inclusive")}
}

func at(p, off treePos) treePos { return treePos{p[0] + off[0], p[1] + off[1], p[2] + off[2]} }

// blockNames reads a HolderSet: one name, a list of names, or a #tag.
func blockSetOf(v any) func(registry.Block) bool {
	var names []string
	switch t := v.(type) {
	case string:
		names = []string{t}
	case []any:
		for _, n := range t {
			names = append(names, n.(string))
		}
	}
	if len(names) == 1 && names[0][0] == '#' {
		id := registry.BlockTagOf(names[0][1:])
		return func(b registry.Block) bool { return b.InTag(id) }
	}
	set := new([registry.BlockCount]bool)
	for _, n := range names {
		b, ok := registry.BlockByName(n)
		if !ok {
			panic("world: tree data: unknown block " + n)
		}
		set[b] = true
	}
	return func(b registry.Block) bool { return set[b] }
}
