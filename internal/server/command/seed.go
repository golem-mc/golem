package command

import (
	"strconv"

	"github.com/golem-mc/golem/internal/protocol"
)

// Seed is the /seed command.
var Seed = Command{
	Name:   "seed",
	Level:  PermLevelGamemasters,
	Build:  seedNode,
	Run:    discardResult(runSeed),
	Result: runSeed,
}

// seedNode builds the /seed command tree, which runs bare.
func seedNode() *Node {
	return &Node{
		Type:       NodeLiteral,
		Name:       "seed",
		Executable: true,
		Required:   PermLevelGamemasters,
	}
}

// runSeed runs /seed, which names the seed the world was generated from.
// A world that has never had one given reads as zero, which is what the
// vanilla server prints for the same. It answers the seed truncated to 32
// bits, what an /execute store writes.
// vanilla: SeedCommand
func runSeed(src Source, input string, args []string) (int32, error) {
	seed := strconv.FormatInt(src.Seed(), 10)
	// vanilla: ComponentUtils.copyOnClickText
	err := src.Feedback("commands.seed.success", protocol.Text{
		Translate: "chat.square_brackets",
		With: []protocol.Text{{
			Text:      seed,
			Color:     protocol.ColorGreen,
			ClickCopy: seed,
			HoverText: &protocol.Text{Translate: "chat.copy.click"},
			Insertion: seed,
		}},
	})
	if err != nil {
		return 0, err
	}
	return int32(src.Seed()), nil
}
