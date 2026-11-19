package commands

import (
	"bytes"
	"encoding/hex"
	"github.com/golem-mc/golem/internal/server/servertest"
	"slices"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
)

// fillSectionsFixture is the Update Section Blocks packet a vanilla server
// answers "fill 20 -64 20 22 -62 22 glowstone" with: one section at
// (1, -4, 1), twenty-seven glowstone entries, each the state and the
// section-local cell it landed in. The section, count and cells are a
// vanilla capture; the state is glowstone's in the blocks report, 8686.
const fillSectionsFixture = "00000400001ffffc1bc0cafb10d1c8fb10e1cafb10c0ccfb10e1c8fb10e2c8fb10d1ccfb10d0c8fb10d2c8fb10d1cafb10d2cafb10d0cafb10d0ccfb10c2cafb10d2ccfb10c2ccfb10e0c8fb10e2cafb10c1cafb10e0cafb10c0c8fb10c1ccfb10c2c8fb10e0ccfb10e1ccfb10c1c8fb10e2ccfb10"

// awaitSectionBlocks reads packets until an Update Section Blocks packet
// arrives whose section position and set of changed cells are the captured
// ones, answering keep-alives, keeping the System Chat messages it passes on
// the way for the caller - which is where a fill's feedback lands when it
// races the section packet - and holding everything else read past for
// whoever reads next. The entries a vanilla server lists come out of a hash
// of the cells it changed, so the order they ride in is the order that hash
// made of them; the test reads the section and the count exactly and the
// entries as the set they are.
func awaitSectionBlocks(t *testing.T, c *servertest.Client, wantHex string) [][]byte {
	t.Helper()
	wantRaw, err := hex.DecodeString(wantHex)
	if err != nil {
		t.Fatalf("bad fixture hex: %v", err)
	}
	wantPos, wantEntries := servertest.ReadSectionBlocks(t, wantRaw)
	slices.Sort(wantEntries)

	var chats [][]byte
	var skipped []servertest.PushedPacket
	defer func() { c.Unread(skipped...) }()
	for packetID, data := range c.Stream() {
		switch packetID {
		case protocol.IDPlaySectionBlocks:
			gotPos, gotEntries := servertest.ReadSectionBlocks(t, data)
			if gotPos == wantPos && len(gotEntries) == len(wantEntries) {
				slices.Sort(gotEntries)
				if slices.Equal(gotEntries, wantEntries) {
					return chats
				}
			}
		case protocol.IDPlaySystemChat:
			chats = append(chats, data)
			continue
		}
		skipped = append(skipped, servertest.PushedPacket{Id: packetID, Data: data})
	}
	t.Fatalf("no section blocks update for section %d with %d entries arrived", wantPos, len(wantEntries))
	return nil
}

// expectFillFeedback checks the count feedback a fill that changed something
// sends, the number being the count the command reported.
func expectFillFeedback(t *testing.T, c *servertest.Client, chats [][]byte, count int64) {
	t.Helper()
	servertest.ExpectFeedbackSoon(t, c, chats, "commands.fill.success", protocol.Text{Number: &count})
}

// expectFillRefusal checks the single bare red message a fill that refuses
// sends, with no context line after it.
func expectFillRefusal(t *testing.T, c *servertest.Client, key string) {
	t.Helper()
	want := servertest.CommandErrorMessageForTest(key)
	if got := c.ExpectPlay(protocol.IDPlaySystemChat); !bytes.Equal(got, want) {
		t.Errorf("refusal for %s = % X, want % X", key, got, want)
	}
}

// TestFillPutsItsRegionInTheWorldAndTellsTheSections checks /fill against the
// bytes a vanilla server sent for the same command: the section packet carries
// every changed cell of the region - state and section-local position - and
// the feedback names the count.
func TestFillPutsItsRegionInTheWorldAndTellsTheSections(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	glowstone, ok := registry.FindState("minecraft:glowstone", nil)
	if !ok {
		t.Fatal("the registry has no glowstone")
	}

	c.ChatCommand("fill 20 -64 20 22 -62 22 glowstone")
	chats := awaitSectionBlocks(t, c, fillSectionsFixture)
	expectFillFeedback(t, c, chats, 27)
	for x := int32(20); x <= 22; x++ {
		for y := int32(-64); y <= -62; y++ {
			for z := int32(20); z <= 22; z++ {
				if got := srv.BlockAt(x, y, z); got != glowstone {
					t.Errorf("block at (%d, %d, %d) = %d, want %d (glowstone)", x, y, z, got, glowstone)
				}
			}
		}
	}
	c.Disconnect()
}

// TestFillRefusesWhatTheVanillaServerRefuses checks the refusals a fill
// answers with before anything is written: a region over the cap, a corner
// whose chunk is not loaded, a corner outside the world, and a fill that
// changes nothing.
func TestFillRefusesWhatTheVanillaServerRefuses(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	// Thirty-four by a hundred and twenty-eight by thirty-four is over the
	// cap of 32768, and the refusal names both the cap and the area, the
	// area as a long.
	limit, area := int64(32768), int64(147968)
	c.ChatCommand("fill 0 -64 0 33 63 33 stone")
	var toobig protocol.Buffer
	toobig.PutText(protocol.Text{Color: protocol.ColorRed, Extra: []protocol.Text{{
		Translate: "commands.fill.toobig",
		With:      []protocol.Text{{Number: &limit}, {Long: &area}},
	}}})
	toobig.PutBool(false)
	if got := c.ExpectPlay(protocol.IDPlaySystemChat); !bytes.Equal(got, toobig.Bytes()) {
		t.Errorf("too-large fill error = % X, want % X", got, toobig.Bytes())
	}

	// A corner whose chunk is not loaded, and one outside the band of
	// blocks the world has, are both bare red messages.
	c.ChatCommand("fill 2000 64 2000 2000 64 2000 stone")
	expectFillRefusal(t, c, "argument.pos.unloaded")
	if got := srv.BlockAt(2000, 64, 2000); got != 0 {
		t.Errorf("block at the unloaded corner = %d, want air", got)
	}
	c.ChatCommand("fill 0 400 0 0 400 0 stone")
	expectFillRefusal(t, c, "argument.pos.outofworld")

	// A fill that changes nothing - the block already where it is being
	// asked to go - is refused, not celebrated.
	c.ChatCommand("fill 0 70 0 0 70 0 glowstone")
	expectFillFeedback(t, c, nil, 1)
	c.ChatCommand("fill 0 70 0 0 70 0 glowstone")
	expectFillRefusal(t, c, "commands.fill.failed")
	c.Disconnect()
}

