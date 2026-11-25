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
	"github.com/golem-mc/golem/internal/server/pluginruntime"
	"github.com/golem-mc/golem/plugin"
)

type pluginCommandRegistry struct {
	mu     sync.Mutex
	roots  []ownedCommand
	active atomic.Bool
	// epoch changes with roots; trees and perms are valid for it.
	epoch      uint64
	permsEpoch uint64 // epoch+1 that perms was computed for; 0 means never
	perms      []string
	trees      map[treeKey]*command.Node
}

// treeKey identifies a cached tree: the plugin permissions the sender holds
// are one byte each in perms, in sorted node order.
type treeKey struct {
	level   int
	plugins bool
	perms   string
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
		// A single leading slash is allowed so a plugin can register "/set"
		// and have the client type "//set".
		if body := strings.TrimPrefix(name, "/"); body == "" || strings.ContainsAny(body, " :/\\\t\n\r") {
			return fmt.Errorf("plugin: invalid command name %q", name)
		}
	}
	root, err := p.host.commandNode(c.Node, c.Permission)
	if err != nil {
		return err
	}
	prefix := strings.ToLower(p.host.Name()) + ":"
	r := &p.host.srv.pluginCommands
	r.mu.Lock()
	isTaken := func(name string, override bool) bool {
		if !override && command.FindLiteralChild(command.ForLevel(command.PermLevelOwners), name) != nil {
			return true
		}
		for _, entry := range r.roots {
			for _, other := range entry.roots {
				if other.Name == name {
					return true
				}
			}
		}
		return false
	}
	type candidate struct {
		name     string
		override bool
	}
	var candidates []candidate
	var skipped []string
	for _, name := range names {
		if isTaken(name, c.Override) {
			skipped = append(skipped, name)
		} else {
			candidates = append(candidates, candidate{name: name, override: c.Override})
		}
		if prefixed := prefix + name; !isTaken(prefixed, false) {
			candidates = append(candidates, candidate{name: prefixed})
		}
	}
	if len(candidates) == 0 {
		r.mu.Unlock()
		return fmt.Errorf("plugin: command %q is taken", c.Name)
	}
	root.Type, root.Name, root.Plugin, root.Override = command.NodeLiteral, candidates[0].name, true, candidates[0].override
	roots := []*command.Node{root}
	for _, cand := range candidates[1:] {
		roots = append(roots, &command.Node{Type: command.NodeLiteral, Plugin: true, Override: cand.override, Name: cand.name, Permission: root.Permission, Redirect: root, Executable: root.Executable, Handler: root.Handler})
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
	r.invalidate()
	r.active.Store(true)
	r.mu.Unlock()
	retained := true
	p.host.host.Own(func() { retained = false; p.Unregister(c.Name) })
	p.host.srv.refreshPluginCommands()
	if !retained {
		return errors.New("plugin: command owner is disabled")
	}
	if len(skipped) > 0 {
		return fmt.Errorf("%w: %s", plugin.ErrNameTaken, strings.Join(skipped, ", "))
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
	r.invalidate()
}

// invalidate drops the cached trees. mu is held.
func (r *pluginCommandRegistry) invalidate() {
	r.epoch++
	r.trees = nil
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

// pluginCommandTree returns the command graph src may use at level. Extending
// the vanilla graph copies it, and a function looks a command up per line, so
// trees are cached per level and per set of plugin permissions the sender
// holds, until a plugin command is registered or unregistered.
func (s *Server) pluginCommandTree(src command.Source, level int) *command.Node {
	r := &s.pluginCommands
	r.mu.Lock()
	var roots []*command.Node
	for _, c := range r.roots {
		roots = append(roots, c.roots...)
	}
	epoch := r.epoch
	if r.permsEpoch != epoch+1 {
		r.perms, r.permsEpoch = distinctPermissions(roots), epoch+1
	}
	perms := r.perms
	r.mu.Unlock()
	key := treeKey{level: level, plugins: s.plugins.Any()}
	var sender plugin.CommandSender
	if len(perms) != 0 {
		sender = s.pluginCommandSender(src)
		sig := make([]byte, len(perms))
		for i, p := range perms {
			sig[i] = '0'
			if s.plugins.HasPermission(sender, p) {
				sig[i] = '1'
			}
		}
		key.perms = string(sig)
	}
	r.mu.Lock()
	t := r.trees[key]
	r.mu.Unlock()
	if t != nil {
		return t
	}
	hasCommands := len(roots) != 0
	taken := make(map[string]bool, len(roots))
	for _, n := range roots {
		taken[n.Name] = true
	}
	base := command.ForLevel(level)
	roots = append(roots, s.bukkitHelpRoots(taken)...)
	roots = append(roots, s.pluginsRoots(taken)...)
	if hasCommands || key.plugins {
		roots = append(roots, minecraftAliases(base, roots)...)
	}
	if sender == nil {
		sender = s.pluginCommandSender(src)
	}
	tree := command.Extend(base, roots, func(n *command.Node) bool { return n.Permission == "" || s.plugins.HasPermission(sender, n.Permission) })
	r.mu.Lock()
	if r.epoch == epoch {
		if r.trees == nil {
			r.trees = make(map[treeKey]*command.Node)
		}
		r.trees[key] = tree
	}
	r.mu.Unlock()
	return tree
}

// distinctPermissions lists the permission nodes the graphs under roots gate on, sorted.
func distinctPermissions(roots []*command.Node) []string {
	set := make(map[string]bool)
	var walk func(*command.Node)
	walk = func(n *command.Node) {
		if n.Permission != "" {
			set[n.Permission] = true
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	for _, n := range roots {
		walk(n)
	}
	out := make([]string, 0, len(set))
	for p := range set {
		out = append(out, p)
	}
	slices.Sort(out)
	return out
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
			value, err := h.runCommand(n.Run, ctx, input)
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

// runCommand runs a plugin command, turning a panic into an error logged
// under the plugin's name with its stack, which the caller answers with
// command.failed.
func (h *pluginHost) runCommand(run func(*plugin.CommandContext) (int, error), ctx *plugin.CommandContext, input string) (value int, err error) {
	defer func() {
		if p := recover(); p != nil {
			perr := pluginruntime.Recovered(p)
			h.Logger().Error("Error in command", "input", input, "error", perr)
			value, err = 0, perr
		}
	}()
	return run(ctx)
}
