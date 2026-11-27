package server

import (
	"cmp"
	"math"
	"slices"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// Filled maps from vanilla worlds: the data under
// data/minecraft/maps/<id>.dat (internal/server/world's Maps), sent as Map Item Data
// to whoever holds one, in their inventory or in a frame within tracking
// range, with its markers (saved, the holder's, each frame's). A map made
// from an empty map is drawn once when made; one held in hand is redrawn a
// stripe at a time as the holder walks, terrain and markers sent on the
// update interval. A map only read or framed keeps its sent colours.

// frameMark is one frame holding a map: the cell it hangs in and its
// facing, dir.
type frameMark struct {
	x, z int32
	dir  int32
}

// frameDirection2D is each face's 2D data value, which a frame's marker
// rotates by, with vertical faces at -1 as Direction.get2DDataValue gives.
// vanilla: Direction.get2DDataValue
var frameDirection2D = [6]int32{
	protocol.FaceBottom: -1,
	protocol.FaceTop:    -1,
	protocol.FaceNorth:  2,
	protocol.FaceSouth:  0,
	protocol.FaceWest:   1,
	protocol.FaceEast:   3,
}

// filledMapID returns a map's id and whether the stack is a MapItem with
// one: whatever else carries a minecraft:map_id is no map.
// vanilla: ServerEntity.sendChanges, MapItem.getSavedData
func filledMapID(stack protocol.ItemStack) (int32, bool) {
	if !registry.Item(stack.ItemID).IsMap() {
		return 0, false
	}
	return stack.MapIDOf()
}

// toggleMapBanner is a map's useOn: on a banner it adds or removes the
// map's marker for it, reporting whether the click was handled.
// vanilla: MapItem.useOn
func (c *client) toggleMapBanner(hand, x, y, z int32) bool {
	w := c.theWorld()
	if !registry.BlockOf(w.BlockAt(x, y, z)).InTag(registry.TagIDBanners) {
		return false
	}
	id, ok := filledMapID(c.heldItem(hand))
	if !ok {
		return true
	}
	if banner, ok := w.BannerAt(x, y, z); ok {
		c.srv.maps.ToggleBanner(id, banner)
	}
	return true
}

// handleUseMap is Use Item on an empty map: a fresh map is made at the
// player's block position, replacing the held one or landing in the
// inventory as a copy, and the cartography table take sound plays to
// everyone in earshot, the player included, as their client does not
// predict it. vanilla: EmptyMapItem.use
func (c *client) handleUseMap(hand int32) bool {
	held := c.heldItem(hand)
	if held.Empty() || registry.Item(held.ItemID) != registry.ItemMap {
		return false
	}
	// A spectator's use is checked before the item's.
	// vanilla: ServerPlayerGameMode.useItem
	if c.player.GameMode() == protocol.GameModeSpectator {
		return false
	}
	filledID := int32(registry.ItemFilledMap)
	pos := c.player.Position()
	// The map is centred on the scale-0 area containing the player's block
	// position, as EmptyMapItem asks. vanilla: EmptyMapItem.use, MapItem.create
	id, _ := c.srv.maps.Create(c.dimension().name, int32(math.Floor(pos.X)), int32(math.Floor(pos.Z)), 0)
	c.srv.maps.Draw(id, c.theWorld(), pos.X, pos.Z)
	filled := protocol.ItemStack{ItemID: filledID, Count: 1}
	filled.SetMapID(id)
	c.srv.broadcastNear(c.theWorld(), pos.X, pos.Y, pos.Z, soundRange(1), protocol.IDPlayEntitySoundEffect,
		entitySoundEffectID(registry.SoundUiCartographyTableTakeResult, protocol.SoundCategoryPlayer, c.player.EntityID, 1.0, 1.0))
	// vanilla: EmptyMapItem.use
	c.awardItemStat(registry.StatTypeUsed, held.ItemID, 1)
	c.filledResult(hand, filled, false)
	return true
}

// mapUpdateInterval is the ticks between marker resends of a sent map;
// colour patches go every tick they are dirty.
// vanilla: MapItemSavedData.HoldingPlayer.nextUpdatePacket
const mapUpdateInterval = 5

// mapFrameMarks imports item decorations and collects visible frame markers in registration order.
func (s *Server) mapFrameMarks(w *world.World, x, z float64, radius int32) map[int32][]frameMark {
	s.framesMu.Lock()
	defer s.framesMu.Unlock()
	if len(s.frames) == 0 {
		return nil
	}
	// A frame given a map mid-tick is seen from the next tick, within the
	// 10-tick cadence of vanilla's framed-map updates.
	// vanilla: ServerEntity.sendChanges
	if now := s.ticksRun.Load(); s.mapFrameWorlds == nil || s.mapFramesTick != now {
		s.mapFramesTick = now
		s.mapFrameWorlds = make(map[*world.World]bool)
		for _, f := range s.frames {
			if registry.Item(f.item.ItemID).IsMap() {
				s.mapFrameWorlds[f.w] = true
			}
		}
	}
	if !s.mapFrameWorlds[w] {
		return nil
	}

	pct := int(s.entityBroadcastRange.Load())
	visible := func(f *frameEntity) bool {
		return registry.Item(f.item.ItemID).IsMap() && f.w == w && frameSees(f, x, z, radius, pct)
	}
	var marks map[int32][]frameMark
	add := func(f *frameEntity) {
		if id, ok := filledMapID(f.item); ok {
			s.maps.AddItemDecorations(id, f.item.MapDecorations)
			if marks == nil {
				marks = make(map[int32][]frameMark)
			}
			marks[id] = append(marks[id], frameMark{x: f.x, z: f.z, dir: f.dir})
		}
	}

	// frameSees uses the hanging cell's corner. The index uses the frame's centre.
	bound := float64(trackingRangeBlocks(frameTracker.rangeChunks, pct, radius) + 1)
	minX, maxX := world.ChunkOf(x-bound), world.ChunkOf(x+bound)
	minZ, maxZ := world.ChunkOf(z-bound), world.ChunkOf(z+bound)
	if int(maxX-minX+1)*int(maxZ-minZ+1) > len(s.frames) {
		for _, f := range s.frames {
			if visible(f) {
				add(f)
			}
		}
		return marks
	}

	var matched []*frameEntity
	from, to := world.ChunkPos{X: minX, Z: minZ}, world.ChunkPos{X: maxX, Z: maxZ}
	for pos := range s.frameChunks.ChunksIn(from, to) {
		for f := range s.frameChunks.In(pos) {
			if visible(f) {
				matched = append(matched, f)
			}
		}
	}
	// Marker order goes on the wire, and the first item with a decoration key wins.
	slices.SortFunc(matched, func(a, b *frameEntity) int { return cmp.Compare(a.order, b.order) })
	for _, f := range matched {
		add(f)
	}
	return marks
}

// tickMaps sends map data for maps the client newly holds: its inventory
// stacks plus every visible frame, the union of vanilla's two scans. A map
// in hand is redrawn a stripe at a time and its markers move, so changes
// are resent: the touched rectangle every dirty tick, the marker list on
// the update interval when it moved. A map no longer held or framed in view
// is forgotten and resent whole on return; one without a saved file sends
// nothing. Only the tick goroutine touches this state, including mapDirty,
// which other clients' ticks fill via markMapDirty.
// vanilla: ServerPlayer.synchronizeSpecialItemUpdates, ServerEntity.sendChanges,
// vanilla: MapItemSavedData.HoldingPlayer.nextUpdatePacket
func (c *client) tickMaps() {
	pos := c.player.Position()
	w := c.theWorld()
	radius := c.chunkRadius()

	// Maps the viewer carries and frames in view holding one, by map id. The
	// 2x2 grid and its output, first in the window, are not scanned by vanilla.
	held := []int32{}
	byViewer := map[int32]bool{}
	inHand := map[int32]bool{}
	c.player.Inventory(func(inv *player.Inventory) {
		main, off := player.HotbarSlot+int(inv.Selected), player.OffHandSlot
		for slot := player.ArmorSlot; slot <= player.OffHandSlot; slot++ {
			if !registry.Item(inv.Slots[slot].ItemID).IsMap() {
				continue
			}
			id, ok := filledMapID(inv.Slots[slot])
			if !ok {
				continue
			}
			c.srv.maps.AddItemDecorations(id, inv.Slots[slot].MapDecorations)
			if !slices.Contains(held, id) {
				held = append(held, id)
			}
			byViewer[id] = true
			if slot == main || slot == off {
				inHand[id] = true
			}
		}
	})
	marks := c.srv.mapFrameMarks(w, pos.X, pos.Z, radius)

	seen := map[int32]bool{}
	for _, id := range held {
		seen[id] = true
	}
	for id := range marks {
		if !seen[id] {
			seen[id] = true
			held = append(held, id)
		}
	}
	if c.sentMapDecorations == nil {
		c.sentMapDecorations = map[int32][]world.Decoration{}
	}
	if c.mapDirty == nil {
		c.mapDirty = map[int32]mapRect{}
	}
	if c.mapSteps == nil {
		c.mapSteps = map[int32]uint32{}
	}
	for id := range c.sentMapDecorations {
		if !seen[id] {
			delete(c.sentMapDecorations, id)
		}
	}
	for id := range c.mapSteps {
		if !seen[id] {
			delete(c.mapSteps, id)
		}
	}
	for id := range c.mapDirty {
		if !seen[id] {
			delete(c.mapDirty, id)
		}
	}

	// Hand-held maps redraw before the sends: the step's stripe, or the whole
	// face for a map just drawn from its file's colours.
	for id := range inHand {
		if data, ok := c.srv.maps.Get(id); ok && data.Dimension == c.dimension().name {
			c.srv.maps.CheckBanners(id, w)
		}
		// The step is incremented before use, so the first stripe is 1.
		// vanilla: MapItemSavedData.HoldingPlayer.step
		step := c.mapSteps[id] + 1
		c.mapSteps[id] = step
		if patch, changed := c.srv.maps.Update(id, w, pos.X, pos.Z, step); changed {
			c.srv.markMapDirty(id, patch)
		}
	}

	gameTime, _ := w.Time()
	dimension := c.dimension().name
	c.mapTicks++
	markerTick := c.mapTicks%mapUpdateInterval == 0
	var others map[int32][]mapHolder
	for _, id := range held {
		data, ok := c.srv.maps.Get(id)
		if !ok {
			continue
		}
		last, sent := c.sentMapDecorations[id]
		patch, dirty := c.mapDirty[id]
		if sent && !dirty && !markerTick {
			continue
		}
		if others == nil {
			// Only when building a marker list: the lookup scans every player.
			others = c.otherHolders(held)
		}
		decorations := c.mapDecorations(data, pos, gameTime, dimension, byViewer[id], others[id], marks[id])
		if !sent {
			c.sentMapDecorations[id] = decorations
			// The whole face already carries the first redraw's stripes, so their
			// patch would repeat it.
			delete(c.mapDirty, id)
			c.send(protocol.IDPlayMapData, mapItemDataPayload(id, data, decorations))
			continue
		}
		changed := markerTick && !sameDecorations(last, decorations)
		if !dirty && !changed {
			continue
		}
		if !dirty {
			c.sentMapDecorations[id] = decorations
			c.send(protocol.IDPlayMapData, mapIconsPayload(id, data, decorations))
			continue
		}
		delete(c.mapDirty, id)
		if changed {
			// Both changed: one packet with the patch and markers, as vanilla's
			// nextUpdatePacket writes. The markers are then current, so the clock runs
			// on.
			c.sentMapDecorations[id] = decorations
			c.send(protocol.IDPlayMapData, mapPatchPayload(id, data, decorations, patch))
			continue
		}
		// Only colours changed: an absent marker list leaves the client's in place.
		c.send(protocol.IDPlayMapData, mapPatchPayload(id, data, nil, patch))
	}
}

// markMapDirty adds a redraw's patch to every client's pending colours,
// since everyone holding or framing the map gets the change. Tick loop only.
// vanilla: MapItemSavedData.setColorsDirty
func (s *Server) markMapDirty(id int32, patch world.MapPatch) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, o := range s.clients {
		if o.mapDirty == nil {
			o.mapDirty = map[int32]mapRect{}
		}
		o.mapDirty[id] = o.mapDirty[id].add(patch)
	}
}

