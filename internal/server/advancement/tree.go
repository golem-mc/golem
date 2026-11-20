// Package advancement is the advancement tree, each player's progress and the
// players/advancements file. It sends nothing: Progress.Flush returns the
// next Update Advancements' contents for server to write.
package advancement

import (
	"log/slog"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/golem-mc/golem/registry"
)

// Node is one advancement in the tree. vanilla: AdvancementNode
type Node struct {
	*registry.Advancement
	ParentNode *Node
	Children   []*Node
	// X and Y are the screen position the layout gave a displayed node:
	// X its depth, Y its row.
	X, Y float32
	// index is the node's place in Tree.nodes, so per-player state can be
	// slices rather than maps.
	index int
}

// Root is the root of the node's tree, the tab it is drawn in.
func (n *Node) Root() *Node {
	for n.ParentNode != nil {
		n = n.ParentNode
	}
	return n
}

// HasCriterion reports whether name is one of the node's criteria.
func (n *Node) HasCriterion(name string) bool { return hasCriterion(n, name) }

// Tree is the loaded advancements. vanilla: AdvancementTree
type Tree struct {
	nodes []*Node
	byID  map[string]*Node
	roots []*Node
	// byTrigger is each trigger's criteria, so a firing skips the other
	// nodes; the recipe-unlock advancements alone are over 1700.
	byTrigger map[string][]criterionRef
	// heldBy is the inventory_changed criteria of one item predicate by
	// each item it accepts, and heldAny the others, so a change tests only
	// the criteria a changed item can pass.
	heldBy  map[int32][]criterionRef
	heldAny []criterionRef
	// stackTests is set when an inventory_changed criterion tests whole stacks.
	stackTests bool
}

// criterionRef is one criterion of a node, by its index in n.Criteria.
type criterionRef struct {
	n *Node
	i int
}

var vanilla = sync.OnceValue(func() *Tree { return NewTree(registry.Advancements) })

// Vanilla is the tree of the vanilla data pack alone.
func Vanilla() *Tree { return vanilla() }

// packed is the tree with the data packs' advancements over the vanilla
// ones, nil while no pack has any.
var packed atomic.Pointer[Tree]

// Current is the tree in force: the vanilla one with the enabled packs'
// advancements layered over it. The layer is process-wide, like the pack
// tags and loot tables.
func Current() *Tree {
	if t := packed.Load(); t != nil {
		return t
	}
	return Vanilla()
}

// ApplyPack layers the packs' advancements over the vanilla ones, an ID the
// pack has replacing the vanilla advancement of that ID whole, and returns
// the tree now in force. No advancements leaves the vanilla tree.
// vanilla: ServerAdvancementManager.apply
func ApplyPack(pack []registry.Advancement) *Tree {
	if len(pack) == 0 {
		packed.Store(nil)
		return Vanilla()
	}
	byID := make(map[string]registry.Advancement, len(registry.Advancements)+len(pack))
	for _, a := range registry.Advancements {
		byID[a.ID] = a
	}
	for _, a := range pack {
		byID[a.ID] = a
	}
	merged := make([]registry.Advancement, 0, len(byID))
	for _, a := range byID {
		merged = append(merged, a)
	}
	slices.SortFunc(merged, func(a, b registry.Advancement) int { return strings.Compare(a.ID, b.ID) })
	t := NewTree(merged)
	packed.Store(t)
	return t
}

// NewTree builds the tree and lays out every displayed root, leaving out a
// node whose parent is missing.
// vanilla: ServerAdvancementManager.apply, AdvancementTree.addAll
func NewTree(advancements []registry.Advancement) *Tree {
	t := &Tree{byID: map[string]*Node{}, byTrigger: map[string][]criterionRef{}, heldBy: map[int32][]criterionRef{}}
	pending := make([]*registry.Advancement, len(advancements))
	for i := range advancements {
		pending[i] = &advancements[i]
	}
	for len(pending) > 0 {
		rest := pending[:0]
		for _, a := range pending {
			var parent *Node
			if a.Parent != "" {
				if parent = t.byID[a.Parent]; parent == nil {
					rest = append(rest, a)
					continue
				}
			}
			n := &Node{Advancement: a, ParentNode: parent, index: len(t.nodes)}
			t.nodes = append(t.nodes, n)
			t.byID[a.ID] = n
			if parent == nil {
				t.roots = append(t.roots, n)
			} else {
				parent.Children = append(parent.Children, n)
			}
		}
		if len(rest) == len(pending) {
			ids := make([]string, len(rest))
			for i, a := range rest {
				ids[i] = a.ID
			}
			slog.Error("Couldn't load advancements, their parents are missing or form a cycle", "advancements", ids)
			break
		}
		pending = rest
	}
	for _, n := range t.nodes {
		for i, c := range n.Criteria {
			ref := criterionRef{n, i}
			switch {
			case c.Unmodelled:
			case c.Trigger == TriggerInventoryChanged && c.ItemTests != nil:
				t.heldAny = append(t.heldAny, ref)
				t.stackTests = true
			case c.Trigger == TriggerInventoryChanged && len(c.Items) == 1:
				for _, item := range c.Items[0] {
					if refs := t.heldBy[item]; len(refs) == 0 || refs[len(refs)-1] != ref {
						t.heldBy[item] = append(refs, ref)
					}
				}
			case c.Trigger == TriggerInventoryChanged:
				t.heldAny = append(t.heldAny, ref)
			default:
				t.byTrigger[c.Trigger] = append(t.byTrigger[c.Trigger], ref)
			}
		}
	}
	// Vanilla's children are an identity hash set, so its layout varies by run;
	// sorting by ID keeps this one stable.
	for _, n := range t.nodes {
		slices.SortFunc(n.Children, func(a, b *Node) int { return strings.Compare(a.ID, b.ID) })
	}
	for _, root := range t.roots {
		if root.Display != nil {
			layout(root)
		}
	}
	return t
}

// Get returns the node with the given ID, or nil.
func (t *Tree) Get(id string) *Node { return t.byID[id] }

// Roots returns the tree's roots in load order.
func (t *Tree) Roots() []*Node { return t.roots }

// All returns every node in load order; the caller must not modify it.
func (t *Tree) All() []*Node { return t.nodes }

// Len is the number of advancements in the tree.
func (t *Tree) Len() int { return len(t.nodes) }
