package server

import (
	"math"
	"reflect"
	"slices"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/plugin"
	"github.com/google/uuid"
)

func (h *pluginHost) BossBars() map[plugin.Key]plugin.BossBar {
	out := make(map[plugin.Key]plugin.BossBar)
	for _, bar := range h.srv.bossBars.All() {
		key, _ := plugin.ParseKey(bar.ID)
		out[key] = &pluginBossBar{host: h, id: bar.ID, wire: bar.Wire}
	}
	return out
}
func (h *pluginHost) CreateBossBar(title plugin.Text, color plugin.BossBarColor, style plugin.BossBarStyle) plugin.BossBar {
	if color > plugin.BarWhite {
		color = plugin.BarWhite
	}
	if style > plugin.BarNotched20 {
		style = plugin.BarProgress
	}
	b := &pluginBossBar{host: h, progress: 1, viewers: make(map[uuid.UUID]int32), private: &world.BossBar{Wire: uuid.New(), Name: protocol.TextTag(pluginProtocolText(title)), Visible: true, Color: int32(color), Overlay: int32(style)}}
	h.host.Own(b.Remove)
	return b
}

type pluginBossBar struct {
	host    *pluginHost
	id      string
	wire    uuid.UUID
	private *world.BossBar
	// A UUID survives reconnecting; the entity ID identifies this connection.
	viewers  map[uuid.UUID]int32
	progress float32
	removed  bool
}

