package server

import (
	"maps"
	"net"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/pluginruntime"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/plugin"
	"github.com/google/uuid"
)

// pluginPlayer binds a handle to a connection, never to a reusable UUID lookup.
type pluginPlayer struct {
	host *pluginHost
	c    *client
}

// hostOrAdapter is the listener's host the handle was bound to, else the
// adapter, so a handle built from a connection alone works too.
func (p pluginPlayer) hostOrAdapter() *pluginHost {
	if p.host != nil {
		return p.host
	}
	return p.c.srv.pluginAdapter
}

// ArrowsInBody is the arrows stuck in the player.
// vanilla: LivingEntity.getArrowCount
func (p pluginPlayer) ArrowsInBody() int { return p.c.player.Vitals().ArrowCount }

func (p pluginPlayer) SetArrowsInBody(n int) {
	p.c.player.UpdateVitals(func(v *player.Vitals) { v.ArrowCount = max(n, 0) })
}

// ActiveItem is the item the player is using: a drawn bow or food being
// eaten. A raised trident or shield has no use session here and reads as
// not using.
// vanilla: LivingEntity.getUseItem, getUsedItemHand, isUsingItem
func (p pluginPlayer) ActiveItem() (plugin.ItemStack, plugin.Hand, bool) {
	p.c.eatMu.Lock()
	eating := p.c.eat
	p.c.eatMu.Unlock()
	p.c.bowMu.Lock()
	drawing := p.c.bow
	p.c.bowMu.Unlock()
	handID := drawing.hand
	switch {
	case eating.ticks > 0:
		handID = eating.hand
	case drawing.ticks <= 0:
		return plugin.ItemStack{}, plugin.MainHand, false
	}
	var held protocol.ItemStack
	p.c.player.Inventory(func(inv *player.Inventory) {
		held = inv.Slots[rules.SlotOfHand(inv, handID)]
	})
	hand := plugin.MainHand
	if handID == protocol.HandOff {
		hand = plugin.OffHand
	}
	return pluginStack(held), hand, !held.Empty()
}

func (h *pluginHost) Players() []plugin.Player {
	players := make([]plugin.Player, 0)
	for _, c := range h.srv.playerClients() {
		p := pluginPlayer{host: h, c: c}
		if p.IsValid() {
			players = append(players, p)
		}
	}
	return players
}
func (h *pluginHost) Player(id plugin.UUID) (plugin.Player, bool) {
	if c := h.srv.clientByUUID(uuid.UUID(id)); c != nil {
		p := pluginPlayer{host: h, c: c}
		if p.IsValid() {
			return p, true
		}
	}
	return nil, false
}
func (h *pluginHost) PlayerNamed(name string) (plugin.Player, bool) {
	for _, p := range h.Players() {
		if strings.EqualFold(p.Name(), name) {
			return p, true
		}
	}
	return nil, false
}
func (h *pluginHost) Broadcast(msg plugin.Text) {
	if h.srv.plugins != nil && h.srv.plugins.Has(pluginruntime.BroadcastMessage) {
		h.enqueueBroadcast(msg)
		return
	}
	h.srv.broadcastSystemMessage(pluginProtocolText(msg))
}

// Remove leaves the player in place: /kill's removal is a death for them.
func (p pluginPlayer) Remove() {}

func (p pluginPlayer) IsValid() bool {
	if p.c.srv.plugins != nil && p.c.pluginConnectionPhase.Load() != int32(protocol.StatePlay) {
		return false
	}
	return !p.c.leaving.Load() && p.c.pluginReady.Load() && p.c.srv.clientByUUID(p.c.profile.UUID) == p.c
}
func (p pluginPlayer) UUID() plugin.UUID {
	if !p.IsValid() {
		return plugin.UUID{}
	}
	return plugin.UUID(p.c.profile.UUID)
}
func (p pluginPlayer) Name() string {
	if !p.IsValid() {
		return ""
	}
	return p.c.profile.Name
}
func (p pluginPlayer) Online() (plugin.Player, bool) {
	if p.IsValid() {
		return p, true
	}
	return nil, false
}
func (p pluginPlayer) Profile() plugin.Profile {
	if !p.IsValid() {
		return plugin.Profile{}
	}
	return pluginProfile(p.c.profile)
}
func pluginProfile(value player.Profile) plugin.Profile {
	profile := plugin.Profile{ID: value.UUID, Name: value.Name}
	for _, prop := range value.Properties {
		profile.Properties = append(profile.Properties, plugin.ProfileProperty{Name: prop.Name, Value: prop.Value, Signature: prop.Signature})
	}
	return profile
}

