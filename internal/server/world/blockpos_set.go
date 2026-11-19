package world

// blockPosSetOrder returns Java HashSet<BlockPos> iteration order. Only equal
// full hashes in a tree bin need unsupported JVM identity tie-breaking.
// blockPosSet models treeification, which changes iteration order, for
// insertion and for removal.
// vanilla: Vec3i.hashCode; Java 25 HashMap.putVal, resize, TreeNode
func blockPosSetOrder(r *decorRegion, in [][3]int32) [][3]int32 {
	s := blockPosSet{table: make([]*blockPosNode, 16), region: r}
	seen := make(map[[3]int32]bool, len(in))
	for _, p := range in {
		if seen[p] {
			continue
		}
		seen[p] = true
		h := hashPosSpread(hashPosHash(p))
		s.put(&blockPosNode{pos: p, hash: int32(h)})
	}
	out := make([][3]int32, 0, len(seen))
	for _, head := range s.table {
		for p := head; p != nil; p = p.next {
			out = append(out, p.pos)
		}
	}
	return out
}

type blockPosNode struct {
	pos                             [3]int32
	hash                            int32
	next, prev, parent, left, right *blockPosNode
	tree, red                       bool
}

type blockPosSet struct {
	table []*blockPosNode
	size  int
	// region is told about an identity-hash tie; nil for a set that does not
	// report one.
	region *decorRegion
	// lo is a lower bound on the first occupied bucket, so take does not
	// rescan the empty ones.
	lo int
	// free holds the nodes of taken entries and of the last reset.
	free []*blockPosNode
}

// reset empties the set for a new walk, keeping its nodes.
func (s *blockPosSet) reset() {
	for i, head := range s.table {
		for p := head; p != nil; p = p.next {
			s.free = append(s.free, p)
		}
		s.table[i] = nil
	}
	if len(s.table) > 16 {
		s.table = s.table[:16]
	}
	s.size, s.lo = 0, 0
}

// add is HashSet.add: a position the set holds changes nothing.
func (s *blockPosSet) add(pos [3]int32) {
	if len(s.table) == 0 {
		s.table = make([]*blockPosNode, 16)
	}
	h := int32(hashPosSpread(hashPosHash(pos)))
	for e := s.table[s.index(h)]; e != nil; e = e.next {
		if e.pos == pos {
			return
		}
	}
	var x *blockPosNode
	if n := len(s.free); n > 0 {
		x, s.free = s.free[n-1], s.free[:n-1]
	} else {
		x = new(blockPosNode)
	}
	*x = blockPosNode{pos: pos, hash: h}
	s.put(x)
}

// take is it = set.iterator(); it.next(); it.remove(): the first position in
// iteration order, removed.
// vanilla: HashMap.HashIterator.remove, HashMap.removeNode
func (s *blockPosSet) take() ([3]int32, bool) {
	for ; s.lo < len(s.table); s.lo++ {
		x := s.table[s.lo]
		if x == nil {
			continue
		}
		pos := x.pos
		if x.tree {
			s.removeTreeNode(x)
		} else {
			s.table[s.lo] = x.next
		}
		s.size--
		s.free = append(s.free, x)
		return pos, true
	}
	return [3]int32{}, false
}

