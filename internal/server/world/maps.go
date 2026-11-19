package world

import (
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"math"
	"slices"
	"sync"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// MapSide is the width and the height of a map's face in pixels; a saved
// map's colours are the square of it. vanilla: MapItemSavedData.MAP_SIZE
const MapSide = 128

// The map_decoration_type registry ids of map markers, from the registry
// report; banner colours run from white at 10 to black at 25.
// vanilla: MapDecorationTypes, MapDecorationType.STREAM_CODEC
const (
	DecorationPlayer          = 0
	DecorationFrame           = 1
	DecorationPlayerOffMap    = 6
	DecorationPlayerOffLimits = 7
)

// Decoration is a map marker as the wire carries it: type id, pixel and
// direction. Name is a banner's name, nil otherwise.
type Decoration struct {
	Type int32
	X, Y int8
	Rot  int8
	Name *protocol.Text
	// key names a banner's marker so toggling it finds its entry; empty
	// otherwise.
	key string
}

// MapBanner is a banner a map tracks: block position, base colour name and
// custom name. vanilla: MapBanner
type MapBanner struct {
	X, Y, Z int32
	Color   string
	Name    *protocol.Text
}

func (b MapBanner) key() string { return fmt.Sprintf("banner-%d,%d,%d", b.X, b.Y, b.Z) }

func (b MapBanner) equal(o MapBanner) bool {
	if b.X != o.X || b.Y != o.Y || b.Z != o.Z || b.Color != o.Color || (b.Name == nil) != (o.Name == nil) {
		return false
	}
	return b.Name == nil || b.Name.Plain() == o.Name.Plain()
}

// bannerColors names the colour of each banner block, standing or wall.
var bannerColors = func() map[registry.Block]string {
	m := map[registry.Block]string{}
	for color := range bannerDecorations {
		for _, name := range []string{color + "_banner", color + "_wall_banner"} {
			if b, ok := registry.BlockByName("minecraft:" + name); ok {
				m[b] = color
			}
		}
	}
	return m
}()

// BannerAt reads the banner at a position: its block's colour and the block
// entity's custom name; false without a banner block entity.
// vanilla: MapBanner.fromWorld
func (w *World) BannerAt(x, y, z int32) (MapBanner, bool) {
	color, ok := bannerColors[registry.BlockOf(w.BlockAt(x, y, z))]
	if !ok {
		return MapBanner{}, false
	}
	be := w.BlockEntity(x, y, z)
	if be == nil || be.Kind != KindBanner {
		return MapBanner{}, false
	}
	return MapBanner{X: x, Y: y, Z: z, Color: color, Name: be.CustomName}, true
}

// MapData is one saved map: scale, whether a cartography table locked it, one
// colour byte per pixel, the saved banner and frame markers, and the centre and
// dimension new markers are placed against.
type MapData struct {
	Scale  int8
	Locked bool
	Colors [MapSide * MapSide]byte

	CenterX, CenterZ int32
	Dimension        string
	// TrackingPosition and UnlimitedTracking are whether a holder's marker is
	// drawn (default true) and whether the map tracks past its edge (default
	// false).
	TrackingPosition  bool
	UnlimitedTracking bool

	// Decorations are the file's banner and frame markers, in file order.
	Decorations []Decoration
	// Banners are the banners the map tracks, saved as the "banners" list.
	Banners []MapBanner
	// frames is the file's "frames" list, kept so a rewrite writes it back.
	frames nbt.List
	// ItemDecorations are targets imported from carried items, not saved map markers.
	ItemDecorations    []Decoration
	itemDecorationKeys map[string]bool
}

// mapIDCounterFile holds the highest map id allocated. A missing file reads -1
// (vanilla's NO_MAP_ID), so a world's first map is 0.
// vanilla: MapIndex
const mapIDCounterFile = "data/minecraft/maps/last_id.dat"

// Maps is the world's saved maps, one file per map at
// data/minecraft/maps/<id>.dat, server-wide outside every dimension's folder,
// so a map brought from vanilla keeps its pixels. A file is read when a player
// first holds its map and cached, missing or unreadable ones as absent. Maps an
// empty map item makes are stored here, drawn from the holder's surroundings
// and redrawn as they walk.
// vanilla: SavedDataStorage.readSavedData, MapItemSavedData.CODEC
type Maps struct {
	root string
	mu   sync.RWMutex
	// files caches each map read by id; nil is a file that gave nothing to
	// show.
	files map[int32]*MapData
	// lastID is the highest map id allocated, read from the counter file on the
	// first create; -1 is NO_MAP_ID.
	lastID int32
	// counterRead is whether lastID has been read from the counter file.
	counterRead bool
	// dirty is the maps made this run that the next save still owes.
	dirty map[int32]bool
	// counterDirty is whether lastID moved since the last save.
	counterDirty bool
}

// NewMaps holds the maps of the world under root.
func NewMaps(root string) *Maps {
	return &Maps{root: root, files: map[int32]*MapData{}, dirty: map[int32]bool{}}
}

// Get returns the saved map for id and whether one exists. An id no file
// answers (negative, missing or unreadable) is no map, as vanilla treats a
// filled map without saved data.
// vanilla: MapItem.getSavedData, ServerLevel.getMapData
func (m *Maps) Get(id int32) (*MapData, bool) {
	if id < 0 {
		return nil, false
	}
	m.mu.RLock()
	data, read := m.files[id]
	m.mu.RUnlock()
	if read {
		return data, data != nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	// Another reader may have loaded it while this call waited for the write lock.
	if data, read = m.files[id]; !read {
		data = m.read(id)
		m.files[id] = data
	}
	return data, data != nil
}

// Create makes a fresh map (blank, tracking on, unlimited tracking off) centred
// on the area its origin falls in (128 pixels doubled per scale step), stores
// it under the next id, and returns both.
// vanilla: MapItem.create, MapItemSavedData.createFresh, MapIndex.getNextMapId
func (m *Maps) Create(dimension string, originX, originZ int32, scale int8) (int32, *MapData) {
	m.mu.Lock()
	defer m.mu.Unlock()
	size := int32(MapSide) << uint(scale)
	areaX := int32(math.Floor((float64(originX) + 64) / float64(size)))
	areaZ := int32(math.Floor((float64(originZ) + 64) / float64(size)))
	data := &MapData{
		Scale:            scale,
		CenterX:          areaX*size + size/2 - 64,
		CenterZ:          areaZ*size + size/2 - 64,
		Dimension:        dimension,
		TrackingPosition: true,
	}
	return m.store(data), data
}

// store saves data under the next id and returns it; m.mu is held.
// vanilla: MapIndex.getNextMapId
func (m *Maps) store(data *MapData) int32 {
	if !m.counterRead {
		m.lastID = m.readCounter()
		m.counterRead = true
	}
	m.lastID++
	m.files[m.lastID] = data
	m.dirty[m.lastID] = true
	m.counterDirty = true
	return m.lastID
}

// Scalable reports whether the map can be extended: it has saved data, is not
// an exploration map and is below the largest scale.
// vanilla: MapExtendingRecipe.matches
func (m *Maps) Scalable(id int32) bool {
	data, ok := m.Get(id)
	if !ok || data.Scale >= 4 {
		return false
	}
	for _, d := range m.Markers(data) {
		if registry.MapDecorationIsExploration(d.Type) {
			return false
		}
	}
	return true
}

// Markers is a fresh copy of the map's file markers followed by its
// carried-item markers, taken under the lock: ToggleBanner, CheckBanners and
// AddItemDecorations replace those slices from other goroutines.
func (m *Maps) Markers(data *MapData) []Decoration {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return append(slices.Clone(data.Decorations), data.ItemDecorations...)
}

// Scaled saves a blank map one scale larger, around the same centre, under a
// fresh id and returns that id.
// vanilla: MapItemSavedData.scaled, MapItem.scaleMap
func (m *Maps) Scaled(id int32) (int32, bool) {
	data, ok := m.Get(id)
	if !ok {
		return 0, false
	}
	newID, scaled := m.Create(data.Dimension, data.CenterX, data.CenterZ, min(data.Scale+1, 4))
	scaled.TrackingPosition, scaled.UnlimitedTracking = data.TrackingPosition, data.UnlimitedTracking
	return newID, true
}

// Locked saves a locked copy of the map (centre, scale, tracking, markers
// and colours) under a fresh id and returns that id.
// vanilla: MapItemSavedData.locked, MapItem.lockMap
func (m *Maps) Locked(id int32) (int32, bool) {
	data, ok := m.Get(id)
	if !ok {
		return 0, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	locked := &MapData{
		Scale:              data.Scale,
		Locked:             true,
		Colors:             data.Colors,
		CenterX:            data.CenterX,
		CenterZ:            data.CenterZ,
		Dimension:          data.Dimension,
		TrackingPosition:   data.TrackingPosition,
		UnlimitedTracking:  data.UnlimitedTracking,
		Decorations:        slices.Clone(data.Decorations),
		Banners:            slices.Clone(data.Banners),
		ItemDecorations:    slices.Clone(data.ItemDecorations),
		itemDecorationKeys: maps.Clone(data.itemDecorationKeys),
	}
	return m.store(locked), true
}

// State is the map's scale and whether it is locked; ok is false for an id
// with no saved data.
func (m *Maps) State(id int32) (scale int8, locked, ok bool) {
	data, ok := m.Get(id)
	if !ok {
		return 0, false, false
	}
	return data.Scale, data.Locked, true
}

// readCounter reads the highest allocated map id, -1 (NO_MAP_ID) for a missing
// or unreadable counter.
// vanilla: MapIndex.CODEC
func (m *Maps) readCounter() int32 {
	data, err := readSavedData(m.root, mapIDCounterFile)
	if errors.Is(err, errNoSavedData) {
		return -1
	} else if err != nil {
		slog.Warn("could not read the map id counter", "error", err)
		return -1
	}
	if last, ok := nbt.IntValue(data["map"]); ok {
		return last
	}
	return -1
}

// read loads a map file, gzipped {DataVersion, data} in the MapItemSavedData
// shape. An unreadable file gives nothing (vanilla's colours for it would be
// zeros anyway), and so does one missing the codec's required dimension or
// centre.
// vanilla: MapItemSavedData.CODEC
func (m *Maps) read(id int32) *MapData {
	data, err := readSavedData(m.root, fmt.Sprintf("data/minecraft/maps/%d.dat", id))
	if errors.Is(err, errNoSavedData) {
		return nil
	} else if err != nil {
		slog.Warn("could not read a map's saved data", "map", id, "error", err)
		return nil
	}
	dimension, ok := nbt.Get[nbt.String](data, "dimension")
	if !ok {
		return nil
	}
	xCenter, ok := nbt.IntValue(data["xCenter"])
	if !ok {
		return nil
	}
	zCenter, ok := nbt.IntValue(data["zCenter"])
	if !ok {
		return nil
	}
	saved := &MapData{
		CenterX:          xCenter,
		CenterZ:          zCenter,
		Dimension:        string(dimension),
		TrackingPosition: true,
	}
	if tracking, ok := nbt.IntValue(data["trackingPosition"]); ok {
		saved.TrackingPosition = tracking != 0
	}
	if unlimited, ok := nbt.IntValue(data["unlimitedTracking"]); ok {
		saved.UnlimitedTracking = unlimited != 0
	}
	if scale, ok := nbt.IntValue(data["scale"]); ok {
		// The codec clamps the scale to 0-4 whatever the file says.
		// vanilla: Mth.clamp in the codec constructor
		saved.Scale = int8(min(max(scale, 0), 4))
	}
	if locked, ok := nbt.IntValue(data["locked"]); ok {
		saved.Locked = locked != 0
	}
	if colors, ok := nbt.Get[nbt.ByteArray](data, "colors"); ok && len(colors) == MapSide*MapSide {
		copy(saved.Colors[:], colors)
	}
	decorations, ok := saved.fileDecorations(data)
	if !ok {
		return nil
	}
	saved.Decorations = decorations
	saved.frames, _ = nbt.Get[nbt.List](data, "frames")
	return saved
}

// Save writes the maps made since the last save and the counter, if either
// moved; maps read from files are never rewritten.
func (m *Maps) Save() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id := range m.dirty {
		data := m.files[id]
		if data == nil {
			delete(m.dirty, id)
			continue
		}
		if err := writeSavedData(m.root, fmt.Sprintf("data/minecraft/maps/%d.dat", id), data.encode()); err != nil {
			slog.Error("could not save a map", "map", id, "error", err)
			continue
		}
		delete(m.dirty, id)
	}
	if !m.counterDirty {
		return
	}
	if err := writeSavedData(m.root, mapIDCounterFile, nbt.Compound{"map": nbt.Int(m.lastID)}); err != nil {
		slog.Error("could not save the map id counter", "error", err)
		return
	}
	m.counterDirty = false
}

// encode writes a map in the codec's shape: dimension, centre and colours
// always, scale and flags only where they differ from the defaults (optional
// fields).
// vanilla: MapItemSavedData.CODEC
func (d *MapData) encode() nbt.Compound {
	data := nbt.Compound{
		"dimension": nbt.String(d.Dimension),
		"xCenter":   nbt.Int(d.CenterX),
		"zCenter":   nbt.Int(d.CenterZ),
		"colors":    nbt.ByteArray(d.Colors[:]),
	}
	if d.Scale != 0 {
		data["scale"] = nbt.Byte(d.Scale)
	}
	if !d.TrackingPosition {
		data["trackingPosition"] = nbt.Byte(0)
	}
	if d.UnlimitedTracking {
		data["unlimitedTracking"] = nbt.Byte(1)
	}
	if d.Locked {
		data["locked"] = nbt.Byte(1)
	}
	if len(d.Banners) > 0 {
		list := nbt.List{Elem: nbt.TypeCompound}
		for _, b := range d.Banners {
			entry := nbt.Compound{"pos": nbt.IntArray{b.X, b.Y, b.Z}, "color": nbt.String(b.Color)}
			if b.Name != nil {
				entry["name"] = protocol.TextTag(*b.Name)
			}
			list.Items = append(list.Items, entry)
		}
		data["banners"] = list
	}
	if len(d.frames.Items) > 0 {
		data["frames"] = d.frames
	}
	return data
}

// bannerDecorations maps each banner colour to its map_decoration_type id, from
// the registry report.
// vanilla: MapBanner.getDecoration, MapDecorationTypes
var bannerDecorations = map[string]int32{
	"white":      10,
	"orange":     11,
	"magenta":    12,
	"light_blue": 13,
	"yellow":     14,
	"lime":       15,
	"pink":       16,
	"gray":       17,
	"light_gray": 18,
	"cyan":       19,
	"purple":     20,
	"blue":       21,
	"brown":      22,
	"green":      23,
	"red":        24,
	"black":      25,
}

// fileDecorations places the file's banner and frame markers, false for a file
// the codec would refuse (an unknown banner colour, an unrepresentable name, a
// frame without rotation or id).
// vanilla: MapItemSavedData.CODEC, the MapBanner and MapFrame lists
func (d *MapData) fileDecorations(data nbt.Compound) ([]Decoration, bool) {
	var decorations []Decoration
	if banners, ok := nbt.Get[nbt.List](data, "banners"); ok {
		for _, item := range banners.Items {
			banner, ok := item.(nbt.Compound)
			if !ok {
				return nil, false
			}
			pos, ok := nbt.Get[nbt.IntArray](banner, "pos")
			if !ok || len(pos) != 3 {
				return nil, false
			}
			color := "white"
			if name, ok := nbt.Get[nbt.String](banner, "color"); ok {
				color = string(name)
			}
			typeID, ok := bannerDecorations[color]
			if !ok {
				return nil, false
			}
			mb := MapBanner{X: pos[0], Y: pos[1], Z: pos[2], Color: color}
			if name, ok := banner["name"]; ok {
				text, ok := protocol.StyledTextOf(name)
				if !ok {
					return nil, false
				}
				mb.Name = &text
			}
			d.Banners = append(d.Banners, mb)
			decoration, ok := d.PlaceDecoration(typeID, float64(pos[0]), float64(pos[2]), 180, 0, false)
			if !ok {
				continue
			}
			decoration.Name, decoration.key = mb.Name, mb.key()
			decorations = append(decorations, decoration)
		}
	}
	if frames, ok := nbt.Get[nbt.List](data, "frames"); ok {
		for _, item := range frames.Items {
			frame, ok := item.(nbt.Compound)
			if !ok {
				return nil, false
			}
			pos, ok := nbt.Get[nbt.IntArray](frame, "pos")
			if !ok || len(pos) != 3 {
				return nil, false
			}
			rotation, ok := nbt.IntValue(frame["rotation"])
			if !ok {
				return nil, false
			}
			decoration, ok := d.PlaceDecoration(DecorationFrame, float64(pos[0]), float64(pos[2]), float64(rotation), 0, false)
			if !ok {
				continue
			}
			decorations = append(decorations, decoration)
		}
	}
	return decorations, true
}

// PlaceDecoration turns a world position into a marker's pixel, false for a
// marker off a map that does not track past its edge. gameTime and hasLevel are
// the level the marker came from (a saved marker has none): a nether map turns
// its markers by the level's time, others by yRot degrees.
// vanilla: MapItemSavedData.addDecoration, calculateDecorationLocationAndType
func (d *MapData) PlaceDecoration(typeID int32, x, z, yRot float64, gameTime int64, hasLevel bool) (Decoration, bool) {
	scaling := float64(int32(1) << uint(d.Scale))
	xDelta := (x - float64(d.CenterX)) / scaling
	zDelta := (z - float64(d.CenterZ)) / scaling
	if typeID == DecorationPlayer {
		if !insideMap(xDelta, zDelta) {
			if !(math.Abs(xDelta) < 320 && math.Abs(zDelta) < 320) {
				if !d.UnlimitedTracking {
					return Decoration{}, false
				}
				typeID = DecorationPlayerOffLimits
			} else {
				typeID = DecorationPlayerOffMap
			}
			return Decoration{Type: typeID, X: clampMapCoordinate(xDelta), Y: clampMapCoordinate(zDelta)}, true
		}
		return Decoration{Type: typeID, X: clampMapCoordinate(xDelta), Y: clampMapCoordinate(zDelta), Rot: d.rotation(yRot, gameTime, hasLevel)}, true
	}
	if !insideMap(xDelta, zDelta) && !d.UnlimitedTracking {
		return Decoration{}, false
	}
	return Decoration{Type: typeID, X: clampMapCoordinate(xDelta), Y: clampMapCoordinate(zDelta), Rot: d.rotation(yRot, gameTime, hasLevel)}, true
}

// rotation is a marker's direction: the nether's scramble of the level's time
// on a nether map, else the degrees rounded to sixteenths, masked to the wire's
// four bits.
// vanilla: MapItemSavedData.calculateRotation, MapDecoration's rot & 0xF
func (d *MapData) rotation(yRot float64, gameTime int64, hasLevel bool) int8 {
	if d.Dimension == NetherName && hasLevel {
		s := int32(gameTime / 10)
		return int8(((s*s*34187121 + s*121) >> 15) & 0xF)
	}
	adjusted := yRot + 8
	if yRot < 0 {
		adjusted = yRot - 8
	}
	return int8(adjusted*16/360) & 0xF
}

// insideMap reports whether a block offset from the centre is on the face,
// within the -63..63 the clamp pins.
// vanilla: MapItemSavedData.isInsideMap
func insideMap(xDelta, zDelta float64) bool {
	return xDelta >= -63 && zDelta >= -63 && xDelta <= 63 && zDelta <= 63
}

// clampMapCoordinate is a block offset's wire pixel, half a pixel per block,
// edges pinned.
// vanilla: MapItemSavedData.clampMapCoordinate
func clampMapCoordinate(delta float64) int8 {
	if delta <= -63 {
		return -128
	}
	if delta >= 63 {
		return 127
	}
	return int8(delta*2 + 0.5)
}

// AddItemDecorations imports previously unseen target keys from a carried map.
// These markers live on the item on disk, and in the map's marker set at runtime.
// vanilla: MapItemSavedData.tickCarriedBy
func (m *Maps) AddItemDecorations(id int32, entries map[string]protocol.MapDecoration) {
	if len(entries) == 0 {
		return
	}
	data, ok := m.Get(id)
	if !ok {
		return
	}
	m.mu.RLock()
	unseen := false
	for key := range entries {
		if !data.itemDecorationKeys[key] {
			unseen = true
			break
		}
	}
	m.mu.RUnlock()
	if !unseen {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, key := range slices.Sorted(maps.Keys(entries)) {
		if data.itemDecorationKeys[key] {
			continue
		}
		entry := entries[key]
		typeID, _, ok := registry.MapDecorationType(entry.Type)
		if !ok {
			continue
		}
		marker, ok := data.PlaceDecoration(typeID, entry.X, entry.Z, float64(entry.Rotation), 0, false)
		if !ok {
			continue
		}
		if data.itemDecorationKeys == nil {
			data.itemDecorationKeys = map[string]bool{}
		}
		data.itemDecorationKeys[key] = true
		data.ItemDecorations = append(slices.Clone(data.ItemDecorations), marker)
	}
}

// maxTrackedDecorations bounds the banner and frame markers one map keeps.
// vanilla: MapItemSavedData.TRACKED_DECORATION_LIMIT
const maxTrackedDecorations = 256

// ToggleBanner removes the map's marker for the banner if it tracks that very
// banner, else adds one; false when the banner is off the face or the map
// holds too many markers. Slices are replaced, never edited, since
// CheckBanners ranges over Banners after releasing the lock.
// vanilla: MapItemSavedData.toggleBanner
func (m *Maps) ToggleBanner(id int32, banner MapBanner) bool {
	data, ok := m.Get(id)
	if !ok {
		return false
	}
	scaling := float64(int32(1) << uint(data.Scale))
	if !insideMap((float64(banner.X)+0.5-float64(data.CenterX))/scaling, (float64(banner.Z)+0.5-float64(data.CenterZ))/scaling) {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, have := range data.Banners {
		if have.key() == banner.key() && have.equal(banner) {
			data.removeBanner(i)
			m.dirty[id] = true
			return true
		}
	}
	if len(data.Decorations) >= maxTrackedDecorations {
		return false
	}
	// A banner at the same position with another colour or name is replaced.
	if i := slices.IndexFunc(data.Banners, func(b MapBanner) bool { return b.key() == banner.key() }); i >= 0 {
		data.removeBanner(i)
	}
	data.Banners = append(slices.Clone(data.Banners), banner)
	if decoration, ok := data.PlaceDecoration(bannerDecorations[banner.Color], float64(banner.X)+0.5, float64(banner.Z)+0.5, 180, 0, false); ok {
		decoration.Name, decoration.key = banner.Name, banner.key()
		data.Decorations = append(slices.Clone(data.Decorations), decoration)
	}
	m.dirty[id] = true
	return true
}

func (d *MapData) removeBanner(i int) {
	key := d.Banners[i].key()
	d.Banners = slices.Delete(slices.Clone(d.Banners), i, i+1)
	d.Decorations = slices.DeleteFunc(slices.Clone(d.Decorations), func(x Decoration) bool { return x.key == key })
}

// CheckBanners drops the markers of banners that are gone or changed in w, for
// a map of w's dimension.
// vanilla: MapItemSavedData.checkBanners
func (m *Maps) CheckBanners(id int32, w *World) {
	data, ok := m.Get(id)
	if !ok {
		return
	}
	m.mu.RLock()
	banners := data.Banners
	m.mu.RUnlock()
	for _, b := range banners {
		if !w.ChunkLoadedAt(b.X, b.Z) {
			continue
		}
		if now, ok := w.BannerAt(b.X, b.Y, b.Z); ok && now.equal(b) {
			continue
		}
		m.mu.Lock()
		if i := slices.IndexFunc(data.Banners, func(x MapBanner) bool { return x.equal(b) }); i >= 0 {
			data.removeBanner(i)
			m.dirty[id] = true
		}
		m.mu.Unlock()
	}
}
