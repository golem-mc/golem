package server

import (
	"log/slog"
	"net"
	"reflect"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/chatsecurity"
	"github.com/golem-mc/golem/internal/server/pluginruntime"
	"github.com/golem-mc/golem/plugin"
)

// enqueueChatRelay retains a connection's last continuation so subsequent commands
// cannot relay a newer signed link or failure before it finishes.
func (c *client) enqueueChatRelay(fn func()) {
	done := make(chan struct{})
	c.chatRelayDone = done
	c.srv.plugins.Enqueue(func() { defer close(done); fn() })
}

// awaitChatRelay waits without holding locks needed by listeners or renderers.
func (c *client) awaitChatRelay() error {
	if c.chatRelayDone == nil {
		return nil
	}
	select {
	case <-c.chatRelayDone:
		c.chatRelayDone = nil
		return nil
	case <-c.srv.stopping:
		return net.ErrClosed
	}
}

// pluginChat relays content as sender's chat line, after signature validation
// and with no chat-order or server lock held; signed is the message the client
// sent, nil for text a plugin made the player say, which goes out as unsigned
// system chat.
func (s *Server) pluginChat(sender *client, content string, signed *chatsecurity.Message) {
	p := pluginPlayer{c: sender}
	if !p.IsValid() || sender.kicked.Load() {
		return
	}
	event := &plugin.PlayerChatEvent{Player: p, Message: content}
	for _, c := range s.chatRecipients() {
		event.Recipients = append(event.Recipients, pluginPlayer{c: c})
	}
	s.plugins.Fire(event)
	if !event.Cancelled() {
		name := sender.DisplayName()
		changed := signed == nil || event.Message != content || event.Format != nil
		if event.Format != nil {
			// The console shows the line the format renders (viewed by the
			// sender), as Bukkit logs the formatted message.
			fallback := protocol.Text{Translate: chatDecorations[chatType], With: []protocol.Text{name, {Text: event.Message}}}
			line := renderPluginChat(event.Format, event.Player, plugin.Plain(event.Message), event.Player, fallback)
			slog.Info(line.Plain(), protocol.LogComponentKey, line)
		} else {
			s.logChatMessage(chatType, name, protocol.Text{Text: event.Message}, !changed && signed.Signature() != nil)
		}
		// Render all recipients before acquiring chatOrderMu: a renderer may call back into the server.
		type delivery struct {
			c    *client
			text protocol.Text
		}
		var deliveries []delivery
		seen := make(map[*client]bool, len(event.Recipients))
		for _, recipient := range event.Recipients {
			viewer, ok := recipient.(pluginPlayer)
			if !ok || viewer.c.srv != s || !viewer.IsValid() || !viewer.c.acceptsChatMessages() || seen[viewer.c] {
				continue
			}
			seen[viewer.c] = true
			text := protocol.Text{Translate: chatDecorations[chatType], With: []protocol.Text{name, {Text: event.Message}}}
			if event.Format != nil {
				text = renderPluginChat(event.Format, event.Player, plugin.Plain(event.Message), viewer, text)
			}
			deliveries = append(deliveries, delivery{viewer.c, text})
		}
		s.chatOrderMu.Lock()
		for _, d := range deliveries {
			if changed {
				if d.c.sendOutboundIn(nil, outbound{id: protocol.IDPlaySystemChat, data: systemChatPayload(d.text, false), playerChat: true}) {
					d.c.out.signal()
				}
			} else {
				d.c.sendVerifiedChat(chatType, name, *signed, protocol.Text{Text: event.Message}, nil)
			}
		}
		s.chatOrderMu.Unlock()
	}
	if signed != nil {
		sender.detectChatRateSpam()
	}
}

// pluginBroadcast is called on the tick goroutine by the plugin API and /say continuation.
func (s *Server) pluginBroadcast(text *protocol.Text) bool {
	if s.plugins == nil || !s.plugins.Has(pluginruntime.BroadcastMessage) {
		return true
	}
	msg, _ := pluginText(*text)
	event := &plugin.BroadcastMessageEvent{Message: pluginruntime.CloneText(msg)}
	s.plugins.Fire(event)
	if event.Cancelled() {
		return false
	}
	if !reflect.DeepEqual(msg, event.Message) {
		*text = pluginProtocolText(event.Message)
	}
	return true
}

