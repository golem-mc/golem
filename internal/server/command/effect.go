package command

import (
	"strings"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/effect"
	"github.com/golem-mc/golem/registry"
)

// Effect is the /effect command.
var Effect = Command{
	Name:   "effect",
	Level:  PermLevelGamemasters,
	Build:  effectNode,
	Run:    discardResult(runEffect),
	Result: runEffect,
}

// EffectTarget is an entity that carries status effects. Each method
// reports whether it changed anything (vanilla: LivingEntity.addEffect,
// removeEffect, removeAllEffects).
type EffectTarget interface {
	AddEffect(in effect.Instance) bool
	RemoveEffect(id int32) bool
	ClearEffects() bool
}

const mobEffectRegistry = "minecraft:mob_effect"

// effectNode builds the /effect tree, matching the vanilla report.
func effectNode() *Node {
	targets := func(executable bool) *Node {
		return &Node{
			Type:       NodeArgument,
			Name:       "targets",
			Executable: executable,
			Parser:     ParserEntity,
			Properties: func(b *protocol.Buffer) { b.PutU8(0x00) }, // entities, several
			EntityArg:  entityTargets(),
			Suggests:   AskServerSuggestion,
			Completions: func(ctx any, partial string) (int, []string) {
				return EntityCompletions(ctx, partial)
			},
		}
	}
	effectArg := func(executable bool) *Node {
		return &Node{
			Type:       NodeArgument,
			Name:       "effect",
			Executable: executable,
			Parser:     ParserResource,
			Properties: func(b *protocol.Buffer) { b.PutString(mobEffectRegistry) },
			Resource:   &ResourceArg{Registry: mobEffectRegistry},
			Suggests:   AskServerSuggestion,
			Completions: func(_ any, partial string) (int, []string) {
				return 0, ResourceSuggestions(partial, registry.MobEffects...)
			},
		}
	}
	amplifier := func() *Node {
		return &Node{
			Type:       NodeArgument,
			Name:       "amplifier",
			Executable: true,
			Parser:     ParserInteger,
			Int:        &IntArg{Min: 0, Max: 255, HasMin: true, HasMax: true},
			Children:   []*Node{{Type: NodeArgument, Name: "hideParticles", Executable: true, Parser: ParserBool}},
		}
	}
	seconds := &Node{
		Type:       NodeArgument,
		Name:       "seconds",
		Executable: true,
		Parser:     ParserInteger,
		Int:        &IntArg{Min: 1, Max: 1000000, HasMin: true, HasMax: true},
		Children:   []*Node{amplifier()},
	}
	infinite := &Node{Type: NodeLiteral, Name: "infinite", Executable: true, Children: []*Node{amplifier()}}
	giveEffect := effectArg(true)
	giveEffect.Children = []*Node{infinite, seconds}
	giveTargets := targets(false)
	giveTargets.Children = []*Node{giveEffect}
	clearTargets := targets(true)
	clearTargets.Children = []*Node{effectArg(true)}
	return &Node{
		Type:     NodeLiteral,
		Name:     "effect",
		Required: PermLevelGamemasters,
		Children: []*Node{
			{Type: NodeLiteral, Name: "clear", Executable: true, Children: []*Node{clearTargets}},
			{Type: NodeLiteral, Name: "give", Children: []*Node{giveTargets}},
		},
	}
}

// runEffect runs /effect give|clear, answering the entities changed
// (vanilla: EffectCommands).
func runEffect(src Source, input string, args []string) (int32, error) {
	if len(args) == 0 {
		return 0, nil
	}
	if args[0] == "give" {
		return effectGive(src, input, args[1:])
	}
	word, rest := splitTargets(input, args[1:])
	targets, err := resolveEntitiesOrSelf(src, word)
	if err != nil {
		return 0, err
	}
	if len(rest) == 0 {
		return effectFeedback(src, targets, "commands.effect.clear.everything", nil, func(t EffectTarget) bool {
			return t.ClearEffects()
		})
	}
	id, ok := mobEffectArg(rest[0])
	if !ok {
		return 0, nil
	}
	name := effectName(id)
	return effectFeedback(src, targets, "commands.effect.clear.specific", &name, func(t EffectTarget) bool {
		return t.RemoveEffect(id)
	})
}

// effectGive runs /effect give <targets> <effect> [seconds|infinite]
// [amplifier] [hideParticles]. A duration left out is thirty seconds, one
// tick for an instant effect; an instant effect's seconds are taken as ticks.
func effectGive(src Source, input string, args []string) (int32, error) {
	word, rest := splitTargets(input, args)
	if len(rest) < 1 {
		return 0, nil
	}
	targets, err := resolveEntities(src, word)
	if err != nil {
		return 0, err
	}
	id, ok := mobEffectArg(rest[0])
	if !ok {
		return 0, nil
	}
	instant := registry.MobEffectOf(id).Instant
	duration := int64(600)
	if instant {
		duration = 1
	}
	amplifier, particles := int64(0), true
	if rest := rest[1:]; len(rest) > 0 {
		switch n, ok := parseInteger(rest[0]); {
		case rest[0] == "infinite":
			duration = effect.Infinite
		case ok:
			duration = n
			if !instant {
				duration *= 20
			}
		default:
			return 0, nil
		}
		if len(rest) > 1 {
			if amplifier, ok = parseInteger(rest[1]); !ok {
				return 0, nil
			}
		}
		if len(rest) > 2 {
			value, ok := boolArgValue(rest[2:])
			if !ok {
				return 0, nil
			}
			particles = value != "true"
		}
	}
	in := effect.New(id, int32(duration), int32(amplifier), false, particles, particles)
	var tracker responseTracker[Entity]
	for _, target := range targets {
		if t, ok := target.(EffectTarget); ok {
			commandCause(t)
			tracker.trackBool(target, t.AddEffect(in))
		}
	}
	return tracker.feedback(src, "commands.effect.give.failed", "commands.effect.give.success.", Entity.DisplayName,
		func(who protocol.Text, _ int) []protocol.Text {
			return []protocol.Text{effectName(id), who, entityCount(int(duration / 20))}
		})
}

// effectFeedback runs a clear on every target that carries effects and
// answers with the key's .failed, .success.single or .success.multiple form.
func effectFeedback(src Source, targets []Entity, key string, name *protocol.Text, clear func(EffectTarget) bool) (int32, error) {
	var tracker responseTracker[Entity]
	for _, target := range targets {
		if t, ok := target.(EffectTarget); ok {
			commandCause(t)
			tracker.trackBool(target, clear(t))
		}
	}
	return tracker.feedback(src, key+".failed", key+".success.", Entity.DisplayName,
		func(who protocol.Text, _ int) []protocol.Text {
			if name != nil {
				return []protocol.Text{*name, who}
			}
			return []protocol.Text{who}
		})
}

// effectName is an effect's display name, its translation.
func effectName(id int32) protocol.Text {
	name := strings.TrimPrefix(registry.MobEffectName(id), "minecraft:")
	return protocol.Text{Translate: "effect.minecraft." + name}
}

// mobEffectArg reads an effect argument the tree walk already checked.
func mobEffectArg(word string) (int32, bool) {
	name, ok := parseResourceLocation(word)
	if !ok {
		return 0, false
	}
	return registry.MobEffectID(name)
}

// commandCause tells a target that supports it the next change is /effect's.
func commandCause(t EffectTarget) {
	if t, ok := t.(interface{ SetEffectCause(effect.Cause) }); ok {
		t.SetEffectCause(effect.CauseCommand)
	}
}