// removeTreeNode unlinks a node of a tree bin and rebalances; the iterator's
// remove never untreeifies or moves the root to the front.
// vanilla: HashMap.TreeNode.removeTreeNode (movable = false)
func (s *blockPosSet) removeTreeNode(p *blockPosNode) {
	i := s.index(p.hash)
	first := s.table[i]
	root := first
	succ, pred := p.next, p.prev
	if pred == nil {
		s.table[i] = succ
		first = succ
	} else {
		pred.next = succ
	}
	if succ != nil {
		succ.prev = pred
	}
	if first == nil {
		return
	}
	for root.parent != nil {
		root = root.parent
	}
	pl, pr := p.left, p.right
	var replacement *blockPosNode
	switch {
	case pl != nil && pr != nil:
		succNode := pr
		for succNode.left != nil {
			succNode = succNode.left
		}
		sc := succNode
		sc.red, p.red = p.red, sc.red
		sr := sc.right
		pp := p.parent
		if sc == pr {
			p.parent = sc
			sc.right = p
		} else {
			sp := sc.parent
			if p.parent = sp; sp != nil {
				if sc == sp.left {
					sp.left = p
				} else {
					sp.right = p
				}
			}
			if sc.right = pr; pr != nil {
				pr.parent = sc
			}
		}
		p.left = nil
		if p.right = sr; sr != nil {
			sr.parent = p
		}
		if sc.left = pl; pl != nil {
			pl.parent = sc
		}
		if sc.parent = pp; pp == nil {
			root = sc
		} else if p == pp.left {
			pp.left = sc
		} else {
			pp.right = sc
		}
		if sr != nil {
			replacement = sr
		} else {
			replacement = p
		}
	case pl != nil:
		replacement = pl
	case pr != nil:
		replacement = pr
	default:
		replacement = p
	}
	if replacement != p {
		pp := p.parent
		replacement.parent = pp
		if pp == nil {
			root = replacement
			root.red = false
		} else if p == pp.left {
			pp.left = replacement
		} else {
			pp.right = replacement
		}
		p.left, p.right, p.parent = nil, nil, nil
	}
	if !p.red {
		blockPosBalanceDeletion(root, replacement)
	}
	if replacement == p {
		pp := p.parent
		p.parent = nil
		if pp != nil {
			switch p {
			case pp.left:
				pp.left = nil
			case pp.right:
				pp.right = nil
			}
		}
	}
}

// blockPosBalanceDeletion is TreeNode.balanceDeletion.
func blockPosBalanceDeletion(root, x *blockPosNode) *blockPosNode {
	for {
		if x == nil || x == root {
			return root
		}
		xp := x.parent
		if xp == nil {
			x.red = false
			return x
		}
		if x.red {
			x.red = false
			return root
		}
		if xp.left == x {
			xpr := xp.right
			if xpr != nil && xpr.red {
				xpr.red = false
				xp.red = true
				root = blockPosRotateLeft(root, xp)
				if xp = x.parent; xp == nil {
					xpr = nil
				} else {
					xpr = xp.right
				}
			}
			if xpr == nil {
				x = xp
				continue
			}
			sl, sr := xpr.left, xpr.right
			if (sr == nil || !sr.red) && (sl == nil || !sl.red) {
				xpr.red = true
				x = xp
				continue
			}
			if sr == nil || !sr.red {
				if sl != nil {
					sl.red = false
				}
				xpr.red = true
				root = blockPosRotateRight(root, xpr)
				if xp = x.parent; xp == nil {
					xpr = nil
				} else {
					xpr = xp.right
				}
			}
			if xpr != nil {
				xpr.red = xp != nil && xp.red
				if sr = xpr.right; sr != nil {
					sr.red = false
				}
			}
			if xp != nil {
				xp.red = false
				root = blockPosRotateLeft(root, xp)
			}
			x = root
			continue
		}
		xpl := xp.left
		if xpl != nil && xpl.red {
			xpl.red = false
			xp.red = true
			root = blockPosRotateRight(root, xp)
			if xp = x.parent; xp == nil {
				xpl = nil
			} else {
				xpl = xp.left
			}
		}
		if xpl == nil {
			x = xp
			continue
		}
		sl, sr := xpl.left, xpl.right
		if (sl == nil || !sl.red) && (sr == nil || !sr.red) {
			xpl.red = true
			x = xp
			continue
		}
		if sl == nil || !sl.red {
			if sr != nil {
				sr.red = false
			}
			xpl.red = true
			root = blockPosRotateLeft(root, xpl)
			if xp = x.parent; xp == nil {
				xpl = nil
			} else {
				xpl = xp.left
			}
		}
		if xpl != nil {
			xpl.red = xp != nil && xp.red
			if sl = xpl.left; sl != nil {
				sl.red = false
			}
		}
		if xp != nil {
			xp.red = false
			root = blockPosRotateRight(root, xp)
		}
		x = root
	}
}

