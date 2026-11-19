package command

import (
	"strings"

	"github.com/golem-mc/golem/internal/protocol"
)

// Difficulty is the /difficulty command.
var Difficulty = Command{
	Name:   "difficulty",
	Level:  PermLevelGamemasters,
	Build:  difficultyNode,
	Run:    discardResult(runDifficulty),
	Result: runDifficulty,
}

// difficultyNames maps the word /difficulty takes onto the number the
// level.dat and the Change Difficulty packet carry, in the order the vanilla
// command's literals are registered.
var difficultyNames = []string{"peaceful", "easy", "normal", "hard"}

// difficultyNameText is a difficulty's options.difficulty.* translation, so
// the client renders it in its own language. vanilla: Difficulty.getDisplayName
func difficultyNameText(d uint8) protocol.Text {
	if int(d) < len(difficultyNames) {
		return protocol.Text{Translate: "options.difficulty." + difficultyNames[d]}
	}
	return protocol.Text{}
}

// parseDifficultyArg reads one word as a difficulty, names only, and
// case-insensitively the way the vanilla server's argument does.
func parseDifficultyArg(word string) (uint8, bool) {
	for d, name := range difficultyNames {
		if strings.EqualFold(name, word) {
			return uint8(d), true
		}
	}
	return 0, false
}

// difficultyNode builds the /difficulty command tree, matching the vanilla
// report: the command runs bare, naming the current difficulty, and
// takes any one of the four difficulties as a literal to change to.
func difficultyNode() *Node {
	node := &Node{
		Type:       NodeLiteral,
		Name:       "difficulty",
		Executable: true,
		Required:   PermLevelGamemasters,
	}
	for _, name := range difficultyNames {
		node.Children = append(node.Children, &Node{
			Type:       NodeLiteral,
			Name:       name,
			Executable: true,
		})
	}
	return node
}

// runDifficulty runs /difficulty, which names the world's difficulty when
// it runs bare and sets it - for everyone in the world, not only the
// runner - when it names one. The change is persisted in the world's
// level.dat by the next save, so it survives a restart. A query answers the
// difficulty's id, a change 0.
// vanilla: DifficultyCommand
func runDifficulty(src Source, input string, args []string) (int32, error) {
	if len(args) > 0 {
		d, ok := parseDifficultyArg(args[0])
		if !ok {
			src.Debug("unknown difficulty", "difficulty", args[0])
			return 0, nil
		}
		if d == src.Difficulty() {
			return 0, refuse(src, "commands.difficulty.failure", difficultyNameText(d))
		}
		src.SetDifficulty(d)
		// The feedback goes out before the broadcast, so that a reader of
		// the runner's stream sees the answer before the change it describes.
		if err := feedbackToAdmins(src, "commands.difficulty.success", difficultyNameText(d)); err != nil {
			return 0, err
		}
		src.BroadcastDifficulty()
		src.Info("difficulty set", "difficulty", difficultyNames[src.Difficulty()], "by", src.Name())
		return 0, nil
	}
	return resultOf(int32(src.Difficulty()), src.Feedback("commands.difficulty.query", difficultyNameText(src.Difficulty())))
}
