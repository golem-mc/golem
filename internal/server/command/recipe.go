package command

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// Recipe is the /recipe command.
var Recipe = Command{
	Name:   "recipe",
	Level:  PermLevelGamemasters,
	Build:  recipeNode,
	Run:    discardResult(runRecipe),
	Result: runRecipe,
}

// RecipeTarget is a player whose recipe book /recipe changes. Each method
// returns how many of the named recipes it added or removed.
// vanilla: ServerPlayer.awardRecipes, resetRecipes
type RecipeTarget interface {
	GiveRecipes(names []string) int
	TakeRecipes(names []string) int
}

const recipeRegistry = "minecraft:recipe"

// recipeNode builds /recipe (give|take) <targets> (<recipe>|*). The literal
// sorts before the argument, as brigadier orders a node's children.
// vanilla: RecipeCommand.register
func recipeNode() *Node {
	branch := func(name string) *Node {
		return &Node{
			Type: NodeLiteral,
			Name: name,
			Children: []*Node{{
				Type:       NodeArgument,
				Name:       "targets",
				Parser:     ParserEntity,
				Properties: func(b *protocol.Buffer) { b.PutU8(0x02) }, // players, several
				EntityArg:  &EntityArg{PlayersOnly: true},
				Suggests:   AskServerSuggestion,
				Completions: func(ctx any, partial string) (int, []string) {
					return EntityCompletions(ctx, partial)
				},
				Children: []*Node{
					{Type: NodeLiteral, Name: "*", Executable: true},
					{
						Type:       NodeArgument,
						Name:       "recipe",
						Executable: true,
						Parser:     ParserResourceKey,
						Properties: func(b *protocol.Buffer) { b.PutString(recipeRegistry) },
					},
				},
			}},
		}
	}
	return &Node{
		Type:     NodeLiteral,
		Name:     "recipe",
		Required: PermLevelGamemasters,
		Children: []*Node{branch("give"), branch("take")},
	}
}

// runRecipe runs /recipe give|take, returning the targets' total added or
// removed; feedback counts the recipes asked for.
// vanilla: RecipeCommand.giveRecipes, takeRecipes
func runRecipe(src Source, input string, args []string) (int32, error) {
	if len(args) < 2 {
		return 0, nil
	}
	give := args[0] == "give"
	word, rest := splitTargets(input, args[1:])
	if len(rest) != 1 {
		return 0, nil
	}
	players, err := resolvePlayers(src, word)
	if err != nil {
		return 0, err
	}
	var names []string
	if rest[0] == "*" {
		for _, display := range registry.CraftingDisplaysNow() {
			if len(names) == 0 || names[len(names)-1] != display.Name {
				names = append(names, display.Name)
			}
		}
		for _, display := range registry.FurnaceDisplaysNow() {
			names = append(names, display.Name)
		}
		for _, display := range registry.StonecutterDisplaysNow() {
			names = append(names, display.Name)
		}
		for _, display := range registry.SmithingDisplaysNow() {
			names = append(names, display.Name)
		}
	} else {
		name, ok := parseResourceLocation(rest[0])
		if !ok {
			return 0, nil
		}
		if _, ok := registry.RecipeDisplayIDOfName(name); !ok {
			return 0, refuse(src, "recipe.notFound", protocol.Text{Text: name})
		}
		names = []string{name}
	}
	var tracker responseTracker[Target]
	for _, player := range players {
		if target, ok := player.(RecipeTarget); ok {
			if give {
				tracker.track(player, target.GiveRecipes(names))
			} else {
				tracker.track(player, target.TakeRecipes(names))
			}
		}
	}
	key := "commands.recipe.take"
	if give {
		key = "commands.recipe.give"
	}
	return tracker.feedback(src, key+".failed", key+".success.", Target.DisplayName,
		func(who protocol.Text, total int) []protocol.Text { return []protocol.Text{entityCount(total), who} })
}
