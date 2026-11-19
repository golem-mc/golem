package command

import (
	"bytes"
	"testing"
)

// TestRecipeTree pins the shape vanilla registers: give and take, each with
// a players argument whose children are the "*" literal and a resource_key
// argument over the recipe registry.
func TestRecipeTree(t *testing.T) {
	node := recipeNode()
	if node.Required != PermLevelGamemasters || len(node.Children) != 2 ||
		node.Children[0].Name != "give" || node.Children[1].Name != "take" {
		t.Fatalf("recipe tree root = %+v", node)
	}
	for _, branch := range node.Children {
		targets := branch.Children[0]
		if targets.Name != "targets" || targets.Executable || len(targets.Children) != 2 {
			t.Fatalf("%s targets = %+v", branch.Name, targets)
		}
		star, recipe := targets.Children[0], targets.Children[1]
		if star.Type != NodeLiteral || star.Name != "*" || !star.Executable {
			t.Errorf("%s star = %+v", branch.Name, star)
		}
		if recipe.Type != NodeArgument || recipe.Name != "recipe" || !recipe.Executable ||
			recipe.Parser != ParserResourceKey || recipe.Suggests != "" {
			t.Errorf("%s recipe = %+v", branch.Name, recipe)
		}
	}
	wire := Encode(&Node{Type: NodeRoot, Children: []*Node{node}})
	want := append([]byte{ParserResourceKey, byte(len(recipeRegistry))}, recipeRegistry...)
	if !bytes.Contains(wire, want) {
		t.Errorf("the encoded tree does not carry the resource_key parser over %s", recipeRegistry)
	}
}
