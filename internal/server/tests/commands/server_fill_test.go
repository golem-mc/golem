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
	edge := servertest.JoinAtViewEdge(t, srv, connect, "Edge")
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