// TestFillModesShapeTheRegion checks the modes that decide which cells of the
// region the fill's block lands in: the default fills everything, hollow
// walls the region and empties its inside, keep fills only the air, outline
// walls it and leaves its inside alone, and strict replaces without waking
// the world.
func TestFillModesShapeTheRegion(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	glass, ok := registry.FindState("minecraft:glass", nil)
	if !ok {
		t.Fatal("the registry has no glass")
	}
	interior := func(want uint16, what string) {
		t.Helper()
		if got := srv.BlockAt(6, 70, 6); got != want {
			t.Errorf("interior block = %d, want %d (%s)", got, want, what)
		}
		if got := srv.BlockAt(4, 70, 4); got != glass {
			t.Errorf("wall block = %d, want %d (glass)", got, glass)
		}
	}

	c.ChatCommand("fill 4 68 4 8 72 8 glass")
	expectFillFeedback(t, c, nil, 125)
	interior(glass, "a solid fill")

	c.ChatCommand("fill 4 68 4 8 72 8 glass hollow")
	expectFillFeedback(t, c, nil, 27)
	interior(0, "a hollowed inside")

	// Keep fills only what holds air, so it puts the walls' inside back.
	c.ChatCommand("fill 4 68 4 8 72 8 glass keep")
	expectFillFeedback(t, c, nil, 27)
	interior(glass, "a kept fill")

	// Outline touches only the walls, so over a solid region it changes
	// nothing and is refused.
	c.ChatCommand("fill 4 68 4 8 72 8 glass outline")
	expectFillRefusal(t, c, "commands.fill.failed")

	c.ChatCommand("fill 4 68 4 8 72 8 air")
	expectFillFeedback(t, c, nil, 125)

	// Over an empty region an outline walls it and leaves the inside
	// holding air.
	c.ChatCommand("fill 4 68 4 8 72 8 glass outline")
	expectFillFeedback(t, c, nil, 98)
	interior(0, "an outlined inside")

	// Strict is the plain fill without the wake.
	c.ChatCommand("fill 4 68 4 8 72 8 glass strict")
	expectFillFeedback(t, c, nil, 27)
	interior(glass, "a strict fill")
	c.Disconnect()
}

// TestFillWakesTheWorldUnlessStrict checks that a fill's cells reach the
// physics engine the way a placement does - sand left hanging over air falls
// - unless the strict mode was named, which leaves it hanging.
func TestFillWakesTheWorldUnlessStrict(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	sand, ok := registry.FindState("minecraft:sand", nil)
	if !ok {
		t.Fatal("the registry has no sand")
	}

	// One sand block hung over the ground falls to it. The feedback and
	// the update travel by different roads, so which arrives first is the
	// mailbox's choice; the update is awaited through what earlier readers
	// held back, the way the feedback is.
	c.ChatCommand("fill 24 80 24 24 80 24 sand")
	expectFillFeedback(t, c, nil, 1)
	servertest.AwaitBlockUpdateSoon(t, c, 24, 80, 24, sand)
	servertest.UntilTicks(t, srv, 100, "block at (24, 64, 24)", func() bool { return srv.BlockAt(24, 64, 24) == sand })
	if got := srv.BlockAt(24, 80, 24); got != 0 {
		t.Errorf("block the sand fell from = %d, want 0 (air)", got)
	}
	drainPendingPackets(t, srv, c)

	// A strict fill's sand hangs where it was put, and no block update
	// follows it.
	c.ChatCommand("fill 24 90 24 26 92 26 sand strict")
	expectFillFeedback(t, c, nil, 27)
	c.ExpectNoneWithinTicks(srv, 40, servertest.PacketIs(protocol.IDPlayBlockUpdate))
	if got := srv.BlockAt(24, 90, 24); got != sand {
		t.Errorf("strict sand = %d, want %d (still hanging)", got, sand)
	}
	c.Disconnect()
}

// drainPendingPackets drops whatever the connection still holds - the tail
// of a fill's physics arriving behind the feedback that ordered it - so that
// a later expectNoPacket judges only what arrives after it.
func drainPendingPackets(t *testing.T, srv *server.Server, c *servertest.Client) {
	t.Helper()
	c.Pushback = nil
	c.ReadTicks(srv, 30, func(int32, []byte) bool { return false })
}

// fillLightBound is how many Update Light packets the relight that follows
// the cube fill may cost the client: one relight over the whole region's
// box, where a column-at-a-time relight storms the client with one packet
// per chunk it touched per column.
const fillLightBound = 48

// drainFillRelights counts packets while the workers finish, then flushes their
// last results. Held ticks cannot bound work on the independent lighting pool.
func drainFillRelights(t *testing.T, srv *server.Server, c *servertest.Client) int {
	t.Helper()
	// Pace pending work on the wall clock so waiting cannot advance the
	// other client's keep-alive deadline faster than it can answer.
	servertest.ReleaseTicks(t, srv)
	done := make(chan struct{})
	go func() {
		srv.WaitRelights()
		close(done)
	}()
	timer := time.NewTimer(servertest.ExpectTimeout)
	defer timer.Stop()
	seen := 0
	read := func(id int32, _ []byte) bool {
		if id == protocol.IDPlayUpdateLight {
			seen++
		}
		return false
	}
	for {
		select {
		case <-done:
			servertest.HoldTicks(t, srv)
			c.ReadTicks(srv, 1, read)
			return seen
		case <-timer.C:
			t.Fatal("the fill's relight workers did not finish")
		default:
			// Keep flushing while work is pending so a packet flood is counted,
			// rather than coalesced into one flush after all workers finish.
			c.ReadTicks(srv, 1, read)
		}
	}
}