func (s *Server) pluginSay(kind string, sender, resolved protocol.Text, message *chatsecurity.Message, enqueue func(func())) bool {
	if kind != "minecraft:say_command" || s.plugins == nil || !s.plugins.Has(pluginruntime.BroadcastMessage) {
		return false
	}
	enqueue(func() {
		original := protocol.Text{Translate: chatDecorations[kind], With: []protocol.Text{sender, resolved}}
		text := original
		if !s.pluginBroadcast(&text) {
			return
		}
		if reflect.DeepEqual(original, text) {
			s.relayCommandChat(kind, sender, message, resolved)
			return
		}
		slog.Info(text.Plain(), protocol.LogComponentKey, text)
		payload := systemChatPayload(text, false)
		for _, c := range s.chatRecipients() {
			if c.sendOutboundIn(nil, outbound{id: protocol.IDPlaySystemChat, data: payload, playerChat: true}) {
				c.out.signal()
			}
		}
	})
	return true
}

func bindPluginEvent(event plugin.Event, api plugin.Server) func() {
	host := api.(*pluginHost)
	switch event := event.(type) {
	case *plugin.PlayerCommandEvent:
		event.Player = bindPluginPlayer(event.Player, host)
	case *plugin.PlayerCommandSendEvent:
		event.Player = bindPluginPlayer(event.Player, host)
	case *plugin.PlayerChatEvent:
		if p, ok := event.Player.(pluginPlayer); ok {
			p.host = host
			event.Player = p
		}
		for i, recipient := range event.Recipients {
			if p, ok := recipient.(pluginPlayer); ok {
				p.host = host
				event.Recipients[i] = p
			}
		}
		return func() {
			if format := event.Format; format != nil {
				// Wrapping after each listener retains the renderer's owner even if a later listener only observes it.
				event.Format = func(sender plugin.Player, message plugin.Text, viewer plugin.Player) plugin.Text {
					return format(bindPluginPlayer(sender, host), message, bindPluginPlayer(viewer, host))
				}
			}
		}
	}
	switch e := event.(type) {
	case *plugin.WorldLoadEvent:
		e.World = bindPluginWorld(e.World, host)
	case *plugin.WorldUnloadEvent:
		e.World = bindPluginWorld(e.World, host)
	case *plugin.WorldSaveEvent:
		e.World = bindPluginWorld(e.World, host)
	case *plugin.SpawnChangeEvent:
		e.World = bindPluginWorld(e.World, host)
		e.Previous.World = bindPluginWorld(e.Previous.World, host)
	case *plugin.TimeSkipEvent:
		e.World = bindPluginWorld(e.World, host)
	case *plugin.ChunkLoadEvent:
		e.Chunk = bindPluginChunk(e.Chunk, host)
	case *plugin.ChunkUnloadEvent:
		e.Chunk = bindPluginChunk(e.Chunk, host)
	case *plugin.ChunkPopulateEvent:
		e.Chunk = bindPluginChunk(e.Chunk, host)
	case *plugin.LootGenerateEvent:
		e.World = bindPluginWorld(e.World, host)
	case *plugin.PortalCreateEvent:
		e.World = bindPluginWorld(e.World, host)
	case *plugin.WeatherChangeEvent:
		e.World = bindPluginWorld(e.World, host)
	case *plugin.LightningStrikeEvent:
		e.World = bindPluginWorld(e.World, host)
		if b, ok := e.Lightning.(pluginEntity); ok {
			b.host = host
			e.Lightning = b
		}
	case *plugin.EntityDamageEvent:
		if l, ok := e.Entity.(plugin.LivingEntity); ok {
			e.Entity = bindPluginLiving(l, host)
		} else {
			e.Entity = bindPluginEntity(e.Entity, host)
		}
		bindPluginSource(&e.Source, host)
	case *plugin.ItemDespawnEvent:
		e.Item = bindPluginItem(e.Item, host)
	case *plugin.EntityDropItemEvent:
		if p, ok := e.Entity.(pendingMob); ok {
			e.Entity = bindPending(e.Entity, p.id, host)
		}
		e.Item = bindPluginItem(e.Item, host)
	case *plugin.EntityRemoveEvent:
		if h, ok := e.Entity.(pluginEntity); ok {
			h.host = host
			e.Entity = h
		}
	case *plugin.ExplosionPrimeEvent:
		e.Entity = bindPluginEntity(e.Entity, host)
	case *plugin.EntityExplodeEvent:
		e.Entity = bindPluginEntity(e.Entity, host)
		for i := range e.Blocks {
			e.Blocks[i].World = bindPluginWorld(e.Blocks[i].World, host)
		}
	case *plugin.EntityBreedEvent:
		e.Mother, e.Father = bindPluginLiving(e.Mother, host), bindPluginLiving(e.Father, host)
		if e.Breeder != nil {
			e.Breeder = bindPluginPlayer(e.Breeder, host)
		}
	case *plugin.PiglinBarterEvent:
		if p, ok := e.Piglin.(pendingMob); ok {
			e.Piglin = bindPending(e.Piglin, p.id, host)
		}
	case *plugin.HangingPlaceEvent:
		if p, ok := e.Entity.(pendingHanging); ok {
			e.Entity = bindPending[plugin.Hanging](e.Entity, p.id, host)
		}
		e.Player = bindPluginPlayer(e.Player, host)
		e.Block.World = bindPluginWorld(e.Block.World, host)
	case *plugin.EntityTransformEvent:
		if p, ok := e.Entity.(pendingMob); ok {
			e.Entity = bindPending(e.Entity, p.id, host)
		}
	case *plugin.EntityChangeBlockEvent:
		if p, ok := e.Entity.(pendingMob); ok {
			e.Entity = bindPending(e.Entity, p.id, host)
		}
		e.Block.World = bindPluginWorld(e.Block.World, host)
	case *plugin.EntityBreakDoorEvent:
		if p, ok := e.Entity.(pendingMob); ok {
			e.Entity = bindPending(e.Entity, p.id, host)
		}
		e.Block.World = bindPluginWorld(e.Block.World, host)
	case *plugin.PlayerPickupArrowEvent:
		e.Player = bindPluginPlayer(e.Player, host)
		e.Arrow = bindPluginEntity(e.Arrow, host)
	case *plugin.EntityCombustEvent:
		if p, ok := e.Entity.(pendingMob); ok {
			e.Entity = bindPending(e.Entity, p.id, host)
		}
		e.Entity = bindPluginEntity(e.Entity, host)
	case *plugin.HangingBreakEvent:
		if p, ok := e.Entity.(pendingHanging); ok {
			e.Entity = bindPending[plugin.Hanging](e.Entity, p.id, host)
		}
		if e.Remover != nil {
			e.Remover = bindPluginEntity(e.Remover, host)
		}
	case *plugin.VillagerCareerChangeEvent:
		if p, ok := e.Villager.(pendingVillager); ok {
			e.Villager = bindPending[plugin.Villager](e.Villager, p.id, host)
		}
	case *plugin.VillagerAcquireTradeEvent:
		if p, ok := e.Villager.(pendingVillager); ok {
			e.Villager = bindPending[plugin.Villager](e.Villager, p.id, host)
		}
	case *plugin.PlayerTradeEvent:
		e.Player = bindPluginPlayer(e.Player, host)
		if m, ok := e.Merchant.(pluginLivingEntity); ok {
			e.Merchant = bindPluginLiving(m, host)
		}
	case *plugin.RaidEvent:
		e.World = bindPluginWorld(e.World, host)
		if e.Player != nil {
			e.Player = bindPluginPlayer(e.Player, host)
		}
		for i := range e.Winners {
			e.Winners[i] = bindPluginPlayer(e.Winners[i], host)
		}
	case *plugin.ItemMergeEvent:
		e.Item, e.Target = bindPluginItem(e.Item, host), bindPluginItem(e.Target, host)
	case *plugin.FoodLevelChangeEvent:
		e.Player = bindPluginPlayer(e.Player, host)
	case *plugin.EntityRegainHealthEvent:
		e.Entity = bindPluginLiving(e.Entity, host)
	case *plugin.EntityResurrectEvent:
		e.Entity = bindPluginLiving(e.Entity, host)
	case *plugin.EntityDeathEvent:
		e.Entity = bindPluginLiving(e.Entity, host)
		bindPluginSource(&e.Source, host)
	case *plugin.PlayerDeathEvent:
		e.Player = bindPluginPlayer(e.Player, host)
		e.Entity = e.Player
		bindPluginSource(&e.Source, host)
	case *plugin.ProjectileLaunchEvent, *plugin.ProjectileHitEvent, *plugin.EntityTargetEvent, *plugin.EntitySpawnEvent, *plugin.EntityInteractEvent, *plugin.EntityPotionEffectEvent, *plugin.EntityToggleGlideEvent, *plugin.EntityMountEvent, *plugin.EntityDismountEvent, *plugin.VehicleEvent, *plugin.SheepDyeWoolEvent, *plugin.ExpBottleEvent, *plugin.EntityEnterLoveModeEvent, *plugin.EntityAirChangeEvent, *plugin.EntityShootBowEvent, *plugin.EntityTameEvent, *plugin.SlimeSplitEvent, *plugin.CreeperPowerEvent:
		bindPluginVehicleEvents(event, host)
	case *plugin.PlayerJoinEvent:
		e.Player = bindPluginPlayer(e.Player, host)
	case *plugin.PlayerQuitEvent:
		e.Player = bindPluginPlayer(e.Player, host)
	case *plugin.PlayerKickEvent:
		e.Player = bindPluginPlayer(e.Player, host)
	case *plugin.PlayerLoginEvent:
		e.Player = bindPluginPlayer(e.Player, host)
	case *plugin.PlayerConfigureEvent:
		e.Player = bindPluginPlayer(e.Player, host)
	case *plugin.PlayerClientSettingsEvent:
		e.Player = bindPluginPlayer(e.Player, host)
	case *plugin.PlayerResourcePackStatusEvent:
		e.Player = bindPluginPlayer(e.Player, host)
	case *plugin.PlayerCustomClickEvent:
		e.Player = bindPluginPlayer(e.Player, host)
	case *plugin.InventoryOpenEvent:
		e.Player = bindPluginPlayer(e.Player, host)
	case *plugin.InventoryCloseEvent:
		e.Player = bindPluginPlayer(e.Player, host)
	case *plugin.InventoryClickEvent:
		e.Player = bindPluginPlayer(e.Player, host)
	}
	return nil
}

