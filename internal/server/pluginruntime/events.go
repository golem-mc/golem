package pluginruntime

import (
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/golem-mc/golem/plugin"
)

// Hook identifiers index the listener mask.
const (
	ServerLoad = iota
	PluginEnable
	PluginDisable
	ServerTickStart
	ServerTickEnd
	PlayerCommand
	PlayerCommandSend
	ServerCommand
	RemoteServerCommand
	PlayerChat
	BroadcastMessage
	ServerListPing
	PlayerPreLogin
	PlayerLogin
	PlayerConfigure
	PlayerClientSettings
	PlayerResourcePackStatus
	PlayerCustomClick
	PlayerJoin
	PlayerQuit
	PlayerKick
	BlockBreak
	BlockDamage
	BlockDamageAbort
	BlockPlace
	SignChange
	BlockDispense
	BlockDropItem
	BellRing
	BlockIgnite
	WorldLoad
	WorldUnload
	WorldSave
	SpawnChange
	TimeSkip
	WeatherChange
	ThunderChange
	LightningStrike
	PortalCreate
	ChunkLoad
	ChunkUnload
	ChunkPopulate
	LootGenerate
	EntityDamage
	PlayerDeath
	EntityDeath
	EntityResurrect
	FoodLevelChange
	EntityRegainHealth
	BlockBurn
	BlockSpread
	BlockForm
	BlockFade
	BlockGrow
	BlockFromTo
	BlockPhysics
	BlockRedstone
	BlockPiston
	LeavesDecay
	NotePlay
	SpongeAbsorb
	BlockCook
	FurnaceBurn
	Brew
	CauldronLevelChange
	TNTPrime
	BlockExplode
	EntityToggleGlide
	EntityMount
	EntityDismount
	Vehicle
	SheepDyeWool
	ExpBottle
	EntityEnterLoveMode
	EntityAirChange
	EntityShootBow
	ProjectileLaunch
	ProjectileHit
	EntityTarget
	EntityPotionEffect
	EntitySpawn
	EntityInteract
	EntityTame
	EntityCombust
	EntityChangeBlock
	EntityBreakDoor
	PlayerPickupArrow
	EntityTransform
	SlimeSplit
	InventoryOpen
	InventoryClose
	InventoryClick
	InventoryDrag
	InventoryMoveItem
	InventoryPickupItem
	PrepareCraft
	CraftItem
	FurnaceExtract
	ItemDespawn
	ItemMerge
	PrepareEnchant
	EnchantItem
	GenericGameEvent
	BlockFertilize
	StructureGrow
	EntityDropItem
	EntityRemove
	ExplosionPrime
	EntityExplode
	CreeperPower
	EntityBreed
	PiglinBarter
	HangingPlace
	HangingBreak
	VillagerCareerChange
	VillagerAcquireTrade
	PlayerTrade
	Raid
	PlayerInteract
	PlayerToggleSneak
	PlayerToggleSprint
	PlayerToggleFlight
	PlayerItemHeld
	PlayerSwapHandItems
	PlayerMove
	EntityTeleport
	PlayerAttack
	PlayerInteractEntity
	PlayerGameModeChange
	PlayerRespawn
	PlayerAnimation
	PlayerItemConsume
	PlayerBucket
	PlayerBedEnter
	PlayerBedLeave
	PlayerExpChange
	PlayerLevelChange
	PlayerAdvancementDone
	PlayerDropItem
	PlayerEditBook
	PlayerChangedWorld
	PlayerShearEntity
	EntityPickupItem
	PlayerItemBreak
	PlayerItemDamage
	EntityPortal
	PlayerFish
	PlayerArmorStandManipulate
	PlayerEggThrow
	PlayerAdvancementCriterionGrant
	VehicleMove
	EntityMove
	EntityPortalEnter
	EntityPortalExit
)