// TestFillAnswersTheMaximumRegionPromptly checks the worst a player can ask
// of /fill: a region of the 32768-block maximum, two shapes of it — a 32³
// cube and a wide slab — answered within two ticks each, and the
// relight that follows the cube covering the region's box once rather than
// once per column.
func TestFillAnswersTheMaximumRegionPromptly(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	srv, connect := servertest.Start(t, cfg, servertest.OpsListing(t, "Testificate"))
	c := connect().Join("Testificate")
	// Watch the fill from the border of its chunks' view as well.
	edge := servertest.JoinAtViewEdge(t, connect, "Edge")
	servertest.HoldTicks(t, srv)

	stone, ok := registry.FindState("minecraft:stone", nil)
	if !ok {
		t.Fatal("the registry has no stone")
	}

	// 32×32×32 is the cap at once, in the air over the flat ground: every
	// one of its cells changes, so the count names the cap.
	changed := int64(32768)
	var answer protocol.Buffer
	answer.PutText(protocol.Text{Translate: "commands.fill.success", With: []protocol.Text{{Number: &changed}}})
	answer.PutBool(false)

	c.ChatCommand("fill 0 66 0 31 97 31 stone")
	if !c.ReadTicks(srv, 2, func(id int32, data []byte) bool {
		return id == protocol.IDPlaySystemChat && bytes.Equal(data, answer.Bytes())
	}) {
		t.Fatal("the cube fill's answer did not arrive within 2 ticks")
	}

	seenLight := drainFillRelights(t, srv, edge)
	t.Logf("the cube fill's relight took %d Update Light packets", seenLight)
	if seenLight == 0 {
		t.Errorf("the cube fill's relight sent no Update Light packets")
	}
	if seenLight > fillLightBound {
		t.Errorf("the cube fill's relight sent %d Update Light packets, want at most %d", seenLight, fillLightBound)
	}

	// The same cap in a wide slab: 64×64×8 spans sixteen chunks, the shape
	// that pays for one re-encode per chunk instead of one per block.
	c.ChatCommand("fill -48 100 -48 15 107 15 stone")
	if !c.ReadTicks(srv, 2, func(id int32, data []byte) bool {
		return id == protocol.IDPlaySystemChat && bytes.Equal(data, answer.Bytes())
	}) {
		t.Fatal("the slab fill's answer did not arrive within 2 ticks")
	}

	// The light work of the slab is drained too, so the run leaves nothing
	// behind for the cleanup to race, but only the cube's flood is judged.
	drainFillRelights(t, srv, c)

	for x := int32(-48); x <= 15; x++ {
		for z := int32(-48); z <= 15; z++ {
			for y := int32(100); y <= 107; y++ {
				if got := srv.BlockAt(x, y, z); got != stone {
					t.Errorf("block at (%d, %d, %d) = %d, want %d (stone)", x, y, z, got, stone)
				}
			}
		}
	}
	for x := int32(0); x <= 31; x++ {
		for z := int32(0); z <= 31; z++ {
			for y := int32(66); y <= 97; y++ {
				if got := srv.BlockAt(x, y, z); got != stone {
					t.Errorf("block at (%d, %d, %d) = %d, want %d (stone)", x, y, z, got, stone)
				}
			}
		}
	}
	c.Disconnect()
}

// TestFillDestroyBreaksWhatItCovers checks the destroy mode: the blocks the
// region held break the way a player's break does, dropping what their loot
// tables hold, and the fill counts them changed even where its block went
// nowhere new.
func TestFillDestroyBreaksWhatItCovers(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	c.ChatCommand("fill 5 70 5 5 70 5 glowstone")
	expectFillFeedback(t, c, nil, 1)

	c.ChatCommand("fill 5 70 5 5 70 5 air destroy")
	expectFillFeedback(t, c, nil, 1)
	servertest.ExpectOneDroppedItem(t, srv, "minecraft:glowstone_dust")
	if got := srv.BlockAt(5, 70, 5); got != 0 {
		t.Errorf("block the destroy broke = %d, want 0 (air)", got)
	}
	c.Disconnect()
}

// fillDropTotal sums one item's stacks lying in the overworld, the count a
// destroy's spilled contents and rolled loot should come out with.
func fillDropTotal(srv *server.Server, name string) int32 {
	var n int32
	for _, it := range srv.ItemsInDimension(world.DimensionName) {
		if it.Item == name {
			n += it.Count
		}
	}
	return n
}

// TestFillDestroySpillsABrokenContainersContents checks destroy's break of a
// container cell: the chest's own loot drops and the diamonds it held spill,
// both on the ground, and the fill's block replaces it.
func TestFillDestroySpillsABrokenContainersContents(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	stone, ok := registry.FindState("minecraft:stone", nil)
	if !ok {
		t.Fatal("the registry has no stone")
	}
	if !srv.PlaceContainer(5, 70, 5, "minecraft:chest") {
		t.Fatal("the chest would not stand up")
	}
	if !srv.SetInventoryAt(5, 70, 5, 0, servertest.ItemStack(t, "minecraft:diamond", 3)) {
		t.Fatal("the chest would not take the diamonds")
	}

	one := int64(1)
	c.ChatCommand("fill 5 70 5 5 70 5 stone destroy")
	servertest.ExpectFeedbackSoon(t, c, nil, "commands.fill.success", protocol.Text{Number: &one})

	if got := fillDropTotal(srv, "minecraft:chest"); got != 1 {
		t.Errorf("the destroy dropped %d chests, want the one it broke", got)
	}
	if got := fillDropTotal(srv, "minecraft:diamond"); got != 3 {
		t.Errorf("the destroy dropped %d diamonds, want the 3 the chest held", got)
	}
	if got := srv.BlockAt(5, 70, 5); got != stone {
		t.Errorf("block the destroy filled = %d, want %d (stone)", got, stone)
	}
	c.Disconnect()
}

// TestFillDestroyEmptiesAChestItFillsOver checks destroy over the same block:
// the old chest's contents spill with it and a fresh empty chest takes its
// place, even though the state the write asks for is the one already there.
func TestFillDestroyEmptiesAChestItFillsOver(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	chest, ok := registry.FindState("minecraft:chest", nil)
	if !ok {
		t.Fatal("the registry has no chest")
	}
	if !srv.PlaceContainer(5, 70, 5, "minecraft:chest") {
		t.Fatal("the chest would not stand up")
	}
	if !srv.SetInventoryAt(5, 70, 5, 0, servertest.ItemStack(t, "minecraft:diamond", 7)) {
		t.Fatal("the chest would not take the diamonds")
	}

	one := int64(1)
	c.ChatCommand("fill 5 70 5 5 70 5 chest destroy")
	servertest.ExpectFeedbackSoon(t, c, nil, "commands.fill.success", protocol.Text{Number: &one})

	if got := fillDropTotal(srv, "minecraft:chest"); got != 1 {
		t.Errorf("the destroy dropped %d chests, want the one it broke", got)
	}
	if got := fillDropTotal(srv, "minecraft:diamond"); got != 7 {
		t.Errorf("the destroy dropped %d diamonds, want the 7 the chest held", got)
	}
	if got := srv.BlockAt(5, 70, 5); got != chest {
		t.Errorf("block the destroy filled = %d, want %d (chest)", got, chest)
	}
	if !srv.HasBlockEntity(5, 70, 5) {
		t.Fatal("the filled-over chest has no block entity")
	}
	for i, stack := range srv.InventoryAt(5, 70, 5) {
		if !stack.Empty() {
			t.Errorf("the fresh chest still holds %+v in slot %d", stack, i)
		}
	}
	c.Disconnect()
}