func bindPluginPlayer(player plugin.Player, host *pluginHost) plugin.Player {
	if p, ok := player.(pluginConnectionPlayer); ok {
		p.host = host
		return p
	}
	if p, ok := player.(pluginPlayer); ok {
		p.host = host
		return p
	}
	return player
}

func renderPluginChat(format func(plugin.Player, plugin.Text, plugin.Player) plugin.Text, sender plugin.Player, message plugin.Text, viewer plugin.Player, fallback protocol.Text) (text protocol.Text) {
	text = fallback
	defer func() {
		if failure := recover(); failure != nil {
			slog.Error("Error in chat renderer", "error", failure)
		}
	}()
	return pluginProtocolText(format(sender, message, viewer))
}

func (h *pluginHost) enqueueBroadcast(msg plugin.Text) {
	text := pluginProtocolText(msg)
	h.srv.plugins.Enqueue(func() {
		if h.srv.pluginBroadcast(&text) {
			h.srv.broadcastSystemMessage(text)
		}
	})
}

func bindPluginWorld(w plugin.World, host *pluginHost) plugin.World {
	if pw, ok := w.(pluginWorld); ok {
		pw.host = host
		return pw
	}
	return w
}

func bindPluginChunk(c plugin.Chunk, host *pluginHost) plugin.Chunk {
	if pc, ok := c.(pluginChunk); ok {
		pc.world.host = host
		return pc
	}
	return c
}
