package server

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/plugin"
)

type pluginCommandRegistry struct {
	mu     sync.Mutex
	roots  []ownedCommand
	active atomic.Bool
}
type ownedCommand struct {
	owner *pluginHost
	name  string
	roots []*command.Node
	info  plugin.CommandInfo
}
type pluginCommands struct{ host *pluginHost }

func (h *pluginHost) Commands() plugin.Commands { return pluginCommands{h} }

func (p pluginCommands) Register(c plugin.Command) error {
	names := []string{c.Name}
	unique := map[string]bool{c.Name: true}
	for _, a := range c.Aliases {
		// Bukkit plugin.yml files repeat the name among the aliases.
		if !unique[a] {
			unique[a] = true
			names = append(names, a)
		}
	}
	for _, name := range names {
		if name == "" || strings.ContainsAny(name, " :/\\\t\n\r") {
			return fmt.Errorf("plugin: invalid command name %q", name)
		}
	}
	root, err := p.host.commandNode(c.Node, c.Permission)
	if err != nil {
		return err
	}
	root.Type, root.Name, root.Plugin, root.Override = command.NodeLiteral, c.Name, true, c.Override
	roots := []*command.Node{root}
	for _, name := range names {
		if name != c.Name {
			roots = append(roots, &command.Node{Type: command.NodeLiteral, Plugin: true, Override: c.Override, Name: name, Permission: root.Permission, Redirect: root, Executable: root.Executable, Handler: root.Handler})
		}
		roots = append(roots, &command.Node{Type: command.NodeLiteral, Plugin: true, Name: strings.ToLower(p.host.Name()) + ":" + name, Permission: root.Permission, Redirect: root, Executable: root.Executable, Handler: root.Handler})
	}
	r := &p.host.srv.pluginCommands
	r.mu.Lock()
	for _, n := range roots {
		if !n.Override && command.FindLiteralChild(command.ForLevel(command.PermLevelOwners), n.Name) != nil {
			r.mu.Unlock()
			return fmt.Errorf("plugin: command %q is taken", n.Name)
		}
		for _, entry := range r.roots {
			for _, other := range entry.roots {
				if other.Name == n.Name {
					r.mu.Unlock()
					return fmt.Errorf("plugin: command %q is taken", n.Name)
				}
			}
		}
	}
	targets := make(map[string]*command.Node)
	for _, n := range command.ForLevel(command.PermLevelOwners).Children {
		targets[n.Name] = n
	}
	for _, entry := range r.roots {
		for _, n := range entry.roots {
			targets[n.Name] = n
		}
	}
	for _, n := range roots {
		targets[n.Name] = n
	}
	if err := checkPluginRedirects(root, targets); err != nil {
		r.mu.Unlock()
		return err
	}
	info := plugin.CommandInfo{Name: c.Name, Aliases: slices.Clone(c.Aliases), Description: c.Description, Usage: c.Usage, Permission: c.Permission, Plugin: p.host.Name()}
	r.roots = append(r.roots, ownedCommand{p.host, c.Name, roots, info})
	r.active.Store(true)
	r.mu.Unlock()
	retained := true
	p.host.host.Own(func() { retained = false; p.Unregister(c.Name) })
	p.host.srv.refreshPluginCommands()
	if !retained {
		return errors.New("plugin: command owner is disabled")
	}
	return nil
}

func (p pluginCommands) Unregister(name string) {
	r := &p.host.srv.pluginCommands
	r.mu.Lock()
	defer p.host.srv.refreshPluginCommands()
	defer r.mu.Unlock()
	r.roots = slices.DeleteFunc(r.roots, func(c ownedCommand) bool { return c.owner == p.host && c.name == name })
	r.active.Store(len(r.roots) != 0)
}