// TestFillDestroyPlaysTheBreakEvent checks destroy's World Event 2001: every
// non-fire block it breaks plays the event with the old block's state, which
// is what gives the client the break sound and particles.
func TestFillDestroyPlaysTheBreakEvent(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	glowstone, ok := registry.FindState("minecraft:glowstone", nil)
	if !ok {
		t.Fatal("the registry has no glowstone")
	}
	if !srv.SetBlock(5, 70, 5, glowstone) || !srv.SetBlock(6, 70, 5, glowstone) {
		t.Fatal("the world did not take the glowstone")
	}

	c.ChatCommand("fill 5 70 5 6 70 5 air destroy")
	seen := map[[2]int32]bool{}
	for id, data := range c.Stream() {
		if id != protocol.IDPlayWorldEvent {
			continue
		}
		r := protocol.NewReader(data)
		event := r.ReadI32()
		x, _, z := r.ReadPosition()
		eventData := r.ReadI32()
		if event != protocol.WorldEventBlockBreak {
			continue
		}
		if uint16(eventData) != glowstone {
			t.Errorf("the break event at (%d, 70, %d) carried state %d, want the broken glowstone's %d", x, z, eventData, glowstone)
		}
		seen[[2]int32{x, z}] = true
		if len(seen) == 2 {
			break
		}
	}
	for _, x := range []int32{5, 6} {
		if !seen[[2]int32{x, 5}] {
			t.Errorf("the destroy played no block-break event 2001 at (%d, 70, 5)", x)
		}
	}
	c.Disconnect()
}

// TestFillDestroyCountsOnlyBrokenOrChangedCells checks destroy's count: a
// cell whose old block is air breaks nothing, and one already holding its
// fluid's block is left where the break's write leaves it, so a fill whose
// every cell is one of those changed nothing and is refused.
func TestFillDestroyCountsOnlyBrokenOrChangedCells(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	// Water filled with water: the break leaves water and the placement
	// writes water, so nothing changed.
	water, ok := registry.FindState("minecraft:water", nil)
	if !ok {
		t.Fatal("the registry has no water")
	}
	if !srv.SetBlock(5, 70, 5, water) {
		t.Fatal("the world did not take the water")
	}
	c.ChatCommand("fill 5 70 5 5 70 5 water destroy")
	expectFillRefusal(t, c, "commands.fill.failed")

	// An air variant filled with itself: the break returns false for air
	// and the placement changes nothing either.
	caveAir, ok := registry.FindState("minecraft:cave_air", nil)
	if !ok {
		t.Fatal("the registry has no cave_air")
	}
	if !srv.SetBlock(5, 70, 5, caveAir) {
		t.Fatal("the world did not take the cave air")
	}
	c.ChatCommand("fill 5 70 5 5 70 5 cave_air destroy")
	expectFillRefusal(t, c, "commands.fill.failed")
	c.Disconnect()
}

// TestFillDestroySpawnsOreExperience checks the experience a broken cell owes
// after its drops, the spawnAfterBreak Block.dropResources runs: a diamond
// ore rolls its diamond and leaves its 3-7 orbs too.
func TestFillDestroySpawnsOreExperience(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	ore, ok := registry.FindState("minecraft:diamond_ore", nil)
	if !ok {
		t.Fatal("the registry has no diamond ore")
	}
	if !srv.SetBlock(5, 70, 5, ore) {
		t.Fatal("the world did not take the diamond ore")
	}

	one := int64(1)
	c.ChatCommand("fill 5 70 5 5 70 5 air destroy")
	servertest.ExpectFeedbackSoon(t, c, nil, "commands.fill.success", protocol.Text{Number: &one})

	if got := fillDropTotal(srv, "minecraft:diamond"); got != 1 {
		t.Errorf("the destroy dropped %d diamonds, want the ore's one", got)
	}
	if got := srv.OrbCount(); got == 0 {
		t.Error("the destroy spawned no orbs, want the ore's experience")
	}
	c.Disconnect()
}

// TestFillDestroyRollsLootWithTheNeighbouringBlock checks the location check
// a broken half's table runs past the block itself: filling a tall grass
// lower half with air reads the upper half still standing above it, the
// neighbour that table's check wants.
func TestFillDestroyRollsLootWithTheNeighbouringBlock(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))
	srv.StopTick()

	lower, ok := registry.FindState("minecraft:tall_grass", map[string]string{"half": "lower"})
	if !ok {
		t.Fatal("the registry has no tall grass lower half")
	}
	upper, ok := registry.FindState("minecraft:tall_grass", map[string]string{"half": "upper"})
	if !ok {
		t.Fatal("the registry has no tall grass upper half")
	}
	// The seed lands the table's one-in-eight seed chance, so the location
	// check's answer is the only thing the drop turns on.
	srv.LootSeed(9)
	if !srv.SetBlock(5, 70, 5, lower) || !srv.SetBlock(5, 71, 5, upper) {
		t.Fatal("the world did not take the tall grass")
	}

	one := int64(1)
	c.ChatCommand("fill 5 70 5 5 70 5 air destroy")
	servertest.ExpectFeedbackSoon(t, c, nil, "commands.fill.success", protocol.Text{Number: &one})

	if got := fillDropTotal(srv, "minecraft:wheat_seeds"); got != 1 {
		t.Errorf("the destroyed lower half dropped %d wheat seeds, want the one its neighbour allows", got)
	}
	c.Disconnect()
}

