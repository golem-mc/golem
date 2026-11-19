package command

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
)

// Spawnpoint is the /spawnpoint command.
var Spawnpoint = Command{
	Name:   "spawnpoint",
	Level:  PermLevelGamemasters,
	Build:  spawnpointNode,
	Run:    discardResult(runSpawnpoint),
	Result: runSpawnpoint,
}

// spawnpointNode builds the /spawnpoint command tree, matching the
// vanilla report: optional player targets, then an optional position
// for it, then an optional rotation to arrive facing.
func spawnpointNode() *Node {
	targetsArg := &Node{
		Type:       NodeArgument,
		Name:       "targets",
		Executable: true,
		Parser:     ParserEntity,
		Properties: func(b *protocol.Buffer) { b.PutU8(0x02) }, // players, several
		EntityArg:  &EntityArg{PlayersOnly: true},
		Suggests:   AskServerSuggestion,
		Completions: func(ctx any, partial string) (int, []string) {
			return EntityCompletions(ctx, partial)
		},
	}
	posArg := &Node{
		Type:       NodeArgument,
		Name:       "pos",
		Executable: true,
		Parser:     ParserBlockPos,
	}
	rotationArg := &Node{
		Type:       NodeArgument,
		Name:       "rotation",
		Executable: true,
		Parser:     ParserRotation,
	}
	posArg.Children = []*Node{rotationArg}
	targetsArg.Children = []*Node{posArg}
	return &Node{
		Type:       NodeLiteral,
		Name:       "spawnpoint",
		Executable: true,
		Required:   PermLevelGamemasters,
		Children:   []*Node{targetsArg},
	}
}

// runSpawnpoint runs /spawnpoint [targets] [pos] [rotation], which sets
// where the named players - or the body the runner's @s names - come back
// when they die; the spawn is saved with them and used by their respawn.
func runSpawnpoint(src Source, input string, args []string) (int32, error) {
	word, rest := "", args
	if len(args) > 0 && !isCoordish(args[0]) {
		word, rest = splitTargets(input, args)
	}
	targets, err := resolvePlayersOrSelf(src, word)
	if err != nil {
		return 0, err
	}

	// The position is where the runner stands unless the command names one.
	// A rotation after it is folded the way the vanilla server folds the one
	// its feedback carries, and kept with the point.
	pos := world.SpawnPos{
		X: rules.BlockOf(src.Position().X),
		Y: rules.BlockOf(src.Position().Y),
		Z: rules.BlockOf(src.Position().Z),
	}
	yaw, pitch := float32(0), float32(0)
	if len(rest) > 0 {
		x, y, z, ok := resolveBlockPos(src, rest)
		if !ok {
			src.Debug("spawnpoint position did not parse", "args", rest)
			return 0, nil
		}
		// The block_pos argument is a spawnable one, which the vanilla
		// server reads through getSpawnablePos.
		if !inSpawnableBounds(float64(x), float64(y), float64(z)) {
			src.Debug("spawnpoint position is out of bounds", "x", x, "y", y, "z", z)
			return 0, refuse(src, keyPosOutOfBounds)
		}
		at := src.Position()
		pos = world.SpawnPos{X: x, Y: y, Z: z}
		if r := rest[3:]; len(r) > 0 {
			ry, rp, ok := parseRotation(r)
			if !ok {
				src.Debug("spawnpoint rotation did not parse", "args", r)
				return 0, nil
			}
			yaw = WrapDegrees(float32(ry.resolve(float64(at.Yaw))))
			pitch = ClampPitch(float32(rp.resolve(float64(at.Pitch))))
		}
	}

	for _, target := range targets {
		target.SetSpawnPoint(pos, yaw, pitch, src.DimensionName())
	}

	// The vanilla feedback reads where the spawn point is, which way it
	// faces, in which dimension, and who it was set for.
	key := "commands.spawnpoint.success.single"
	with := []protocol.Text{
		entityCount(int(pos.X)),
		entityCount(int(pos.Y)),
		entityCount(int(pos.Z)),
		entityFraction(yaw),
		entityFraction(pitch),
		{Text: src.DimensionName()},
		formatNames(targets),
	}
	if len(targets) > 1 {
		key = "commands.spawnpoint.success.multiple"
		with[len(with)-1] = protocol.Text{Text: formatInt(int64(len(targets)))}
	}
	return resultOf(int32(len(targets)), feedbackToAdmins(src, key, with...))
}

// WrapDegrees folds an angle into [-180, 180).
func WrapDegrees(v float32) float32 {
	return world.WrapDegrees(v)
}

// ClampPitch keeps a pitch between looking straight down and straight up,
// which is the range a spawn point's pitch can hold.
func ClampPitch(v float32) float32 {
	return min(max(v, -90), 90)
}