func (p pluginPlayer) Address() net.Addr {
	if !p.IsValid() {
		return nil
	}
	return p.c.conn.RemoteAddr()
}
func (p pluginPlayer) Ping() time.Duration {
	if !p.IsValid() {
		return 0
	}
	return time.Duration(p.c.player.Latency()) * time.Millisecond
}
func (p pluginPlayer) Locale() string {
	if !p.IsValid() {
		return ""
	}
	if s := p.c.pluginLocale.Load(); s != nil {
		return *s
	}
	return "en_us"
}
func (p pluginPlayer) Brand() string {
	if !p.IsValid() {
		return ""
	}
	if s := p.c.pluginBrand.Load(); s != nil {
		return *s
	}
	return ""
}
func (p pluginPlayer) ViewDistance() int {
	if !p.IsValid() {
		return 0
	}
	return int(p.c.pluginViewDistance.Load())
}
func (p pluginPlayer) OpLevel() int {
	if !p.IsValid() {
		return 0
	}
	return p.c.PermLevel()
}
func (p pluginPlayer) HasPermission(node string) bool {
	return p.IsValid() && p.c.srv.plugins.HasPermission(p, node)
}
func (p pluginPlayer) PermissionSet(node string) bool {
	return p.IsValid() && p.c.srv.plugins.PermissionSet(p, node)
}

func (p pluginPlayer) AttachedPermission(node string) (bool, bool) {
	if !p.IsValid() {
		return false, false
	}
	p.c.pluginMu.Lock()
	defer p.c.pluginMu.Unlock()
	for _, v := range slices.Backward(p.c.pluginAttachments) {
		if value, ok := v.values[node]; ok {
			return value, true
		}
	}
	return false, false
}

// pluginPermissionAttachment holds one plugin's transient permission map for a
// single client connection. Lock order: host.attachMu before c.pluginMu.
type pluginPermissionAttachment struct {
	host    *pluginHost
	c       *client
	values  map[string]bool
	removed bool
}

func (p pluginPlayer) AddAttachment() plugin.PermissionAttachment {
	if !p.IsValid() {
		return nil
	}
	h := p.hostOrAdapter()
	if h != nil && h.host != nil {
		h.attachOnce.Do(func() { h.host.Own(h.releaseAttachments) })
		h.attachMu.Lock()
		defer h.attachMu.Unlock()
		if h.attachClosed {
			return nil
		}
	}
	p.c.pluginMu.Lock()
	defer p.c.pluginMu.Unlock()
	if p.c.leaving.Load() || p.c.pluginQuitAt.Load() != 0 {
		return nil
	}
	att := &pluginPermissionAttachment{host: h, c: p.c, values: make(map[string]bool)}
	p.c.pluginAttachments = append(p.c.pluginAttachments, att)
	if h != nil && h.host != nil {
		if h.attachments == nil {
			h.attachments = make(map[*pluginPermissionAttachment]struct{})
		}
		h.attachments[att] = struct{}{}
	}
	return att
}

func (a *pluginPermissionAttachment) Permissions() map[string]bool {
	a.c.pluginMu.Lock()
	defer a.c.pluginMu.Unlock()
	if a.removed || a.c.leaving.Load() || a.c.pluginQuitAt.Load() != 0 {
		return map[string]bool{}
	}
	return maps.Clone(a.values)
}

func (a *pluginPermissionAttachment) Set(node string, value bool) {
	a.c.pluginMu.Lock()
	defer a.c.pluginMu.Unlock()
	if a.removed || a.c.leaving.Load() || a.c.pluginQuitAt.Load() != 0 {
		return
	}
	a.values[node] = value
}

func (a *pluginPermissionAttachment) Unset(node string) {
	a.c.pluginMu.Lock()
	defer a.c.pluginMu.Unlock()
	if a.removed || a.c.leaving.Load() || a.c.pluginQuitAt.Load() != 0 {
		return
	}
	delete(a.values, node)
}

// detachLocked marks the attachment removed, clears its values, and drops it
// from c.pluginAttachments. Caller holds a.c.pluginMu.
func (a *pluginPermissionAttachment) detachLocked() {
	if a.removed {
		return
	}
	a.removed = true
	clear(a.values)
	a.c.pluginAttachments = slices.DeleteFunc(a.c.pluginAttachments, func(other *pluginPermissionAttachment) bool {
		return other == a
	})
}

func (a *pluginPermissionAttachment) untrackHost() {
	if a.host == nil || a.host.host == nil {
		return
	}
	a.host.attachMu.Lock()
	delete(a.host.attachments, a)
	a.host.attachMu.Unlock()
}