// TestFillDestroyShattersADecoratedPot checks the dynamic drop a table takes
// below the block itself: a cracked decorated pot comes out as the sherds
// its faces supply - four bricks, a plain pot item's - rather than the pot.
func TestFillDestroyShattersADecoratedPot(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	pot, ok := registry.FindState("minecraft:decorated_pot", map[string]string{"cracked": "true"})
	if !ok {
		t.Fatal("the registry has no cracked decorated pot")
	}
	if !srv.SetBlock(5, 70, 5, pot) {
		t.Fatal("the world did not take the decorated pot")
	}
	srv.SetBlockEntityTagAt(5, 70, 5, "minecraft:decorated_pot",
		nbt.Compound{"sherds": registry.PotDecorationsTag(registry.PotAllBrick())})

	one := int64(1)
	c.ChatCommand("fill 5 70 5 5 70 5 air destroy")
	servertest.ExpectFeedbackSoon(t, c, nil, "commands.fill.success", protocol.Text{Number: &one})

	if got := fillDropTotal(srv, "minecraft:brick"); got != 4 {
		t.Errorf("the shattered pot dropped %d bricks, want the four its faces supply", got)
	}
	if got := fillDropTotal(srv, "minecraft:decorated_pot"); got != 0 {
		t.Errorf("the shattered pot dropped %d pots, want none", got)
	}
	c.Disconnect()
}

// TestFillDestroyLeavesFireUnannounced checks the one block a destroy breaks
// without the 2001 event: fire breaks silently, so no block-break event is
// sent for it.
func TestFillDestroyLeavesFireUnannounced(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))
	srv.StopTick()

	fire, ok := registry.FindState("minecraft:fire", nil)
	if !ok {
		t.Fatal("the registry has no fire")
	}
	if !srv.SetBlock(5, 70, 5, fire) {
		t.Fatal("the world did not take the fire")
	}

	one := int64(1)
	c.ChatCommand("fill 5 70 5 5 70 5 air destroy")
	servertest.ExpectFeedbackSoon(t, c, nil, "commands.fill.success", protocol.Text{Number: &one})

	c.ReadTicks(srv, 0, func(id int32, data []byte) bool {
		if id != protocol.IDPlayWorldEvent {
			return false
		}
		r := protocol.NewReader(data)
		if event := r.ReadI32(); event == protocol.WorldEventBlockBreak {
			t.Errorf("the destroy announced a block break for fire: % X", data)
		}
		return false
	})
	c.Disconnect()
}

// TestFillDestroyDropsAShulkerBoxWhole checks the container a break must not
// spill: a shulker box's contents ride the box's own drop, so the destroy
// lays one box and no loose contents.
func TestFillDestroyDropsAShulkerBoxWhole(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	if !srv.PlaceContainer(5, 70, 5, "minecraft:shulker_box") {
		t.Fatal("the shulker box would not stand up")
	}
	if !srv.SetInventoryAt(5, 70, 5, 0, servertest.ItemStack(t, "minecraft:diamond", 4)) {
		t.Fatal("the shulker box would not take the diamonds")
	}

	one := int64(1)
	c.ChatCommand("fill 5 70 5 5 70 5 air destroy")
	servertest.ExpectFeedbackSoon(t, c, nil, "commands.fill.success", protocol.Text{Number: &one})

	if got := fillDropTotal(srv, "minecraft:shulker_box"); got != 1 {
		t.Errorf("the destroy dropped %d shulker boxes, want the one it broke", got)
	}
	if got := fillDropTotal(srv, "minecraft:diamond"); got != 0 {
		t.Errorf("the destroy spilled %d diamonds, want none: they ride the dropped box", got)
	}
	c.Disconnect()
}

// TestFillCarriesTheTagToEveryCell checks the tag a fill's block argument
// carries: every cell the fill puts the block in takes the tag the way the
// vanilla fill gives it to each block it places, the spawners tell the
// clients their data, a fill that changes nothing is refused, and one whose
// tag changes what the cells hold counts them again.
func TestFillCarriesTheTagToEveryCell(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	spawner, ok := registry.FindState("minecraft:spawner", nil)
	if !ok {
		t.Fatal("the registry has no spawner")
	}

	c.ChatCommand("fill 20 64 20 21 64 21 minecraft:spawner{MaxNearbyEntities:9}")
	var four int64 = 4
	servertest.ExpectFeedbackSoon(t, c, nil, "commands.fill.success", protocol.Text{Number: &four})
	for x := int32(20); x <= 21; x++ {
		for z := int32(20); z <= 21; z++ {
			if got := srv.BlockAt(x, 64, z); got != spawner {
				t.Errorf("block at (%d, 64, %d) = %d, want %d (spawner)", x, z, got, spawner)
			}
			tag, ok := srv.BlockEntityTagAt(x, 64, z)
			if !ok {
				t.Fatalf("the spawner at (%d, 64, %d) carries no tag", x, z)
			}
			if got, _ := nbt.Get[nbt.Short](tag, "MaxNearbyEntities"); got != 9 {
				t.Errorf("the spawner at (%d, 64, %d) holds MaxNearbyEntities %d, want 9", x, z, got)
			}
		}
	}
	// A spawner is one of the entities the clients are told of, so each
	// cell speaks once.
	for range 4 {
		expectBlockEntityData(t, c)
	}
	c.Drain()

	// The same fill again changes nothing, which the vanilla server
	// refuses.
	c.ChatCommand("fill 20 64 20 21 64 21 minecraft:spawner{MaxNearbyEntities:9}")
	servertest.AwaitCommandFailure(t, c, "commands.fill.failed")
	c.Drain()

	// A tag that changes what the cells hold counts them all again.
	c.ChatCommand("fill 20 64 20 21 64 21 minecraft:spawner{MaxNearbyEntities:12}")
	servertest.ExpectFeedbackSoon(t, c, nil, "commands.fill.success", protocol.Text{Number: &four})
	for x := int32(20); x <= 21; x++ {
		for z := int32(20); z <= 21; z++ {
			tag, ok := srv.BlockEntityTagAt(x, 64, z)
			if !ok {
				t.Fatalf("the spawner at (%d, 64, %d) carries no tag", x, z)
			}
			if got, _ := nbt.Get[nbt.Short](tag, "MaxNearbyEntities"); got != 12 {
				t.Errorf("the spawner at (%d, 64, %d) holds MaxNearbyEntities %d, want 12", x, z, got)
			}
		}
	}
	c.Drain()
	c.Disconnect()
}

