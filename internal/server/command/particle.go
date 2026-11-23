package command

import (
	"strconv"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// Particle is the /particle command.
var Particle = Command{
	Name:   "particle",
	Level:  PermLevelGamemasters,
	Build:  particleNode,
	Run:    discardResult(runParticle),
	Result: runParticle,
}

// particleRange and particleForcedRange are how far from a player's block
// centre a particle reaches them, normally and in force mode.
// vanilla: ServerLevel.sendParticles
const (
	particleRange       = 32.0
	particleForcedRange = 512.0
)

// particleNode builds the /particle tree: a particle, a position, then a
// delta, a speed and a count, then an optional force or normal literal, each
// of which may end in the viewers.
// vanilla: ParticleCommand.register
func particleNode() *Node {
	viewers := func() *Node {
		return &Node{
			Type:       NodeArgument,
			Name:       "viewers",
			Executable: true,
			Parser:     ParserEntity,
			Properties: func(b *protocol.Buffer) { b.PutU8(0x02) }, // players, several
			EntityArg:  &EntityArg{PlayersOnly: true},
			Suggests:   AskServerSuggestion,
			Completions: func(ctx any, partial string) (int, []string) {
				return EntityCompletions(ctx, partial)
			},
		}
	}
	mode := func(name string) *Node {
		return &Node{Type: NodeLiteral, Name: name, Executable: true, Children: []*Node{viewers()}}
	}
	count := &Node{
		Type:       NodeArgument,
		Name:       "count",
		Executable: true,
		Parser:     ParserInteger,
		Properties: func(b *protocol.Buffer) { b.PutU8(0x01); b.PutI32(0) }, // min 0
		Int:        &IntArg{Min: 0, HasMin: true},
		Children:   []*Node{mode("force"), mode("normal")},
	}
	speed := &Node{
		Type:     NodeArgument,
		Name:     "speed",
		Parser:   ParserFloat,
		Float:    &FloatArg{Min: 0, HasMin: true},
		Children: []*Node{count},
	}
	delta := &Node{Type: NodeArgument, Name: "delta", Parser: ParserVec3, Children: []*Node{speed}}
	pos := &Node{Type: NodeArgument, Name: "pos", Executable: true, Parser: ParserVec3, Children: []*Node{delta}}
	name := &Node{
		Type:       NodeArgument,
		Name:       "name",
		Executable: true,
		Parser:     ParserParticle,
		Children:   []*Node{pos},
	}
	return &Node{Type: NodeLiteral, Name: "particle", Required: PermLevelGamemasters, Children: []*Node{name}}
}

// parseParticleArgument validates a particle argument during the command walk:
// the particle ID, then an SNBT compound when '{' follows directly (otherwise
// an empty compound), decoded through registry.ParticleData before coordinates
// are read.
// vanilla: ParticleArgument.readParticle
func parseParticleArgument(tokens []Token) *ParseError {
	span := joinSelector(tokens)
	name, n, ok := readIdentifier(span.text)
	if !ok {
		return &ParseError{Key: keyIDInvalid, Cursor: span.absolute(0)}
	}
	if _, _, known := registry.ParticleID(name); !known {
		return &ParseError{Key: "particle.notFound", With: []string{name}, Cursor: span.absolute(n)}
	}
	opts := nbt.Compound{}
	end := n
	if n < len(span.text) && span.text[n] == '{' {
		tag, consumed, pe := nbt.ParseOrderedValueTag(span.text[n:])
		if pe != nil {
			return snbtRefusal(pe, span, n+pe.Offset)
		}
		if c, ok := nbt.CompoundView(tag); ok {
			opts = c
		}
		end = n + consumed
	}
	if _, err := registry.ParticleData(name, opts); err != nil {
		return &ParseError{Key: "particle.invalidOptions", With: []string{err.Error()}, NoContext: true}
	}
	if end < len(span.text) {
		return &ParseError{Key: keyExpectedSeparator, Cursor: span.absolute(end)}
	}
	return nil
}

// runParticle sends the particle to the viewers within range of its position
// and answers how many it was sent to.
// A particle type with data reads its options off the name as SNBT.
// vanilla: ParticleCommand.sendParticles
func runParticle(src Source, input string, args []string) (int32, error) {
	// The options are an SNBT compound that may span words.
	span := blockSpanCount(args)
	text := wordsIn(input, args, 0, span)
	name, n, ok := readIdentifier(text)
	if !ok {
		src.Debug("particle is not a resource location", "particle", text)
		return 0, nil
	}
	id, withData, ok := registry.ParticleID(name)
	if !ok {
		return 0, refuse(src, "particle.notFound", protocol.Text{Text: name})
	}
	var data []byte
	if text[n:] != "" || withData {
		verdict, _, _, opts := readSummonTag(text[n:])
		switch {
		case text[n:] == "":
			opts = nbt.Compound{}
		case verdict != nbtIsCompound:
			src.Debug("particle options did not parse", "particle", text)
			return 0, nil
		}
		var err error
		if data, err = registry.ParticleData(name, opts); err != nil {
			return 0, refuse(src, "particle.invalidOptions", protocol.Text{Text: err.Error()})
		}
	}

	at := src.Position()
	x, y, z := at.X, at.Y, at.Z
	var dx, dy, dz float64
	var speed float32
	var count int32
	force := false
	viewers := src.OnlineTargets()

	rest := args[span:]
	if len(rest) > 0 {
		if len(rest) < 3 {
			return 0, nil
		}
		if x, y, z, ok = resolveVec3(src, rest[:3]); !ok {
			src.Debug("particle position did not parse", "args", rest[:3])
			return 0, nil
		}
		rest = rest[3:]
	}
	if len(rest) > 0 {
		if len(rest) < 5 {
			return 0, nil
		}
		if dx, dy, dz, ok = resolveVec3Exact(src, rest[:3]); !ok {
			src.Debug("particle delta did not parse", "args", rest[:3])
			return 0, nil
		}
		s, err := strconv.ParseFloat(rest[3], 32)
		n, err2 := strconv.ParseInt(rest[4], 10, 32)
		if err != nil || err2 != nil || s < 0 {
			return 0, nil
		}
		speed, count = float32(s), int32(n)
		rest = rest[5:]
	}
	if len(rest) > 0 {
		force = rest[0] == "force"
		rest = rest[1:]
	}
	if len(rest) > 0 {
		word, _ := splitTargets(input, rest)
		var err error
		if viewers, err = resolvePlayers(src, word); err != nil {
			return 0, err
		}
	}

	reach := particleRange
	if force {
		reach = particleForcedRange
	}
	dim := src.DimensionName()
	sent := 0
	for _, target := range viewers {
		if target.DimensionName() != dim {
			continue
		}
		// The distance runs from the centre of the block the player stands in.
		// vanilla: Vec3i.closerToCenterThan
		pos := target.Position()
		bx := float64(rules.BlockOf(pos.X)) + 0.5 - x
		by := float64(rules.BlockOf(pos.Y)) + 0.5 - y
		bz := float64(rules.BlockOf(pos.Z)) + 0.5 - z
		if bx*bx+by*by+bz*bz >= reach*reach {
			continue
		}
		target.SendParticle(id, data, force, x, y, z, float32(dx), float32(dy), float32(dz), speed, count)
		sent++
	}
	if sent == 0 {
		return 0, refuse(src, "commands.particle.failed")
	}
	return resultOf(int32(sent), feedbackToAdmins(src, "commands.particle.success", protocol.Text{Text: name}))
}
