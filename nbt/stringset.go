package nbt

import (
	"slices"
	"unicode/utf16"
)

// StringSet retains Java HashSet iteration order, including collision trees
// and capacity after Reset. The zero value is empty; copies share storage.
type StringSet struct {
	bins []stringBin
	size int
}

type stringBin struct {
	nodes []*stringNode
	root  *stringNode
}
type stringNode struct {
	key    string
	hash   int32
	parent *stringNode
	child  [2]*stringNode
	red    bool
}

// NewStringSet inserts keys into an empty set.
func NewStringSet(keys []string) StringSet {
	var s StringSet
	s.Reset(keys)
	return s
}

// Len returns the number of keys.
func (s StringSet) Len() int { return s.size }

// Contains reports whether key is present without visiting other buckets.
func (s StringSet) Contains(key string) bool {
	if s.size == 0 {
		return false
	}
	b := &s.bins[stringHash(key)&uint32(len(s.bins)-1)]
	for _, n := range b.nodes {
		if n.key == key {
			return true
		}
	}
	return false
}

// Values returns an independent slice in Java iteration order.
func (s StringSet) Values() []string {
	out := make([]string, 0, s.size)
	for _, b := range s.bins {
		for _, n := range b.nodes {
			out = append(out, n.key)
		}
	}
	return out
}

// Reset clears the set without shrinking its table, then inserts keys.
func (s *StringSet) Reset(keys []string) {
	clear(s.bins)
	s.size = 0
	for _, key := range keys {
		s.Add(key)
	}
}

// Add inserts key, reporting whether it was absent.
// vanilla: HashMap.putVal, treeifyBin, TreeNode.putTreeVal
func (s *StringSet) Add(key string) bool {
	if len(s.bins) == 0 {
		s.bins = make([]stringBin, 16)
	}
	hash := int32(stringHash(key))
	b := &s.bins[uint32(hash)&uint32(len(s.bins)-1)]
	for _, n := range b.nodes {
		if n.key == key {
			return false
		}
	}
	n := &stringNode{key: key, hash: hash}
	if b.root == nil {
		b.nodes = append(b.nodes, n)
		if len(b.nodes) >= 9 {
			if len(s.bins) < 64 {
				s.grow()
			} else {
				b.treeify()
			}
		}
	} else {
		p := b.insert(n)
		at := slices.Index(b.nodes, p) + 1
		b.nodes = slices.Insert(b.nodes, at, n)
		b.frontRoot()
	}
	s.size++
	if s.size > len(s.bins)*3/4 {
		s.grow()
	}
	return true
}

// Remove deletes key, reporting whether it was present.
// vanilla: HashMap.removeNode, TreeNode.removeTreeNode
func (s *StringSet) Remove(key string) bool {
	if len(s.bins) == 0 {
		return false
	}
	b := &s.bins[stringHash(key)&uint32(len(s.bins)-1)]
	for i, n := range b.nodes {
		if n.key != key {
			continue
		}
		b.nodes = slices.Delete(b.nodes, i, i+1)
		s.size--
		if b.root != nil {
			if b.root.child[1] == nil || b.root.child[0] == nil || b.root.child[0].child[0] == nil {
				b.root = nil
			} else {
				b.removeTree(n)
				b.frontRoot()
			}
		}
		return true
	}
	return false
}

func (s *StringSet) grow() {
	old := s.bins
	s.bins = make([]stringBin, 2*len(old))
	for i, b := range old {
		for _, n := range b.nodes {
			at := i
			if uint32(n.hash)&uint32(len(old)) != 0 {
				at += len(old)
			}
			s.bins[at].nodes = append(s.bins[at].nodes, n)
		}
		if b.root == nil {
			continue
		}
		lo, hi := &s.bins[i], &s.bins[i+len(old)]
		for _, part := range []*stringBin{lo, hi} {
			if len(part.nodes) <= 6 {
				continue
			}
			if len(lo.nodes) != 0 && len(hi.nodes) != 0 {
				part.treeify()
			} else {
				part.root = b.root
			}
		}
	}
}

func (b *stringBin) frontRoot() {
	at := slices.Index(b.nodes, b.root)
	if at > 0 {
		copy(b.nodes[1:at+1], b.nodes[:at])
		b.nodes[0] = b.root
	}
}

