package items

// The filled maps a vanilla world already holds: the pixels each map keeps
// under data/minecraft/maps/<id>.dat, sent as Map Item Data to the player
// carrying the map — the player file can bring one in hand — and to every
// player near an item frame holding one.

import (
	"bytes"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
)

// mapColours are the known colours of the test's saved map: one byte per
// pixel, none of them the zero a blank map's would be.
func mapColours() []byte {
	colours := make([]byte, servertest.MapSide*servertest.MapSide)
	for i := range colours {
		colours[i] = byte(i%255) + 1
	}
	return colours
}

// TestAHeldFilledMapShowsItsSavedData is the issue's first half: a world
// brought over from vanilla holds a map under data/minecraft/maps/0.dat, the
// player file hands the player the filled map pointing at it, and the join
// sends the map's own pixels — the scale and the locked flag and the full
// colour patch — instead of showing blank parchment.
func TestAHeldFilledMapShowsItsSavedData(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	const name = "Cartographer"
	colours := mapColours()
	servertest.WriteMapSavedData(t, cfg.LevelName, 0, 1, true, colours)
	servertest.WritePlayerFile(t, servertest.PlayerFile(cfg.LevelName, name), nbt.Compound{
		"Inventory": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
			nbt.Compound{
				"Slot":  nbt.Byte(0),
				"id":    nbt.String("minecraft:filled_map"),
				"count": nbt.Int(1),
				"components": nbt.Compound{
					"minecraft:map_id": nbt.Int(0),

					"minecraft:map_decorations": nbt.Compound{"+": nbt.Compound{"type": nbt.String("minecraft:monument"), "x": nbt.Double(64), "z": nbt.Double(64), "rotation": nbt.Float(180)}},
				},
			},
		}},
		"SelectedItemSlot": nbt.Int(0),
	})

	_, connect := servertest.Start(t, cfg)
	c := connect()
	c.Login(name)
	c.Configure()
	slots, passed := c.JoinWindow(t)

	// The map itself must make it into the window: a stack whose file
	// carries a component the stack codec cannot read stays the file's
	// own, which is the blank parchment of the bug itself.
	held := slots[player.HotbarSlot]
	if held.ItemID != servertest.ItemID(t, "minecraft:filled_map") || held.Count != 1 {
		t.Fatalf("the held slot = %+v, want the file's filled map", held)
	}
	if id, ok := held.MapIDOf(); !ok || id != 0 {
		t.Errorf("the held map's id = %d, %v; want the file's map_id 0", id, ok)
	}

	// The map data rides within the join itself — the first tick after
	// the join puts the player in the world, while the chunk stream is
	// still loading — so whatever of it slipped past the window read is
	// still waiting behind it.
	if passed == nil {
		data, ok := servertest.ReadUntil(t, c, servertest.JoinTimeout, protocol.IDPlayMapData)
		if !ok {
			t.Fatalf("no map item data for the held map within %s", servertest.JoinTimeout)
		}
		passed = data
	}
	update := servertest.ParseMapData(t, passed)
	c.MarkLoaded()
	if update.Scale != 1 {
		t.Errorf("the map's scale = %d, want the file's 1", update.Scale)
	}
	if !update.Locked {
		t.Error("the map is unlocked, want the file's locked flag")
	}
	// The holder's own marker: the player starts at (0.5, 64, 0.5) facing
	// zero, 31.75 pixels west and north of the centre (64, 64) at scale 1;
	// the placement pins both to -63 and turns the marker to 0.
	if !update.HasIcons || len(update.Icons) != 2 {
		t.Fatalf("the map carries %d markers (present %v), want the target and holder",
			len(update.Icons), update.HasIcons)
	}
	if icon := update.Icons[0]; icon.TypeID != 9 || icon.X != 0 || icon.Y != 0 || icon.Rot != 8 {
		t.Fatalf("target marker %+v", icon)
	}
	if icon := update.Icons[1]; icon.TypeID != 0 || int8(icon.X) != -63 || int8(icon.Y) != -63 || int8(icon.Rot) != 0 {
		t.Errorf("the holder's marker = type %d at (%d, %d) rot %d, want the player marker at (-63, -63) rot 0",
			icon.TypeID, int8(icon.X), int8(icon.Y), int8(icon.Rot))
	}
	if update.Columns != servertest.MapSide || update.Rows != servertest.MapSide || update.X != 0 || update.Z != 0 {
		t.Errorf("the patch = %dx%d at (%d, %d), want the whole face 128x128 at (0, 0)",
			update.Columns, update.Rows, update.X, update.Z)
	}
	if !bytes.Equal(update.Colours, colours) {
		t.Errorf("the patch = %d bytes, want the file's %d, starting %v...",
			len(update.Colours), len(colours), colours[:8])
	}
}

