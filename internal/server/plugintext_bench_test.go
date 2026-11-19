package server

import (
	"testing"

	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

func BenchmarkPluginTextConversion(b *testing.B) {
	for name, text := range map[string]plugin.Text{
		"plain":  plugin.Plain("Welcome"),
		"styled": plugin.Translate("test.welcome", plugin.Plain("Tester").Color("gold")).Bold().NotItalic().Append(plugin.Text{Keybind: "key.jump"}),
		"item":   plugin.Plain("Diamond").OnHover(plugin.Plain("Tooltip")),
	} {
		if name == "item" {
			text.Style.Hover = &plugin.Hover{Item: &plugin.ItemStack{Item: registry.ItemDiamond, Count: 2}}
		}
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				_ = pluginProtocolText(text)
			}
		})
	}
}

func BenchmarkPluginMOTDServerData(b *testing.B) {
	s := &Server{}
	h := &pluginHost{srv: s}
	h.SetMOTD(plugin.Translate("test.welcome", plugin.Plain("Tester").Color("gold")).Bold().NotItalic())
	c := &client{srv: s}
	b.ReportAllocs()
	for b.Loop() {
		_ = c.serverDataPayload()
	}
}