func (s *blockPosSet) index(h int32) int { return int(uint32(h) & uint32(len(s.table)-1)) }

func (s *blockPosSet) put(x *blockPosNode) {
	i := s.index(x.hash)
	s.lo = min(s.lo, i)
	p := s.table[i]
	if p == nil {
		s.table[i] = x
	} else if p.tree {
		root := p
		for root.parent != nil {
			root = root.parent
		}
		for {
			dir := s.compare(x, p)
			parent := p
			if dir < 0 {
				p = p.left
			} else {
				p = p.right
			}
			if p != nil {
				continue
			}
			x.tree = true
			// Tree iteration follows this list, with insertion after the tree parent.
			x.parent, x.prev, x.next = parent, parent, parent.next
			if dir < 0 {
				parent.left = x
			} else {
				parent.right = x
			}
			if x.next != nil {
				x.next.prev = x
			}
			parent.next = x
			s.moveRootToFront(blockPosBalanceInsertion(root, x))
			break
		}
	} else {
		count := 0
		for p.next != nil {
			p = p.next
			count++
		}
		p.next = x
		// vanilla: HashMap.putVal treeifies after the ninth node.
		if count >= 7 {
			if len(s.table) < 64 {
				s.resize()
			} else {
				s.treeify(s.table[i])
			}
		}
	}
	s.size++
	if s.size > len(s.table)*3/4 {
		s.resize()
	}
}

// compare uses signed spread hashes; BlockPos does not implement Comparable<BlockPos>.
func (s *blockPosSet) compare(x, p *blockPosNode) int {
	if x.hash < p.hash {
		return -1
	}
	if x.hash > p.hash {
		return 1
	}
	if s.region != nil {
		s.region.unorderedSet = true
	}
	// Deterministic fallback for the JVM's identity-hash tie, never Vec3i.compareTo.
	for i := range x.pos {
		if x.pos[i] < p.pos[i] {
			return -1
		}
		if x.pos[i] > p.pos[i] {
			return 1
		}
	}
	return 0
}

func (s *blockPosSet) moveRootToFront(root *blockPosNode) {
	i := s.index(root.hash)
	first := s.table[i]
	if root == first {
		return
	}
	s.table[i] = root
	if root.next != nil {
		root.next.prev = root.prev
	}
	if root.prev != nil {
		root.prev.next = root.next
	}
	if first != nil {
		first.prev = root
	}
	root.next, root.prev = first, nil
}

func (s *blockPosSet) treeify(head *blockPosNode) {
	var root, prev *blockPosNode
	for x := head; x != nil; x = x.next {
		x.tree, x.prev = true, prev
		prev = x
		x.left, x.right = nil, nil
		if root == nil {
			x.parent, x.red = nil, false
			root = x
			continue
		}
		for p := root; ; {
			dir := s.compare(x, p)
			parent := p
			if dir < 0 {
				p = p.left
			} else {
				p = p.right
			}
			if p != nil {
				continue
			}
			x.parent = parent
			if dir < 0 {
				parent.left = x
			} else {
				parent.right = x
			}
			root = blockPosBalanceInsertion(root, x)
			break
		}
	}
	s.moveRootToFront(root)
}

func blockPosUntreeify(head *blockPosNode) {
	for p := head; p != nil; p = p.next {
		p.tree, p.red = false, false
		p.prev, p.parent, p.left, p.right = nil, nil, nil, nil
	}
}