// TestAnItemFrameShowsTheMapItHolds is the issue's other half: a map hung in
// an item frame sends the same data to a player nearby who carries no map of
// their own, the frame being the only source of what they are sent.
func TestAnItemFrameShowsTheMapItHolds(t *testing.T) {
	t.Parallel()
	frameShowsMap(t, "minecraft:filled_map")
}

// frameShowsMap hangs a map of the named item and map id 0 in a frame and
// checks the watcher beside it is sent the map's data.
func frameShowsMap(t *testing.T, item string) {
	t.Helper()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	colours := mapColours()
	// Locked, so the copy the actor still holds in creative is not
	// redrawn under the pixels the watcher is sent.
	servertest.WriteMapSavedData(t, cfg.LevelName, 0, 2, true, colours)

	srv, connect := servertest.Start(t, cfg)
	actor, watcher, _ := servertest.PaintingWatchers(t, connect)
	servertest.BuildPaintingWall(srv, 4, 64, 65, 5, 5)

	// The frame hangs on the wall's north face, the painting tests' own
	// spot, and the watcher hears all of it.
	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{
		ItemID: servertest.FrameItemID(t, "minecraft:item_frame"), Count: 1})
	actor.MoveToGround(4.5, 64, 3.5, true)
	actor.Settle()
	actor.PlaceBlock(4, 64, 5, protocol.FaceNorth)
	hung := watcher.ExpectFrameSpawn(t, protocol.EntityTypeItemFrame, servertest.WallFrameX, servertest.WallFrameY, servertest.WallFrameZ, 180, 0, protocol.FaceNorth)
	actor.ExpectBlockAck()
	actor.ExpectUseEchoes(4, 64, 5, protocol.FaceNorth)

	// The map goes into the frame from the actor's hand; the frame keeps
	// one of the stack, and the watcher — holding nothing — is sent the
	// map's pixels.
	mapStack := servertest.ItemStack(t, item, 1)
	mapStack.SetMapID(0)
	actor.SetCreativeSlot(player.HotbarSlot, mapStack)
	actor.UseOn(hung.EntityID)
	if got := servertest.FrameItemSlot(t, watcher, hung.EntityID); got.ItemID != mapStack.ItemID || got.Count != 1 {
		t.Fatalf("the frame holds %+v after the use, want the map it was handed", got)
	} else if id, ok := got.MapIDOf(); !ok || id != 0 {
		t.Fatalf("the framed map's id = %d, %v; want map_id 0", id, ok)
	}

	update := watcher.ExpectMapData(t, 0)
	if update.Scale != 2 {
		t.Errorf("the map's scale = %d, want the file's 2", update.Scale)
	}
	if !update.Locked {
		t.Error("the map is unlocked, want the file's locked flag")
	}
	// The frame marker: the frame hangs in the cell (4, 64, 4) facing
	// north, 15 pixels west and north of the centre (64, 64) at scale 2;
	// the placement pins both to -29 and turns the marker to 8.
	// The actor still carries the map, so its player marker rides along.
	var frames []servertest.MapIcon
	for _, icon := range update.Icons {
		if icon.TypeID == 1 {
			frames = append(frames, icon)
		}
	}
	if !update.HasIcons || len(frames) != 1 {
		t.Fatalf("the map carries %d frame markers (present %v), want the frame's own",
			len(frames), update.HasIcons)
	}
	if icon := frames[0]; icon.TypeID != 1 || int8(icon.X) != -29 || int8(icon.Y) != -29 || int8(icon.Rot) != 8 {
		t.Errorf("the frame's marker = type %d at (%d, %d) rot %d, want the frame marker at (-29, -29) rot 8",
			icon.TypeID, int8(icon.X), int8(icon.Y), int8(icon.Rot))
	}
	if update.Columns != servertest.MapSide || update.Rows != servertest.MapSide || update.X != 0 || update.Z != 0 {
		t.Errorf("the patch = %dx%d at (%d, %d), want the whole face 128x128 at (0, 0)",
			update.Columns, update.Rows, update.X, update.Z)
	}
	if !bytes.Equal(update.Colours, colours) {
		t.Errorf("the patch = %d bytes, want the file's %d, starting %v...",
			len(update.Colours), len(colours), colours[:8])
	}
}