// mapRect is a pixel rectangle on a map face, the union of stripes redrawn
// since the last colour patch.
type mapRect struct{ x, z, w, h int }

// add grows the rectangle to cover the patch, an empty one taking it whole.
func (r mapRect) add(patch world.MapPatch) mapRect {
	if r.w == 0 {
		return mapRect{int(patch.X), int(patch.Z), int(patch.Width), int(patch.Height)}
	}
	x0 := min(r.x, int(patch.X))
	z0 := min(r.z, int(patch.Z))
	x1 := max(r.x+r.w, int(patch.X)+int(patch.Width))
	z1 := max(r.z+r.h, int(patch.Z)+int(patch.Height))
	return mapRect{x0, z0, x1 - x0, z1 - z0}
}

// sameDecorations reports whether two marker lists draw the same; names
// compare by value, so a rebuilt list matches.
func sameDecorations(a, b []world.Decoration) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		x, y := a[i], b[i]
		if x.Type != y.Type || x.X != y.X || x.Y != y.Y || x.Rot != y.Rot {
			return false
		}
		if (x.Name == nil) != (y.Name == nil) {
			return false
		}
		if x.Name != nil && x.Name.Plain() != y.Name.Plain() {
			return false
		}
	}
	return true
}

// mapHolder is where a player carrying a map stands and in which dimension.
type mapHolder struct {
	pos       player.Position
	dimension string
}