// All lists vanilla's commands with their aliases, then the plugins'. A
// vanilla name a plugin overrides is listed as minecraft:name, as the tree
// the client gets names it.
func (p pluginCommands) All() []plugin.CommandInfo {
	r := &p.host.srv.pluginCommands
	r.mu.Lock()
	defer r.mu.Unlock()
	overridden := make(map[string]bool)
	for _, c := range r.roots {
		for _, n := range c.roots {
			overridden[n.Name] = overridden[n.Name] || n.Override
		}
	}
	name := func(n string) string {
		if overridden[n] {
			return "minecraft:" + n
		}
		return n
	}
	var out []plugin.CommandInfo
	index := make(map[string]int)
	for _, n := range command.ForLevel(command.PermLevelOwners).Children {
		target := n.RedirectName
		if n.Redirect != nil {
			target = n.Redirect.Name
		}
		if target == "" {
			index[n.Name] = len(out)
			info := plugin.CommandInfo{Name: name(n.Name)}
			if !overridden[n.Name] {
				info.Aliases = []string{"minecraft:" + n.Name}
			}
			out = append(out, info)
		}
	}
	for _, n := range command.ForLevel(command.PermLevelOwners).Children {
		target := n.RedirectName
		if n.Redirect != nil {
			target = n.Redirect.Name
		}
		if i, ok := index[target]; ok && target != "" {
			out[i].Aliases = append(out[i].Aliases, name(n.Name))
		}
	}
	for _, c := range r.roots {
		out = append(out, c.info)
	}
	return out
}

func (s *Server) pluginCommandTree(src command.Source, level int) *command.Node {
	base := command.ForLevel(level)
	s.pluginCommands.mu.Lock()
	var roots []*command.Node
	for _, c := range s.pluginCommands.roots {
		roots = append(roots, c.roots...)
	}
	s.pluginCommands.mu.Unlock()
	// With no plugin the tree depends on the level alone; a function runs a
	// command per lookup, so copying the graph each time is too slow.
	cacheable := len(roots) == 0 && !s.pluginCommands.active.Load() && !s.plugins.Any() && level >= 0 && level < len(s.bareTrees)
	if cacheable {
		if t := s.bareTrees[level].Load(); t != nil {
			return t
		}
	}
	taken := make(map[string]bool, len(roots))
	for _, n := range roots {
		taken[n.Name] = true
	}
	roots = append(roots, s.bukkitHelpRoots(taken)...)
	if s.pluginCommands.active.Load() || s.plugins.Any() {
		roots = append(roots, minecraftAliases(base, roots)...)
	}
	sender := s.pluginCommandSender(src)
	tree := command.Extend(base, roots, func(n *command.Node) bool { return n.Permission == "" || s.plugins.HasPermission(sender, n.Permission) })
	if cacheable {
		s.bareTrees[level].Store(tree)
	}
	return tree
}

// minecraftAliases names every vanilla root minecraft:<name>, as Bukkit does,
// except the ones an overriding root already moved there.
func minecraftAliases(base *command.Node, roots []*command.Node) []*command.Node {
	overridden := make(map[string]bool)
	for _, n := range roots {
		overridden[n.Name] = n.Override
	}
	var out []*command.Node
	for _, n := range base.Children {
		if n.Redirect != nil || n.RedirectName != "" || overridden[n.Name] {
			continue
		}
		out = append(out, &command.Node{Type: command.NodeLiteral, Name: "minecraft:" + n.Name, Redirect: n, Executable: n.Executable, Required: n.Required, Dispatch: command.CanonicalName(n.Name)})
	}
	return out
}

func (s *Server) pluginCommandSender(src command.Source) plugin.CommandSender {
	switch origin := command.Origin(src).(type) {
	case *client:
		return pluginPlayer{host: s.pluginAdapter, c: origin}
	case pluginSenderSource:
		return origin.sender
	default:
		return pluginSourceSender{srv: s, src: origin}
	}
}

type pluginSourceSender struct {
	srv *Server
	src command.Source
}

func (s pluginSourceSender) Name() string              { return s.src.Name() }
func (s pluginSourceSender) OpLevel() int              { return s.src.PermLevel() }
func (s pluginSourceSender) SendMessage(t plugin.Text) { _ = s.src.FeedbackText(pluginProtocolText(t)) }
func (s pluginSourceSender) HasPermission(node string) bool {
	return s.srv.plugins.HasPermission(s, node)
}

func (s pluginSourceSender) PermissionSet(node string) bool {
	return s.srv.plugins.PermissionSet(s, node)
}

func (h *pluginHost) commandContext(src command.Source, input string) *plugin.CommandContext {
	pos := src.Position()
	w, _ := h.World(plugin.MinecraftKey("overworld"))
	if dim := h.srv.findDimension(src.DimensionName()); dim != nil {
		w = pluginWorld{srv: h.srv, dim: dim, host: h}
	}
	ctx := &plugin.CommandContext{Sender: h.srv.pluginCommandSender(src), Input: input, Location: plugin.Location{World: w, Pos: plugin.Vec3{X: pos.X, Y: pos.Y, Z: pos.Z}, Yaw: pos.Yaw, Pitch: pos.Pitch}, Args: make(map[string]any)}
	if c, ok := command.Origin(src).(*client); ok {
		ctx.Sender = pluginPlayer{host: h, c: c}
	}
	if e := src.SelfEntity(); e != nil {
		ctx.Entity = h.pluginEntityOf(e)
	}
	return ctx
}

