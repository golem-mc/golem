package server

import (
	"fmt"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/pluginruntime"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/plugin"
)

func (c *client) enqueuePluginNotification(event plugin.Event) {
	notify := func() {
		if c.connectionPlayer().IsValid() {
			c.srv.plugins.Fire(event)
		}
	}
	if c.pluginConnecting.Load() {
		c.srv.plugins.Call(notify)
	} else {
		c.srv.plugins.Enqueue(notify)
	}
}

// handleCustomClick leaves the payload as network NBT, matching plugin.Click.Payload.
// vanilla: ServerboundCustomClickActionPacket.STREAM_CODEC
func (c *client) handleCustomClick(r *protocol.Reader) error {
	if c.srv.plugins == nil || !c.srv.plugins.Has(pluginruntime.PlayerCustomClick) {
		r.Rest()
		return nil
	}
	id, err := plugin.ParseKey(r.ReadString(32767))
	data := r.ReadBytes(65536)
	if err != nil || r.Err() != nil {
		return c.decodeFailure("custom_click_action", fmt.Errorf("invalid custom click: %v, %v", err, r.Err()))
	}
	if len(data) == 1 && data[0] == 0 {
		data = nil
	} else {
		_, consumed, decodeErr := nbt.DecodeNetworkLimits(data, 32768, 16)
		if decodeErr != nil || consumed != len(data) {
			return c.decodeFailure("custom_click_action", fmt.Errorf("invalid custom click payload"))
		}
	}
	c.enqueuePluginNotification(&plugin.PlayerCustomClickEvent{Player: c.connectionPlayer(), ID: id, Payload: data})
	return nil
}

func (c *client) pluginSignClick(event nbt.Compound) {
	if c.srv.plugins == nil || !c.srv.plugins.Has(pluginruntime.PlayerCustomClick) {
		return
	}
	id, _ := nbt.Get[nbt.String](event, "id")
	key, err := plugin.ParseKey(string(id))
	if err != nil {
		return
	}
	var data []byte
	if payload := event["payload"]; payload != nil {
		data, _ = nbt.EncodeNetwork(payload)
	}
	c.enqueuePluginNotification(&plugin.PlayerCustomClickEvent{Player: pluginPlayer{c: c}, ID: key, Payload: data})
}