func (a *pluginPermissionAttachment) Remove() {
	a.untrackHost()
	a.c.pluginMu.Lock()
	defer a.c.pluginMu.Unlock()
	a.detachLocked()
}

func (h *pluginHost) releaseAttachments() {
	h.attachMu.Lock()
	defer h.attachMu.Unlock()
	h.attachClosed = true
	all := h.attachments
	h.attachments = nil
	for att := range all {
		att.c.pluginMu.Lock()
		att.detachLocked()
		att.c.pluginMu.Unlock()
	}
}

func (c *client) clearPluginAttachments() {
	c.pluginMu.Lock()
	atts := c.pluginAttachments
	c.pluginAttachments = nil
	for _, att := range atts {
		att.detachLocked()
	}
	c.pluginMu.Unlock()
	for _, att := range atts {
		att.untrackHost()
	}
}
func (p pluginPlayer) SendMessage(msg plugin.Text) {
	if p.IsValid() {
		p.c.Notify(pluginProtocolText(msg))
	}
}
func (p pluginPlayer) DisplayName() plugin.Text {
	if !p.IsValid() {
		return plugin.Text{}
	}
	t, _ := pluginText(p.c.DisplayName())
	return t
}
func (p pluginPlayer) SetDisplayName(t plugin.Text) {
	if p.IsValid() {
		text := pluginProtocolText(pluginruntime.CloneText(t))
		p.c.pluginDisplayName.Store(&text)
	}
}
func (p pluginPlayer) ListName() *plugin.Text {
	if !p.IsValid() {
		return nil
	}
	if name := p.c.pluginListName.Load(); name != nil {
		t, _ := pluginText(*name)
		return &t
	}
	return nil
}
func (p pluginPlayer) SetListName(t *plugin.Text) {
	if !p.IsValid() {
		return
	}
	var text *protocol.Text
	if t != nil {
		v := pluginProtocolText(pluginruntime.CloneText(*t))
		text = &v
	}
	p.c.pluginListName.Store(text)
	p.c.srv.broadcast(protocol.IDPlayPlayerInfoUpdate, p.c.pluginListNamePacket())
}
func (c *client) pluginListNamePacket() []byte {
	var b protocol.Buffer
	b.PutU8(0x20) // vanilla: ClientboundPlayerInfoUpdatePacket.Action.UPDATE_DISPLAY_NAME
	b.PutVarInt(1)
	b.PutUUID(c.profile.UUID)
	text := c.pluginListName.Load()
	b.PutBool(text != nil)
	if text != nil {
		b.PutText(*text)
	}
	return b.Bytes()
}
func (p pluginPlayer) SetListHeaderFooter(header, footer plugin.Text) {
	if !p.IsValid() {
		return
	}
	var b protocol.Buffer
	b.PutText(pluginProtocolText(header))
	b.PutText(pluginProtocolText(footer))
	p.c.send(protocol.IDPlayTabList, b.Bytes())
}
func (p pluginPlayer) FirstPlayed() time.Time {
	if !p.IsValid() {
		return time.Time{}
	}
	return p.c.pluginFirstPlayed
}
func (p pluginPlayer) LastSeen() time.Time {
	if !p.IsValid() {
		return time.Time{}
	}
	return p.c.pluginSeenTime()
}
func (c *client) pluginSeenTime() time.Time {
	if stamp := c.pluginQuitAt.Load(); stamp != 0 {
		return time.UnixMilli(stamp)
	}
	return time.Now()
}
func (p pluginPlayer) RespawnPoint() (plugin.Location, bool) {
	if !p.IsValid() {
		return plugin.Location{}, false
	}
	if r := p.c.spawnPoint.Load(); r != nil {
		return pluginRespawn(p.host, *r)
	}
	return plugin.Location{}, false
}
func pluginRespawn(h *pluginHost, r respawnPos) (plugin.Location, bool) {
	s := h.srv
	d := s.findDimension(r.dim)
	if d == nil {
		return plugin.Location{}, false
	}
	return plugin.Location{World: pluginWorld{srv: s, dim: d, host: h}, Pos: plugin.Vec3{X: float64(r.pos.X), Y: float64(r.pos.Y), Z: float64(r.pos.Z)}, Yaw: r.yaw, Pitch: r.pitch}, true
}

type pluginOfflinePlayer struct {
	host        *pluginHost
	id          plugin.UUID
	name        string
	once        sync.Once
	first, last time.Time
	spawn       *respawnPos
}

