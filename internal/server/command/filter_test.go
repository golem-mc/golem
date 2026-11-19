package command

import "testing"

func TestCopyGraphFiltersAndRemapsRedirects(t *testing.T) {
	root := &Node{Type: NodeRoot}
	hidden := &Node{Name: "hidden", Children: []*Node{{Name: "denied"}}}
	keep := &Node{Name: "keep", Children: []*Node{{Name: "denied"}, {Name: "run", Redirect: root}}}
	alias := &Node{Name: "alias", Redirect: hidden}
	root.Children = []*Node{keep, hidden, alias}
	extra := &Node{Name: "extra", Children: []*Node{{Name: "denied"}, {Name: "allowed"}}}
	usable := func(n *Node) bool { return n.Name != "hidden" && n.Name != "denied" }
	for _, extend := range []bool{false, true} {
		var copied *Node
		if extend {
			copied = Extend(root, []*Node{extra, {Name: "extra_alias", Redirect: extra}}, usable)
		} else {
			copied = FilterRoots(root, usable)
		}
		if copied == root || copied.Children[0] == keep || copied.Children[1] == alias {
			t.Fatal("copier retained original nodes")
		}
		if got := copied.Children[1].Redirect; got == hidden || got == nil || got.Name != "hidden" {
			t.Fatalf("hidden redirect target=%v", got)
		}
		children := copied.Children[0].Children
		if extend {
			if len(children) != 1 || len(copied.Children[1].Redirect.Children) != 0 {
				t.Fatal("Extend did not filter descendants")
			}
			if len(copied.Children) != 4 || len(copied.Children[2].Children) != 1 || copied.Children[3].Redirect != copied.Children[2] {
				t.Fatal("additional roots or their redirects were not copied")
			}
		} else if len(children) != 2 || len(copied.Children[1].Redirect.Children) != 1 {
			t.Fatal("FilterRoots filtered descendants")
		}
		if children[len(children)-1].Redirect != copied {
			t.Fatal("root redirect does not point into copied graph")
		}
	}
	if len(root.Children) != 3 || len(keep.Children) != 2 || len(extra.Children) != 2 {
		t.Fatal("copier mutated original graph")
	}
}
