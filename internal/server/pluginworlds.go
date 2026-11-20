package server

import (
	"errors"
	"fmt"
	"slices"

	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/pluginruntime"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
)

// vanillaStemSettings is the noise settings of each vanilla dimension type's
// generator, and the biome source vanilla pairs with it.
var vanillaStemSettings = map[string][2]string{
	world.DimensionType:         {"minecraft:overworld", "overworld"},
	"minecraft:overworld_caves": {"minecraft:overworld", "overworld"},
	world.NetherType:            {"minecraft:nether", "nether"},
	world.EndType:               {"minecraft:end", "the_end"},
}

// vanillaStem is the level stem generator compound vanilla's data pack gives
// the dimension type.
func vanillaStem(typeName string) (nbt.Compound, bool) {
	v, ok := vanillaStemSettings[typeName]
	if !ok {
		return nil, false
	}
	source := nbt.Compound{"type": nbt.String("minecraft:multi_noise"), "preset": nbt.String("minecraft:" + v[1])}
	if v[1] == "the_end" {
		source = nbt.Compound{"type": nbt.String("minecraft:the_end")}
	}
	return nbt.Compound{"type": nbt.String("minecraft:noise"), "settings": nbt.String(v[0]), "biome_source": source}, true
}

// pluginSeaLevel is a plugin generator's sea level for the dimension type.
// vanilla: NoiseGeneratorSettings.seaLevel
func pluginSeaLevel(shape world.DimensionShape) int32 {
	switch shape.Type() {
	case world.NetherType:
		return 32
	case world.EndType:
		return 0
	}
	return world.SeaLevel
}

// CreateWorld loads or creates the dimension opts names, on the tick
// goroutine. A key already loaded answers that world; one whose files exist
// is loaded from them, with the seed and spawn it was created with.
// vanilla: MinecraftServer.createLevels
func (h *pluginHost) CreateWorld(opts plugin.WorldOptions) (plugin.World, error) {
	s := h.srv
	name := opts.Key.String()
	if key, err := plugin.ParseKey(name); err != nil || key != opts.Key {
		return nil, fmt.Errorf("invalid world key %q", name)
	}
	if opts.Dimension == (plugin.Key{}) {
		opts.Dimension = plugin.Key{Namespace: "minecraft", Path: "overworld"}
	}
	typeName := opts.Dimension.String()
	vanilla := name == world.DimensionName || name == world.NetherName || name == world.EndName
	if d := s.findDimension(name); d != nil {
		w := pluginWorld{srv: s, dim: d, host: h}
		if vanilla && (opts.Generator != nil || w.Dimension() != opts.Dimension) {
			return nil, fmt.Errorf("world %s is a vanilla dimension and cannot be changed", name)
		}
		return w, nil
	}
	if vanilla {
		return nil, fmt.Errorf("world %s is not loaded", name)
	}
	types, _ := registry.Get("minecraft:dimension_type")
	typeID := types.ID(typeName)
	if typeID < 0 {
		return nil, fmt.Errorf("unknown dimension type %s", typeName)
	}
	shape, err := world.ShapeOfType(typeName, types.Data[typeName])
	if err != nil {
		return nil, err
	}

	meta, existed := world.ReadPluginMeta(s.levelDir, name)
	seed := meta.Seed
	if !existed {
		if seed = opts.Seed; seed == 0 {
			seed = s.world.SeedValue()
		}
	}
	spec := world.PluginGenSpec{Type: string(opts.Type), Settings: opts.GeneratorSettings, Biome: opts.Biome.String(), NoStructures: opts.NoStructures}
	if opts.Biome == (plugin.Key{}) {
		spec.Biome = ""
	}
	if existed {
		spec = meta.Gen
	}
	var gen world.Generator
	if opts.Generator != nil {
		gen = &pluginGenerator{gen: opts.Generator, seed: seed, minY: int(shape.MinY()), height: int(shape.Height()), seaLevel: pluginSeaLevel(shape), dimensionN: name}
	} else {
		stem, ok := vanillaStem(typeName)
		if !ok && spec.Type != "flat" {
			return nil, fmt.Errorf("dimension type %s has no vanilla generator, give the world a Generator", typeName)
		}
		switch {
		case spec.Type == "flat" || (!spec.Plain() && typeName == world.DimensionType):
			gen, err = world.NewPluginOverworldGenerator(spec, shape, seed)
		case !spec.Plain():
			err = fmt.Errorf("world type, generator settings and biome only apply to flat worlds and the overworld type")
		default:
			gen, err = world.StemGenerator(stem, shape, seed, s.Config.GenerateStructures && !spec.NoStructures)
			if og, ok := gen.(*world.OverworldGenerator); ok && spec.NoStructures {
				og.DisableStructures()
			}
		}
		if err != nil {
			return nil, err
		}
	}
	d := &dimension{name: name, typeID: typeID, gen: gen, plugin: true, genSpec: spec, w: world.NewCustomDimensionWorld(s.levelDir, name, shape, gen)}
	if err := s.initDimension(d, false); err != nil {
		return nil, err
	}
	d.w.HoldForcedChunks()
	spawn := meta.Spawn
	d.w.SetOwnSeedAndSpawn(seed, spawn)
	s.addDimension(d)
	handle := pluginWorld{srv: s, dim: d, host: h}
	if !existed {
		spawn = initialPluginSpawn(opts.Generator, gen, handle)
		d.w.SetOwnSeedAndSpawn(seed, spawn)
		d.w.SavePluginMeta(world.PluginMeta{Seed: seed, Spawn: spawn, Gen: spec})
	}
	s.fireWorldLoad(d)
	return handle, nil
}