// TestAHeldMapMarkerFollowsTheHolder is the tick half of the block: a map
// sent once keeps its marker on the move. The holder turns east and the
// marker is sent again, without the pixels, at its new pixel and direction.
// The map is locked, so only the marker moves: an unlocked one would also
// redraw the terrain under the holder into the same re-send.
func TestAHeldMapMarkerFollowsTheHolder(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	const name = "Cartographer"
	servertest.WriteMapSavedData(t, cfg.LevelName, 0, 1, true, mapColours())
	servertest.WritePlayerFile(t, servertest.PlayerFile(cfg.LevelName, name), nbt.Compound{
		"Inventory": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
			nbt.Compound{
				"Slot":  nbt.Byte(0),
				"id":    nbt.String("minecraft:filled_map"),
				"count": nbt.Int(1),
				"components": nbt.Compound{
					"minecraft:map_id": nbt.Int(0),
				},
			},
		}},
		"SelectedItemSlot": nbt.Int(0),
	})

	srv, connect := servertest.Start(t, cfg)
	c := connect()
	c.Login(name)
	c.Configure()
	_, passed := c.JoinWindow(t)
	if passed == nil {
		data, ok := servertest.ReadUntil(t, c, servertest.JoinTimeout, protocol.IDPlayMapData)
		if !ok {
			t.Fatalf("no first map item data within %s", servertest.JoinTimeout)
		}
		passed = data
	}
	first := servertest.ParseMapData(t, passed)
	c.MarkLoaded()
	if !first.HasIcons || len(first.Icons) != 1 || int8(first.Icons[0].Rot) != 0 {
		t.Fatalf("the first send's markers = %+v, want the holder's rot 0", first.Icons)
	}

	// Walk to the map's centre and turn east: the marker moves to the
	// centre pixel and points 90 degrees round, and the client orders the
	// marker list again with no colour patch behind it.
	c.MoveTo(64.5, 64, 64.5, 90, 0)
	update := c.AwaitMarkerAt(t, 0, func(icon servertest.MapIcon) bool {
		return icon.TypeID == 0 && int8(icon.X) == 1 && int8(icon.Y) == 1 && int8(icon.Rot) == 4
	})
	if update.Columns != 0 {
		t.Errorf("the update's patch = %d columns, want none", update.Columns)
	}

	// The holder stands still now, so the list is unchanged and no more
	// data is sent.
	servertest.ExpectNoPacket(t, srv, c, 40, protocol.IDPlayMapData)
}

