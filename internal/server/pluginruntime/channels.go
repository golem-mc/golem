package pluginruntime

import (
	"slices"
	"sync"
	"sync/atomic"

	"github.com/golem-mc/golem/plugin"
)

type pluginChannel struct {
	host *Host
	fn   func(plugin.Player, []byte)
}
type pluginChannels struct {
	active    atomic.Bool
	mu        sync.Mutex
	listeners map[plugin.Key][]pluginChannel
	keys      []plugin.Key
	changed   func(plugin.Key, bool)
}

// ChannelsChanged installs the adapter advertising channel additions and removals.
func (r *Runtime) ChannelsChanged(fn func(plugin.Key, bool)) { r.channels.changed = fn }

// HasChannels is the allocation-free custom-payload hook gate.
func (r *Runtime) HasChannels() bool { return r.channels.active.Load() }

// HasChannel reports whether key has listeners before a packet is copied or queued.
func (r *Runtime) HasChannel(key plugin.Key) bool {
	r.channels.mu.Lock()
	defer r.channels.mu.Unlock()
	return len(r.channels.listeners[key]) != 0
}

// ChannelKeys returns distinct registered keys in registration order.
func (r *Runtime) ChannelKeys() []plugin.Key {
	r.channels.mu.Lock()
	defer r.channels.mu.Unlock()
	return slices.Clone(r.channels.keys)
}

// ListenChannel registers a listener owned by this host.
func (h *Host) ListenChannel(key plugin.Key, fn func(plugin.Player, []byte)) {
	if _, err := plugin.ParseKey(key.String()); err != nil || fn == nil || key == plugin.MinecraftKey("register") || key == plugin.MinecraftKey("unregister") {
		return
	}
	channels := &h.runtime.channels
	channels.mu.Lock()
	if h.closed.Load() {
		channels.mu.Unlock()
		return
	}
	if channels.listeners == nil {
		channels.listeners = make(map[plugin.Key][]pluginChannel)
	}
	first := len(channels.listeners[key]) == 0
	if first {
		channels.keys = append(channels.keys, key)
	}
	channels.listeners[key] = append(channels.listeners[key], pluginChannel{h, fn})
	channels.active.Store(true)
	channels.mu.Unlock()
	if first && channels.changed != nil {
		channels.changed(key, true)
	}
}

// DeliverChannel invokes matching listeners with isolated data and owner-bound handles.
func (r *Runtime) DeliverChannel(key plugin.Key, data []byte, player func(plugin.Server) plugin.Player) {
	r.channels.mu.Lock()
	listeners := slices.Clone(r.channels.listeners[key])
	r.channels.mu.Unlock()
	for _, l := range listeners {
		if l.host.closed.Load() {
			continue
		}
		if err := l.host.guard(func() error { l.fn(player(l.host.api), slices.Clone(data)); return nil }); err != nil {
			l.host.Logger().Error("Error in plugin channel", "channel", key.String(), "error", err)
		}
	}
}

func (h *Host) removeChannels() {
	channels := &h.runtime.channels
	channels.mu.Lock()
	var removed []plugin.Key
	for _, key := range channels.keys {
		listeners := slices.DeleteFunc(channels.listeners[key], func(l pluginChannel) bool { return l.host == h })
		if len(listeners) == 0 {
			delete(channels.listeners, key)
			removed = append(removed, key)
		} else {
			channels.listeners[key] = listeners
		}
	}
	channels.keys = slices.DeleteFunc(channels.keys, func(key plugin.Key) bool {
		return len(channels.listeners[key]) == 0
	})
	channels.active.Store(len(channels.listeners) != 0)
	channels.mu.Unlock()
	if channels.changed != nil {
		for _, key := range removed {
			channels.changed(key, false)
		}
	}
}
