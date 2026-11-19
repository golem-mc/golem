package server

import (
	"fmt"
	"slices"
	"strings"

	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/plugin"
)

// pluginsRoots are the roots /plugins runs from: golem:plugins, and plugins
// and pl unless a plugin took them. Every sender may list the plugins.
// vanilla-bukkit: PluginsCommand (bukkit.command.plugins defaults to true)
func (s *Server) pluginsRoots(taken map[string]bool) []*command.Node {
	root := &command.Node{Type: command.NodeLiteral, Plugin: true, Name: "golem:plugins", Executable: true, Handler: s.runPlugins}
	roots := []*command.Node{root}
	for _, name := range []string{"plugins", "pl"} {
		if !taken[name] {
			roots = append(roots, &command.Node{Type: command.NodeLiteral, Plugin: true, Name: name, Redirect: root, Executable: true, Handler: s.runPlugins})
		}
	}
	return roots
}

// runPlugins is golem:plugins: "Plugins (n): " and the plugins by name,
// ignoring case, an enabled one green and one that failed or was disabled
// red. vanilla-bukkit: PluginsCommand.getPluginList
func (s *Server) runPlugins(src command.Source, _ string, _ map[string]command.ArgumentValue) (int32, error) {
	infos := s.plugins.Plugins()
	slices.SortStableFunc(infos, func(a, b plugin.Info) int {
		return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
	var list strings.Builder
	for i, p := range infos {
		if i > 0 {
			list.WriteString("§f, ")
		}
		if p.Enabled {
			list.WriteString("§a")
		} else {
			list.WriteString("§c")
		}
		list.WriteString(p.Name)
	}
	_ = src.Message(legacyText(fmt.Sprintf("Plugins (%d): %s", len(infos), list.String())))
	return 1, nil
}