// TestAHeldMapGainsTheFrameMarker is the ordering half: a map has already
// been sent to its holder, so its marker list was fixed, and a frame takes
// the map after that send. The holder's copy is told of the frame's marker
// in a re-send that carries no colours. The map is locked for the same
// reason as the marker test's: only the markers move.
func TestAHeldMapGainsTheFrameMarker(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	servertest.WriteMapSavedData(t, cfg.LevelName, 0, 2, true, mapColours())

	srv, connect := servertest.Start(t, cfg)
	actor, _, _ := servertest.PaintingWatchers(t, connect)
	servertest.BuildPaintingWall(srv, 4, 64, 65, 5, 5)

	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{
		ItemID: servertest.FrameItemID(t, "minecraft:item_frame"), Count: 1})
	actor.MoveToGround(4.5, 64, 3.5, true)
	actor.Settle()
	actor.PlaceBlock(4, 64, 5, protocol.FaceNorth)
	hung := actor.ExpectFrameSpawn(t, protocol.EntityTypeItemFrame, servertest.WallFrameX, servertest.WallFrameY, servertest.WallFrameZ, 180, 0, protocol.FaceNorth)
	actor.ExpectBlockAck()
	actor.ExpectUseEchoes(4, 64, 5, protocol.FaceNorth)

	// The holder carries a copy before the frame takes one, so the first
	// send is the whole face with the holder's marker alone.
	mapStack := servertest.ItemStack(t, "minecraft:filled_map", 1)
	mapStack.SetMapID(0)
	actor.SetCreativeSlot(player.HotbarSlot, mapStack)
	before := actor.ExpectMapData(t, 0)
	if before.Columns != servertest.MapSide {
		t.Fatalf("the first send's patch = %d columns, want the whole face", before.Columns)
	}
	if len(before.Icons) != 1 || before.Icons[0].TypeID != 0 {
		t.Fatalf("the first send's markers = %+v, want the holder's alone", before.Icons)
	}

	// The frame takes the map; the holder's copy learns of the frame's
	// marker in a re-send that carries no colours.
	actor.UseOn(hung.EntityID)
	if got := servertest.FrameItemSlot(t, actor, hung.EntityID); got.ItemID != mapStack.ItemID || got.Count != 1 {
		t.Fatalf("the frame holds %+v after the use, want the map it was handed", got)
	}
	update := actor.AwaitMapMarker(t, 0, 1)
	if update.Columns != 0 {
		t.Errorf("the re-send's patch = %d columns, want none", update.Columns)
	}
	var sawPlayer, sawFrame bool
	for _, icon := range update.Icons {
		switch icon.TypeID {
		case 0:
			sawPlayer = true
		case 1:
			sawFrame = true
		}
	}
	if !sawPlayer || !sawFrame {
		t.Errorf("the re-send's markers = %+v, want the holder's and the frame's", update.Icons)
	}
}

// awaitMapColour reads until a colour patch for the id carries a pixel of the
// want colour index, answering the update; it is how a test waits for the
// terrain a sweep picked up rather than the stripes still loading around it.
func awaitMapColour(c *servertest.Client, t *testing.T, id int32, want byte) servertest.MapDataUpdate {
	t.Helper()
	for packetID, data := range c.Packets(servertest.ExpectTimeout) {
		if packetID != protocol.IDPlayMapData {
			continue
		}
		update := servertest.ParseMapData(t, data)
		if update.ID() != id {
			continue
		}
		for _, colour := range update.Colours {
			if colour>>2 == want {
				return update
			}
		}
	}
	t.Fatalf("no colour patch for map %d carried colour %d within %s", id, want, servertest.ExpectTimeout)
	return servertest.MapDataUpdate{}
}

