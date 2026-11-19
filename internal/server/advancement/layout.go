package advancement

import "slices"

// position is a displayed node's Buchheim-Walker layout state; undisplayed
// nodes' displayed descendants count as children of their nearest displayed
// ancestor.
// vanilla: TreeNodePosition
type position struct {
	node            *Node
	parent          *position
	previousSibling *position
	// childIndex counts from 1.
	childIndex int
	children   []*position
	ancestor   *position
	thread     *position
	x          int
	y          float32
	mod        float32
	change     float32
	shift      float32
}

func newPosition(node *Node, parent, previous *position, childIndex, depth int) *position {
	p := &position{node: node, parent: parent, previousSibling: previous, childIndex: childIndex, x: depth, y: -1}
	p.ancestor = p
	var last *position
	for _, child := range node.Children {
		last = p.addChild(child, last)
	}
	return p
}

func (p *position) addChild(node *Node, previous *position) *position {
	if node.Display != nil {
		previous = newPosition(node, p, previous, len(p.children)+1, p.x+1)
		p.children = append(p.children, previous)
		return previous
	}
	for _, grandchild := range node.Children {
		previous = p.addChild(grandchild, previous)
	}
	return previous
}

func (p *position) firstWalk() {
	if len(p.children) == 0 {
		if p.previousSibling != nil {
			p.y = p.previousSibling.y + 1
		} else {
			p.y = 0
		}
		return
	}
	var defaultAncestor *position
	for _, child := range p.children {
		child.firstWalk()
		if defaultAncestor == nil {
			defaultAncestor = child
		}
		defaultAncestor = child.apportion(defaultAncestor)
	}
	p.executeShifts()
	midpoint := (p.children[0].y + p.children[len(p.children)-1].y) / 2
	if p.previousSibling != nil {
		p.y = p.previousSibling.y + 1
		p.mod = p.y - midpoint
	} else {
		p.y = midpoint
	}
}

func (p *position) secondWalk(modSum float32, depth int, least float32) float32 {
	p.y += modSum
	p.x = depth
	if p.y < least {
		least = p.y
	}
	for _, child := range p.children {
		least = child.secondWalk(modSum+p.mod, depth+1, least)
	}
	return least
}

func (p *position) thirdWalk(offset float32) {
	p.y += offset
	for _, child := range p.children {
		child.thirdWalk(offset)
	}
}

func (p *position) executeShifts() {
	var shift, change float32
	for _, child := range slices.Backward(p.children) {

		child.y += shift
		child.mod += shift
		change += child.change
		shift += child.shift + change
	}
}

func (p *position) previousOrThread() *position {
	if p.thread != nil {
		return p.thread
	}
	if len(p.children) > 0 {
		return p.children[0]
	}
	return nil
}

func (p *position) nextOrThread() *position {
	if p.thread != nil {
		return p.thread
	}
	if len(p.children) > 0 {
		return p.children[len(p.children)-1]
	}
	return nil
}

func (p *position) apportion(defaultAncestor *position) *position {
	if p.previousSibling == nil {
		return defaultAncestor
	}
	vir, vor := p, p
	vil := p.previousSibling
	vol := p.parent.children[0]
	sir, sor := p.mod, p.mod
	sil, sol := vil.mod, vol.mod
	for vil.nextOrThread() != nil && vir.previousOrThread() != nil {
		vil = vil.nextOrThread()
		vir = vir.previousOrThread()
		vol = vol.previousOrThread()
		vor = vor.nextOrThread()
		vor.ancestor = p
		shift := vil.y + sil - (vir.y + sir) + 1
		if shift > 0 {
			vil.getAncestor(p, defaultAncestor).moveSubtree(p, shift)
			sir += shift
			sor += shift
		}
		sil += vil.mod
		sir += vir.mod
		sol += vol.mod
		sor += vor.mod
	}
	if vil.nextOrThread() != nil && vor.nextOrThread() == nil {
		vor.thread = vil.nextOrThread()
		vor.mod += sil - sor
		return defaultAncestor
	}
	if vir.previousOrThread() != nil && vol.previousOrThread() == nil {
		vol.thread = vir.previousOrThread()
		vol.mod += sir - sol
	}
	return p
}

func (p *position) moveSubtree(right *position, shift float32) {
	subtrees := float32(right.childIndex - p.childIndex)
	if subtrees != 0 {
		right.change -= shift / subtrees
		p.change += shift / subtrees
	}
	right.shift += shift
	right.y += shift
	right.mod += shift
}

func (p *position) getAncestor(other, defaultAncestor *position) *position {
	if slices.Contains(other.parent.children, p.ancestor) {
		return p.ancestor
	}
	return defaultAncestor
}

func (p *position) finalize() {
	p.node.X, p.node.Y = float32(p.x), p.y
	for _, child := range p.children {
		child.finalize()
	}
}

// layout positions a displayed root's tree. vanilla: TreeNodePosition.run
func layout(root *Node) {
	p := newPosition(root, nil, nil, 1, 0)
	p.firstWalk()
	if least := p.secondWalk(0, 0, p.y); least < 0 {
		p.thirdWalk(-least)
	}
	p.finalize()
}