// otherHolders returns, by map id, each other player carrying the map, in
// join order, in any dimension. A player wearing a map-invisibility item is
// omitted; the viewer's own marker never is.
// vanilla: MapItemSavedData.tickCarriedBy, hasMapInvisibilityItemEquipped
func (c *client) otherHolders(ids []int32) map[int32][]mapHolder {
	out := map[int32][]mapHolder{}
	if len(ids) == 0 {
		return out
	}
	for _, o := range c.srv.playerClients() {
		if o == c {
			continue
		}
		h := mapHolder{pos: o.player.Position(), dimension: o.dimension().name}
		o.player.MapHoldings(func(carried []int32, hidden bool) {
			if hidden {
				return
			}
			for _, id := range carried {
				if slices.Contains(ids, id) {
					out[id] = append(out[id], h)
				}
			}
		})
	}
	return out
}

// mapDecorations is the packet's marker list: the saved file's, the
// viewer's own when carrying it in the map's dimension, one per other
// holder there, and one per frame in view holding it.
// vanilla: MapItemSavedData.getDecorations, tickCarriedBy
func (c *client) mapDecorations(data *world.MapData, pos player.Position, gameTime int64, dimension string, byViewer bool, others []mapHolder, marks []frameMark) []world.Decoration {
	decorations := c.srv.maps.Markers(data)
	if byViewer && data.TrackingPosition && data.Dimension == dimension {
		if decoration, ok := data.PlaceDecoration(world.DecorationPlayer, pos.X, pos.Z, float64(pos.Yaw), gameTime, true); ok {
			decorations = append(decorations, decoration)
		}
	}
	if data.TrackingPosition {
		for _, other := range others {
			if other.dimension != data.Dimension {
				continue
			}
			if decoration, ok := data.PlaceDecoration(world.DecorationPlayer, other.pos.X, other.pos.Z, float64(other.pos.Yaw), gameTime, true); ok {
				decorations = append(decorations, decoration)
			}
		}
	}
	// Frame markers share the flag: a map tracking nothing draws no frames.
	if !data.TrackingPosition {
		return decorations
	}
	for _, mark := range marks {
		yRot := float64(frameDirection2D[mark.dir] * 90)
		if decoration, ok := data.PlaceDecoration(world.DecorationFrame, float64(mark.x), float64(mark.z), yRot, gameTime, true); ok {
			decorations = append(decorations, decoration)
		}
	}
	return decorations
}

