package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

type pluginJukebox struct{ pluginBlockEntity }

func (b pluginJukebox) Record() plugin.ItemStack {
	return pluginBlockEntityRead(b.pluginBlockEntity, func(e *world.BlockEntity) plugin.ItemStack {
		return pluginStack(e.Slots[0])
	})
}
func (b pluginJukebox) Playing() bool {
	return pluginBlockEntityRead(b.pluginBlockEntity, (*world.BlockEntity).JukeboxPlaying)
}
func (b pluginJukebox) SetRecord(record plugin.ItemStack) {
	stack, ok := pluginProtocolStack(record)
	if !ok {
		return
	}
	song, playable := registry.JukeboxSongForStack(stack)
	wasPlaying := false
	if !b.mutate(func(e *world.BlockEntity) {
		wasPlaying = e.JukeboxPlaying()
		e.Put(0, stack)
		e.TicksSinceSongStarted = -1
		if playable {
			e.TicksSinceSongStarted = 0
		}
	}) {
		return
	}
	// vanilla: JukeboxBlockEntity.setTheItem, JukeboxSongPlayer.play/stop
	s, w := b.world.srv, b.world.dim.w
	x, y, z := b.entity.X, b.entity.Y, b.entity.Z
	s.setJukeboxHasRecord(w, x, y, z, !stack.Empty())
	if playable {
		s.broadcastLevelEvent(w, protocol.WorldEventJukeboxPlay, x, y, z, registry.JukeboxSongID(song))
	} else if wasPlaying {
		s.broadcastLevelEvent(w, protocol.WorldEventJukeboxStop, x, y, z, 0)
	}
	if playable || wasPlaying {
		s.signalNeighborsChanged(w, x, y, z)
	}
}

type pluginLectern struct{ pluginBlockEntity }

func (b pluginLectern) Book() plugin.ItemStack {
	return pluginBlockEntityRead(b.pluginBlockEntity, func(e *world.BlockEntity) plugin.ItemStack {
		return pluginStack(e.Slots[0])
	})
}
func (b pluginLectern) Page() int {
	return pluginBlockEntityRead(b.pluginBlockEntity, func(e *world.BlockEntity) int { return e.Page })
}
func (b pluginLectern) SetBook(book plugin.ItemStack) {
	stack, ok := pluginProtocolStack(book)
	if !ok || !b.IsValid() {
		return
	}
	s, w := b.world.srv, b.world.dim.w
	x, y, z := b.entity.X, b.entity.Y, b.entity.Z
	// A lectern resolves selectors as a gamemaster at its centre, without @s.
	// vanilla: LecternBlockEntity.createCommandSourceStack, resolveBook
	src := s.newCommandBlockSource(w, b.entity, false)
	src.name, src.pos.Yaw = "Lectern", 0
	stack, _ = rules.ResolveBook(stack, func(t protocol.Text) protocol.Text { return command.ResolveText(src, t) })
	hasBook := false
	if !b.mutate(func(e *world.BlockEntity) {
		e.Put(0, stack)
		e.Page = 0
		hasBook = e.HasBook()
	}) {
		return
	}
	s.lecternBookChanged(w, x, y, z, hasBook, rules.GameEventContext{})
	s.syncContainerViewers(w, x, y, z)
	s.lecternPageSent(w, x, y, z, 0)
}
func (b pluginLectern) SetPage(page int) {
	moved := false
	if !b.mutate(func(e *world.BlockEntity) { moved = e.SetLecternPage(page) }) || !moved {
		return
	}
	s, w := b.world.srv, b.world.dim.w
	x, y, z := b.entity.X, b.entity.Y, b.entity.Z
	s.lecternPageChanged(w, x, y, z)
	s.lecternPageSent(w, x, y, z, int32(b.Page()))
}

var (
	_ plugin.Jukebox = pluginJukebox{}
	_ plugin.Lectern = pluginLectern{}
)
