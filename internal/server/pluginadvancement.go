package server

import (
	"github.com/golem-mc/golem/internal/server/advancement"
	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/plugin"
)

func (h *pluginHost) Advancement(key plugin.Key) (plugin.Advancement, bool) {
	if n := advancement.Current().Get(key.String()); n != nil {
		return pluginAdvancement{n}, true
	}
	return nil, false
}

type pluginAdvancement struct{ node *advancement.Node }

func (a pluginAdvancement) Key() plugin.Key { key, _ := plugin.ParseKey(a.node.ID); return key }
func (a pluginAdvancement) Title() plugin.Text {
	if d := a.node.Display; d != nil && !d.Hidden {
		text, _ := pluginText(command.DisplayText(d.Title, d.TitleLiteral))
		return text
	}
	return plugin.Text{}
}
func (a pluginAdvancement) Description() plugin.Text {
	if d := a.node.Display; d != nil && !d.Hidden {
		text, _ := pluginText(command.DisplayText(d.Description, d.DescriptionLiteral))
		return text
	}
	return plugin.Text{}
}
func (a pluginAdvancement) Criteria() []string {
	out := make([]string, len(a.node.Criteria))
	for i, c := range a.node.Criteria {
		out[i] = c.Name
	}
	return out
}
func (a pluginAdvancement) Parent() (plugin.Advancement, bool) {
	if n := a.node.ParentNode; n != nil {
		return pluginAdvancement{n}, true
	}
	return nil, false
}
