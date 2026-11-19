package server

import (
	"slices"
	"strings"

	"github.com/golem-mc/golem/plugin"
)

type pluginMessenger struct{ host *pluginHost }

func (h *pluginHost) Messenger() plugin.Messenger { return pluginMessenger{h} }
func (m pluginMessenger) Listen(key plugin.Key, fn func(plugin.Player, []byte)) {
	m.host.host.ListenChannel(key, fn)
}
func (m pluginMessenger) Send(p plugin.Player, key plugin.Key, data []byte) {
	switch p := p.(type) {
	case pluginPlayer:
		if p.c.srv == m.host.srv {
			p.SendPluginMessage(key, data)
		}
	case pluginConnectionPlayer:
		if p.c.srv == m.host.srv {
			p.SendPluginMessage(key, data)
		}
	}
}

func (s *Server) pluginChannelsChanged(key plugin.Key, registered bool) {
	// Later joins receive the current snapshot, never queued advertisements from Enable.
	clients := s.playerClients()
	if len(clients) == 0 {
		return
	}
	s.plugins.Enqueue(func() {
		channel := plugin.MinecraftKey("register")
		if !registered {
			channel = plugin.MinecraftKey("unregister")
		}
		for _, c := range clients {
			(pluginPlayer{c: c}).SendPluginMessage(channel, []byte(key.String()))
		}
	})
}
func (c *client) sendPluginChannels() {
	if c.srv.plugins == nil || !c.srv.plugins.HasChannels() {
		return
	}
	var keys []string
	for _, key := range c.srv.plugins.ChannelKeys() {
		keys = append(keys, key.String())
	}
	(pluginPlayer{c: c}).SendPluginMessage(plugin.MinecraftKey("register"), []byte(strings.Join(keys, "\x00")))
}
func (c *client) pluginChannelMessage(channel string, data []byte) {
	if c.srv.plugins == nil || !c.srv.plugins.HasChannels() {
		return
	}
	key, err := plugin.ParseKey(channel)
	if err != nil || !c.srv.plugins.HasChannel(key) {
		return
	}
	payload := slices.Clone(data)
	deliver := func() {
		if !c.connectionPlayer().IsValid() {
			return
		}
		c.srv.plugins.DeliverChannel(key, payload, func(api plugin.Server) plugin.Player {
			return bindPluginPlayer(c.connectionPlayer(), api.(*pluginHost))
		})
	}
	if c.pluginConnecting.Load() {
		c.srv.plugins.Call(deliver)
	} else {
		c.srv.plugins.Enqueue(deliver)
	}
}
