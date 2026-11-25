package command

import (
	"math"
	"sync/atomic"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
)

// Teleport is the /teleport command, and Tp its alias.
var (
	Teleport = Command{
		Name:   "teleport",
		Level:  PermLevelGamemasters,
		Build:  teleportNode,
		Run:    discardResult(runTeleport),
		Result: runTeleport,
	}
	Tp = Command{
		Name:  "tp",
		Level: PermLevelGamemasters,
		Build: tpNode,
	}
)

// teleportNode builds the /teleport command tree, matching the vanilla
// report: a single destination entity, a position, or a set of targets
// followed by either, a position possibly followed by a rotation or a
// facing clause.
func teleportNode() *Node {
	// destination <entity, single> — teleporting to another entity.
	destinationArg := &Node{
		Type:       NodeArgument,
		Name:       "destination",
		Executable: true,
		Parser:     ParserEntity,
		// 0x01: a single entity, of any kind. Everything in this world is
		// a player, so the suggestion list is the players'.
		Properties: func(b *protocol.Buffer) { b.PutU8(0x01) },
		EntityArg:  &EntityArg{Single: true},
		Suggests:   AskServerSuggestion,
		Completions: func(ctx any, partial string) (int, []string) {
			return EntityCompletions(ctx, partial)
		},
	}

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

	// location <vec3> on its own, teleporting the body the runner's @s
	// names; vanilla takes no clause after it there.
	ownLocationArg := &Node{
		Type:       NodeArgument,
		Name:       "location",
		Executable: true,
		Parser:     ParserVec3,
	}

	// location <vec3> after targets, with the rotation and facing clauses under it.
	locationArg := &Node{
		Type:       NodeArgument,
		Name:       "location",
		Executable: true,
		Parser:     ParserVec3,
	}
	rotationArg := &Node{
		Type:       NodeArgument,
		Name:       "rotation",
		Executable: true,
		Parser:     ParserRotation,
	}
	locationArg.Children = []*Node{rotationArg, facingNode}

	// targets <entity, multiple> under which the same two forms appear.
	targetsArg := &Node{
		Type:       NodeArgument,
		Name:       "targets",
		Parser:     ParserEntity,
		Properties: func(b *protocol.Buffer) { b.PutU8(0x00) },
		EntityArg:  entityTargets(),
		Suggests:   AskServerSuggestion,
		Completions: func(ctx any, partial string) (int, []string) {
			return EntityCompletions(ctx, partial)
		},
		Children: []*Node{locationArg, destinationArg},
	}

	return &Node{
		Type:     NodeLiteral,
		Name:     "teleport",
		Required: PermLevelGamemasters,
		Children: []*Node{
			ownLocationArg,
			destinationArg,
			targetsArg,
		},
	}
}

// tpNode builds /tp, the alias of /teleport the vanilla server registers as
// a redirect: everything after the word tp parses as if it followed
// teleport. The redirect target is wired up by name, so that the packet
// carries the one teleport subtree rather than a copy of it.
func tpNode() *Node {
	return &Node{
		Type:         NodeLiteral,
		Name:         "tp",
		Required:     PermLevelGamemasters,
		RedirectName: "teleport",
	}
}

// shapeMemo is the last input teleportShape walked: an execute forks one run
// tail over many contexts, all with the same input and tree.
type shapeMemo struct {
	tree  *Node
	input string
	allow bool
	shape []string
}

var lastShape atomic.Pointer[shapeMemo]

// teleportShape is the names of the nodes /teleport's input took through the
// tree, from its first argument on. The result is shared and must not be
// changed.
func teleportShape(src Source, input string) []string {
	tree := src.CommandTree()
	allow := src.PermLevel() >= PermLevelGamemasters
	if m := lastShape.Load(); m != nil && m.tree == tree && m.allow == allow && m.input == input {
		return m.shape
	}
	path, _ := MatchPath(tree, input, Tokenize(input), allow)
	var shape []string
	for _, node := range path {
		if node.Type == NodeArgument {
			shape = append(shape, node.Name)
		} else if len(shape) > 0 {
			shape = append(shape, node.Name)
		}
	}
	lastShape.Store(&shapeMemo{tree: tree, input: input, allow: allow, shape: shape})
	return shape
}

