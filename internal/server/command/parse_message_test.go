package command

import (
	"reflect"
	"testing"
)

func TestMatchPathMessageRanges(t *testing.T) {
	tree := ForLevel(PermLevelGamemasters)
	for _, test := range []struct {
		input, name, value string
	}{
		{"say héllo  世界  ", "message", "héllo  世界  "},
		{"say   héllo  世界 ", "message", "  héllo  世界 "},
		{"say   ", "message", "  "},
		{"me waves  at @a", "action", "waves  at @a"},
		{"msg @a hello  @s  ", "message", "hello  @s  "},
		{"tell Player café", "message", "café"},
		{"w Player  世界", "message", " 世界"},
		{"teammsg hi  team", "message", "hi  team"},
		{"tm hi team", "message", "hi team"},
		{"execute as @a run say hi  @s ", "message", "hi  @s "},
		{"execute as @a run execute at @s run w Player héllo  世界", "message", "héllo  世界"},
		{"execute positioned 1 2 3 run me waves", "action", "waves"},
	} {
		t.Run(test.input, func(t *testing.T) {
			tokens := Tokenize(test.input)
			path, arguments, fail := MatchPathArguments(tree, test.input, tokens, true)
			if path == nil {
				t.Fatalf("parse failed: %+v", fail)
			}
			start := len(test.input) - len(test.value)
			want := []MessageArgumentRange{{Name: test.name, Start: start, End: len(test.input)}}
			if !reflect.DeepEqual(arguments, want) {
				t.Fatalf("ranges = %+v, want %+v", arguments, want)
			}
			if got := test.input[arguments[0].Start:arguments[0].End]; got != test.value {
				t.Fatalf("signed text = %q, want %q", got, test.value)
			}
			oldPath, oldFail := MatchPath(tree, test.input, tokens, true)
			if !reflect.DeepEqual(path, oldPath) || !reflect.DeepEqual(fail, oldFail) {
				t.Fatal("MatchPath results differ")
			}
		})
	}
	for _, input := range []string{"list", "say", "say @e[bad=1]", "unknown hi", "", "execute run list"} {
		_, arguments, _ := MatchPathArguments(tree, input, Tokenize(input), true)
		if len(arguments) != 0 {
			t.Fatalf("%q has message ranges: %+v", input, arguments)
		}
	}
}

func TestSignableArgumentsRootRedirect(t *testing.T) {
	tree := ForLevel(PermLevelGamemasters)
	for _, input := range []string{"say héllo  世界 ", "w Player hi", "tm team", "me waves"} {
		_, all, _ := MatchPathArguments(tree, input, Tokenize(input), true)
		signed, _ := SignableArguments(tree, input, Tokenize(input), true)
		if len(all) != 1 || !reflect.DeepEqual(all, signed) {
			t.Fatalf("%q: signed=%+v all=%+v", input, signed, all)
		}
	}
	for _, input := range []string{
		"execute run say hello", "execute as @a run w Player héllo",
		"execute run execute run me waves", "list", "unknown",
	} {
		signed, _ := SignableArguments(tree, input, Tokenize(input), true)
		if len(signed) != 0 {
			t.Fatalf("%q: root redirect leaked signatures: %+v", input, signed)
		}
	}

	// A non-root redirect contributes its own arguments and retains a parsed
	// message on the redirecting node. Root redirects exclude only the child
	// context, preserving any arguments parsed before that boundary.
	message := &Node{Type: NodeArgument, Name: "before", Parser: ParserMessage}
	root := &Node{Children: []*Node{message}}
	message.Redirect = &Node{Children: []*Node{{Type: NodeArgument, Name: "after", Parser: ParserMessage, Greedy: true, Executable: true}}}
	input := "hi héllo  世界 "
	signed, _ := SignableArguments(root, input, Tokenize(input), true)
	if !reflect.DeepEqual(signed, []MessageArgumentRange{{"before", 0, 2}, {"after", 3, len(input)}}) {
		t.Fatalf("non-root redirect lost arguments: %+v", signed)
	}
	root.Children = append(root.Children, &Node{Type: NodeLiteral, Name: "run", Redirect: root})
	message.Redirect = root
	input = "hi run hi"
	signed, _ = SignableArguments(root, input, Tokenize(input), true)
	if !reflect.DeepEqual(signed, []MessageArgumentRange{{"before", 0, 2}}) {
		t.Fatalf("root redirect changed parent context: %+v", signed)
	}
}