// sectionBlocksHas reports whether an Update Section Blocks packet carries
// the block at (x, y, z): its section position and its local cell.
func sectionBlocksHas(t *testing.T, data []byte, x, y, z int32) bool {
	t.Helper()
	pos, entries := servertest.ReadSectionBlocks(t, data)
	want := int64(uint64((x>>4)&0x3FFFFF)<<42 | uint64((z>>4)&0x3FFFFF)<<20 | uint64((y>>4)&0xFFFFF))
	if pos != want {
		return false
	}
	cell := int64((x&15)<<8 | (z&15)<<4 | (y & 15))
	for _, entry := range entries {
		if entry&0xFFF == cell {
			return true
		}
	}
	return false
}

// spawnDataID is the entity id inside a block entity compound's SpawnData,
// empty while it has none.
func spawnDataID(tag nbt.Compound) string {
	data, _ := nbt.Get[nbt.Compound](tag, "SpawnData")
	entity, _ := nbt.Get[nbt.Compound](data, "entity")
	id, _ := nbt.Get[nbt.String](entity, "id")
	return string(id)
}

// awaitFillEntityData waits for the block entity data packet a fill sends
// for the cell and fails when it arrives before the block: a client drops
// block entity data for a block it has not been told of yet, so a fill has
// to tell the block first. It returns the packet's compound.
func awaitFillEntityData(t *testing.T, c *servertest.Client, x, y, z int32) nbt.Compound {
	t.Helper()
	toldBlock := false
	for id, data := range c.Stream() {
		switch id {
		case protocol.IDPlayBlockUpdate:
			r := protocol.NewReader(data)
			if bx, by, bz := r.ReadPosition(); bx == x && by == y && bz == z {
				toldBlock = true
			}
		case protocol.IDPlaySectionBlocks:
			if sectionBlocksHas(t, data, x, y, z) {
				toldBlock = true
			}
		case protocol.IDPlayBlockEntityData:
			r := protocol.NewReader(data)
			if bx, by, bz := r.ReadPosition(); bx != x || by != y || bz != z {
				continue
			}
			r.ReadVarInt() // the block entity type, which the packet names itself
			if !toldBlock {
				t.Errorf("the block entity data for (%d, %d, %d) arrived before its block", x, y, z)
			}
			tag, _, err := nbt.DecodeNetwork(r.Rest())
			if err != nil {
				t.Fatalf("block entity data NBT: %v", err)
			}
			compound, _ := tag.(nbt.Compound)
			return compound
		}
	}
	t.Fatalf("no block entity data for (%d, %d, %d) arrived", x, y, z)
	return nil
}

// TestFillSendsTheSpawnerDataAfterTheBlock fills one cell with a skeleton
// spawner: the block reaches the client before its block entity data, which
// names the skeleton, and the block entity keeps it across a save and
// reload.
func TestFillSendsTheSpawnerDataAfterTheBlock(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()

	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	c.ChatCommand(`fill 5 64 5 5 64 5 minecraft:spawner{SpawnData:{entity:{id:"minecraft:skeleton"}}}`)
	compound := awaitFillEntityData(t, c, 5, 64, 5)
	if got := spawnDataID(compound); got != "minecraft:skeleton" {
		t.Errorf("the fill's block entity data names %q, want minecraft:skeleton", got)
	}

	tag, ok := srv.BlockEntityTagAt(5, 64, 5)
	if !ok {
		t.Fatal("the filled spawner carries no block entity")
	}
	if got := spawnDataID(tag); got != "minecraft:skeleton" {
		t.Errorf("the filled spawner holds %q, want minecraft:skeleton", got)
	}

	c.Disconnect()
	srv.Shutdown()

	back, c2 := servertest.Dial(t, cfg, servertest.OpsListing(t, "Testificate"))
	defer c2.Disconnect()
	c2.Join("Testificate")
	tag, ok = back.BlockEntityTagAt(5, 64, 5)
	if !ok {
		t.Fatal("the reloaded spawner carries no block entity")
	}
	if got := spawnDataID(tag); got != "minecraft:skeleton" {
		t.Errorf("the reloaded spawner holds %q, want minecraft:skeleton", got)
	}
}

// TestFillSendsTheSignTextAfterTheBlock fills one cell with a sign whose
// front text the tag sets: the block reaches the client before its block
// entity data, and the text is what the tag wrote.
func TestFillSendsTheSignTextAfterTheBlock(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()

	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	c.ChatCommand(`fill 7 64 7 7 64 7 minecraft:oak_sign{front_text:{messages:["Hello","","",""]}}`)
	compound := awaitFillEntityData(t, c, 7, 64, 7)
	front, ok := nbt.Get[nbt.Compound](compound, "front_text")
	if !ok {
		t.Fatalf("the sign's block entity data carries no front_text: %v", compound)
	}
	messages, ok := nbt.Get[nbt.List](front, "messages")
	if !ok || len(messages.Items) == 0 {
		t.Fatalf("the sign's front_text carries no messages: %v", front)
	}
	if got, _ := messages.Items[0].(nbt.String); string(got) != "Hello" {
		t.Errorf("the sign's front line = %q, want Hello", got)
	}

	tag, ok := srv.BlockEntityTagAt(7, 64, 7)
	if !ok {
		t.Fatal("the filled sign carries no block entity")
	}
	holdsHello := false
	if front, ok := nbt.Get[nbt.Compound](tag, "front_text"); ok {
		if messages, ok := nbt.Get[nbt.List](front, "messages"); ok && len(messages.Items) > 0 {
			if got, _ := messages.Items[0].(nbt.String); string(got) == "Hello" {
				holdsHello = true
			}
		}
	}
	if !holdsHello {
		t.Errorf("the filled sign holds %v, want front line Hello", tag)
	}
	c.Disconnect()
}