var pluginHookTypes = [...]reflect.Type{
	reflect.TypeFor[*plugin.ServerLoadEvent](),
	reflect.TypeFor[*plugin.PluginEnableEvent](),
	reflect.TypeFor[*plugin.PluginDisableEvent](),
	reflect.TypeFor[*plugin.ServerTickStartEvent](),
	reflect.TypeFor[*plugin.ServerTickEndEvent](),
	reflect.TypeFor[*plugin.PlayerCommandEvent](),
	reflect.TypeFor[*plugin.PlayerCommandSendEvent](),
	reflect.TypeFor[*plugin.ServerCommandEvent](),
	reflect.TypeFor[*plugin.RemoteServerCommandEvent](),
	reflect.TypeFor[*plugin.PlayerChatEvent](),
	reflect.TypeFor[*plugin.BroadcastMessageEvent](),
	reflect.TypeFor[*plugin.ServerListPingEvent](),
	reflect.TypeFor[*plugin.PlayerPreLoginEvent](),
	reflect.TypeFor[*plugin.PlayerLoginEvent](),
	reflect.TypeFor[*plugin.PlayerConfigureEvent](),
	reflect.TypeFor[*plugin.PlayerClientSettingsEvent](),
	reflect.TypeFor[*plugin.PlayerResourcePackStatusEvent](),
	reflect.TypeFor[*plugin.PlayerCustomClickEvent](),
	reflect.TypeFor[*plugin.PlayerJoinEvent](),
	reflect.TypeFor[*plugin.PlayerQuitEvent](),
	reflect.TypeFor[*plugin.PlayerKickEvent](),
	reflect.TypeFor[*plugin.BlockBreakEvent](),
	reflect.TypeFor[*plugin.BlockDamageEvent](),
	reflect.TypeFor[*plugin.BlockDamageAbortEvent](),
	reflect.TypeFor[*plugin.BlockPlaceEvent](),
	reflect.TypeFor[*plugin.SignChangeEvent](),
	reflect.TypeFor[*plugin.BlockDispenseEvent](),
	reflect.TypeFor[*plugin.BlockDropItemEvent](),
	reflect.TypeFor[*plugin.BellRingEvent](),
	reflect.TypeFor[*plugin.BlockIgniteEvent](),
	reflect.TypeFor[*plugin.WorldLoadEvent](),
	reflect.TypeFor[*plugin.WorldUnloadEvent](),
	reflect.TypeFor[*plugin.WorldSaveEvent](),
	reflect.TypeFor[*plugin.SpawnChangeEvent](),
	reflect.TypeFor[*plugin.TimeSkipEvent](),
	reflect.TypeFor[*plugin.WeatherChangeEvent](),
	reflect.TypeFor[*plugin.ThunderChangeEvent](),
	reflect.TypeFor[*plugin.LightningStrikeEvent](),
	reflect.TypeFor[*plugin.PortalCreateEvent](),
	reflect.TypeFor[*plugin.ChunkLoadEvent](),
	reflect.TypeFor[*plugin.ChunkUnloadEvent](),
	reflect.TypeFor[*plugin.ChunkPopulateEvent](),
	reflect.TypeFor[*plugin.LootGenerateEvent](),
	reflect.TypeFor[*plugin.EntityDamageEvent](),
	reflect.TypeFor[*plugin.PlayerDeathEvent](),
	reflect.TypeFor[*plugin.EntityDeathEvent](),
	reflect.TypeFor[*plugin.EntityResurrectEvent](),
	reflect.TypeFor[*plugin.FoodLevelChangeEvent](),
	reflect.TypeFor[*plugin.EntityRegainHealthEvent](),
	reflect.TypeFor[*plugin.BlockBurnEvent](),
	reflect.TypeFor[*plugin.BlockSpreadEvent](),
	reflect.TypeFor[*plugin.BlockFormEvent](),
	reflect.TypeFor[*plugin.BlockFadeEvent](),
	reflect.TypeFor[*plugin.BlockGrowEvent](),
	reflect.TypeFor[*plugin.BlockFromToEvent](),
	reflect.TypeFor[*plugin.BlockPhysicsEvent](),
	reflect.TypeFor[*plugin.BlockRedstoneEvent](),
	reflect.TypeFor[*plugin.BlockPistonEvent](),
	reflect.TypeFor[*plugin.LeavesDecayEvent](),
	reflect.TypeFor[*plugin.NotePlayEvent](),
	reflect.TypeFor[*plugin.SpongeAbsorbEvent](),
	reflect.TypeFor[*plugin.BlockCookEvent](),
	reflect.TypeFor[*plugin.FurnaceBurnEvent](),
	reflect.TypeFor[*plugin.BrewEvent](),
	reflect.TypeFor[*plugin.CauldronLevelChangeEvent](),
	reflect.TypeFor[*plugin.TNTPrimeEvent](),
	reflect.TypeFor[*plugin.BlockExplodeEvent](),
	reflect.TypeFor[*plugin.EntityToggleGlideEvent](),
	reflect.TypeFor[*plugin.EntityMountEvent](),
	reflect.TypeFor[*plugin.EntityDismountEvent](),
	reflect.TypeFor[*plugin.VehicleEvent](),
	reflect.TypeFor[*plugin.SheepDyeWoolEvent](),
	reflect.TypeFor[*plugin.ExpBottleEvent](),
	reflect.TypeFor[*plugin.EntityEnterLoveModeEvent](),
	reflect.TypeFor[*plugin.EntityAirChangeEvent](),
	reflect.TypeFor[*plugin.EntityShootBowEvent](),
	reflect.TypeFor[*plugin.ProjectileLaunchEvent](),
	reflect.TypeFor[*plugin.ProjectileHitEvent](),
	reflect.TypeFor[*plugin.EntityTargetEvent](),
	reflect.TypeFor[*plugin.EntityPotionEffectEvent](),
	reflect.TypeFor[*plugin.EntitySpawnEvent](),
	reflect.TypeFor[*plugin.EntityInteractEvent](),
	reflect.TypeFor[*plugin.EntityTameEvent](),
	reflect.TypeFor[*plugin.EntityCombustEvent](),
	reflect.TypeFor[*plugin.EntityChangeBlockEvent](),
	reflect.TypeFor[*plugin.EntityBreakDoorEvent](),
	reflect.TypeFor[*plugin.PlayerPickupArrowEvent](),
	reflect.TypeFor[*plugin.EntityTransformEvent](),
	reflect.TypeFor[*plugin.SlimeSplitEvent](),
	reflect.TypeFor[*plugin.InventoryOpenEvent](),
	reflect.TypeFor[*plugin.InventoryCloseEvent](),
	reflect.TypeFor[*plugin.InventoryClickEvent](),
	reflect.TypeFor[*plugin.InventoryDragEvent](),
	reflect.TypeFor[*plugin.InventoryMoveItemEvent](),
	reflect.TypeFor[*plugin.InventoryPickupItemEvent](),
	reflect.TypeFor[*plugin.PrepareItemCraftEvent](),
	reflect.TypeFor[*plugin.CraftItemEvent](),
	reflect.TypeFor[*plugin.FurnaceExtractEvent](),
	reflect.TypeFor[*plugin.ItemDespawnEvent](),
	reflect.TypeFor[*plugin.ItemMergeEvent](),
	reflect.TypeFor[*plugin.PrepareItemEnchantEvent](),
	reflect.TypeFor[*plugin.EnchantItemEvent](),
	reflect.TypeFor[*plugin.GenericGameEvent](),
	reflect.TypeFor[*plugin.BlockFertilizeEvent](),
	reflect.TypeFor[*plugin.StructureGrowEvent](),
	reflect.TypeFor[*plugin.EntityDropItemEvent](),
	reflect.TypeFor[*plugin.EntityRemoveEvent](),
	reflect.TypeFor[*plugin.ExplosionPrimeEvent](),
	reflect.TypeFor[*plugin.EntityExplodeEvent](),
	reflect.TypeFor[*plugin.CreeperPowerEvent](),
	reflect.TypeFor[*plugin.EntityBreedEvent](),
	reflect.TypeFor[*plugin.PiglinBarterEvent](),
	reflect.TypeFor[*plugin.HangingPlaceEvent](),
	reflect.TypeFor[*plugin.HangingBreakEvent](),
	reflect.TypeFor[*plugin.VillagerCareerChangeEvent](),
	reflect.TypeFor[*plugin.VillagerAcquireTradeEvent](),
	reflect.TypeFor[*plugin.PlayerTradeEvent](),
	reflect.TypeFor[*plugin.RaidEvent](),
	reflect.TypeFor[*plugin.PlayerInteractEvent](),
	reflect.TypeFor[*plugin.PlayerToggleSneakEvent](),
	reflect.TypeFor[*plugin.PlayerToggleSprintEvent](),
	reflect.TypeFor[*plugin.PlayerToggleFlightEvent](),
	reflect.TypeFor[*plugin.PlayerItemHeldEvent](),
	reflect.TypeFor[*plugin.PlayerSwapHandItemsEvent](),
	reflect.TypeFor[*plugin.PlayerMoveEvent](),
	reflect.TypeFor[*plugin.EntityTeleportEvent](),
	reflect.TypeFor[*plugin.PlayerAttackEvent](),
	reflect.TypeFor[*plugin.PlayerInteractEntityEvent](),
	reflect.TypeFor[*plugin.PlayerGameModeChangeEvent](),
	reflect.TypeFor[*plugin.PlayerRespawnEvent](),
	reflect.TypeFor[*plugin.PlayerAnimationEvent](),
	reflect.TypeFor[*plugin.PlayerItemConsumeEvent](),
	reflect.TypeFor[*plugin.PlayerBucketEvent](),
	reflect.TypeFor[*plugin.PlayerBedEnterEvent](),
	reflect.TypeFor[*plugin.PlayerBedLeaveEvent](),
	reflect.TypeFor[*plugin.PlayerExpChangeEvent](),
	reflect.TypeFor[*plugin.PlayerLevelChangeEvent](),
	reflect.TypeFor[*plugin.PlayerAdvancementDoneEvent](),
	reflect.TypeFor[*plugin.PlayerDropItemEvent](),
	reflect.TypeFor[*plugin.PlayerEditBookEvent](),
	reflect.TypeFor[*plugin.PlayerChangedWorldEvent](),
	reflect.TypeFor[*plugin.PlayerShearEntityEvent](),
	reflect.TypeFor[*plugin.EntityPickupItemEvent](),
	reflect.TypeFor[*plugin.PlayerItemBreakEvent](),
	reflect.TypeFor[*plugin.PlayerItemDamageEvent](),
	reflect.TypeFor[*plugin.EntityPortalEvent](),
	reflect.TypeFor[*plugin.PlayerFishEvent](),
	reflect.TypeFor[*plugin.PlayerArmorStandManipulateEvent](),
	reflect.TypeFor[*plugin.PlayerEggThrowEvent](),
	reflect.TypeFor[*plugin.PlayerAdvancementCriterionGrantEvent](),
	reflect.TypeFor[*plugin.VehicleMoveEvent](),
	reflect.TypeFor[*plugin.EntityMoveEvent](),
	reflect.TypeFor[*plugin.EntityPortalEnterEvent](),
	reflect.TypeFor[*plugin.EntityPortalExitEvent](),
}

