package command

import (
	"math"
	"strconv"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/registry"
)

// PlaySound is the /playsound command.
var PlaySound = Command{
	Name:   "playsound",
	Level:  PermLevelGamemasters,
	Build:  playsoundNode,
	Run:    discardResult(runPlaySound),
	Result: runPlaySound,
}

// availableSoundsSuggestion is the sound argument's suggestion provider:
// minecraft:sound_event is built into the client, so it may complete the
// names itself, but the server answers a request for them too.
// vanilla: SuggestionProviders.AVAILABLE_SOUNDS
const availableSoundsSuggestion = "minecraft:available_sounds"

// playSoundSource is one source literal of /playsound: the word the tree
// offers and the SoundSource ordinal the Sound Effect packet carries.
type playSoundSource struct {
	name     string
	category int32
}

// playSoundSources are the categories in SoundSource's order, the order the
// tree adds its literals in.
// vanilla: PlaySoundCommand.register
var playSoundSources = []playSoundSource{
	{"master", protocol.SoundCategoryMaster},
	{"music", protocol.SoundCategoryMusic},
	{"record", protocol.SoundCategoryRecord},
	{"weather", protocol.SoundCategoryWeather},
	{"block", protocol.SoundCategoryBlock},
	{"hostile", protocol.SoundCategoryHostile},
	{"neutral", protocol.SoundCategoryNeutral},
	{"player", protocol.SoundCategoryPlayer},
	{"ambient", protocol.SoundCategoryAmbient},
	{"voice", protocol.SoundCategoryVoice},
	{"ui", protocol.SoundCategoryUI},
}