// TestAHeldMapRedrawsTheTerrainAroundTheHolder is the walking half of the
// block: a map made from an empty map is drawn once, and a holder who keeps
// carrying it must see the terrain under them change. The saved map is
// blank, so a redrawn pixel carries the world's own colour, and the block
// built after the join is what a later sweep picks up.
func TestAHeldMapRedrawsTheTerrainAroundTheHolder(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	const name = "Cartographer"
	servertest.WriteMapSavedData(t, cfg.LevelName, 0, 0, false, make([]byte, servertest.MapSide*servertest.MapSide))
	servertest.WritePlayerFile(t, servertest.PlayerFile(cfg.LevelName, name), nbt.Compound{
		"Inventory": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
			nbt.Compound{
				"Slot":  nbt.Byte(0),
				"id":    nbt.String("minecraft:filled_map"),
				"count": nbt.Int(1),
				"components": nbt.Compound{
					"minecraft:map_id": nbt.Int(0),
				},
			},
		}},
		"SelectedItemSlot": nbt.Int(0),
	})

	srv, connect := servertest.Start(t, cfg)
	c := connect()
	c.Login(name)
	c.Configure()
	_, passed := c.JoinWindow(t)
	if passed == nil {
		data, ok := servertest.ReadUntil(t, c, servertest.JoinTimeout, protocol.IDPlayMapData)
		if !ok {
			t.Fatalf("no first map item data within %s", servertest.JoinTimeout)
		}
		passed = data
	}
	c.MarkLoaded()
	if first := servertest.ParseMapData(t, passed); first.Columns != servertest.MapSide {
		t.Fatalf("the first send's patch = %d columns, want the whole face", first.Columns)
	}

	// Build in the map's own chunk, at a pixel a later sweep samples again:
	// the map's centre is (64, 64) and the player stands at (0.5, 64, 0.5),
	// so the block at (3, 64, 3) is pixel (3, 3). A blank saved face means
	// the dirt's colour can only come from the block just built, not the
	// stone the loading stripes around it carry.
	dirt := servertest.MustState(t, "minecraft:dirt", nil)
	servertest.WaitFor(t, srv, 1200, func() bool { return srv.SetBlock(3, world.GroundLevel, 3, dirt) })
	awaitMapColour(c, t, 0, 10)
}

// TestARedrawReachesEveryPlayerHoldingTheMap checks the redraw one holder's
// walking makes is sent to a second player who only carries the map in their
// inventory and so redraws nothing themselves.
func TestARedrawReachesEveryPlayerHoldingTheMap(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	servertest.WriteMapSavedData(t, cfg.LevelName, 0, 0, false, make([]byte, servertest.MapSide*servertest.MapSide))
	mapStack := func(slot int) nbt.Compound {
		return nbt.Compound{
			"Slot":  nbt.Byte(slot),
			"id":    nbt.String("minecraft:filled_map"),
			"count": nbt.Int(1),
			"components": nbt.Compound{
				"minecraft:map_id": nbt.Int(0),
			},
		}
	}
	for name, slot := range map[string]int{"Cartographer": 0, "Bystander": 9} {
		servertest.WritePlayerFile(t, servertest.PlayerFile(cfg.LevelName, name), nbt.Compound{
			"Inventory":        nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{mapStack(slot)}},
			"SelectedItemSlot": nbt.Int(0),
		})
	}

	srv, connect := servertest.Start(t, cfg)
	join := func(name string) *servertest.Client {
		c := connect()
		c.Login(name)
		c.Configure()
		if _, passed := c.JoinWindow(t); passed == nil {
			if _, ok := servertest.ReadUntil(t, c, servertest.JoinTimeout, protocol.IDPlayMapData); !ok {
				t.Fatalf("%s: no first map item data within %s", name, servertest.JoinTimeout)
			}
		}
		c.MarkLoaded()
		return c
	}
	join("Cartographer")
	bystander := join("Bystander")

	dirt := servertest.MustState(t, "minecraft:dirt", nil)
	servertest.WaitFor(t, srv, 1200, func() bool { return srv.SetBlock(3, world.GroundLevel, 3, dirt) })
	awaitMapColour(bystander, t, 0, 10)
}

// holdersOfMapZero starts a server where Cartographer and Bystander both
// carry locked map 0, Bystander also with the given extra inventory entries,
// and joins them in that order.
func holdersOfMapZero(t *testing.T, bystanderExtra ...nbt.Tag) (srv *server.Server, cartographer, bystander *servertest.Client) {
	t.Helper()
	return holdersOfMapZeroIn(t, world.DimensionName, bystanderExtra...)
}

