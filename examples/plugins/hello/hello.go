// Package hello is an example plugin: a listener, a command, tick and async
// tasks, block writes, per-player data and a menu. Copy it and rename.
package hello

import (
	"fmt"
	"time"

	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

// visitsKey is where a player's join count is kept; keys of a plugin use its
// own namespace.
var visitsKey = plugin.Key{Namespace: "hello", Path: "visits"}

// Hello is the plugin; it keeps what it needs between events in its fields.
type Hello struct {
	server plugin.Server
	ticks  int
}

func init() { plugin.Register("hello", &Hello{}) }

// Enable registers everything the plugin does. What it registers through s is
// removed when the plugin is disabled, so Disable has nothing to undo.
func (h *Hello) Enable(s plugin.Server) error {
	h.server = s
	s.Permissions().Register(plugin.Permission{
		Name:        "hello.build",
		Description: "Use /hello build and break gold blocks",
		Default:     plugin.PermissionDefault{OpLevel: 2},
	})

	// A listener: the default priority is Normal; IgnoreCancelled skips the
	// event when a listener before this one cancelled it.
	plugin.Listen(s, h.onBreak, plugin.ListenOptions{Priority: plugin.High, IgnoreCancelled: true})
	plugin.Listen(s, h.onJoin)
	plugin.Listen(s, h.onMenuClick)

	// A tick task: runs on the tick goroutine, every 20 ticks after 20.
	s.Scheduler().RunTimer(20, 20, func() { h.ticks += 20 })

	return s.Commands().Register(h.command())
}

// Disable has nothing to do: tasks, listeners and commands are removed by the
// server.
func (h *Hello) Disable() {}

// onBreak keeps gold blocks for players with the hello.build permission.
func (h *Hello) onBreak(e *plugin.BlockBreakEvent) {
	if e.State.Block() == registry.BlockGoldBlock && !e.Player.HasPermission("hello.build") {
		e.SetCancelled(true)
		e.Player.SendMessage(plugin.Plain("Only builders may break gold.").Color("red"))
	}
}

// onJoin counts a player's joins in their Data, which is saved with them.
func (h *Hello) onJoin(e *plugin.PlayerJoinEvent) {
	visits, _ := plugin.GetData[int32](e.Player.Data(), visitsKey)
	e.Player.Data().Set(visitsKey, visits+1)
	e.Player.SendMessage(plugin.Plain(fmt.Sprintf("Welcome, visit number %d.", visits+1)))
}

// command builds /hello with one subcommand per feature.
func (h *Hello) command() plugin.Command {
	return plugin.Command{
		Name:        "hello",
		Description: "The hello plugin's examples",
		Node: plugin.Node{
			Children: []plugin.Node{
				{Literal: "visits", Run: h.visits},
				{Literal: "uptime", Run: h.uptime},
				{Literal: "menu", Run: h.menu},
				{
					// A literal with a permission: the node is hidden from
					// players without it, and refused.
					Literal: "build", Permission: "hello.build",
					Children: []plugin.Node{{
						Arg: plugin.BlockPosArg("pos"),
						Children: []plugin.Node{{
							Arg: plugin.IntArg("size", 1, 16),
							Run: h.build,
						}},
					}},
				},
				{
					Literal: "slow",
					Children: []plugin.Node{{
						Arg: plugin.WordArg("word"),
						Run: h.slow,
					}},
				},
			},
		},
	}
}

func (h *Hello) visits(ctx *plugin.CommandContext) (int, error) {
	p, ok := ctx.Sender.(plugin.Player)
	if !ok {
		return 0, plugin.CommandError{Message: plugin.Plain("Only a player has visits.")}
	}
	visits, _ := plugin.GetData[int32](p.Data(), visitsKey)
	p.SendMessage(plugin.Plain(fmt.Sprintf("You joined %d times.", visits)))
	return int(visits), nil
}

func (h *Hello) uptime(ctx *plugin.CommandContext) (int, error) {
	ctx.Sender.SendMessage(plugin.Plain(fmt.Sprintf("The timer has counted %d ticks.", h.ticks)))
	return h.ticks, nil
}

// build fills a size x size square of gold under pos. The chunk may not be
// loaded, so it is loaded first; done runs on the tick goroutine.
func (h *Hello) build(ctx *plugin.CommandContext) (int, error) {
	pos := plugin.Arg[plugin.BlockPos](ctx, "pos")
	size := int(plugin.Arg[int32](ctx, "size"))
	world := ctx.Location.World
	gold := plugin.DefaultState(registry.BlockGoldBlock)

	// Every chunk the square touches is loaded before the blocks are set.
	var chunks []plugin.ChunkPos
	for x := pos.X >> 4; x <= (pos.X+size-1)>>4; x++ {
		for z := pos.Z >> 4; z <= (pos.Z+size-1)>>4; z++ {
			chunks = append(chunks, plugin.ChunkPos{X: x, Z: z})
		}
	}
	pending := len(chunks)
	for _, c := range chunks {
		world.LoadChunk(c, true, func(plugin.Chunk) {
			if pending--; pending > 0 {
				return
			}
			for dx := range size {
				for dz := range size {
					plugin.Block{World: world, Pos: plugin.BlockPos{X: pos.X + dx, Y: pos.Y, Z: pos.Z + dz}}.Set(gold)
				}
			}
			ctx.Sender.SendMessage(plugin.Plain(fmt.Sprintf("Built %d gold blocks.", size*size)))
		})
	}
	return size * size, nil
}

// slow does its work off the tick goroutine, then goes back to it. The
// goroutine keeps the player's UUID, not the handle's state, and looks the
// player up again on the tick: they may have left meanwhile.
func (h *Hello) slow(ctx *plugin.CommandContext) (int, error) {
	p, ok := ctx.Sender.(plugin.Player)
	if !ok {
		return 0, plugin.CommandError{Message: plugin.Plain("Only a player can wait.")}
	}
	id, word := p.UUID(), plugin.Arg[string](ctx, "word")
	h.server.Scheduler().RunAsync(func() {
		time.Sleep(time.Second) // a database or HTTP call would be here
		result := fmt.Sprintf("%s has %d letters", word, len(word))
		h.server.Scheduler().RunTask(func() {
			if p, ok := h.server.Player(id); ok {
				p.SendMessage(plugin.Plain(result))
			}
		})
	})
	return 1, nil
}

// menu opens a one-row chest with a single clickable item. holder tells the
// click listener the inventory is this plugin's menu.
func (h *Hello) menu(ctx *plugin.CommandContext) (int, error) {
	p, ok := ctx.Sender.(plugin.Player)
	if !ok {
		return 0, plugin.CommandError{Message: plugin.Plain("Only a player can open a menu.")}
	}
	inv := h.server.CreateInventory(plugin.InvChest, 1, plugin.Plain("Hello menu"), menuHolder{})
	name := plugin.Plain("Say hello").NotItalic()
	stack := plugin.NewStack(registry.ItemDiamond, 1)
	stack.Patch.CustomName = &name
	inv.SetItem(4, stack)
	p.OpenInventory(inv)
	return 1, nil
}

type menuHolder struct{}

// onMenuClick cancels every click in the menu, so nothing can be taken, and
// reacts to the diamond.
func (h *Hello) onMenuClick(e *plugin.InventoryClickEvent) {
	if _, ok := e.View.Top().Holder().(menuHolder); !ok {
		return
	}
	e.SetCancelled(true)
	if e.RawSlot == 4 {
		e.Player.SendMessage(plugin.Plain("Hello!").Color("gold"))
		e.View.Close()
	}
}