// runTeleport runs /teleport and its /tp alias, in the forms the tree
// allows: a destination entity, a position, or a set of targets followed by
// either. Relative coordinates and rotations resolve against the player who
// ran the command, so a /tp @a ~10 ~ ~ moves everyone to the same place
// rather than ten blocks east of each of them. It answers how many entities
// it moved.
// vanilla: TeleportCommand.teleportToEntity, teleportToPos
func runTeleport(src Source, input string, words []string) (int32, error) {
	if len(words) == 0 {
		src.Debug("incomplete teleport command")
		return 0, nil
	}

	// The words' path through the tree decides the form: three numbers are
	// a position, a destination, or a target and position depending on the
	// branches. The dispatcher already walked this input, so the walk wins
	// the same way again; literals count, since the facing clause's is what
	// tells two forms apart.
	shape := teleportShape(src, input)
	if len(shape) == 0 {
		src.Debug("teleport command walked no arguments", "input", input)
		return 0, nil
	}

	word, rest := "", words
	if shape[0] == "targets" {
		word, rest = splitTargets(input, words)
		shape = shape[1:]
	}
	targets, err := resolveEntitiesOrSelf(src, word)
	if err != nil {
		return 0, err
	}

	// A destination teleports to where that one stands, facing the same
	// way they do.
	if shape[0] == "destination" {
		destWord, _ := splitTargets(input, rest)
		destination, err := resolveEntities(src, destWord)
		if err != nil {
			return 0, err
		}
		// vanilla: TeleportCommand.teleportToEntity arrives in the
		// destination's level, not the source's.
		move := Move{Pos: destination[0].Position()}
		for _, target := range targets {
			target.Teleport(commandMove(resolvedMove(move, target)), destination[0].DimensionName(), src.Name())
		}
		return resultOf(int32(len(targets)), teleportFeedback(src, targets, player.Position{}, destination[0]))
	}

	// A position, resolved once against the runner, and whatever aims the
	// arrival after it: a rotation of the target's own, or a facing clause.
	p, ok := parseVec3(rest)
	if !ok {
		src.Debug("teleport position did not parse", "args", rest)
		return 0, nil
	}
	x, y, z := p.at(src)
	// vanilla: TeleportCommand.performTeleport, which checks each target
	// before moving it; the position is the same for all, so the first
	// refusal is the only one.
	if len(targets) > 0 && !inSpawnableBounds(x, y, z) {
		return 0, refuse(src, "commands.teleport.invalidPosition")
	}
	at := src.Position()
	to := player.Position{X: x, Y: y, Z: z}
	yaw, pitch := at.Yaw, at.Pitch
	relYaw, relPitch := false, false
	keepRotation := false
	var lookAt *player.Position
	var lookEntity int32
	var lookEntityEyes, lookFromEyes bool

	remaining := rest[3:]
	switch {
	case len(remaining) > 0 && shape[1] == "facing":
		fp, err := teleportFacingPoint(src, input, remaining[1:])
		if err != nil || !fp.ok {
			return 0, err
		}
		// A facing clause names no rotation: the rotation rides as a zero
		// relative offset, and the body is turned here for a mob or by a
		// Player Look At at the point for a player, worked out per target in
		// resolvedMove. vanilla: TeleportCommand.performTeleport, LookAt
		relYaw, relPitch = true, true
		lookAt = &fp.pos
		lookEntity, lookEntityEyes, lookFromEyes = fp.entity, fp.eyes, src.AnchorEyes()
	case len(remaining) > 0:
		ryaw, rpitch, ok := parseRotation(remaining)
		if !ok {
			src.Debug("teleport rotation did not parse", "args", remaining)
			return 0, nil
		}
		relYaw, relPitch = ryaw.relative, rpitch.relative
		yaw, pitch = float32(ryaw.resolve(float64(at.Yaw))), float32(rpitch.resolve(float64(at.Pitch)))
		if !relYaw {
			yaw = WrapDegrees(yaw)
		}
		if !relPitch {
			pitch = WrapDegrees(pitch)
		}
	default:
		// No rotation given: vanilla sends each target a zero rotation
		// offset, so the client keeps the facing it already has.
		// vanilla: TeleportCommand.getRelatives
		keepRotation = true
		relYaw, relPitch = true, true
	}
	to.Yaw, to.Pitch = yaw, pitch
	move := Move{
		Pos:              to,
		LookAt:           lookAt,
		LookAtFromEyes:   lookFromEyes,
		LookAtEntity:     lookEntity,
		LookAtEntityEyes: lookEntityEyes,
	}
	if p.local {
		move.RelX, move.RelY, move.RelZ = true, true, true
		move.DeltaX, move.DeltaY, move.DeltaZ = true, true, true
	} else {
		move.RelX, move.RelY, move.RelZ = p.x.relative, p.y.relative, p.z.relative
		move.DeltaX, move.DeltaY, move.DeltaZ = p.x.relative, p.y.relative, p.z.relative
	}
	move.RelYaw, move.RelPitch = relYaw, relPitch
	move.KeepRotation = keepRotation
	for _, target := range targets {
		target.Teleport(commandMove(resolvedMove(move, target)), src.DimensionName(), src.Name())
	}
	return resultOf(int32(len(targets)), teleportFeedback(src, targets, to, nil))
}