// TestFillReplaceAppliesAFilterWithoutStates checks that a replace filter
// naming a bare block id, with no block states, still picks the cells: a
// filter no cell matches fills nothing, and one some cells match fills only
// those.
func TestFillReplaceAppliesAFilterWithoutStates(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	c.ChatCommand("fill 10 70 1 12 70 3 water")
	expectFillFeedback(t, c, nil, 9)
	c.ChatCommand("fill 11 70 2 11 70 2 glowstone")
	expectFillFeedback(t, c, nil, 1)
	water := srv.BlockAt(10, 70, 1)

	c.ChatCommand("fill 10 70 1 12 70 3 minecraft:stone replace minecraft:glass")
	expectFillRefusal(t, c, "commands.fill.failed")
	if got := srv.BlockAt(10, 70, 1); got != water {
		t.Errorf("block a non-matching filter covered = %d, want %d (water)", got, water)
	}

	stone, ok := registry.FindState("minecraft:stone", nil)
	if !ok {
		t.Fatal("the registry has no stone")
	}
	c.ChatCommand("fill 10 70 1 12 70 3 stone replace glowstone")
	expectFillFeedback(t, c, nil, 1)
	if got := srv.BlockAt(11, 70, 2); got != stone {
		t.Errorf("block the filter matched = %d, want %d (stone)", got, stone)
	}
	if got := srv.BlockAt(10, 70, 1); got != water {
		t.Errorf("block the filter skipped = %d, want %d (water)", got, water)
	}
	c.Disconnect()
}

// TestFillReplaceTakesATagFilterAndAMode checks the filter of a replace: a
// block tag picks every block in it, and a mode named after the filter
// shapes the fill the filter picked the cells of.
func TestFillReplaceTakesATagFilterAndAMode(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	stone, ok := registry.FindState("minecraft:stone", nil)
	if !ok {
		t.Fatal("the registry has no stone")
	}
	glass, ok := registry.FindState("minecraft:glass", nil)
	if !ok {
		t.Fatal("the registry has no glass")
	}

	c.ChatCommand("fill 20 70 20 22 70 22 glass")
	expectFillFeedback(t, c, nil, 9)
	c.ChatCommand("fill 20 70 20 20 70 20 oak_log")
	expectFillFeedback(t, c, nil, 1)
	c.ChatCommand("fill 22 70 22 22 70 22 birch_log")
	expectFillFeedback(t, c, nil, 1)

	c.ChatCommand("fill 20 70 20 22 70 22 stone replace #minecraft:logs")
	expectFillFeedback(t, c, nil, 2)
	for _, cell := range [][3]int32{{20, 70, 20}, {22, 70, 22}} {
		if got := srv.BlockAt(cell[0], cell[1], cell[2]); got != stone {
			t.Errorf("log at %v the tag matched = %d, want %d (stone)", cell, got, stone)
		}
	}
	if got := srv.BlockAt(21, 70, 21); got != glass {
		t.Errorf("block the tag skipped = %d, want %d (glass)", got, glass)
	}

	// Hollow after the filter airs the inside cells the filter picked.
	c.ChatCommand("fill 30 68 30 32 70 32 glass")
	expectFillFeedback(t, c, nil, 27)
	c.ChatCommand("fill 30 68 30 32 70 32 glass replace glass hollow")
	expectFillFeedback(t, c, nil, 1)
	if got := srv.BlockAt(31, 69, 31); got != 0 {
		t.Errorf("inside of a filtered hollow fill = %d, want 0 (air)", got)
	}
	if got := srv.BlockAt(30, 69, 30); got != glass {
		t.Errorf("wall of a filtered hollow fill = %d, want %d (glass)", got, glass)
	}

	c.Disconnect()
}

// TestFillKeepCountsEveryAirAsEmpty checks that keep fills cave air and
// void air as well as plain air.
func TestFillKeepCountsEveryAirAsEmpty(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	glass, ok := registry.FindState("minecraft:glass", nil)
	if !ok {
		t.Fatal("the registry has no glass")
	}
	c.ChatCommand("fill 40 70 40 42 70 40 glass")
	expectFillFeedback(t, c, nil, 3)
	c.ChatCommand("fill 40 70 40 40 70 40 cave_air")
	expectFillFeedback(t, c, nil, 1)
	c.ChatCommand("fill 41 70 40 41 70 40 void_air")
	expectFillFeedback(t, c, nil, 1)

	c.ChatCommand("fill 40 70 40 42 70 40 stone keep")
	expectFillFeedback(t, c, nil, 2)
	stone, ok := registry.FindState("minecraft:stone", nil)
	if !ok {
		t.Fatal("the registry has no stone")
	}
	for _, x := range []int32{40, 41} {
		if got := srv.BlockAt(x, 70, 40); got != stone {
			t.Errorf("cell %d a keep fill found empty = %d, want %d (stone)", x, got, stone)
		}
	}
	if got := srv.BlockAt(42, 70, 40); got != glass {
		t.Errorf("cell a keep fill found filled = %d, want %d (glass)", got, glass)
	}
	c.Disconnect()
}

// TestBlockArgumentsTakeSpacesInsideTheirBrackets checks that a block state
// or predicate may carry whitespace inside its property list and block
// entity tag, and that fill and clone read the predicate across its words.
func TestBlockArgumentsTakeSpacesInsideTheirBrackets(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))
	servertest.HoldTicks(t, srv)

	named := func(name string) uint16 {
		state, ok := registry.FindState(name, nil)
		if !ok {
			t.Fatalf("the registry has no %s", name)
		}
		return state
	}
	stairs, ok := registry.FindState("minecraft:oak_stairs", map[string]string{"facing": "east", "half": "top"})
	if !ok {
		t.Fatal("the registry has no such oak stairs")
	}
	await := func(x, y, z int32, want uint16) {
		t.Helper()
		c.Settle()
		servertest.UntilTicks(t, srv, 20, "block set", func() bool { return srv.BlockAt(x, y, z) == want })
	}

	c.ChatCommand("setblock 1 64 1 oak_stairs[ facing = east , half=top ]")
	await(1, 64, 1, stairs)
	c.ChatCommand("execute if block 1 64 1 #minecraft:stairs[half=top, facing=east] run setblock 2 64 1 gold_block")
	await(2, 64, 1, named("minecraft:gold_block"))
	c.ChatCommand(`setblock 3 64 1 chest{Items:[{Slot:0b, id:"minecraft:stone", count:1}]}`)
	c.Settle()
	servertest.UntilTicks(t, srv, 20, "chest", func() bool { return srv.BlockAt(3, 64, 1) != 0 })
	c.ChatCommand(`execute if block 3 64 1 chest{Items:[{Slot:0b, id:"minecraft:stone"}]} run setblock 4 64 1 diamond_block`)
	await(4, 64, 1, named("minecraft:diamond_block"))
	c.ChatCommand("clone 1 64 1 1 64 1 1 64 5 filtered oak_stairs[half=top, facing=east]")
	await(1, 64, 5, stairs)
	c.ChatCommand("fill 1 64 1 1 64 1 stone replace oak_stairs[half=top, facing=east]")
	await(1, 64, 1, named("minecraft:stone"))
	c.Disconnect()
}