type pluginBus struct {
	mask      [(len(pluginHookTypes) + 63) / 64]atomic.Uint64
	mu        sync.Mutex
	listeners map[reflect.Type][]*pluginListener
	defined   map[reflect.Type]bool
}

// has is the allocation-free gate every hook checks before building an event.
func (b *pluginBus) has(id int) bool { return b.mask[id>>6].Load()&(1<<(id&63)) != 0 }

type pluginEvents struct{ host *Host }
type pluginListener struct {
	host    *Host
	t       reflect.Type
	fn      func(plugin.Event)
	opts    plugin.ListenOptions
	removed atomic.Bool
}

func validEventType(t reflect.Type) bool {
	return t != nil && t.Kind() == reflect.Pointer && t.Elem().Kind() == reflect.Struct && t.Elem().PkgPath() == reflect.TypeFor[plugin.Base]().PkgPath() && t.Implements(reflect.TypeFor[plugin.Event]()) && strings.HasSuffix(t.Elem().Name(), "Event")
}

// definable is a pointer to a struct named ...Event that embeds Base: the
// shape of an event a plugin defines itself.
func definable(t reflect.Type) bool {
	return t != nil && t.Kind() == reflect.Pointer && t.Elem().Kind() == reflect.Struct && t.Implements(reflect.TypeFor[plugin.Event]()) &&
		strings.HasSuffix(t.Elem().Name(), "Event") && t.Elem().Name() != "Event"
}