func (h *pluginHost) commandNode(n plugin.Node, permission string) (*command.Node, error) {
	if n.Redirect != "" && len(n.Children) != 0 {
		return nil, errors.New("plugin: a redirect cannot have children")
	}
	if n.Permission != "" {
		permission = n.Permission
	}
	node := &command.Node{Type: command.NodeLiteral, Name: n.Literal, Permission: permission, Executable: n.Run != nil, RedirectName: n.Redirect}
	if n.Arg.Name != "" {
		var err error
		node, err = pluginArgumentNode(n.Arg)
		if err != nil {
			return nil, err
		}
		node.Permission, node.Executable, node.RedirectName = permission, n.Run != nil, n.Redirect
		if node.Parser == command.ParserDimension {
			node.Suggests = command.AskServerSuggestion
			node.Completions = func(_ any, partial string) (int, []string) {
				names := make([]string, len(h.srv.allDims()))
				for i, dim := range h.srv.allDims() {
					names[i] = dim.name
				}
				return 0, command.ResourceSuggestions(partial, names...)
			}
		}
	}
	for _, child := range n.Children {
		if (child.Literal == "") == (child.Arg.Name == "") {
			return nil, errors.New("plugin: a command node needs one literal or argument")
		}
		c, err := h.commandNode(child, permission)
		if err != nil {
			return nil, err
		}
		for _, sibling := range node.Children {
			if sibling.Type == c.Type && sibling.Name == c.Name {
				return nil, fmt.Errorf("plugin: duplicate command node %q", c.Name)
			}
		}
		node.Children = append(node.Children, c)
	}
	if n.Run != nil {
		node.Handler = func(src command.Source, input string, values map[string]command.ArgumentValue) (int32, error) {
			ctx := h.commandContext(src, input)
			if err := h.fillCommandArguments(src, ctx, values); err != nil {
				return 0, err
			}
			value, err := n.Run(ctx)
			if errors.Is(err, plugin.ErrHandled) {
				return 0, command.ErrRefused
			}
			if err != nil {
				message := protocol.Text{Translate: "command.failed"}
				if failure, ok := errors.AsType[plugin.CommandError](err); ok {
					message = pluginProtocolText(failure.Message)
				} else if failure, ok := errors.AsType[*plugin.CommandError](err); ok {
					message = pluginProtocolText(failure.Message)
				}
				_ = src.FeedbackText(protocol.Text{Color: protocol.ColorRed, Extra: []protocol.Text{message}})
				return 0, command.ErrRefused
			}
			return int32(value), nil
		}
	}
	if n.Suggest != nil {
		node.Suggests = command.AskServerSuggestion
		node.InputCompletions = func(src command.Source, input, partial string, values map[string]command.ArgumentValue) (int, []string) {
			ctx := h.commandContext(src, input)
			if err := h.fillCommandArguments(src, ctx, values); err != nil {
				return 0, nil
			}
			return 0, n.Suggest(ctx, partial)
		}
	}
	return node, nil
}

func (s *Server) refreshPluginCommands() {
	s.plugins.Post(func() {
		for _, c := range s.playerClients() {
			c.send(protocol.IDPlayCommands, c.commandTreePacket(c.PermLevel()))
		}
	})
}

func checkPluginRedirects(node *command.Node, targets map[string]*command.Node) error {
	if node.RedirectName != "" {
		seen := make(map[*command.Node]bool)
		for target := node; target != nil; {
			if seen[target] {
				return fmt.Errorf("plugin: cyclic command redirect %q", node.RedirectName)
			}
			seen[target] = true
			if target.Redirect != nil {
				target = target.Redirect
				continue
			}
			if target.RedirectName == "" {
				break
			}
			name := target.RedirectName
			target = targets[name]
			if target == nil {
				return fmt.Errorf("plugin: unknown command redirect %q", name)
			}
		}
	}
	for _, child := range node.Children {
		if err := checkPluginRedirects(child, targets); err != nil {
			return err
		}
	}
	return nil
}