// mapItemDataPayload builds one map's Map Data: id, scale, locked flag,
// markers, and the whole face as the colour patch, the 128x128 update of
// vanilla's first send to a holder.
// vanilla: ClientboundMapItemDataPacket.STREAM_CODEC, HoldingPlayer.nextUpdatePacket, createPatch
func mapItemDataPayload(id int32, data *world.MapData, decorations []world.Decoration) []byte {
	var b protocol.Buffer
	putMapDataHeader(&b, id, data, decorations)
	b.PutU8(world.MapSide) // the patch's columns and rows
	b.PutU8(world.MapSide)
	b.PutU8(0) // the patch's origin, x then z: the whole face
	b.PutU8(0)
	b.PutVarInt(world.MapSide * world.MapSide)
	b.PutRaw(data.Colors[:])
	return b.Bytes()
}

// mapIconsPayload builds a marker-only Map Data with no colour patch, as
// vanilla resends a moved marker.
// vanilla: ClientboundMapItemDataPacket, HoldingPlayer.nextUpdatePacket
func mapIconsPayload(id int32, data *world.MapData, decorations []world.Decoration) []byte {
	var b protocol.Buffer
	putMapDataHeader(&b, id, data, decorations)
	b.PutU8(0) // no columns: the update carries no colours
	return b.Bytes()
}