// resolvedMove fills a target's relative-axis offsets and restores its own
// facing when no rotation was given. A facing clause aims the body from the
// target's anchor: the source's anchor type at the target's position and eye
// height. vanilla: PositionMoveRotation.calculateAbsolute, Entity.lookAt
func resolvedMove(move Move, target Entity) Move {
	cur := target.Position()
	if move.LookAt == nil && move.KeepRotation {
		move.Pos.Yaw, move.Pos.Pitch = cur.Yaw, cur.Pitch
	}
	move.Want = move.Pos
	if move.LookAt != nil {
		from := move.Pos
		if move.LookAtFromEyes {
			from.Y += target.EyePosition().Y - target.Position().Y
		}
		move.Pos.Yaw, move.Pos.Pitch = lookAnglesFrom(from, *move.LookAt)
	}
	move.Offset = player.Position{
		X:     move.Pos.X - cur.X,
		Y:     move.Pos.Y - cur.Y,
		Z:     move.Pos.Z - cur.Z,
		Yaw:   move.Pos.Yaw - cur.Yaw,
		Pitch: move.Pos.Pitch - cur.Pitch,
	}
	if move.LookAt != nil {
		// The turn rides the Player Look At, so the position packet carries
		// a zero rotation offset. vanilla: TeleportCommand.getRelatives
		move.Offset.Yaw, move.Offset.Pitch = 0, 0
		return move
	}
	return move.AgainstAngles(cur.Yaw, cur.Pitch)
}

// AgainstAngles resolves a relative axis of Want against a target's current
// angles, so a target that an earlier one in the same command already turned
// is resolved from where it is now.
// vanilla: TeleportCommand.performTeleport wraps the offset before
// PositionMoveRotation.calculateAbsolute adds it to the target's angle.
func (m Move) AgainstAngles(yaw, pitch float32) Move {
	if m.RelYaw {
		m.Offset.Yaw = WrapDegrees(m.Want.Yaw - yaw)
		m.Pos.Yaw = yaw + m.Offset.Yaw
	}
	if m.RelPitch {
		m.Offset.Pitch = WrapDegrees(m.Want.Pitch - pitch)
		m.Pos.Pitch = ClampPitch(pitch + m.Offset.Pitch)
	}
	return m
}

