package command

import "testing"

func TestFindLiteralChildIndexKeepsFirstMatch(t *testing.T) {
	first := &Node{Type: NodeLiteral, Name: "a"}
	root := &Node{Type: NodeRoot, Children: []*Node{
		{Type: NodeArgument, Name: "a"}, first, {Type: NodeLiteral, Name: "a"}, {Type: NodeLiteral, Name: "b"},
	}}
	indexLiterals(root)
	if got := FindLiteralChild(root, "a"); got != first {
		t.Fatalf("a = %p, want first literal", got)
	}
	if FindLiteralChild(root, "c") != nil {
		t.Fatal("unknown word found")
	}
}
