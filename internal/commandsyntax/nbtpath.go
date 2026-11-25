package commandsyntax

import (
	"strconv"

	"github.com/golem-mc/golem/nbt"
)

const pathNodeInvalidKey = "arguments.nbtpath.node.invalid"

// NBTPath retains a path's original spelling and the end of each typed node.
type NBTPath struct {
	Original string
	Nodes    []PathNode
	Ends     []int
}

// PathNode is one parsed NBT path operation, without execution behavior.
type PathNode interface{ pathNode() }

// CompoundChildNode names a compound member.
type CompoundChildNode struct{ Name string }

// IndexedElementNode indexes a list or array.
type IndexedElementNode struct{ Index int }

// AllElementsNode selects every list or array element.
type AllElementsNode struct{}

// MatchElementNode matches list elements against a compound.
type MatchElementNode struct{ Pattern nbt.CompoundTag }

// MatchObjectNode names a compound member with matching contents.
type MatchObjectNode struct {
	Name    string
	Pattern nbt.CompoundTag
}

// MatchRootObjectNode matches the path's root compound.
type MatchRootObjectNode struct{ Pattern nbt.CompoundTag }

func (CompoundChildNode) pathNode()   {}
func (IndexedElementNode) pathNode()  {}
func (AllElementsNode) pathNode()     {}
func (MatchElementNode) pathNode()    {}
func (MatchObjectNode) pathNode()     {}
func (MatchRootObjectNode) pathNode() {}

// ParseNBTPath reads typed nodes and the offsets used by path-operation errors.
// vanilla: NbtPathArgument$NbtPath.of
func ParseNBTPath(text string) (NBTPath, *Error) {
	p := &nbtPathParser{src: text}
	var path NBTPath
	for first := true; p.canRead(); first = false {
		node, err := p.parseNode(first)
		if err != nil {
			return path, err
		}
		path.Nodes = append(path.Nodes, node)
		path.Original, path.Ends = text, append(path.Ends, p.pos)
		switch {
		case !p.canRead():
		case p.peek() == '[' || p.peek() == '{':
			// A bracket opens the next element on its own, the way a
			// key's member or comparator follows without a separator.
		case p.peek() == '.':
			p.pos++
		default:
			return path, &Error{Text: "expected .", Cursor: p.pos}
		}
	}
	if len(path.Nodes) == 0 {
		return path, &Error{Key: pathNodeInvalidKey, Cursor: 0}
	}
	return path, nil
}

type nbtPathParser struct {
	src string
	pos int
}

func (p *nbtPathParser) canRead() bool { return p.pos < len(p.src) }

func (p *nbtPathParser) peek() byte { return p.src[p.pos] }

func (p *nbtPathParser) at(c byte) bool { return p.canRead() && p.src[p.pos] == c }

func (p *nbtPathParser) invalidNode() *Error {
	return &Error{Key: pathNodeInvalidKey, Cursor: p.pos}
}

// vanilla: NbtPathArgument.parseNode
func (p *nbtPathParser) parseNode(first bool) (PathNode, *Error) {
	switch c := p.peek(); c {
	case '"', '\'':
		name, err := p.readQuotedName()
		if err != nil {
			return nil, err
		}
		return p.readObjectNode(name)
	case '[':
		p.pos++
		if p.at(']') {
			p.pos++
			return AllElementsNode{}, nil
		}
		return p.parseSelectorNode()
	case '{':
		if !first {
			return nil, p.invalidNode()
		}
		pattern, err := p.readMatcherCompound()
		if err != nil {
			return nil, err
		}
		return MatchRootObjectNode{Pattern: pattern}, nil
	default:
		name := p.readUnquotedName()
		return p.readObjectNode(name)
	}
}

func (p *nbtPathParser) parseSelectorNode() (PathNode, *Error) {
	if p.at('{') {
		pattern, err := p.readMatcherCompound()
		if err != nil {
			return nil, err
		}
		if !p.at(']') {
			return nil, &Error{Text: "expected ]", Cursor: p.pos}
		}
		p.pos++
		return MatchElementNode{Pattern: pattern}, nil
	}
	index, err := p.readIndex()
	if err != nil {
		return nil, err
	}
	if !p.at(']') {
		return nil, &Error{Text: "expected ]", Cursor: p.pos}
	}
	p.pos++
	return IndexedElementNode{Index: index}, nil
}

// vanilla: NbtPathArgument.readObjectNode
func (p *nbtPathParser) readObjectNode(name string) (PathNode, *Error) {
	if name == "" {
		return nil, p.invalidNode()
	}
	if !p.at('{') {
		return CompoundChildNode{Name: name}, nil
	}
	pattern, err := p.readMatcherCompound()
	if err != nil {
		return nil, err
	}
	return MatchObjectNode{Name: name, Pattern: pattern}, nil
}

// vanilla: TagParser.parseCompoundAsArgument
func (p *nbtPathParser) readMatcherCompound() (nbt.CompoundTag, *Error) {
	value, n, err := nbt.ParseOrderedValueTag(p.src[p.pos:])
	if err != nil {
		return nil, &Error{Key: pathNodeInvalidKey, Cursor: p.pos + n}
	}
	compound, ok := value.(nbt.CompoundTag)
	if !ok {
		return nil, &Error{Key: pathNodeInvalidKey, Cursor: p.pos + n}
	}
	p.pos += n
	return compound, nil
}

func (p *nbtPathParser) readQuotedName() (string, *Error) {
	value, n, err := nbt.ParseValueTag(p.src[p.pos:])
	text, ok := value.(nbt.String)
	if err != nil || !ok {
		return "", p.invalidNode()
	}
	p.pos += n
	return string(text), nil
}

// readUnquotedName stops at path separators, quotes, brackets and braces.
// vanilla: NbtPathArgument.readUnquotedName
func (p *nbtPathParser) readUnquotedName() string {
	start := p.pos
	for p.canRead() {
		switch p.src[p.pos] {
		case ' ', '"', '\'', '[', ']', '.', '{', '}':
			return p.src[start:p.pos]
		}
		p.pos++
	}
	return p.src[start:p.pos]
}

// vanilla: StringReader.readInt
func (p *nbtPathParser) readIndex() (int, *Error) {
	start := p.pos
	for p.canRead() {
		c := p.peek()
		if (c < '0' || c > '9') && c != '.' && c != '-' {
			break
		}
		p.pos++
	}
	number := p.src[start:p.pos]
	if number == "" {
		return 0, &Error{Key: "parsing.int.expected", Cursor: start}
	}
	v, err := strconv.ParseInt(number, 10, 32)
	if err != nil {
		p.pos = start
		return 0, &Error{Key: "parsing.int.invalid", With: []string{number}, Cursor: start}
	}
	return int(v), nil
}