func (b *pluginBossBar) snapshot() (world.BossBar, bool) {
	if b.removed {
		return world.BossBar{}, false
	}
	if b.private != nil {
		return *b.private, true
	}
	bar, ok := b.host.srv.bossBars.Get(b.id)
	if !ok || bar.Wire != b.wire {
		return world.BossBar{}, false
	}
	return bar, true
}
func (b *pluginBossBar) change(fn func(*world.BossBar) bool) world.BossBarChange {
	if b.removed {
		return world.BossBarChange{}
	}
	if b.private == nil {
		return b.host.srv.bossBars.Update(b.id, b.wire, fn)
	}
	before := *b.private
	before.Players = slices.Clone(before.Players)
	changed := fn(b.private)
	return world.BossBarChange{Existed: true, Changed: changed, Before: before, After: *b.private}
}
func (b *pluginBossBar) send(players []uuid.UUID, bar world.BossBar, action int32) {
	progress := bar.Fill()
	if b.private != nil {
		progress = b.progress
	}
	data := bossEventPayloadProgress(bar, action, progress)
	for _, id := range players {
		if c := b.host.srv.clientByUUID(id); c != nil && (b.private == nil || b.viewers[id] == c.player.EntityID) {
			c.send(protocol.IDPlayBossEvent, data)
		}
	}
}
func (b *pluginBossBar) update(action int32, fn func(*world.BossBar) bool) {
	ch := b.change(fn)
	if ch.Changed && ch.After.Visible {
		b.send(ch.After.Players, ch.After, action)
	}
}
func (b *pluginBossBar) Title() plugin.Text { bar, _ := b.snapshot(); return pluginScoreText(bar.Name) }
func (b *pluginBossBar) SetTitle(v plugin.Text) {
	name := protocol.TextTag(pluginProtocolText(v))
	b.update(protocol.BossEventName, func(bar *world.BossBar) bool {
		if reflect.DeepEqual(bar.Name, name) {
			return false
		}
		bar.Name = name
		return true
	})
}
func (b *pluginBossBar) Progress() float32 {
	bar, ok := b.snapshot()
	if !ok {
		return 0
	}
	if b.private != nil {
		return b.progress
	}
	return bar.Fill()
}
func (b *pluginBossBar) SetProgress(v float32) {
	if math.IsNaN(float64(v)) {
		return
	}
	v = min(max(v, 0), 1)
	b.update(protocol.BossEventProgress, func(bar *world.BossBar) bool {
		if b.private != nil {
			if b.progress == v {
				return false
			}
			b.progress = v
			return true
		}
		// CustomBossEvent keeps progress as an integer value over its maximum.
		value := int32(math.Round(float64(v) * float64(bar.Max)))
		if bar.Value == value {
			return false
		}
		bar.Value = value
		return true
	})
}
func (b *pluginBossBar) Color() plugin.BossBarColor {
	bar, _ := b.snapshot()
	return plugin.BossBarColor(bar.Color)
}
func (b *pluginBossBar) SetColor(v plugin.BossBarColor) {
	if v > plugin.BarWhite {
		return
	}
	b.update(protocol.BossEventStyle, func(bar *world.BossBar) bool {
		if bar.Color == int32(v) {
			return false
		}
		bar.Color = int32(v)
		return true
	})
}
func (b *pluginBossBar) Style() plugin.BossBarStyle {
	bar, _ := b.snapshot()
	return plugin.BossBarStyle(bar.Overlay)
}
func (b *pluginBossBar) SetStyle(v plugin.BossBarStyle) {
	if v > plugin.BarNotched20 {
		return
	}
	b.update(protocol.BossEventStyle, func(bar *world.BossBar) bool {
		if bar.Overlay == int32(v) {
			return false
		}
		bar.Overlay = int32(v)
		return true
	})
}
func (b *pluginBossBar) Flags() (bool, bool, bool) {
	bar, _ := b.snapshot()
	return bar.DarkenScreen, bar.PlayMusic, bar.CreateFog
}
func (b *pluginBossBar) SetFlags(darken, music, fog bool) {
	b.update(protocol.BossEventProperties, func(bar *world.BossBar) bool {
		if bar.DarkenScreen == darken && bar.PlayMusic == music && bar.CreateFog == fog {
			return false
		}
		bar.DarkenScreen, bar.PlayMusic, bar.CreateFog = darken, music, fog
		return true
	})
}
func (b *pluginBossBar) Visible() bool { bar, ok := b.snapshot(); return ok && bar.Visible }
func (b *pluginBossBar) SetVisible(v bool) {
	ch := b.change(func(bar *world.BossBar) bool {
		if bar.Visible == v {
			return false
		}
		bar.Visible = v
		return true
	})
	if ch.Changed {
		action := int32(protocol.BossEventRemove)
		if v {
			action = protocol.BossEventAdd
		}
		b.send(ch.After.Players, ch.After, action)
	}
}
func (b *pluginBossBar) Players() []plugin.Player {
	bar, _ := b.snapshot()
	out := make([]plugin.Player, 0, len(bar.Players))
	for _, id := range bar.Players {
		if p, ok := b.host.Player(id); ok && (b.private == nil || b.viewers[id] == p.(pluginPlayer).c.player.EntityID) {
			out = append(out, p)
		}
	}
	return out
}
func (b *pluginBossBar) AddPlayer(p plugin.Player) {
	player, ok := p.(pluginPlayer)
	if !ok || player.c.srv != b.host.srv || !player.IsValid() {
		return
	}
	ch := b.change(func(bar *world.BossBar) bool {
		if slices.Contains(bar.Players, p.UUID()) {
			if b.private != nil && b.viewers[p.UUID()] != player.c.player.EntityID {
				b.viewers[p.UUID()] = player.c.player.EntityID
				return true
			}
			return false
		}
		if b.private != nil {
			b.viewers[p.UUID()] = player.c.player.EntityID
		}
		bar.Players = append(bar.Players, p.UUID())
		return true
	})
	if ch.Changed && ch.After.Visible {
		b.send([]uuid.UUID{p.UUID()}, ch.After, protocol.BossEventAdd)
	}
}
func (b *pluginBossBar) RemovePlayer(p plugin.Player) {
	if p == nil {
		return
	}
	ch := b.change(func(bar *world.BossBar) bool {
		if !slices.Contains(bar.Players, p.UUID()) {
			return false
		}
		bar.Players = slices.DeleteFunc(bar.Players, func(id uuid.UUID) bool { return id == p.UUID() })
		return true
	})
	if ch.Changed && ch.After.Visible {
		b.send([]uuid.UUID{p.UUID()}, ch.After, protocol.BossEventRemove)
	}
}
func (b *pluginBossBar) Remove() {
	bar, ok := b.snapshot()
	if !ok {
		return
	}
	if b.private == nil {
		b.host.srv.bossBars.Remove(b.id)
	}
	if bar.Visible {
		b.send(bar.Players, bar, protocol.BossEventRemove)
	}
	b.removed = true
	if b.private != nil {
		clear(b.viewers)
		b.private.Players = nil
	}
}