func (s *blockPosSet) resize() {
	old := s.table
	s.lo = 0
	s.table = make([]*blockPosNode, len(old)*2)
	for i, head := range old {
		if head == nil {
			continue
		}
		if head.next == nil {
			s.table[s.index(head.hash)] = head
			continue
		}
		tree := head.tree
		// Stable partitions preserve list order, including root-to-front moves.
		var heads, tails [2]*blockPosNode
		var counts [2]int
		for p := head; p != nil; {
			next := p.next
			side := 0
			if uint32(p.hash)&uint32(len(old)) != 0 {
				side = 1
			}
			p.next = nil
			if tree {
				p.prev = tails[side]
			}
			if tails[side] == nil {
				heads[side] = p
			} else {
				tails[side].next = p
			}
			tails[side] = p
			counts[side]++
			p = next
		}
		for side, h := range heads {
			if h == nil {
				continue
			}
			s.table[i+side*len(old)] = h
			if tree {
				// vanilla: HashMap.UNTREEIFY_THRESHOLD
				if counts[side] <= 6 {
					blockPosUntreeify(h)
				} else if heads[1-side] != nil {
					s.treeify(h)
				}
			}
		}
	}
}

func blockPosRotateLeft(root, p *blockPosNode) *blockPosNode {
	if p != nil && p.right != nil {
		r := p.right
		p.right = r.left
		if r.left != nil {
			r.left.parent = p
		}
		r.parent = p.parent
		if p.parent == nil {
			root = r
			r.red = false
		} else if p.parent.left == p {
			p.parent.left = r
		} else {
			p.parent.right = r
		}
		r.left, p.parent = p, r
	}
	return root
}

func blockPosRotateRight(root, p *blockPosNode) *blockPosNode {
	if p != nil && p.left != nil {
		l := p.left
		p.left = l.right
		if l.right != nil {
			l.right.parent = p
		}
		l.parent = p.parent
		if p.parent == nil {
			root = l
			l.red = false
		} else if p.parent.right == p {
			p.parent.right = l
		} else {
			p.parent.left = l
		}
		l.right, p.parent = p, l
	}
	return root
}

func blockPosBalanceInsertion(root, x *blockPosNode) *blockPosNode {
	x.red = true
	for {
		xp := x.parent
		if xp == nil {
			x.red = false
			return x
		}
		xpp := xp.parent
		if !xp.red || xpp == nil {
			return root
		}
		if xp == xpp.left {
			uncle := xpp.right
			if uncle != nil && uncle.red {
				uncle.red, xp.red, xpp.red = false, false, true
				x = xpp
			} else {
				if x == xp.right {
					x = xp
					root = blockPosRotateLeft(root, x)
					xp = x.parent
					xpp = xp.parent
				}
				xp.red = false
				if xpp != nil {
					xpp.red = true
					root = blockPosRotateRight(root, xpp)
				}
			}
		} else {
			uncle := xpp.left
			if uncle != nil && uncle.red {
				uncle.red, xp.red, xpp.red = false, false, true
				x = xpp
			} else {
				if x == xp.left {
					x = xp
					root = blockPosRotateRight(root, x)
					xp = x.parent
					xpp = xp.parent
				}
				xp.red = false
				if xpp != nil {
					xpp.red = true
					root = blockPosRotateLeft(root, xpp)
				}
			}
		}
	}
}

// hashPosHash is Vec3i.hashCode: ((y + 31*z) * 31) + x, in 32-bit overflow.
func hashPosHash(p [3]int32) uint32 {
	return uint32((p[1]+31*p[2])*31 + p[0])
}

// hashPosSpread is HashMap's hash spread: h ^ (h >>> 16), unsigned.
func hashPosSpread(h uint32) uint32 {
	return h ^ h>>16
}

// all lists the positions in iteration order.
func (s *blockPosSet) all() [][3]int32 {
	out := make([][3]int32, 0, s.size)
	for _, head := range s.table {
		for p := head; p != nil; p = p.next {
			out = append(out, p.pos)
		}
	}
	return out
}
