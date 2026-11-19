package command

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
)

// Setworldspawn is the /setworldspawn command.
var Setworldspawn = Command{
	Name:  "setworldspawn",
	Level: PermLevelGamemasters,
	Build: setworldspawnNode,
	Run:   runSetworldspawn,
}

// setworldspawnNode builds the /setworldspawn command tree, matching
// the vanilla report: an optional position, then an optional rotation.
func setworldspawnNode() *Node {
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
	return &Node{
		Type:       NodeLiteral,
		Name:       "setworldspawn",
		Executable: true,
		Required:   PermLevelGamemasters,
		Children:   []*Node{posArg},
	}
}

// runSetworldspawn runs /setworldspawn [pos] [rotation], which moves the
// world spawn: where new players arrive, where a player without a spawn
// point of their own comes back, and where compasses point.
func runSetworldspawn(src Source, input string, args []string) error {
	// The rotation is folded the way RespawnData.of folds it — yaw wrapped,
	// pitch clamped — or 0/0 where none was, the command's ZERO_ROTATION.
	// vanilla: LevelData.RespawnData.of
	yaw, pitch := float32(0), float32(0)
	pos := world.SpawnPos{
		X: rules.BlockOf(src.Position().X),
		Y: rules.BlockOf(src.Position().Y),
		Z: rules.BlockOf(src.Position().Z),
	}
	if len(args) > 0 {
		x, y, z, ok := resolveBlockPos(src, args)
		if !ok {
			src.Debug("setworldspawn position did not parse", "args", args)
			return nil
		}
		// The block_pos argument is a spawnable one, which the vanilla
		// server reads through getSpawnablePos.
		if !inSpawnableBounds(float64(x), float64(y), float64(z)) {
			src.Debug("setworldspawn position is out of bounds", "x", x, "y", y, "z", z)
			return refuse(src, keyPosOutOfBounds)
		}
		at := src.Position()
		pos = world.SpawnPos{X: x, Y: y, Z: z}
		if r := args[3:]; len(r) > 0 {
			ry, rp, ok := parseRotation(r)
			if !ok {
				src.Debug("setworldspawn rotation did not parse", "args", r)
				return nil
			}
			yaw = WrapDegrees(float32(ry.resolve(float64(at.Yaw))))
			pitch = ClampPitch(float32(rp.resolve(float64(at.Pitch))))
		}
	}
	pos.Yaw, pos.Pitch = yaw, pitch

	// vanilla: SetWorldSpawnCommand.setSpawn
	dimension := src.DimensionName()
	src.SetWorldSpawn(pos, dimension)
	return feedbackToAdmins(src, "commands.setworldspawn.success",
		protocol.Text{Text: formatInt(int64(pos.X))},
		protocol.Text{Text: formatInt(int64(pos.Y))},
		protocol.Text{Text: formatInt(int64(pos.Z))},
		protocol.Text{Text: formatJavaFloat(yaw)},
		protocol.Text{Text: formatJavaFloat(pitch)},
		protocol.Text{Text: dimension},
	)
}