// holdersOfMapZeroIn is holdersOfMapZero with Cartographer standing in the
// given dimension.
func holdersOfMapZeroIn(t *testing.T, cartographerDimension string, bystanderExtra ...nbt.Tag) (srv *server.Server, cartographer, bystander *servertest.Client) {
	t.Helper()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	servertest.WriteMapSavedData(t, cfg.LevelName, 0, 1, true, mapColours())
	mapStack := nbt.Compound{
		"Slot":  nbt.Byte(0),
		"id":    nbt.String("minecraft:filled_map"),
		"count": nbt.Int(1),
		"components": nbt.Compound{
			"minecraft:map_id": nbt.Int(0),
		},
	}
	dimensions := map[string]string{"Cartographer": cartographerDimension, "Bystander": world.DimensionName}
	for name, extra := range map[string][]nbt.Tag{"Cartographer": nil, "Bystander": bystanderExtra} {
		items := append([]nbt.Tag{mapStack}, extra...)
		servertest.WritePlayerFile(t, servertest.PlayerFile(cfg.LevelName, name), nbt.Compound{
			"Inventory":        nbt.List{Elem: nbt.TypeCompound, Items: items},
			"SelectedItemSlot": nbt.Int(0),
			"Dimension":        nbt.String(dimensions[name]),
		})
	}
	var connect func() *servertest.Client
	srv, connect = servertest.Start(t, cfg)
	join := func(name string) *servertest.Client {
		c := connect()
		c.Login(name)
		c.Configure()
		if _, passed := c.JoinWindow(t); passed == nil {
			if _, ok := servertest.ReadUntil(t, c, servertest.JoinTimeout, protocol.IDPlayMapData); !ok {
				t.Fatalf("%s: no first map item data within %s", name, servertest.JoinTimeout)
			}
		}
		c.MarkLoaded()
		return c
	}
	cartographer = join("Cartographer")
	bystander = join("Bystander")
	// A step re-sends the marker lists, so the join's own packets, which
	// the helper consumed, do not hide the result.
	bystander.MoveTo(5.5, 64, 5.5, 0, 0)
	return srv, cartographer, bystander
}

// playerMarkers is how many player markers an update's list carries.
func playerMarkers(update servertest.MapDataUpdate) int {
	n := 0
	for _, icon := range update.Icons {
		if icon.TypeID == 0 {
			n++
		}
	}
	return n
}

// TestEveryHolderOfAMapShowsOnIt checks a map carried by two players in its
// dimension shows both markers to each of them.
func TestEveryHolderOfAMapShowsOnIt(t *testing.T) {
	t.Parallel()
	_, cartographer, bystander := holdersOfMapZero(t)
	for name, c := range map[string]*servertest.Client{"Cartographer": cartographer, "Bystander": bystander} {
		found := false
		for packetID, data := range c.Packets(servertest.ExpectTimeout) {
			if packetID == protocol.IDPlayMapData {
				if u := servertest.ParseMapData(t, data); u.ID() == 0 && playerMarkers(u) == 2 {
					found = true
					break
				}
			}
		}
		if !found {
			t.Errorf("%s never saw both holders' markers on map 0", name)
		}
	}
}

// TestAMapInvisibilityItemHidesTheHolderFromOthers checks a holder wearing a
// carved pumpkin is left off the other holders' map, while still seeing both
// markers on theirs.
func TestAMapInvisibilityItemHidesTheHolderFromOthers(t *testing.T) {
	t.Parallel()
	pumpkin := nbt.Compound{
		"Slot":  nbt.Byte(103),
		"id":    nbt.String("minecraft:carved_pumpkin"),
		"count": nbt.Int(1),
	}
	srv, cartographer, bystander := holdersOfMapZero(t, pumpkin)
	found := false
	for packetID, data := range bystander.Packets(servertest.ExpectTimeout) {
		if packetID == protocol.IDPlayMapData {
			if u := servertest.ParseMapData(t, data); u.ID() == 0 && playerMarkers(u) == 2 {
				found = true
				break
			}
		}
	}
	if !found {
		t.Errorf("the pumpkin wearer never saw the other holder's marker")
	}
	cartographer.ReadTicks(srv, 40, func(packetID int32, data []byte) bool {
		if packetID == protocol.IDPlayMapData {
			if u := servertest.ParseMapData(t, data); u.ID() == 0 && playerMarkers(u) > 1 {
				t.Fatalf("the pumpkin wearer's marker reached the other holder: %+v", u.Icons)
			}
		}
		return false
	})
}