// lookAnglesFrom works out which way a player standing at from would have to
// face to look at to, in the yaw and pitch the protocol measures them in:
// yaw clockwise from south, pitch down from the horizon, both in degrees.
// vanilla: CommandSourceStack.facing
func lookAnglesFrom(from, to player.Position) (float32, float32) {
	dx := to.X - from.X
	dy := to.Y - from.Y
	dz := to.Z - from.Z
	fpi := float64(float32(180 / math.Pi))
	// vanilla: Mth.wrapDegrees((float) (Mth.atan2(f, d) * 57.2957763671875D) - 90.0F)
	yaw := WrapDegrees(float32(mthAtan2(dz, dx)*fpi) - 90)
	horizontal := math.Sqrt(dx*dx + dz*dz)
	// vanilla: Mth.wrapDegrees((float) (-(Mth.atan2(e, g) * 57.2957763671875D)))
	pitch := WrapDegrees(float32(-(mthAtan2(dy, horizontal) * fpi)))
	return yaw, pitch
}

// facingPoint is the spot a facing clause aims at: a position, or an entity
// whose id and feet-or-eyes anchor let a player's Look At travel in the entity
// form. ok is false for a clause the words did not complete.
type facingPoint struct {
	pos    player.Position
	entity int32
	eyes   bool
	ok     bool
}

// teleportFacingPoint reads the facing clause's words and returns the spot to
// look at: either an entity's feet or eyes, or a position resolved against
// the runner; the tree has already seen to the clause's shape.
func teleportFacingPoint(src Source, input string, words []string) (facingPoint, error) {
	if len(words) == 0 {
		src.Debug("incomplete teleport facing clause")
		return facingPoint{}, nil
	}
	if words[0] == "entity" {
		if len(words) < 2 {
			src.Debug("incomplete teleport facing entity")
			return facingPoint{}, nil
		}
		targetWord, after := splitTargets(input, words[1:])
		lookAt, err := resolveEntities(src, targetWord)
		if err != nil {
			return facingPoint{}, err
		}
		// The anchor - feet or eyes - is the last word when it is there,
		// and the feet when it is not.
		p := lookAt[0].Position()
		eyes := len(after) > 0 && after[0] == "eyes"
		if eyes {
			p = lookAt[0].EyePosition()
		}
		return facingPoint{pos: p, entity: lookAt[0].EntityID(), eyes: eyes, ok: true}, nil
	}
	x, y, z, ok := resolveVec3(src, words)
	if !ok {
		src.Debug("teleport facing position did not parse", "args", words)
		return facingPoint{}, nil
	}
	return facingPoint{pos: player.Position{X: x, Y: y, Z: z}, ok: true}, nil
}

// teleportFeedback sends the vanilla teleport command's feedback: who moved,
// and to where or to whom. A position move names the coordinates as three
// separate arguments, a move onto someone names that one by its display
// name; the multiple forms name the count as a number.
func teleportFeedback(src Source, targets []Entity, to player.Position, dest Entity) error {
	if !wantsFeedback(src) {
		return nil
	}
	key := "commands.teleport.success.location.single"
	with := []protocol.Text{formatDisplayNames(targets),
		{Text: formatCoord(to.X)},
		{Text: formatCoord(to.Y)},
		{Text: formatCoord(to.Z)},
	}
	// The destination's name is read here, after the moves, so a teleport
	// callback that renames it is seen (vanilla: TeleportCommand.java:70).
	var destName protocol.Text
	if dest != nil {
		destName = dest.DisplayName()
		key = "commands.teleport.success.entity.single"
		with = []protocol.Text{formatDisplayNames(targets), destName}
	}
	if len(targets) > 1 {
		count := entityCount(len(targets))
		if dest != nil {
			key = "commands.teleport.success.entity.multiple"
			with = []protocol.Text{count, destName}
		} else {
			key = "commands.teleport.success.location.multiple"
			with = []protocol.Text{count,
				{Text: formatCoord(to.X)},
				{Text: formatCoord(to.Y)},
				{Text: formatCoord(to.Z)},
			}
		}
	}
	return feedbackToAdmins(src, key, with...)
}

func commandMove(move Move) Move {
	move.Command = true
	return move
}
