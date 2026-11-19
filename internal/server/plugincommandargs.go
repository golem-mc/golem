package server

import (
	"fmt"
	"strconv"

	"github.com/golem-mc/golem/internal/commandsyntax"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/plugin"
)

func pluginArgumentNode(a plugin.Argument) (*command.Node, error) {
	n := &command.Node{Type: command.NodeArgument, Name: a.Name}
	switch a.Parser.String() {
	case "brigadier:string":
		n.Parser = command.ParserString
		mode, ok := a.Options["type"].(string)
		if !ok {
			return nil, fmt.Errorf("plugin: missing string argument type")
		}
		var id int32
		switch mode {
		case "word":
			n.Check = func(word string) *command.ParseError {
				for i := range len(word) {
					if !commandsyntax.IsAllowedInUnquotedString(word[i]) {
						return &command.ParseError{Key: "command.expected.separator", Cursor: i}
					}
				}
				return nil
			}
		case "quotable":
			n.Quotable, id = true, 1
		case "greedy":
			n.Greedy, id = true, 2
		default:
			return nil, fmt.Errorf("plugin: unknown string argument type %q", mode)
		}
		n.Properties = func(b *protocol.Buffer) { b.PutVarInt(id) }
	case "brigadier:bool":
		n.Parser = command.ParserBool
		n.Suggests = command.AskServerSuggestion
		n.Completions = func(_ any, partial string) (int, []string) { return 0, command.Suggestions(partial, "true", "false") }
	case "brigadier:integer":
		lo, lok := a.Options["min"].(int32)
		hi, hik := a.Options["max"].(int32)
		if !lok || !hik || lo > hi {
			return nil, fmt.Errorf("plugin: invalid integer bounds")
		}
		n.Parser, n.Int = command.ParserInteger, &command.IntArg{Min: lo, Max: hi, HasMin: true, HasMax: true}
		n.Properties = func(b *protocol.Buffer) { b.PutU8(3); b.PutI32(lo); b.PutI32(hi) }
	case "brigadier:double":
		lo, lok := a.Options["min"].(float64)
		hi, hik := a.Options["max"].(float64)
		if !lok || !hik || lo > hi {
			return nil, fmt.Errorf("plugin: invalid double bounds")
		}
		command.DoubleNode(n, lo, hi)
	case "minecraft:entity":
		single, sok := a.Options["single"].(bool)
		players, pok := a.Options["players"].(bool)
		if !sok || !pok {
			return nil, fmt.Errorf("plugin: invalid entity argument options")
		}
		n = command.EntityArgument(a.Name, single, players)
	case "minecraft:block_pos":
		n = command.ValueArgument(a.Name, command.ParserBlockPos)
	case "minecraft:vec3":
		n = command.ValueArgument(a.Name, command.ParserVec3)
	case "minecraft:block_state":
		n = command.ValueArgument(a.Name, command.ParserBlockState)
	case "minecraft:item_stack":
		n = command.ValueArgument(a.Name, command.ParserItemStack)
	case "minecraft:component":
		n = command.ValueArgument(a.Name, command.ParserComponent)
	case "minecraft:dimension":
		n = command.ValueArgument(a.Name, command.ParserDimension)
	case "minecraft:resource_location":
		n.Parser = command.ParserResourceLoc
	default:
		return nil, fmt.Errorf("plugin: unsupported command parser %s", a.Parser)
	}
	return n, nil
}

func (h *pluginHost) fillCommandArguments(src command.Source, ctx *plugin.CommandContext, values map[string]command.ArgumentValue) error {
	for name, value := range values {
		raw := value.Text
		switch value.Parser {
		case command.ParserBool:
			ctx.Args[name], _ = strconv.ParseBool(command.StringValue(raw))
		case command.ParserInteger:
			v, _ := strconv.ParseInt(raw, 10, 32)
			ctx.Args[name] = int32(v)
		case command.ParserDouble:
			ctx.Args[name], _ = strconv.ParseFloat(raw, 64)
		case command.ParserString:
			if value.Quotable {
				raw = command.StringValue(raw)
			}
			ctx.Args[name] = raw
		case command.ParserResourceLoc:
			ctx.Args[name], _ = plugin.ParseKey(raw)
		default:
			value, err := command.ResolveArgument(src, value)
			if err != nil {
				return err
			}
			switch v := value.(type) {
			case [3]int32:
				ctx.Args[name] = plugin.BlockPos{X: int(v[0]), Y: int(v[1]), Z: int(v[2])}
			case [3]float64:
				ctx.Args[name] = plugin.Vec3{X: v[0], Y: v[1], Z: v[2]}
			case uint16:
				ctx.Args[name] = plugin.BlockState(v)
			case protocol.ItemStack:
				ctx.Args[name] = pluginStack(v)
			case protocol.Text:
				text, ok := pluginText(v)
				if !ok {
					return &command.ParseError{Key: "argument.component.invalid", With: []string{raw}, NoContext: true}
				}
				ctx.Args[name] = text
			case string:
				key, _ := plugin.ParseKey(v)
				ctx.Args[name], _ = h.World(key)
			case []command.Entity:
				entities := make([]plugin.Entity, len(v))
				for i, e := range v {
					entities[i] = h.pluginEntityOf(e)
				}
				ctx.Args[name] = entities
			}

		}
	}
	return nil
}
