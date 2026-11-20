package server

import (
	"encoding/json"
	"slices"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/advancement"
	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/datapack"
	"github.com/golem-mc/golem/plugin"
)

func (h *pluginHost) DataPacks() []plugin.DataPack {
	p := h.srv.packs
	if p == nil {
		return nil
	}
	selected := p.Selected()
	available := p.Available()
	out := make([]plugin.DataPack, 0, len(available))
	for _, pk := range selected {
		out = append(out, pluginDataPack(pk, true))
	}
	for _, pk := range available {
		if !slices.ContainsFunc(selected, func(s datapack.Pack) bool { return s.ID == pk.ID }) {
			out = append(out, pluginDataPack(pk, false))
		}
	}
	return out
}

func pluginDataPack(p datapack.Pack, enabled bool) plugin.DataPack {
	title := plugin.Plain(p.Title)
	if p.TitleKey {
		title = plugin.Translate(p.Title)
	}
	return plugin.DataPack{
		ID:          p.ID,
		Title:       title,
		Description: pluginPackDescription(p),
		Source:      plugin.DataPackSource(p.Source),
		Enabled:     enabled,
	}
}

func pluginPackDescription(p datapack.Pack) plugin.Text {
	var s string
	if json.Unmarshal(p.Description, &s) == nil {
		if p.DescriptionKey {
			return plugin.Translate(s)
		}
		return plugin.Plain(s)
	}
	tag, _, err := protocol.ParseTextJSON(string(p.Description))
	if err != nil {
		return plugin.Text{}
	}
	text, _ := pluginTextOf(tag)
	return text
}

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
