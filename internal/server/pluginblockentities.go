package server

import (
	"slices"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

func pluginBlockEntityHandle(w pluginWorld, entity *world.BlockEntity) plugin.BlockEntity {
	b := pluginBlockEntity{world: w, entity: entity}
	switch entity.Kind {
	case world.KindChest, world.KindTrappedChest, world.KindBarrel, world.KindShulkerBox,
		world.KindHopper, world.KindDispenser, world.KindDropper, world.KindCrafter, world.KindBrewingStand:
		return pluginContainer{pluginNamedBlockEntity{b}}
	case world.KindFurnace, world.KindBlastFurnace, world.KindSmoker:
		return pluginFurnace{pluginContainer{pluginNamedBlockEntity{b}}}
	case world.KindSign, world.KindHangingSign:
		return pluginSign{b}
	case world.KindSpawner:
		return pluginSpawner{b}
	case world.KindCommandBlock:
		return pluginCommandBlock{pluginNamedBlockEntity{b}}
	case world.KindBeacon:
		return pluginBeacon{pluginNamedBlockEntity{b}}
	case world.KindJukebox:
		return pluginJukebox{b}
	case world.KindLectern:
		return pluginLectern{b}
	default:
		return b
	}
}

func pluginBlockEntityRead[T any](b pluginBlockEntity, read func(*world.BlockEntity) T) (out T) {
	b.entity.UseLive(func(e *world.BlockEntity) { out = read(e) })
	return out
}

func (b pluginBlockEntity) write(change func(*world.BlockEntity)) bool {
	changed := b.mutate(change)
	if changed {
		b.world.srv.broadcastBlockEntity(b.world.dim.w, b.entity.X, b.entity.Y, b.entity.Z, b.entity)
	}
	return changed
}

func (b pluginBlockEntity) mutate(change func(*world.BlockEntity)) bool {
	changed := false
	b.entity.UseLive(func(e *world.BlockEntity) {
		// The dirty generation and comparator wake must accompany the mutation
		// under the entity lock, as for a container write.
		if b.world.dim.w.TouchEntity(e) {
			change(e)
			changed = true
		}
	})
	return changed
}

type pluginNamedBlockEntity struct{ pluginBlockEntity }

func (b pluginNamedBlockEntity) CustomName() *plugin.Text {
	return pluginBlockEntityRead(b.pluginBlockEntity, func(e *world.BlockEntity) *plugin.Text {
		if e.CustomName != nil {
			if text, ok := pluginText(*e.CustomName); ok {
				return &text
			}
		}
		return nil
	})
}
func (b pluginNamedBlockEntity) SetCustomName(text *plugin.Text) {
	var name *protocol.Text
	if text != nil {
		value := pluginProtocolText(*text)
		name = &value
	}
	b.write(func(e *world.BlockEntity) { e.CustomName = name })
}

type pluginSign struct{ pluginBlockEntity }

func pluginSignSide(e *world.BlockEntity, front bool) *world.SignText {
	if front {
		return &e.SignFront
	}
	return &e.SignBack
}
func (b pluginSign) Lines(front bool) [4]plugin.Text {
	return pluginBlockEntityRead(b.pluginBlockEntity, func(e *world.BlockEntity) (out [4]plugin.Text) {
		for i, line := range pluginSignSide(e, front).Lines() {
			out[i], _ = pluginTextOf(line)
		}
		return out
	})
}
func (b pluginSign) SetLines(front bool, lines [4]plugin.Text) {
	var tags [4]nbt.Tag
	for i, line := range lines {
		tags[i] = protocol.TextTag(pluginProtocolText(line))
	}
	b.write(func(e *world.BlockEntity) { pluginSignSide(e, front).SetLines(tags) })
}
func (b pluginSign) Color(front bool) plugin.DyeColor {
	return pluginBlockEntityRead(b.pluginBlockEntity, func(e *world.BlockEntity) plugin.DyeColor {
		return plugin.DyeColor(slices.Index(registry.DyeNames, pluginSignSide(e, front).Color))
	})
}
func (b pluginSign) SetColor(front bool, color plugin.DyeColor) {
	if int(color) >= len(registry.DyeNames) || b.Color(front) == color {
		return
	}
	b.write(func(e *world.BlockEntity) { pluginSignSide(e, front).Color = registry.DyeNames[color] })
}
func (b pluginSign) Glowing(front bool) bool {
	return pluginBlockEntityRead(b.pluginBlockEntity, func(e *world.BlockEntity) bool { return pluginSignSide(e, front).Glowing })
}
func (b pluginSign) SetGlowing(front, glow bool) {
	if b.Glowing(front) == glow {
		return
	}
	b.write(func(e *world.BlockEntity) { pluginSignSide(e, front).Glowing = glow })
}
func (b pluginSign) Waxed() bool {
	return pluginBlockEntityRead(b.pluginBlockEntity, func(e *world.BlockEntity) bool { return e.Waxed })
}
func (b pluginSign) SetWaxed(waxed bool) {
	if b.Waxed() == waxed {
		return
	}
	b.write(func(e *world.BlockEntity) { e.Waxed = waxed })
}

type pluginSpawner struct{ pluginBlockEntity }

func (b pluginSpawner) EntityType() registry.EntityType {
	return pluginBlockEntityRead(b.pluginBlockEntity, func(e *world.BlockEntity) registry.EntityType {
		id, ok := registry.EntityTypeByName(e.SpawnEntityID())
		if !ok {
			// vanilla: SpawnData.<init>, BaseSpawner.getOrCreateDisplayEntity
			return -1
		}
		return id
	})
}
func (b pluginSpawner) SetEntityType(kind registry.EntityType) {
	if int(kind) < 0 || int(kind) >= len(registry.EntityTypes) {
		return
	}
	b.write(func(e *world.BlockEntity) {
		// vanilla: BaseSpawner.getOrCreateNextSpawnData
		if e.SpawnData == nil {
			e.RollSpawnPotential(b.world.srv.levelRng.Intn)
		}
		e.SetSpawnEntityID(kind.Name())
	})
}
func (b pluginSpawner) Delay() int {
	return pluginBlockEntityRead(b.pluginBlockEntity, func(e *world.BlockEntity) int { return e.Delay })
}
func (b pluginSpawner) SetDelay(delay int) { b.write(func(e *world.BlockEntity) { e.Delay = delay }) }
func (b pluginSpawner) MinDelay() int {
	return pluginBlockEntityRead(b.pluginBlockEntity, func(e *world.BlockEntity) int { return e.MinSpawnDelay })
}
func (b pluginSpawner) MaxDelay() int {
	return pluginBlockEntityRead(b.pluginBlockEntity, func(e *world.BlockEntity) int { return e.MaxSpawnDelay })
}
func (b pluginSpawner) SetDelays(lo, hi int) {
	b.write(func(e *world.BlockEntity) { e.MinSpawnDelay, e.MaxSpawnDelay = lo, hi })
}
func (b pluginSpawner) SpawnCount() int {
	return pluginBlockEntityRead(b.pluginBlockEntity, func(e *world.BlockEntity) int { return e.SpawnCount })
}
func (b pluginSpawner) SetSpawnCount(count int) {
	b.write(func(e *world.BlockEntity) { e.SpawnCount = count })
}
func (b pluginSpawner) RequiredPlayerRange() int {
	return pluginBlockEntityRead(b.pluginBlockEntity, func(e *world.BlockEntity) int { return e.RequiredPlayerRange })
}
func (b pluginSpawner) SetRequiredPlayerRange(distance int) {
	b.write(func(e *world.BlockEntity) { e.RequiredPlayerRange = distance })
}

type pluginCommandBlock struct{ pluginNamedBlockEntity }

func (b pluginCommandBlock) Command() string {
	return pluginBlockEntityRead(b.pluginBlockEntity, func(e *world.BlockEntity) string { return e.Command })
}
func (b pluginCommandBlock) SetCommand(line string) {
	b.write(func(e *world.BlockEntity) { e.SetCommand(line) })
}
func (b pluginCommandBlock) SuccessCount() int {
	return pluginBlockEntityRead(b.pluginBlockEntity, func(e *world.BlockEntity) int { return e.SuccessCount })
}

type pluginBeacon struct{ pluginNamedBlockEntity }

func (b pluginBeacon) Levels() int {
	return pluginBlockEntityRead(b.pluginBlockEntity, func(e *world.BlockEntity) int { return e.BeaconLevels })
}
func (b pluginBeacon) Effects() (primary, secondary plugin.Effect) {
	effects := pluginBlockEntityRead(b.pluginBlockEntity, func(e *world.BlockEntity) [2]plugin.Effect {
		return [2]plugin.Effect{plugin.Effect(e.BeaconPrimary), plugin.Effect(e.BeaconSecondary)}
	})
	return effects[0], effects[1]
}
func (b pluginBeacon) SetEffects(primary, secondary plugin.Effect) {
	// vanilla: BeaconBlockEntity.filterEffect
	if !registry.BeaconOffersEffect(int32(primary)) {
		primary = -1
	}
	if !registry.BeaconOffersEffect(int32(secondary)) {
		secondary = -1
	}
	if !b.write(func(e *world.BlockEntity) { e.BeaconPrimary, e.BeaconSecondary = int32(primary), int32(secondary) }) {
		return
	}
	// Block entity update tags do not carry the menu's separate data slots.
	// vanilla: AbstractContainerMenu.broadcastChanges, BeaconMenu
	s := b.world.srv
	s.containerViewers(b.world.dim.w, b.entity.X, b.entity.Y, b.entity.Z, nil, func(c *client) {
		window := c.openContainerWindow()
		if window == nil || window.kind != windowBeacon {
			return
		}
		s.queueBeaconProperty(c, window, protocol.BeaconPropertyPrimary, rules.BeaconEncodeEffect(int32(primary)))
		s.queueBeaconProperty(c, window, protocol.BeaconPropertySecondary, rules.BeaconEncodeEffect(int32(secondary)))
	})
}

var (
	_ plugin.Sign         = pluginSign{}
	_ plugin.Spawner      = pluginSpawner{}
	_ plugin.CommandBlock = pluginCommandBlock{}
	_ plugin.Beacon       = pluginBeacon{}
)
