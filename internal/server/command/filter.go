package command

// FilterRoots copies the graph, retaining only usable root children.
// Redirect targets remain available to aliases even when their root name is hidden.
func FilterRoots(root *Node, usable func(*Node) bool) *Node {
	return copyGraph(root, nil, usable, nil)
}

// Extend copies a tree with additional roots, filtering nodes and remapping redirects.
func Extend(root *Node, extra []*Node, usable func(*Node) bool) *Node {
	return copyGraph(root, extra, usable, usable)
}

func copyGraph(root *Node, extra []*Node, roots, descendants func(*Node) bool) *Node {
	converted := make(map[*Node]*Node)
	// An overriding root displaces the vanilla root of its name, which
	// stays reachable under minecraft:<name>, with its aliases following it.
	replaced := make(map[*Node]*Node)
	for _, e := range extra {
		if v := FindLiteralChild(root, e.Name); e.Override && v != nil {
			moved := *v
			moved.Name, moved.Dispatch = "minecraft:"+v.Name, CanonicalName(v.Name)
			replaced[v] = &moved
		}
	}
	var named map[string]*Node
	lookup := func(name string) *Node {
		if named == nil {
			named = make(map[string]*Node, len(root.Children)+len(extra))
			for _, child := range root.Children {
				named[child.Name] = child
			}
			for _, child := range extra {
				named[child.Name] = child
			}
		}
		return named[name]
	}

	var present func(*Node) bool
	present = func(n *Node) bool {
		if descendants == nil {
			return true
		}
		if n.Redirect != nil {
			return present(n.Redirect)
		}
		if n.RedirectName == "" {
			return true
		}
		target := lookup(n.RedirectName)
		// Explicit redirects gate destinations; ordinary aliases share their root's gate.
		return target != nil && descendants(target) && present(target)
	}

	var copyNode func(*Node) *Node
	copyNode = func(n *Node) *Node {
		if c := converted[n]; c != nil {
			return c
		}
		c := *n
		converted[n] = &c
		c.Children, c.literals, c.dispatch, c.dispatched = nil, nil, nil, false
		for _, child := range n.Children {
			if moved := replaced[child]; moved != nil {
				child = moved
			}
			if present(child) && ((n == root && roots(child)) || (n != root && (descendants == nil || descendants(child)))) {
				c.Children = append(c.Children, copyNode(child))
			}
		}
		if n == root {
			for _, child := range extra {
				if present(child) && roots(child) {
					c.Children = append(c.Children, copyNode(child))
				}
			}
		}
		target := n.Redirect
		if target == nil && n.RedirectName != "" {
			target = lookup(n.RedirectName)
		}
		if moved := replaced[target]; moved != nil {
			target = moved
		}
		if target != nil {
			c.Redirect = copyNode(target)
		}
		return &c
	}
	out := copyNode(root)
	indexLiterals(out)
	return out
}