// TestAHolderInAnotherDimensionSeesTheMapsHolders checks the dimension that
// counts is the holder's: a player in the nether carrying an overworld map
// is shown the overworld holder's marker, though not one of their own.
// vanilla: MapItemSavedData.tickCarriedBy
func TestAHolderInAnotherDimensionSeesTheMapsHolders(t *testing.T) {
	t.Parallel()
	_, cartographer, bystander := holdersOfMapZeroIn(t, world.NetherName)
	for packetID, data := range cartographer.Packets(servertest.ExpectTimeout) {
		if packetID == protocol.IDPlayMapData {
			if u := servertest.ParseMapData(t, data); u.ID() == 0 && playerMarkers(u) == 1 {
				return
			}
		}
	}
	_ = bystander
	t.Error("the nether viewer never saw the overworld holder's marker")
}

// TestAFrameViewerSeesTheMapsHolders checks a player who sees the map only
// in an item frame is sent the markers of the players carrying it.
// vanilla: ServerEntity.sendChanges
func TestAFrameViewerSeesTheMapsHolders(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	servertest.WriteMapSavedData(t, cfg.LevelName, 0, 2, true, mapColours())
	srv, connect := servertest.Start(t, cfg)
	actor, watcher, _ := servertest.PaintingWatchers(t, connect)
	servertest.BuildPaintingWall(srv, 4, 64, 65, 5, 5)
	actor.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{
		ItemID: servertest.FrameItemID(t, "minecraft:item_frame"), Count: 1})
	actor.MoveToGround(4.5, 64, 3.5, true)
	actor.Settle()
	actor.PlaceBlock(4, 64, 5, protocol.FaceNorth)
	hung := watcher.ExpectFrameSpawn(t, protocol.EntityTypeItemFrame, servertest.WallFrameX, servertest.WallFrameY, servertest.WallFrameZ, 180, 0, protocol.FaceNorth)
	actor.ExpectBlockAck()
	actor.ExpectUseEchoes(4, 64, 5, protocol.FaceNorth)

	mapStack := servertest.ItemStack(t, "minecraft:filled_map", 1)
	mapStack.SetMapID(0)
	actor.SetCreativeSlot(player.HotbarSlot, mapStack)
	actor.UseOn(hung.EntityID)
	for packetID, data := range watcher.Packets(servertest.ExpectTimeout) {
		if packetID == protocol.IDPlayMapData {
			if u := servertest.ParseMapData(t, data); u.ID() == 0 && playerMarkers(u) == 1 {
				return
			}
		}
	}
	t.Error("the frame viewer never saw the holder's marker")
}

// awaitBannerMarkers reads map 0's updates until one carries a marker list
// whose red banner markers number want.
func awaitBannerMarkers(t *testing.T, c *servertest.Client, want int) []servertest.MapIcon {
	t.Helper()
	const redBanner = 24
	for id, data := range c.Packets(servertest.ExpectTimeout) {
		if id != protocol.IDPlayMapData {
			continue
		}
		update := servertest.ParseMapData(t, data)
		if !update.HasIcons {
			continue
		}
		var banners []servertest.MapIcon
		for _, icon := range update.Icons {
			if icon.TypeID == redBanner {
				banners = append(banners, icon)
			}
		}
		if len(banners) == want {
			return banners
		}
	}
	t.Fatalf("no map update with %d red banner markers within %s", want, servertest.ExpectTimeout)
	return nil
}

