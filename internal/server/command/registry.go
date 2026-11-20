package command

import "sync"

// Command is one command of the registry: the word that names it, the
// permission level the vanilla server's .requires asks for it, the builder
// that shapes its tree for the Commands packet, and the handler that runs
// it. The aliases carry no handler of their own: they are redirects in the
// tree, and CanonicalName maps them onto the command they stand for.
type Command struct {
	Name  string
	Level int
	Build func() *Node
	Run   func(src Source, input string, args []string) error
	// Result, where set, is Run answering the command's return value as
	// well, what an /execute store writes; Run then discards it.
	Result func(src Source, input string, args []string) (int32, error)
}

// Exec runs the command and answers its return value: Result's where the
// command has one, and otherwise 1 for any run Run did not refuse. A
// refused or failed run answers 0.
// vanilla: ExecuteCommand.execute, ExecutionCommandSource.resultConsumer
func (c Command) Exec(src Source, input string, args []string) (int32, error) {
	if c.Result != nil {
		result, err := c.Result(src, input, args)
		if err != nil {
			return 0, err
		}
		return result, nil
	}
	if err := c.Run(src, input, args); err != nil {
		return 0, err
	}
	return 1, nil
}

// resultOf answers value, or 0 with err where err refused the run.
func resultOf(value int32, err error) (int32, error) {
	if err != nil {
		return 0, err
	}
	return value, nil
}

// discardResult is a Result as the Run beside it: the same run, its return
// value dropped.
func discardResult(run func(Source, string, []string) (int32, error)) func(Source, string, []string) error {
	return func(src Source, input string, args []string) error {
		_, err := run(src, input, args)
		return err
	}
}

// registered holds every command this server offers, in the order the
// vanilla server registers them, which is the order their nodes land in the
// Commands packet and the order /help lists them in. It is filled in an
// init because the commands' own handlers reach for Lookup, which reads
// this list — a var initializer would be a cycle the compiler refuses.
var registered []Command

func init() {
	registered = []Command{
		Advancement,
		Attribute,
		Execute,
		Bossbar,
		Clear,
		Clone,
		Compute,
		Damage,
		Data,
		DataPackCommand,
		DebugCommand,
		Defaultgamemode,
		Dialog,
		Difficulty,
		Effect,
		Me,
		Enchant,
		Experience,
		Xp,
		Fill,
		FillBiome,
		Forceload,
		FunctionCommand,
		Gamemode,
		Gamerule,
		Give,
		// deliberate: Bukkit's help replaces vanilla's (HelpCommand is not
		// registered); see Server.bukkitHelpRoots.
		Item,
		Kick,
		Kill,
		List,
		Locate,
		Loot,
		Msg,
		Tell,
		W,
		Swing,
		Particle,
		Place,
		PlaySound,
		PostEffect,
		Random,
		Reload,
		Recipe,
		FetchProfile,
		Return,
		Ride,
		Rotate,
		Say,
		ScheduleCommand,
		ScoreboardCommand,
		Seed,
		Version,
		Setblock,
		Spawnpoint,
		Setworldspawn,
		Spectate,
		Spreadplayers,
		Stopsound,
		Stopwatch,
		Summon,
		Tag,
		TeamCommand,
		TeamMsg,
		Tm,
		Teleport,
		Tp,
		Tellraw,
		Test,
		Tick,
		Time,
		Title,
		Trigger,
		Waypoint,
		Weather,
		Worldborder,
		BanIP,
		BanList,
		Ban,
		Deop,
		Op,
		Pardon,
		PardonIP,
		JfrCommand,
		PerfCommand,
		SaveAll,
		SaveOff,
		SaveOn,
		SetIdleTimeout,
		Stop,
		Transfer,
		Whitelist,
		// Not vanilla: Paper's /tps and /mspt, and a spark-style /metrics.
		Tps,
		Mspt,
		Metrics,
	}
}

// Lookup finds the registered command a dispatch was asked for, reporting
// whether it is one this package runs. The aliases resolve CanonicalName
// first, so what is not found here is a word the server does not know.
func Lookup(name string) (Command, bool) {
	for _, cmd := range registered {
		if cmd.Name == name {
			return cmd, true
		}
	}
	return Command{}, false
}

// ForLevel returns the tree offered at a permission level: every node whose
// requirement is met, so a non-op's completion offers no /time. A node
// gated higher goes with everything under it (bare /random for everyone, its
// sequence and reset nodes for operators), for packet, completion and errors
// alike. Built once per level and shared, so it must not be changed; it
// depends only on the level and the command list init fixes.
// vanilla: Commands.fillUsableCommands
func ForLevel(level int) *Node {
	if level < 0 || level >= len(levelTrees) {
		return buildForLevel(level)
	}
	cached := &levelTrees[level]
	cached.once.Do(func() { cached.tree = buildForLevel(level) })
	return cached.tree
}

// levelTrees holds ForLevel's tree for each permission level, built on
// first use.
var levelTrees [PermLevelOwners + 1]struct {
	once sync.Once
	tree *Node
}

// buildForLevel builds a fresh tree for ForLevel.
func buildForLevel(level int) *Node {
	root := &Node{Type: NodeRoot}
	for _, cmd := range registered {
		if cmd.Level > level {
			continue
		}
		root.Children = append(root.Children, pruneNodes(cmd.Build(), level))
	}
	// The aliases are wired to their targets now that the whole tree
	// exists, so that the packet carries the one subtree rather than a copy.
	// The same pass wires the one node whose reading hands the rest of the
	// input back to the root - an /execute's run - which is a redirect the
	// builder could not name either.
	for _, child := range root.Children {
		if child.RedirectName != "" {
			child.Redirect = FindLiteralChild(root, child.RedirectName)
		}
		wireRedirectRoot(child, root)
	}
	return root
}

// pruneNodes drops the nodes gated above level from node's subtree, with
// everything under them. The tree was built for this call, so the slices
// can be narrowed in place.
func pruneNodes(node *Node, level int) *Node {
	if node.Required > level {
		return nil
	}
	if len(node.Children) == 0 {
		return node
	}
	children := node.Children[:0]
	for _, child := range node.Children {
		if pruned := pruneNodes(child, level); pruned != nil {
			children = append(children, pruned)
		}
	}
	node.Children = children
	return node
}

// wireRedirectRoot walks a command's subtree pointing every node the builder
// marked as reading on from the root at the root itself.
func wireRedirectRoot(node *Node, root *Node) {
	if node.RedirectRoot {
		node.Redirect = root
	}
	for _, child := range node.Children {
		wireRedirectRoot(child, root)
	}
}