// mapPatchPayload builds a colour patch Map Data: the header, the patch's
// rectangle and its pixels, as vanilla's nextUpdatePacket sends a holder's
// dirty rectangle. A nil marker list writes the absent form, read as
// unchanged.
// vanilla: ClientboundMapItemDataPacket.STREAM_CODEC, HoldingPlayer.createPatch
func mapPatchPayload(id int32, data *world.MapData, decorations []world.Decoration, patch mapRect) []byte {
	var b protocol.Buffer
	putMapDataHeader(&b, id, data, decorations)
	b.PutU8(uint8(patch.w))
	b.PutU8(uint8(patch.h))
	b.PutU8(uint8(patch.x))
	b.PutU8(uint8(patch.z))
	b.PutVarInt(int32(patch.w * patch.h))
	for y := 0; y < patch.h; y++ {
		off := (patch.z+y)*world.MapSide + patch.x
		b.PutRaw(data.Colors[off : off+patch.w])
	}
	return b.Bytes()
}

// putMapDataHeader writes the fields all three Map Data shapes share: id,
// scale, locked flag and markers. A nil list writes the absent form, so the
// client keeps its list.
func putMapDataHeader(b *protocol.Buffer, id int32, data *world.MapData, decorations []world.Decoration) {
	b.PutVarInt(id)
	b.PutU8(uint8(data.Scale))
	b.PutBool(data.Locked)
	if decorations == nil {
		b.PutBool(false)
		return
	}
	b.PutBool(true)
	b.PutVarInt(int32(len(decorations)))
	for _, decoration := range decorations {
		b.PutVarInt(decoration.Type)
		b.PutI8(decoration.X)
		b.PutI8(decoration.Y)
		b.PutI8(decoration.Rot)
		if decoration.Name != nil {
			b.PutBool(true)
			b.PutText(*decoration.Name)
		} else {
			b.PutBool(false)
		}
	}
}
