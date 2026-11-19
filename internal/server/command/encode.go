package command

import (
	"github.com/golem-mc/golem/internal/protocol"
)

// Encode builds the Commands packet payload for the given tree, the way the
// vanilla server does during the join sequence.
func Encode(tree *Node) []byte {
	// Flatten the tree with a breadth-first walk, assigning indices the way
	// the vanilla server does: root first, then children in order.
	var nodes []*Node
	indices := map[*Node]int{}
	queue := []*Node{tree}
	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		if _, ok := indices[node]; ok {
			continue
		}
		indices[node] = len(nodes)
		nodes = append(nodes, node)
		for _, child := range node.Children {
			if _, ok := indices[child]; !ok {
				queue = append(queue, child)
			}
		}
		// A redirect is a reference like a child, and must be declared
		// before it is referred to, so the target joins the queue too.
		if node.Redirect != nil {
			if _, ok := indices[node.Redirect]; !ok {
				queue = append(queue, node.Redirect)
			}
		}
	}

	var b protocol.Buffer
	b.PutVarInt(int32(len(nodes)))
	for _, node := range nodes {
		var flags byte
		flags |= byte(node.Type)
		if node.Executable {
			flags |= flagExecutable
		}
		if node.Redirect != nil {
			flags |= flagHasRedirect
		}
		if node.Suggests != "" {
			flags |= flagHasSuggests
		}
		if node.Required > 0 {
			flags |= flagRestricted
		}
		b.PutU8(flags)
		b.PutVarInt(int32(len(node.Children)))
		for _, child := range node.Children {
			b.PutVarInt(int32(indices[child]))
		}
		if node.Redirect != nil {
			b.PutVarInt(int32(indices[node.Redirect]))
		}
		// Only literal and argument nodes carry a name; the root does not.
		if node.Type != NodeRoot {
			b.PutString(node.Name)
		}
		// Argument nodes carry a parser ID and parser-specific properties.
		if node.Type == NodeArgument {
			b.PutVarInt(node.Parser)
			if node.Parser == ParserInteger {
				// An integer's bounds are declared once, in Int: the walk
				// holds the number it reads to them and the wire
				// properties are written from them here, so a node cannot
				// carry the bounds without the walk's own check.
				var flags byte
				if node.Int != nil {
					if node.Int.HasMin {
						flags |= 0x01
					}
					if node.Int.HasMax {
						flags |= 0x02
					}
				}
				b.PutU8(flags)
				if flags&0x01 != 0 {
					b.PutI32(node.Int.Min)
				}
				if flags&0x02 != 0 {
					b.PutI32(node.Int.Max)
				}
			} else if node.Parser == ParserFloat && node.Float != nil {
				// A float's bounds are declared once, in Float, the
				// integer's arrangement with float properties.
				var flags byte
				if node.Float.HasMin {
					flags |= 0x01
				}
				if node.Float.HasMax {
					flags |= 0x02
				}
				b.PutU8(flags)
				if flags&0x01 != 0 {
					b.PutF32(node.Float.Min)
				}
				if flags&0x02 != 0 {
					b.PutF32(node.Float.Max)
				}
			} else if node.Properties != nil {
				node.Properties(&b)
			}
		}
		// The suggestion type identifier follows, when the flag is set.
		if node.Suggests != "" {
			b.PutString(node.Suggests)
		}
	}
	b.PutVarInt(0) // root index: the root is always first in the BFS order
	return b.Bytes()
}