func (p *pluginOfflinePlayer) loadOnce() (nbt.Compound, error) {
	var (
		root nbt.Compound
		err  error
	)
	p.once.Do(func() {
		root, err = p.host.srv.players.load(uuid.UUID(p.id))
		if err != nil {
			p.host.Logger().Warn("could not load player data", "uuid", uuid.UUID(p.id), "error", err)
		}
		if root != nil {
			fixPlayerRespawn(root)
			if r, ok := storedRespawn(root); ok {
				p.spawn = &r
			}
		}
		p.first, p.last = p.host.srv.pluginPlayedTimes(uuid.UUID(p.id), root)
	})
	return root, err
}

func (p *pluginOfflinePlayer) load() {
	_, _ = p.loadOnce()
}

func (h *pluginHost) OfflinePlayer(id plugin.UUID) (plugin.OfflinePlayer, bool) {
	if p, ok := h.Player(id); ok {
		return p, true
	}
	name, cached := h.srv.usercache.nameOf(uuid.UUID(id))
	p := &pluginOfflinePlayer{host: h, id: id, name: name}
	root, err := p.loadOnce()
	// load can return usable backup data alongside a recovery warning.
	if (err != nil || !cached) && root == nil {
		return nil, false
	}
	return p, true
}

func (h *pluginHost) OfflinePlayers() []plugin.OfflinePlayer {
	online := h.Players()
	seen := make(map[plugin.UUID]struct{}, len(online))
	out := make([]plugin.OfflinePlayer, 0, len(online))
	for _, p := range online {
		id := p.UUID()
		if id == (plugin.UUID{}) {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, p)
	}
	ids, err := h.srv.players.list()
	if err != nil {
		h.Logger().Warn("could not list player data", "error", err)
		return out
	}
	if len(ids) == 0 {
		return out
	}
	names := h.srv.usercache.names()
	for _, id := range ids {
		pid := plugin.UUID(id)
		if _, ok := seen[pid]; ok {
			continue
		}
		seen[pid] = struct{}{}
		out = append(out, &pluginOfflinePlayer{host: h, id: pid, name: names[id]})
	}
	return out
}

func (p *pluginOfflinePlayer) UUID() plugin.UUID             { return p.id }
func (p *pluginOfflinePlayer) Name() string                  { return p.name }
func (p *pluginOfflinePlayer) Online() (plugin.Player, bool) { return p.host.Player(p.id) }
func (p *pluginOfflinePlayer) FirstPlayed() time.Time {
	p.load()
	return p.first
}
func (p *pluginOfflinePlayer) LastSeen() time.Time {
	p.load()
	return p.last
}
func (p *pluginOfflinePlayer) RespawnPoint() (plugin.Location, bool) {
	p.load()
	if p.spawn != nil {
		return pluginRespawn(p.host, *p.spawn)
	}
	return plugin.Location{}, false
}
func (s *Server) pluginPlayedTimes(id uuid.UUID, root nbt.Compound) (first, last time.Time) {
	// Vanilla files have no play timestamps; their modification time is the fallback.
	if info, err := os.Stat(s.players.path(id, ".dat")); err == nil {
		first = info.ModTime()
		last = first
	}
	if stamp, ok := nbt.Get[nbt.Long](root, "golem:first_played"); ok && stamp > 0 {
		first = time.UnixMilli(int64(stamp))
	}
	if stamp, ok := nbt.Get[nbt.Long](root, "golem:last_seen"); ok && stamp > 0 {
		last = time.UnixMilli(int64(stamp))
	}
	return first, last
}

// Statistic reads the counter from the player's stats file.
func (p *pluginOfflinePlayer) Statistic(s plugin.Statistic) int {
	stats, err := p.host.srv.players.loadStats(uuid.UUID(p.id))
	if err != nil || stats == nil {
		return 0
	}
	return int(stats.Value(player.Stat{Type: s.Type, Key: s.Value}))
}

func (h *pluginHost) OfflinePlayerNamed(name string) (plugin.OfflinePlayer, bool) {
	profile, ok := (worldScope{srv: h.srv}).FetchProfileByName(name)
	if !ok {
		return nil, false
	}
	return h.OfflinePlayer(plugin.UUID(profile.UUID))
}

func (h *pluginHost) ProfileByName(name string) (plugin.Profile, bool) {
	profile, ok := (worldScope{srv: h.srv}).FetchProfileByName(name)
	if !ok {
		return plugin.Profile{}, false
	}
	out := plugin.Profile{ID: plugin.UUID(profile.UUID), Name: profile.Name}
	for _, p := range profile.Properties {
		out.Properties = append(out.Properties, plugin.ProfileProperty{Name: p.Name, Value: p.Value, Signature: p.Signature})
	}
	return out, true
}
