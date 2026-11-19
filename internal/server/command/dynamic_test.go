package command

import "testing"

func TestParseCommandRetainsWinningArguments(t *testing.T) {
	checks, runs := 0, 0
	arg := &Node{Type: NodeArgument, Name: "value", Parser: ParserString, Quotable: true, Executable: true,
		Check: func(string) *ParseError { checks++; return nil },
		Handler: func(_ Source, input string, values map[string]ArgumentValue) (int32, error) {
			runs++
			if got := values["value"]; got.Text != `"two  words"` || got.Parser != ParserString || !got.Quotable {
				t.Fatalf("parsed value=%+v from %q", got, input)
			}
			return 7, nil
		},
	}
	root := &Node{Type: NodeLiteral, Name: "custom", Children: []*Node{arg}}
	tree := &Node{Type: NodeRoot, Children: []*Node{root, {Type: NodeLiteral, Name: "alias", Redirect: root}}}
	for _, name := range []string{"custom", "alias"} {
		input := name + ` "two  words"`
		cmd, fail := ParseCommand(tree, input, Tokenize(input), true)
		if cmd.Run == nil || fail.Err != nil {
			t.Fatalf("parse=%+v, %+v", cmd, fail)
		}
		if result, err := cmd.Exec(nil, input, nil); result != 7 || err != nil {
			t.Fatalf("dispatch=%d, %v", result, err)
		}
	}
	if checks != 2 || runs != 2 {
		t.Fatalf("checks=%d runs=%d, want one each per invocation", checks, runs)
	}
}