func (e *pluginEvents) Define(t reflect.Type) {
	if !definable(t) {
		panic("plugin: defined type must be a pointer to a struct named ...Event that embeds plugin.Base")
	}
	b := &e.host.runtime.bus
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.defined == nil {
		b.defined = make(map[reflect.Type]bool)
	}
	b.defined[t] = true
}

// known reports whether t is a built-in event type or one a plugin defined.
func (b *pluginBus) known(t reflect.Type) bool {
	if validEventType(t) {
		return true
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.defined[t]
}

func (e *pluginEvents) Register(t reflect.Type, fn func(plugin.Event), opts plugin.ListenOptions) plugin.Listener {
	if !e.host.runtime.bus.known(t) {
		panic("plugin: listener type must be a pointer to a plugin event")
	}
	if fn == nil {
		panic("plugin: nil listener")
	}
	l := &pluginListener{host: e.host, t: t, fn: fn, opts: opts}
	b := &e.host.runtime.bus
	b.mu.Lock()
	defer b.mu.Unlock()
	if e.host.closed.Load() {
		l.removed.Store(true)
		return l
	}
	if b.listeners == nil {
		b.listeners = make(map[reflect.Type][]*pluginListener)
	}
	b.listeners[t] = append(b.listeners[t], l)
	slices.SortStableFunc(b.listeners[t], func(a, c *pluginListener) int { return int(a.opts.Priority) - int(c.opts.Priority) })
	b.updateMask(t)
	return l
}

// updateMask runs under mu; calls into plugins always run outside it.
func (b *pluginBus) updateMask(t reflect.Type) {
	for i, typ := range pluginHookTypes {
		if typ == t {
			if len(b.listeners[t]) == 0 {
				b.mask[i>>6].And(^(uint64(1) << (i & 63)))
			} else {
				b.mask[i>>6].Or(uint64(1) << (i & 63))
			}
		}
	}
}
func (l *pluginListener) Unregister() {
	b := &l.host.runtime.bus
	b.mu.Lock()
	defer b.mu.Unlock()
	l.removed.Store(true)
	b.listeners[l.t] = slices.DeleteFunc(b.listeners[l.t], func(p *pluginListener) bool { return p == l })
	b.updateMask(l.t)
}
func (e *pluginEvents) remove() {
	b := &e.host.runtime.bus
	b.mu.Lock()
	defer b.mu.Unlock()
	for t, ls := range b.listeners {
		b.listeners[t] = slices.DeleteFunc(ls, func(l *pluginListener) bool {
			if l.host == e.host {
				l.removed.Store(true)
				return true
			}
			return false
		})
		b.updateMask(t)
	}
}
func (e *pluginEvents) Fire(ev plugin.Event) {
	if !e.host.runtime.bus.known(reflect.TypeOf(ev)) {
		panic("plugin: fired value must be a pointer to a plugin event")
	}
	e.host.runtime.bus.fire(ev)
}

func (b *pluginBus) fire(event plugin.Event) {
	b.mu.Lock()
	listeners := slices.Clone(b.listeners[reflect.TypeOf(event)])
	b.mu.Unlock()
	for _, l := range listeners {
		if l.removed.Load() {
			continue
		}
		if c, ok := event.(interface{ Cancelled() bool }); ok && c.Cancelled() && l.opts.IgnoreCancelled {
			continue
		}

		var finish func()
		if bind := l.host.runtime.bindEvent; bind != nil {
			finish = bind(event, l.host.api)
		}
		value := reflect.ValueOf(event).Elem()
		before := clonePluginValue(value)
		if err := l.host.guard(func() error { l.fn(event); return nil }); err != nil {
			value.Set(before)
			l.host.Logger().Error("Error in event listener", "event", l.t.String(), "error", err)
		} else if l.opts.Priority == plugin.Monitor {
			value.Set(before)
		}
		if finish != nil {
			finish()
		}
	}
}

// clonePluginValue snapshots event-owned data, retaining live interface handles.
func clonePluginValue(v reflect.Value) reflect.Value {
	c := reflect.New(v.Type()).Elem()
	c.Set(v)
	deepen(c)
	return c
}

// deepen replaces what the addressable copy c still shares with its source:
// exported pointers, slices, maps and map, slice or array interface values.
func deepen(c reflect.Value) {
	t := c.Type()
	if flatType(t) {
		return
	}
	switch c.Kind() {
	case reflect.Struct:
		for i := 0; i < c.NumField(); i++ {
			if t.Field(i).IsExported() && !flatType(t.Field(i).Type) {
				deepen(c.Field(i))
			}
		}
	case reflect.Pointer:
		if !c.IsNil() {
			p := reflect.New(t.Elem())
			p.Elem().Set(c.Elem())
			deepen(p.Elem())
			c.Set(p)
		}
	case reflect.Slice:
		if !c.IsNil() {
			s := reflect.MakeSlice(t, c.Len(), c.Len())
			reflect.Copy(s, c)
			if !flatType(t.Elem()) {
				for i := 0; i < s.Len(); i++ {
					deepen(s.Index(i))
				}
			}
			c.Set(s)
		}
	case reflect.Map:
		if !c.IsNil() {
			m := reflect.MakeMapWithSize(t, c.Len())
			for it := c.MapRange(); it.Next(); {
				m.SetMapIndex(it.Key(), clonePluginValue(it.Value()))
			}
			c.Set(m)
		}
	case reflect.Interface:
		if !c.IsNil() {
			switch c.Elem().Kind() {
			case reflect.Map, reflect.Slice, reflect.Array:
				c.Set(clonePluginValue(c.Elem()))
			}
		}
	case reflect.Array:
		for i := 0; i < c.Len(); i++ {
			deepen(c.Index(i))
		}
	}
}

var flatTypes sync.Map // reflect.Type -> bool

// flatType reports whether a value of t shares no memory through its exported
// fields, so a plain copy is a deep copy.
func flatType(t reflect.Type) bool {
	if f, ok := flatTypes.Load(t); ok {
		return f.(bool)
	}
	flat := true
	switch t.Kind() {
	case reflect.Pointer, reflect.Slice, reflect.Map, reflect.Interface:
		flat = false
	case reflect.Array:
		flat = flatType(t.Elem())
	case reflect.Struct:
		for i := 0; i < t.NumField() && flat; i++ {
			flat = !t.Field(i).IsExported() || flatType(t.Field(i).Type)
		}
	}
	flatTypes.Store(t, flat)
	return flat
}

// CloneText snapshots an API component, including nested style and item data.
func CloneText(t plugin.Text) plugin.Text {
	return clonePluginValue(reflect.ValueOf(t)).Interface().(plugin.Text)
}

// HookEventTypes lists the event type of every hook, for a test that listens to all of them.
func HookEventTypes() []reflect.Type { return pluginHookTypes[:] }