func (b *stringBin) treeify() {
	b.root = nil
	for _, n := range b.nodes {
		n.parent, n.child, n.red = nil, [2]*stringNode{}, false
		if b.root == nil {
			b.root = n
		} else {
			b.insert(n)
		}
	}
	b.frontRoot()
}

// String.compareTo compares UTF-16 code units, including supplementary runes.
func stringNodeLess(a, b *stringNode) bool {
	if a.hash != b.hash {
		return a.hash < b.hash
	}
	return slices.Compare(utf16.Encode([]rune(a.key)), utf16.Encode([]rune(b.key))) < 0
}

func (b *stringBin) insert(n *stringNode) *stringNode {
	p := b.root
	for {
		dir := 0
		if !stringNodeLess(n, p) {
			dir = 1
		}
		if p.child[dir] == nil {
			p.child[dir], n.parent, n.red = n, p, true
			b.balanceInsert(n)
			return p
		}
		p = p.child[dir]
	}
}

func (b *stringBin) rotate(p *stringNode, dir int) {
	n := p.child[1-dir]
	p.child[1-dir] = n.child[dir]
	if n.child[dir] != nil {
		n.child[dir].parent = p
	}
	n.parent = p.parent
	if p.parent == nil {
		b.root = n
		n.red = false
	} else {
		p.parent.child[nodeSide(p)] = n
	}
	n.child[dir], p.parent = p, n
}

func nodeSide(n *stringNode) int {
	if n.parent.child[1] == n {
		return 1
	}
	return 0
}
func nodeRed(n *stringNode) bool { return n != nil && n.red }

func (b *stringBin) balanceInsert(n *stringNode) {
	for {
		p := n.parent
		if p == nil {
			n.red = false
			b.root = n
			return
		}
		if !p.red || p.parent == nil {
			return
		}
		g := p.parent
		dir := nodeSide(p)
		uncle := g.child[1-dir]
		if nodeRed(uncle) {
			uncle.red, p.red, g.red = false, false, true
			n = g
			continue
		}
		if n == p.child[1-dir] {
			n = p
			b.rotate(n, dir)
			p = n.parent
			g = p.parent
		}
		p.red, g.red = false, true
		b.rotate(g, 1-dir)
	}
}

func (b *stringBin) removeTree(n *stringNode) {
	// Swapping keys gives the same surviving tree as Java's linkage swap.
	// Keep each surviving key at its original position in the iteration list.
	if n.child[0] != nil && n.child[1] != nil {
		next := n.child[1]
		for next.child[0] != nil {
			next = next.child[0]
		}
		n.key, next.key = next.key, n.key
		n.hash, next.hash = next.hash, n.hash
		b.nodes[slices.Index(b.nodes, next)] = n
		n = next
	}
	replacement := n.child[0]
	if replacement == nil {
		replacement = n.child[1]
	}
	if replacement == nil {
		replacement = n
	}
	if replacement != n {
		replacement.parent = n.parent
		if n.parent == nil {
			b.root = replacement
			replacement.red = false
		} else {
			n.parent.child[nodeSide(n)] = replacement
		}
	}
	if !n.red {
		b.balanceRemove(replacement)
	}
	if replacement == n && n.parent != nil {
		n.parent.child[nodeSide(n)] = nil
	}
}

func (b *stringBin) balanceRemove(n *stringNode) {
	for n != nil && n != b.root {
		p := n.parent
		if p == nil {
			n.red = false
			b.root = n
			return
		}
		if n.red {
			n.red = false
			return
		}
		dir := nodeSide(n)
		sibling := p.child[1-dir]
		if nodeRed(sibling) {
			sibling.red, p.red = false, true
			b.rotate(p, dir)
			p = n.parent
			sibling = p.child[1-dir]
		}
		if sibling == nil {
			n = p
			continue
		}
		near, far := sibling.child[dir], sibling.child[1-dir]
		if !nodeRed(near) && !nodeRed(far) {
			sibling.red = true
			n = p
			continue
		}
		if !nodeRed(far) {
			if near != nil {
				near.red = false
			}
			sibling.red = true
			b.rotate(sibling, 1-dir)
			p = n.parent
			sibling = p.child[1-dir]
		}
		sibling.red = p.red
		if sibling.child[1-dir] != nil {
			sibling.child[1-dir].red = false
		}
		p.red = false
		b.rotate(p, dir)
		n = b.root
	}
}
