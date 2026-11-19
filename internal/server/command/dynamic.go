package command

import "github.com/golem-mc/golem/internal/protocol"

// LookupSource includes commands owned by this source's server.
func LookupSource(src Source, name string) (Command, bool) {
	tree := src.CommandTree()
	if c, ok := dispatchCommand(tree, name); ok {
		return c, true
	}
	root := FindLiteralChild(tree, name)
	if root == nil {
		return Command{}, false
	}
	run := func(src Source, input string, _ []string) (int32, error) {
		cmd, _ := ParseCommand(tree, input, Tokenize(input), src.PermLevel() >= PermLevelGamemasters)
		if cmd.Run == nil {
			return 0, ErrRefused
		}
		return cmd.Exec(src, input, nil)
	}
	return Command{Name: name, Run: discardResult(run), Result: run}, true
}

// dispatchCommand finds the registry command a root word runs in tree: not
// one a plugin's root took the word from, and for a vanilla root that
// moved to minecraft:<name> the command it moved from.
func dispatchCommand(tree *Node, word string) (Command, bool) {
	if root := FindLiteralChild(tree, word); root != nil {
		if root.Plugin {
			return Command{}, false
		}
		if root.Dispatch != "" {
			return Lookup(root.Dispatch)
		}
	}
	return Lookup(CanonicalName(word))
}

// ParseCommand resolves a command and its arguments from one validated parse.
func ParseCommand(tree *Node, input string, tokens []Token, allowSelectors bool) (Command, *Failure) {
	path, steps, fail := matchPath(tree, input, tokens, allowSelectors)
	if path == nil {
		return Command{}, fail
	}
	name := tokens[0].Text
	if cmd, ok := dispatchCommand(tree, name); ok {
		return cmd, fail
	}
	readPath := steps.argumentNodes()
	if cmd := redirectedCommand(tree, input, tokens, path, readPath); cmd.Run != nil {
		return cmd, fail
	}
	node := path[len(path)-1]
	if node.Handler == nil {
		return Command{}, fail
	}
	values := argumentValues(input, tokens, readPath, path)
	run := func(src Source, _ string, _ []string) (int32, error) {
		return node.Handler(src, input, values)
	}
	return Command{Name: name, Run: discardResult(run), Result: run}, fail
}

// ArgumentValue retains a parsed argument's type and original text.
type ArgumentValue struct {
	Text     string
	Parser   int32
	Quotable bool
	Start    int
	// AnyEntity is set for an entity argument that is not players-only.
	AnyEntity bool
}

// ArgumentValues reads values from the shared parse, preserving spacing.
func ArgumentValues(tree *Node, input string, allowSelectors bool) map[string]ArgumentValue {
	tokens := Tokenize(input)
	path, steps, _ := matchPath(tree, input, tokens, allowSelectors)
	if path == nil {
		path = steps.nodes()
	}
	return argumentValues(input, tokens, steps.argumentNodes(), path)
}

func argumentValues(input string, tokens []Token, readPath, path []*Node) map[string]ArgumentValue {
	values := make(map[string]ArgumentValue)
	idx := 0
	for i, n := range readPath {
		if idx >= len(tokens) {
			break
		}
		if n != path[i] {
			clear(values)
		}
		if n.Type != NodeArgument {
			idx++
			continue
		}
		end := min(idx+ArgumentArity(n, tokens[idx:]), len(tokens))
		stop := TokenEnd(tokens[idx:end])
		if n.Greedy {
			stop = len(input)
		}
		if n != path[i] {
			idx = end
			continue
		}
		values[n.Name] = ArgumentValue{Text: input[tokens[idx].Start:stop], Parser: n.Parser, Quotable: n.Quotable, Start: tokens[idx].Start, AnyEntity: n.EntityArg != nil && !n.EntityArg.PlayersOnly}
		idx = end
	}
	return values
}

// Origin unwraps context views while retaining the command's original sender.
func Origin(src Source) Source {
	for {
		base, ok := src.(interface{ CommandOrigin() Source })
		if !ok {
			return src
		}
		src = base.CommandOrigin()
	}
}

func (s argumentSource) CommandOrigin() Source { return s.Source }

// StringValue decodes a validated Brigadier phrase.
func StringValue(raw string) string { v, _, _ := readStringValue(raw); return v }

// DoubleNode declares Brigadier double bounds once for validation and the wire.
func DoubleNode(n *Node, lo, hi float64) {
	n.Parser = ParserDouble
	n.Properties = func(b *protocol.Buffer) { b.PutU8(3); b.PutF64(lo); b.PutF64(hi) }
	n.Check = func(word string) *ParseError {
		r := coordReader{s: word}
		value, err := r.readDouble()
		if err != nil {
			return err
		}

		if value < lo {
			return &ParseError{Key: "argument.double.low", With: []string{formatJavaDouble(lo), formatJavaDouble(value)}}
		}
		if value > hi {
			return &ParseError{Key: "argument.double.big", With: []string{formatJavaDouble(hi), formatJavaDouble(value)}}
		}
		if r.cur != len(word) {
			return &ParseError{Key: keyExpectedSeparator, Cursor: r.cur}
		}
		return nil
	}
}

// redirectedCommand runs a vanilla destination with the redirect's prefix removed.
func redirectedCommand(tree *Node, input string, tokens []Token, path, readPath []*Node) Command {
	idx := 0
	var found Command
	var rest string
	for i, node := range path {
		n := readPath[i]
		arity := 1
		if n.Type == NodeArgument {
			arity = ArgumentArity(n, tokens[idx:])
		}
		idx = min(idx+arity, len(tokens))
		// A plugin root runs its own handler, not the registry command it displaced.
		if node != n || n.Redirect != nil || n.RedirectName != "" {
			if cmd, ok := Lookup(CanonicalName(node.Name)); ok && !node.Plugin && FindLiteralChild(tree, node.Name) == node {
				found = cmd
				rest = input[TokenEnd(tokens[:idx]):]
				break
			}
		}
	}
	if found.Run == nil {
		return Command{}
	}
	run := func(src Source, _ string, _ []string) (int32, error) {
		line := found.Name + rest
		parsed := Tokenize(line)
		words := wordTexts(parsed)
		return found.Exec(src, line, words[1:])
	}
	return Command{Name: tokens[0].Text, Run: discardResult(run), Result: run}
}
