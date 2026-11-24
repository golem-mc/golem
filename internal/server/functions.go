package server

import (
	"log/slog"
	"slices"
	"strings"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/datapack"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// loadFunctions loads the selected packs' reloadable resources; a function that
// fails is logged and skipped. vanilla: ServerFunctionLibrary.reload
func (s *Server) loadFunctions(dirs []string) error {
	skip := func(file string, err error) {
		slog.Warn("Skipping data pack file " + file + ": " + err.Error())
	}
	files, err := datapack.LoadFunctions(dirs, skip)
	if err != nil {
		return err
	}
	fns := command.CompileFunctions(files, s.Config.FunctionPermissionLevel, func(id string, err error) {
		slog.Error("Failed to load function " + id + ": " + err.Error())
	})
	tags, err := datapack.LoadFunctionTags(dirs, skip)
	if err != nil {
		return err
	}
	fns.LoadTags(tags, func(id string, err error) {
		slog.Error("Couldn't load tag " + id + " as it is missing following references: " + err.Error())
	})
	packTags, err := loadPackTags(dirs, skip)
	if err != nil {
		return err
	}
	var tagLayer *registry.PackTagLayer
	var tagProblems []string
	// The tables are process-wide. Leave them alone if this server has never
	// loaded any pack tags.
	if len(packTags) != 0 || s.packTagsApplied {
		tagLayer, tagProblems = registry.PreparePackTags(packTags)
	}
	// Providers can reference each other and tags from the candidate packs.
	// Decode them before replacing any of the resources used by the server.
	applyProviders, err := loadPackContextProviders(dirs, tagLayer, skip)
	if err != nil {
		return err
	}
	s.templateFiles.SetPacks(dirs)
	s.structures.Clear()
	if tagLayer != nil {
		tagLayer.Apply()
		s.packTagsApplied = len(packTags) != 0
		for _, problem := range tagProblems {
			slog.Error("Couldn't load tag " + problem)
		}
	}
	// Advancement criteria resolve item tags against the loaded tags.
	packAdvancements, hiddenAdvancements, err := loadPackAdvancements(dirs, skip)
	if err != nil {
		return err
	}
	s.applyPackAdvancements(packAdvancements, hiddenAdvancements)
	// Before the loot: a table's context provider names resolve against them.
	applyProviders()
	// Before the loot: a slots entry's source names resolve against them.
	if err := loadPackSlotSources(dirs, skip); err != nil {
		return err
	}
	// After the tags: a table's tag entries resolve against the loaded tags.
	tables, predicates, modifiers, err := loadPackLoot(dirs, skip)
	if err != nil {
		return err
	}
	s.applyPackLoot(tables, predicates, modifiers, hiddenLoot(dirs, tables))
	// After the tags too: an ingredient's tag resolves against the loaded tags.
	recipes, err := loadPackRecipes(dirs, skip)
	if err != nil {
		return err
	}
	s.applyPackRecipes(recipes)
	// A new recipe or ingredient tag can make unchanged contents tickable.
	for _, dim := range s.allDims() {
		for _, be := range dim.w.TickingBlockEntities() {
			be.Wake()
		}
	}
	s.funcs.Store(&loadedFunctions{lib: fns, tick: fns.Tag("minecraft:tick")})
	s.loadPending.Store(true)
	return nil
}

// loadedFunctions is one load's function library and its minecraft:tick tag.
type loadedFunctions struct {
	lib  *command.Functions
	tick []*command.Function
}

// openRepository scans the world's packs and selects those of enabled and the
// automatic ones not in disabled; safe mode selects vanilla alone.
func (s *Server) openRepository(enabled, disabled []string, skip func(file string, err error)) (*datapack.Repository, error) {
	if s.Config.SafeMode {
		slog.Warn("Safe mode active, only vanilla datapack will be loaded")
		return datapack.OpenOnly(s.levelDir, []string{"vanilla"}, skip)
	}
	return datapack.Open(s.levelDir, enabled, disabled, skip)
}

// openDataPacks selects the world's packs from level.dat, or for a world
// with none from server.properties' initial-enabled-packs and
// initial-disabled-packs (or, in safe mode, vanilla alone), and loads their registry entries and functions.
// vanilla: MinecraftServer.configurePackRepository
func (s *Server) openDataPacks() error {
	enabled, disabled, ok := s.world.DataPacks()
	if !s.world.Existed() || !ok {
		enabled, disabled = splitPackList(s.Config.InitialEnabledPacks), splitPackList(s.Config.InitialDisabledPacks)
	}
	skip := func(file string, err error) {
		slog.Warn("Skipping data pack file " + file + ": " + err.Error())
	}
	repo, err := s.openRepository(enabled, disabled, skip)
	if err != nil {
		return err
	}
	s.packs = &serverPacks{Repository: repo, srv: s}
	enabled, disabled = repo.Config()
	if s.Config.SafeMode {
		// vanilla's safe mode passes disableInactive false: no disabled list.
		disabled = nil
	}
	s.world.SetDataPacks(enabled, disabled)
	// Before the functions: pack tags may name pack entries.
	if err := loadPackRegistries(s.packs.Dirs(), skip); err != nil {
		return err
	}
	if err := loadPackJigsaw(s.templateFiles.WorldDir, s.packs.Dirs(), skip); err != nil {
		return err
	}
	if err := loadPackTrades(s.packs.Dirs(), skip); err != nil {
		return err
	}
	if s.packStems, err = loadPackStems(s.packs.Dirs()); err != nil {
		return err
	}
	return s.loadFunctions(s.packs.Dirs())
}

func splitPackList(list string) []string {
	var out []string
	for id := range strings.SplitSeq(list, ",") {
		if id = strings.TrimSpace(id); id != "" {
			out = append(out, id)
		}
	}
	return out
}

// serverPacks is /datapack's repository: the selection is kept in level.dat
// and applied by reloading functions.
type serverPacks struct {
	*datapack.Repository
	srv *Server
}

// Edit selects the packs change names and reloads, with packReloadMu held
// across change too: vanilla reads and replaces the selection on the server
// thread, so two /datapack commands never interleave.
// vanilla: MinecraftServer.reloadResources
func (p *serverPacks) Edit(change func() (ids []string, ok bool)) error {
	p.srv.packReloadMu.Lock()
	defer p.srv.packReloadMu.Unlock()
	ids, ok := change()
	if !ok {
		return nil
	}
	return p.selectLocked(ids)
}

// selectLocked loads the packs and commits their selection with packReloadMu held.
func (p *serverPacks) selectLocked(ids []string) error {
	if err := p.srv.loadFunctions(p.DirsFor(ids)); err != nil {
		return err
	}
	p.SetSelected(ids)
	p.srv.world.SetDataPacks(p.Config())
	p.srv.resendResources()
	return nil
}

// ReloadAll rescans and includes packs that the world's last successful
// reload did not disable. vanilla: ReloadCommand.discoverNewPacks
func (p *serverPacks) ReloadAll() error {
	p.srv.packReloadMu.Lock()
	defer p.srv.packReloadMu.Unlock()
	if err := p.Reload(); err != nil {
		return err
	}
	_, disabled, _ := p.srv.world.DataPacks()
	selected := p.SelectedIDs()
	for _, pk := range p.Available() {
		if pk.Source == datapack.SourceWorld && !slices.Contains(disabled, pk.ID) && !slices.Contains(selected, pk.ID) {
			selected = append(selected, pk.ID)
		}
	}
	return p.selectLocked(selected)
}

// resendResources resends every player the tags, recipe book item groups,
// stonecutter recipes and their book, rebuilt from the packs now enabled.
// vanilla: PlayerList.reloadResources
func (s *Server) resendResources() {
	tags, err := tagsPayload()
	if err != nil {
		slog.Error("Failed to build the tags: " + err.Error())
		return
	}
	s.broadcast(protocol.IDPlayUpdateTags, tags)
	recipes := updateRecipesPayload()
	s.mu.RLock()
	players := s.clientsLocked()
	s.mu.RUnlock()
	for _, c := range players {
		c.send(protocol.IDPlayUpdateRecipes, recipes)
		c.send(protocol.IDPlayRecipeBookSettings, recipeBookSettingsPayload(c.recipeBookState()))
		c.send(protocol.IDPlayRecipeBookAdd, c.recipeBookAddPayload())
	}
}

// runScheduled runs a /schedule event's function or tag as the game loop
// source. vanilla: FunctionCallback.handle, FunctionTagCallback.handle
func (s *Server) runScheduled(cb world.TimerCallback) {
	lib := s.funcs.Load().lib
	fns := lib.Tag(cb.ID)
	if !cb.Tag {
		fn, ok := lib.Get(cb.ID)
		if !ok {
			return
		}
		fns = []*command.Function{fn}
	}
	s.runTag(fns)
}

// tickFunctions runs the load tag once after a (re)load, then the tick tag,
// as the game loop source; the caller skips a frozen game.
// vanilla: ServerFunctionManager.tick
func (s *Server) tickFunctions() {
	// Swap before Load: a reload stores funcs before loadPending, so a load
	// tag seen pending is always run from that reload's library or a later one.
	pending := s.loadPending.Swap(false)
	funcs := s.funcs.Load()
	if pending {
		s.runTag(funcs.lib.Tag("minecraft:load"))
	}
	s.runTag(funcs.tick)
}

func (s *Server) runTag(fns []*command.Function) {
	for _, fn := range fns {
		command.RunFunction(s.gameLoopSource(), fn)
	}
}
