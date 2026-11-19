package commands

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/servertest"
)

// TestExperienceSetPointsNamesOnlyThePlayerItSet sets points past what Bob's
// level 0 holds but within Alice's level 30: the feedback names Alice alone
// and the result counts only her.
// vanilla: ExperienceCommand.setExperience, CommandResponseTracker.sendFeedback
func TestExperienceSetPointsNamesOnlyThePlayerItSet(t *testing.T) {
	t.Parallel()
	srv, c := joinOpForStore(t, "Bob")
	c.ChatCommand("experience set Alice 30 levels")
	c.ChatCommand("experience set @a 50 points")
	servertest.AwaitCommandFeedbackText(t, c, "commands.experience.set.points.success.single",
		servertest.NumberText(50), servertest.PlayerDisplayComponent("Alice"))
	expectStored(t, srv, c, [][2]string{{"experience set @a 50 points", "1"}})
}

// TestRecipeGiveCountsOnlyNewRecipes gives a recipe Bob already knows to
// both players: only Alice learns it, so the feedback names her with the one
// recipe given and the result is 1.
// vanilla: RecipeCommand.giveRecipes, CommandResponseTracker.sendFeedback
func TestRecipeGiveCountsOnlyNewRecipes(t *testing.T) {
	t.Parallel()
	srv, c := joinOpForStore(t, "Bob")
	c.ChatCommand("recipe give Bob minecraft:oak_boat")
	c.ChatCommand("recipe give @a minecraft:oak_boat")
	servertest.AwaitCommandFeedbackText(t, c, "commands.recipe.give.success.single",
		servertest.NumberText(1), servertest.PlayerDisplayComponent("Alice"))
	c.ChatCommand("recipe take Alice minecraft:oak_boat")
	expectStored(t, srv, c, [][2]string{{"recipe give @a minecraft:oak_boat", "1"}})
}