// TestAFilledMapTogglesABannerMarker: using the held map on a banner inside
// its area adds the banner's marker, using it again removes it, and breaking
// a marked banner removes it too.
// vanilla: MapItem.useOn, MapItemSavedData.toggleBanner, checkBanners
func TestAFilledMapTogglesABannerMarker(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	const name = "Cartographer"
	servertest.WriteMapSavedData(t, cfg.LevelName, 0, 1, true, mapColours())
	servertest.WritePlayerFile(t, servertest.PlayerFile(cfg.LevelName, name), nbt.Compound{
		"Inventory": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
			nbt.Compound{
				"Slot": nbt.Byte(0), "id": nbt.String("minecraft:filled_map"), "count": nbt.Int(1),
				"components": nbt.Compound{"minecraft:map_id": nbt.Int(0)},
			},
		}},
		"SelectedItemSlot": nbt.Int(0),
	})
	srv, c := servertest.Join(t, cfg, name)
	c.Settle()
	srv.SetBlock(2, 64, 2, servertest.BlockState(t, "minecraft:red_banner"))
	srv.EnsureBlockEntityInDimension("minecraft:overworld", 2, 64, 2, world.KindBanner)
	srv.SetBlockEntityTagAt(2, 64, 2, "minecraft:banner", nbt.Compound{"CustomName": nbt.String("Home")})
	c.Settle()

	c.UseBlock(2, 64, 2)
	// The banner at (2, 64, 2) is 31 pixels west and north of the centre
	// (64, 64) at scale 1: x = (2.5 - 64) / 2 * 2 + 0.5 truncated.
	banners := awaitBannerMarkers(t, c, 1)
	if icon := banners[0]; int8(icon.X) != -61 || int8(icon.Y) != -61 {
		t.Errorf("the banner marker is at (%d, %d), want (-61, -61)", int8(icon.X), int8(icon.Y))
	}

	c.UseBlock(2, 64, 2)
	awaitBannerMarkers(t, c, 0)

	c.UseBlock(2, 64, 2)
	awaitBannerMarkers(t, c, 1)
	srv.SetBlock(2, 64, 2, 0)
	awaitBannerMarkers(t, c, 0)
}

// TestTogglingABannerWhileAMapIsHeldIsRaceFree toggles a banner marker from
// one connection while the tick sends the map's markers to another holder;
// run with -race, the detector stays silent.
func TestTogglingABannerWhileAMapIsHeldIsRaceFree(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()
	servertest.WriteMapSavedData(t, cfg.LevelName, 0, 1, true, mapColours())
	for _, name := range []string{"Holder", "Toggler"} {
		servertest.WritePlayerFile(t, servertest.PlayerFile(cfg.LevelName, name), nbt.Compound{
			"Inventory": nbt.List{Elem: nbt.TypeCompound, Items: []nbt.Tag{
				nbt.Compound{
					"Slot": nbt.Byte(0), "id": nbt.String("minecraft:filled_map"), "count": nbt.Int(1),
					"components": nbt.Compound{"minecraft:map_id": nbt.Int(0)},
				},
			}},
			"SelectedItemSlot": nbt.Int(0),
		})
	}
	srv, connect := servertest.Start(t, cfg)
	holder := connect().Join("Holder")
	toggler := connect().Join("Toggler")
	srv.SetBlock(2, 64, 2, servertest.BlockState(t, "minecraft:red_banner"))
	srv.EnsureBlockEntityInDimension("minecraft:overworld", 2, 64, 2, world.KindBanner)
	holder.Settle()
	toggler.Settle()
	toggler.UseBlock(2, 64, 2)
	awaitBannerMarkers(t, holder, 1)

	// The sleep paces the toggles rather than waiting on state: spread over
	// about eight free-running ticks, not sent in a burst between two, the
	// connection's writes overlap the tick's marker reads.
	for range 400 {
		toggler.UseBlock(2, 64, 2)
		time.Sleep(time.Millisecond)
	}
	toggler.Settle()
}