// TestFillAirDropsWhatLosesItsSupportInsideTheRegion checks that a fill
// places cell by cell, x innermost then y then z: plants, torches, door and
// bed halves and wire whose support the fill already replaced drop their
// loot before the fill reaches them, and the count leaves out the cells that
// were already air by then. The wall torch's support comes after it in
// order, so it drops nothing. vanilla: FillCommand.fillBlocks, BlockInput.place
func TestFillAirDropsWhatLosesItsSupportInsideTheRegion(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	c.ChatCommand("tp @s 100 64 102")
	// The region is 100 blocks from the spawn: wait for its chunk to load.
	for deadline := time.Now().Add(5 * time.Second); !srv.SetBlock(100, -61, 102, 0); {
		if time.Now().After(deadline) {
			t.Fatal("the fill region's chunk never loaded")
		}
		time.Sleep(10 * time.Millisecond)
	}
	c.ChatCommand("gamerule send_command_feedback false")
	c.ChatCommand("fill 98 -61 100 103 -58 104 air strict")
	c.ChatCommand("fill 98 -61 100 103 -61 104 stone strict")
	c.ChatCommand("setblock 99 -59 104 stone")
	for _, cmd := range []string{
		"setblock 99 -59 103 wall_torch[facing=north]",
		"setblock 102 -60 102 torch",
		"setblock 101 -60 101 rose_bush[half=lower]",
		"setblock 101 -59 101 rose_bush[half=upper]",
		"setblock 100 -60 102 oak_door[half=lower,facing=north]",
		"setblock 100 -59 102 oak_door[half=upper,facing=north]",
		"setblock 103 -60 101 redstone_wire",
		"setblock 98 -60 102 white_bed[part=foot,facing=south]",
		"setblock 98 -60 103 white_bed[part=head,facing=south]",
		// Flowers on stone survive only a write that runs no shape update.
		"fill 99 -60 101 99 -60 101 poppy strict",
		"fill 100 -60 101 100 -60 101 dandelion strict",
	} {
		c.ChatCommand(cmd)
	}
	c.ChatCommand("kill @e[type=item]")
	c.ChatCommand("gamerule send_command_feedback true")
	c.ChatCommand("fill 98 -61 100 103 -58 104 air")
	// 42 cells hold something; the nine whose block lost its support (poppy,
	// dandelion, torch, wire, both rose bush and door halves, the bed head)
	// are air by the time the fill reaches them and do not count.
	expectFillFeedback(t, c, nil, 33)
	for _, name := range []string{
		"minecraft:poppy", "minecraft:dandelion", "minecraft:rose_bush",
		"minecraft:redstone", "minecraft:oak_door", "minecraft:torch",
		"minecraft:white_bed",
	} {
		if got := fillDropTotal(srv, name); got != 1 {
			t.Errorf("%s dropped %d times, want 1", name, got)
		}
	}
	if got := len(srv.ItemsInDimension(world.DimensionName)); got != 7 {
		t.Errorf("%d item entities, want 7", got)
	}
	c.Disconnect()
}

// TestFillAirKeepsARailOutOfTheRegionDrops checks a rail checks its support
// only when the fill's neighbour updates arrive: inside the region it turns
// to air silently and counts, while the repeater and comparator drop; a rail
// whose cell lies outside the region still pops.
// vanilla: BaseRailBlock.updateShape, BaseRailBlock.neighborChanged
func TestFillAirKeepsARailOutOfTheRegionDrops(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	c.ChatCommand("gamerule send_command_feedback false")
	c.ChatCommand("fill -6 -61 0 -2 -60 0 air strict")
	c.ChatCommand("fill -6 -61 0 -2 -61 0 stone strict")
	for _, cmd := range []string{
		"setblock -6 -60 0 rail",
		"setblock -4 -60 0 repeater",
		"setblock -2 -60 0 comparator",
		"setblock -6 -60 4 rail",
		"setblock -6 -61 4 stone",
	} {
		c.ChatCommand(cmd)
	}
	c.ChatCommand("kill @e[type=item]")
	c.ChatCommand("gamerule send_command_feedback true")
	c.ChatCommand("fill -6 -61 0 -2 -60 0 air")
	expectFillFeedback(t, c, nil, 6)
	for _, name := range []string{"minecraft:repeater", "minecraft:comparator"} {
		if got := fillDropTotal(srv, name); got != 1 {
			t.Errorf("%s dropped %d times, want 1", name, got)
		}
	}
	if got := fillDropTotal(srv, "minecraft:rail"); got != 0 {
		t.Errorf("rail dropped %d times inside the region, want 0", got)
	}
	c.ChatCommand("fill -6 -61 4 -6 -61 4 air")
	expectFillFeedback(t, c, nil, 1)
	if got := fillDropTotal(srv, "minecraft:rail"); got != 1 {
		t.Errorf("rail dropped %d times outside the region, want 1", got)
	}
	c.Disconnect()
}

// TestFillDestroyWithFilterBreaksOnlyMatchingCells checks a filtered destroy
// fill tests each cell against the filter before breaking it: the cell the
// filter refuses keeps its block and drops nothing.
func TestFillDestroyWithFilterBreaksOnlyMatchingCells(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.GameMode = "creative"

	srv, c := servertest.Join(t, cfg, "Testificate", servertest.OpsListing(t, "Testificate"))

	glowstone, ok := registry.FindState("minecraft:glowstone", nil)
	if !ok {
		t.Fatal("the registry has no glowstone")
	}
	c.ChatCommand("setblock 5 70 5 glowstone")
	c.ChatCommand("setblock 6 70 5 stone")
	c.ChatCommand("fill 5 70 5 6 70 5 air replace stone destroy")
	expectFillFeedback(t, c, nil, 1)

	if got := srv.BlockAt(5, 70, 5); got != glowstone {
		t.Errorf("block the filter refused = %d, want %d (glowstone)", got, glowstone)
	}
	if got := srv.BlockAt(6, 70, 5); got != 0 {
		t.Errorf("block the filter matched = %d, want 0 (air)", got)
	}
	if got := fillDropTotal(srv, "minecraft:glowstone_dust"); got != 0 {
		t.Errorf("the destroy dropped %d glowstone dust, want 0", got)
	}
	c.Disconnect()
}
