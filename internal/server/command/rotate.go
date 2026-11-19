package command

import (
	"math"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
)

// Rotate is the /rotate command.
var Rotate = Command{
	Name:  "rotate",
	Level: PermLevelGamemasters,
	Build: rotateNode,
	Run:   runRotate,
}

// rotateSuccessKey is the line a turn answers with, naming who was turned.
// vanilla: RotateCommand
const rotateSuccessKey = "commands.rotate.success"

// rotateNode builds the /rotate command tree, matching the vanilla
// report: a set of targets followed by either a rotation or a facing clause.
func rotateNode() *Node {
	// facing <entity|location> — the clause that aims the target somewhere.
	facingEntityArg := &Node{
		Type:       NodeArgument,
		Name:       "facingEntity",
		Executable: true,
		Parser:     ParserEntity,
		Properties: func(b *protocol.Buffer) { b.PutU8(0x01) },
		EntityArg:  &EntityArg{Single: true},
		Suggests:   AskServerSuggestion,
		Completions: func(ctx any, partial string) (int, []string) {
			return EntityCompletions(ctx, partial)
		},
	}
	facingAnchorArg := &Node{
		Type:       NodeArgument,
		Name:       "facingAnchor",
		Executable: true,
		Parser:     ParserEntityAnch,
	}
	facingEntityArg.Children = []*Node{facingAnchorArg}
	facingLocationArg := &Node{
		Type:       NodeArgument,
		Name:       "facingLocation",
		Executable: true,
		Parser:     ParserVec3,
	}
	facingNode := &Node{
		Type: NodeLiteral,
		Name: "facing",
		// A position is the clause's argument straight away; only an entity
		// takes a word in front of it.
		Children: []*Node{
			facingLocationArg,
			{Type: NodeLiteral, Name: "entity", Children: []*Node{facingEntityArg}},
		},
	}

	// rotation <rotation> executable on its own under targets.
	rotationArg := &Node{
		Type:       NodeArgument,
		Name:       "rotation",
		Executable: true,
		Parser:     ParserRotation,
	}

	// target <entity, single> under which the two clause forms appear.
	targetsArg := &Node{
		Type:       NodeArgument,
		Name:       "target",
		Parser:     ParserEntity,
		Properties: func(b *protocol.Buffer) { b.PutU8(0x01) },
		EntityArg:  &EntityArg{Single: true},
		Suggests:   AskServerSuggestion,
		Completions: func(ctx any, partial string) (int, []string) {
			return EntityCompletions(ctx, partial)
		},
		// The rotation argument comes first, which is the order the
		// either-or of the usage line carries it in.
		Children: []*Node{rotationArg, facingNode},
	}

	return &Node{
		Type:     NodeLiteral,
		Name:     "rotate",
		Required: PermLevelGamemasters,
		Children: []*Node{
			targetsArg,
		},
	}
}

// runRotate runs /rotate <target> (<rotation>|facing <location|entity
// [anchor]>), turning the body in place. Relative angles resolve against the
// source; facing aims from the source's anchor on the target, and a
// trailing anchor only picks the point looked at.
// vanilla: RotateCommand.rotate
func runRotate(src Source, input string, words []string) error {
	if len(words) == 0 {
		src.Debug("incomplete rotate command")
		return nil
	}

	word, rest := splitTargets(input, words)
	targets, err := resolveEntities(src, word)
	if err != nil {
		return err
	}

	var facing bool
	var pos player.Position
	var eyes bool
	var yaw, pitch float32
	if rest[0] == "facing" {
		facing = true
		rest = rest[1:]
		if len(rest) > 0 && rest[0] == "entity" {
			rest = rest[1:]
			targetWord, after := splitTargets(input, rest)
			lookAt, err := resolveEntities(src, targetWord)
			if err != nil {
				return err
			}
			eyes = len(after) > 0 && after[0] == "eyes"
			pos = lookAt[0].Position()
			if eyes {
				pos = lookAt[0].EyePosition()
			}
		} else {
			x, y, z, ok := resolveVec3(src, rest)
			if !ok {
				src.Debug("rotate facing position did not parse", "args", rest)
				return nil
			}
			pos = player.Position{X: x, Y: y, Z: z}
		}
	} else {
		ryaw, rpitch, ok := parseRotation(rest)
		if !ok {
			src.Debug("rotate rotation did not parse", "args", rest)
			return nil
		}
		at := src.Position()
		yaw = WrapDegrees(float32(ryaw.resolve(float64(at.Yaw))))
		// vanilla: Entity.setXRot clamps the pitch where the yaw wraps.
		pitch = float32(min(max(math.Mod(rpitch.resolve(float64(at.Pitch)), 360), -90), 90))
	}

	for _, target := range targets {
		if facing {
			from := target.Position()
			if src.AnchorEyes() {
				from = target.EyePosition()
			}
			// The Player Rotation packet has no entity form, so a facing
			// clause's angles are worked out here and sent as absolute ones.
			// vanilla: LookAt.perform
			fy, fp := lookAnglesFrom(from, pos)
			target.Rotate(fy, fp)
			continue
		}
		target.Rotate(yaw, pitch)
	}
	return feedbackToAdmins(src, rotateSuccessKey, formatDisplayNames(targets))
}