// initialPluginSpawn is where a new plugin world's players first spawn: the
// plugin's own answer, else the overworld generator's search, else the origin
// at sea level.
func initialPluginSpawn(plug plugin.Generator, gen world.Generator, handle plugin.World) world.SpawnPos {
	if plug != nil {
		loc := plug.SpawnPoint(handle)
		return world.SpawnPos{X: int32(loc.Pos.Block().X), Y: int32(loc.Pos.Block().Y), Z: int32(loc.Pos.Block().Z), Yaw: world.WrapDegrees(loc.Yaw), Pitch: max(min(loc.Pitch, 90), -90)}
	}
	if og, ok := gen.(*world.OverworldGenerator); ok {
		return og.FindSpawn()
	}
	return world.SpawnPos{Y: gen.SeaLevel() + 1}
}

// WorldFolder answers the folder of the world key names; the overworld's is
// the level directory.
func (h *pluginHost) WorldFolder(key plugin.Key) string {
	name := key.String()
	if name == world.DimensionName {
		return h.srv.levelDir
	}
	return world.DimensionDir(h.srv.levelDir, name)
}

// UnloadWorld unloads a plugin-created world on the tick goroutine after
// WorldUnloadEvent: its players go to the world spawn, its entities are
// written (when save is set) and dropped, and its files close. The vanilla
// dimensions and a data pack's cannot be unloaded.
func (h *pluginHost) UnloadWorld(w plugin.World, save bool) error {
	s := h.srv
	pw, ok := w.(pluginWorld)
	if !ok || !slices.Contains(s.allDims(), pw.dim) {
		return errors.New("world is not loaded")
	}
	d := pw.dim
	if !d.plugin {
		return fmt.Errorf("world %s cannot be unloaded", d.name)
	}
	if r := s.plugins; r != nil && r.Has(pluginruntime.WorldUnload) {
		e := &plugin.WorldUnloadEvent{World: pluginWorld{srv: s, dim: d}}
		r.Fire(e)
		if e.Cancelled() {
			return errors.New("unload cancelled")
		}
	}
	if !s.savingMu.TryLock() {
		return errors.New("a save is running, try again")
	}
	defer s.savingMu.Unlock()

	// vanilla: ServerPlayer.teleport to the respawn dimension
	to, spot := s.worldSpawn()
	if to == d {
		to, spot = s.allDims()[0], borderAdjustedSpawn(s.allDims()[0], s.world.Spawn())
	}
	for _, c := range s.playerClientsIn(d.name) {
		if err := c.enterDimension(to, command.Move{Pos: standingAt(spot)}, respawnNone, false); err != nil {
			c.log.Warn("moving a player out of an unloading world failed", "error", err)
		}
	}
	if len(s.playerClientsIn(d.name)) > 0 {
		return errors.New("players could not be moved out of the world")
	}

	// Chunks are held so saveMobs, which writes loaded chunks, still finds
	// them; the world closes right after.
	for _, pos := range d.w.LoadedChunkPositions() {
		d.w.HoldIfLoaded(pos)
	}
	if save {
		s.saveMobs()
		d.w.Save()
		d.w.SaveBorder(d.border)
		if d.raids.Dirty() {
			d.w.WriteRaids(d.raids.Encode())
		}
	}
	d.w.DrainSaves()
	s.dropEntitiesIn(d.w)
	if err := d.w.Close(); err != nil {
		h.Logger().Error("closing world", "world", d.name, "error", err)
	}
	s.removeDimension(d)
	h.dropTicketsIn(d)
	return nil
}

// dropTicketsIn forgets the plugin's tickets in d, whose world is closed.
func (h *pluginHost) dropTicketsIn(d *dimension) {
	h.ticketMu.Lock()
	defer h.ticketMu.Unlock()
	for key := range h.held {
		if key.dim == d {
			delete(h.held, key)
		}
	}
}

// dropEntitiesIn removes every entity the server lists in w, once its chunks
// have been written: the world is closing and its entities do not outlive it.
func (s *Server) dropEntitiesIn(w *world.World) {
	for _, sweep := range entitySweeps {
		sweep(s, func(ew *world.World) bool { return ew != w })
	}
}

// fireWorldLoad tells the plugins d is loaded.
func (s *Server) fireWorldLoad(d *dimension) {
	if r := s.plugins; r != nil && r.Has(pluginruntime.WorldLoad) {
		r.Fire(&plugin.WorldLoadEvent{World: pluginWorld{srv: s, dim: d}})
	}
}

// fireWorldSaves tells the plugins each dimension is about to be saved.
func (s *Server) fireWorldSaves(dims []*dimension) {
	if r := s.plugins; r != nil && r.Has(pluginruntime.WorldSave) {
		for _, d := range dims {
			r.Fire(&plugin.WorldSaveEvent{World: pluginWorld{srv: s, dim: d}})
		}
	}
}