// playsoundNode builds /playsound: sound, source literal, then optional
// targets, position, volume, pitch and minVolume in turn; each source literal
// has its own copy of the tail.
// vanilla: PlaySoundCommand.register, .source
func playsoundNode() *Node {
	targetSubtree := func() *Node {
		targets := &Node{
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
		pos := &Node{Type: NodeArgument, Name: "pos", Executable: true, Parser: ParserVec3}
		volume := &Node{
			Type:       NodeArgument,
			Name:       "volume",
			Executable: true,
			Parser:     ParserFloat,
			Float:      &FloatArg{Min: 0, HasMin: true},
		}
		pitch := &Node{
			Type:       NodeArgument,
			Name:       "pitch",
			Executable: true,
			Parser:     ParserFloat,
			Float:      &FloatArg{Min: 0, Max: 2, HasMin: true, HasMax: true},
		}
		minVolume := &Node{
			Type:       NodeArgument,
			Name:       "minVolume",
			Executable: true,
			Parser:     ParserFloat,
			Float:      &FloatArg{Min: 0, Max: 1, HasMin: true, HasMax: true},
		}
		pitch.Children = []*Node{minVolume}
		volume.Children = []*Node{pitch}
		pos.Children = []*Node{volume}
		targets.Children = []*Node{pos}
		return targets
	}
	sound := soundArgNode()
	for _, source := range playSoundSources {
		sound.Children = append(sound.Children, &Node{
			Type:       NodeLiteral,
			Name:       source.name,
			Executable: true,
			Children:   []*Node{targetSubtree()},
		})
	}
	return &Node{
		Type:     NodeLiteral,
		Name:     "playsound",
		Required: PermLevelGamemasters,
		Children: []*Node{sound},
	}
}

// runPlaySound plays the named sound to the target players from the position
// with the volume, pitch and minVolume, answering how many it was sent to.
// vanilla: PlaySoundCommand.playSound
func runPlaySound(src Source, input string, args []string) (int32, error) {
	name, ok := parseIdentifier(args[0])
	if !ok {
		src.Debug("sound is not a resource location", "sound", args[0])
		return 0, nil
	}

	// The category defaults to master and the targets to the runner.
	source := int32(protocol.SoundCategoryMaster)
	var targets []Target
	if self, ok := src.SelfEntity().(Target); ok {
		targets = []Target{self}
	}
	at := src.Position()
	x, y, z := at.X, at.Y, at.Z
	volume, pitch, minVolume := float32(1), float32(1), float32(0)

	rest := args[1:]
	if len(rest) > 0 {
		source = playSoundCategory(rest[0])
		rest = rest[1:]
	}
	if len(rest) > 0 {
		var err error
		word, after := splitTargets(input, rest)
		if targets, err = resolvePlayers(src, word); err != nil {
			return 0, err
		}
		rest = after
	}
	if len(rest) > 0 {
		if len(rest) < 3 {
			return 0, nil
		}
		var ok bool
		if x, y, z, ok = resolveVec3(src, rest[:3]); !ok {
			src.Debug("playsound position did not parse", "args", rest[:3])
			return 0, nil
		}
		rest = rest[3:]
	}
	if len(rest) > 0 {
		if volume, ok = playSoundFloat(rest[0]); !ok {
			return 0, nil
		}
		rest = rest[1:]
	}
	if len(rest) > 0 {
		if pitch, ok = playSoundFloat(rest[0]); !ok {
			return 0, nil
		}
		rest = rest[1:]
	}
	if len(rest) > 0 {
		if minVolume, ok = playSoundFloat(rest[0]); !ok {
			return 0, nil
		}
	}

	reach := float32(16)
	if volume > 1 {
		reach = volume * 16
	}
	maxDistSqr := float64(reach * reach)
	dim := src.DimensionName()
	// One seed for the whole command, so a multi-variant sound takes the
	// same variant for every target.
	// vanilla: PlaySoundCommand.playSound
	seed := src.Random().Int63()
	var playedFor []Target
	for _, target := range targets {
		if target.DimensionName() != dim {
			continue
		}
		pos := target.Position()
		dx, dy, dz := x-pos.X, y-pos.Y, z-pos.Z
		distSqr := dx*dx + dy*dy + dz*dz
		sx, sy, sz, vol := x, y, z, volume
		if distSqr > maxDistSqr {
			if minVolume <= 0 {
				continue
			}
			// Out of reach but a floor was asked for: play it from two
			// blocks from the target, toward the true source.
			dist := math.Sqrt(distSqr)
			sx = pos.X + dx/dist*2
			sy = pos.Y + dy/dist*2
			sz = pos.Z + dz/dist*2
			vol = minVolume
		}
		target.PlaySound(name, source, sx, sy, sz, vol, pitch, seed)
		playedFor = append(playedFor, target)
	}

	count := len(playedFor)
	if count == 0 {
		return 0, refuse(src, "commands.playsound.failed")
	}
	sound := protocol.Text{Text: name}
	if count == 1 {
		return resultOf(1, feedbackToAdmins(src, "commands.playsound.success.single", sound, playedFor[0].DisplayName()))
	}
	return resultOf(int32(count), feedbackToAdmins(src, "commands.playsound.success.multiple", sound, entityCount(count)))
}

// playSoundCategory maps a source literal to its SoundSource ordinal; the
// walk has already refused a word that names none, so master is only a
// fallback.
func playSoundCategory(name string) int32 {
	for _, source := range playSoundSources {
		if source.name == name {
			return source.category
		}
	}
	return protocol.SoundCategoryMaster
}

// playSoundFloat reads one word as the float the walk accepted for a volume,
// a pitch or a minimum volume.
func playSoundFloat(word string) (float32, bool) {
	v, err := strconv.ParseFloat(word, 32)
	if err != nil {
		return 0, false
	}
	return float32(v), true
}

// soundArgNode is the <sound> argument shared by /playsound and /stopsound.
func soundArgNode() *Node {
	return &Node{
		Type:       NodeArgument,
		Name:       "sound",
		Executable: true,
		Parser:     ParserResourceLoc,
		Suggests:   availableSoundsSuggestion,
		Completions: func(_ any, partial string) (int, []string) {
			return 0, ResourceSuggestions(partial, registry.SoundEvents...)
		},
	}
}
