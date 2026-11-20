package plugin

import "errors"

// Commands is the command tree vanilla's commands live in. A plugin's
// command joins it, so clients get its suggestions and syntax colours like a
// vanilla command's. Bukkit: CommandMap, with Brigadier's tree (Paper)
type Commands interface {
	// Register adds a command. A name or alias another command holds is
	// registered only as plugin:name, and an error wrapping ErrNameTaken
	// (naming the skipped bare names) is returned; the command still runs
	// under those and any free aliases. It fails without registering only
	// when nothing could be registered (or the command is invalid).
	// Command.Override replaces a vanilla command's bare name.
	// Bukkit: CommandMap.register
	Register(c Command) error
	// Unregister removes a command this plugin registered.
	Unregister(name string)
	// All lists every command: vanilla's first, then the plugins' in the
	// order they registered. Bukkit: CommandMap.getKnownCommands
	All() []CommandInfo
}

// CommandInfo describes a registered command.
type CommandInfo struct {
	Name    string
	Aliases []string
	// Description and Permission are empty for a vanilla command, which has
	// neither here; its permission level is the sender's OpLevel.
	Description string
	Permission  string
	// Usage is the syntax a plugin gave its command, empty for none.
	// Bukkit: Command.getUsage
	Usage string
	// Plugin is the owning plugin's name, empty for a vanilla command.
	Plugin string
}

// Command is a command: a literal root with a tree of arguments.
//
//	plugin.Command{Name: "warp", Permission: "warps.use", Node: plugin.Node{
//		Children: []plugin.Node{{Arg: plugin.WordArg("name"), Run: warpTo}},
//	}}
type Command struct {
	Name        string
	Aliases     []string
	Description string
	// Usage describes the syntax, as plugin.yml's usage does; CommandInfo
	// returns it.
	Usage string
	// Permission is needed to see and run the command; empty for none.
	Permission string
	// Override replaces a vanilla command of the same name (and of each
	// alias) as Bukkit does: the bare name runs this command, in the tree
	// the client gets and for /execute run, functions, command blocks and
	// the console, and the vanilla one stays reachable as minecraft:name.
	// Without it a taken bare name is registered only as plugin:name and
	// Register returns ErrNameTaken. A second override of one name does
	// the same; when the first is unregistered or its plugin disabled,
	// vanilla's command returns.
	Override bool
	Node
}

// Node is a node of a command's tree: a literal word or an argument, and
// what may follow it.
type Node struct {
	// Literal is the word this node matches; Arg the argument it parses.
	// The root's are the Command's Name.
	Literal string
	Arg     Argument
	// Run runs when the input ends at this node; nil when it cannot.
	Run func(ctx *CommandContext) (int, error)
	// Suggest overrides the argument's own suggestions.
	Suggest func(ctx *CommandContext, partial string) []string
	// Permission is needed to see and use this node; empty for the
	// command's.
	Permission string
	Children   []Node
	// Redirect continues parsing at another command's root, as /execute's
	// run does.
	Redirect string
}

// Argument is an argument parser: one of the constructors below, each a
// vanilla argument type, so the client parses and colours it.
type Argument struct {
	Name string
	// Parser is the minecraft:command_argument_type entry, e.g.
	// brigadier:integer; Options are its properties.
	Parser  Key
	Options map[string]any
}

func WordArg(name string) Argument {
	return Argument{name, Key{"brigadier", "string"}, map[string]any{"type": "word"}}
}
func StringArg(name string) Argument {
	return Argument{name, Key{"brigadier", "string"}, map[string]any{"type": "quotable"}}
}

// Greedy takes the rest of the line.
func GreedyArg(name string) Argument {
	return Argument{name, Key{"brigadier", "string"}, map[string]any{"type": "greedy"}}
}
func BoolArg(name string) Argument { return Argument{name, Key{"brigadier", "bool"}, nil} }
func IntArg(name string, lo, hi int32) Argument {
	return Argument{name, Key{"brigadier", "integer"}, map[string]any{"min": lo, "max": hi}}
}
func FloatArg(name string, lo, hi float64) Argument {
	return Argument{name, Key{"brigadier", "double"}, map[string]any{"min": lo, "max": hi}}
}

// Players parses a selector or name; single limits it to one, players to
// players rather than any entity.
func PlayersArg(name string, single bool) Argument {
	return Argument{name, MinecraftKey("entity"), map[string]any{"single": single, "players": true}}
}

// EntitiesArg is PlayersArg over any entity, so a selector such as
// @e[type=cow] gives Mob and other handles.
func EntitiesArg(name string, single bool) Argument {
	return Argument{name, MinecraftKey("entity"), map[string]any{"single": single, "players": false}}
}
func BlockPosArg(name string) Argument   { return Argument{name, MinecraftKey("block_pos"), nil} }
func Vec3Arg(name string) Argument       { return Argument{name, MinecraftKey("vec3"), nil} }
func BlockStateArg(name string) Argument { return Argument{name, MinecraftKey("block_state"), nil} }
func ItemArg(name string) Argument       { return Argument{name, MinecraftKey("item_stack"), nil} }
func TextArg(name string) Argument       { return Argument{name, MinecraftKey("component"), nil} }
func DimensionArg(name string) Argument  { return Argument{name, MinecraftKey("dimension"), nil} }
func KeyArg(name string) Argument        { return Argument{name, MinecraftKey("resource_location"), nil} }

// CommandContext is a command being run or completed: its sender and the
// arguments parsed so far.
type CommandContext struct {
	Sender CommandSender
	// Location and Entity are where and as whom it runs: the sender's,
	// unless /execute changed them.
	Location Location
	Entity   Entity
	// Input is the whole command line, without its slash.
	Input string
	// Args holds the parsed arguments by name: string, bool, int32,
	// float64, []Entity, BlockPos, Vec3, BlockState, ItemStack, Text, World
	// or Key, by parser.
	Args map[string]any
}

// Arg answers the argument name parsed as a T.
func Arg[T any](ctx *CommandContext, name string) T {
	v, _ := ctx.Args[name].(T)
	return v
}

// ErrHandled is what a command's Run answers when it has already told the
// sender why it failed; the command fails without a message of its own.
var ErrHandled = errors.New("plugin: command failure already reported")

// ErrNameTaken is wrapped by Commands.Register's error when one or more bare
// names were already held and registered only as plugin:name; the command
// still runs under those and any free aliases.
var ErrNameTaken = errors.New("plugin: command name taken")

// RegisterAlias registers name as another command line: /name args runs
// "line args" as the same sender, as Bukkit's commands.yml aliases do. It
// fails as Register does when the name is taken.
func RegisterAlias(s Server, name, line string) error {
	run := func(ctx *CommandContext) (int, error) {
		full := line
		if rest := Arg[string](ctx, "args"); rest != "" {
			full += " " + rest
		}
		n, err := s.Dispatch(ctx.Sender, full)
		if err != nil {
			return 0, ErrHandled
		}
		return n, nil
	}
	return s.Commands().Register(Command{Name: name,
		Run:      run,
		Children: []Node{{Arg: GreedyArg("args"), Run: run}}})
}

// CommandError is an error a command answers to show the sender as vanilla
// shows a command failure, in red.
type CommandError struct{ Message Text }

func (e CommandError) Error() string { return e.Message.PlainString() }
