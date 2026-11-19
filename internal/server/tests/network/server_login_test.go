package network

import (
	"bytes"
	"encoding/json"
	"github.com/golem-mc/golem/internal/server/servertest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

func TestStatusPing(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.MOTD = "Hello Minecraft"

	_, c := servertest.Dial(t, cfg)
	c.Handshake(protocol.NextStateStatus)
	c.Send(protocol.IDStatusRequest, nil)

	r := protocol.NewReader(c.Expect(protocol.IDStatusResponse))
	var resp protocol.StatusResponse
	if err := json.Unmarshal([]byte(r.ReadString(32767)), &resp); err != nil {
		t.Fatalf("unmarshal status: %v", err)
	}
	if resp.Description.Text != cfg.MOTD {
		t.Errorf("MOTD = %q, want %q", resp.Description.Text, cfg.MOTD)
	}
	if resp.Players.Max != cfg.MaxPlayers {
		t.Errorf("max players = %d, want %d", resp.Players.Max, cfg.MaxPlayers)
	}
	if resp.Players.Online != 0 {
		t.Errorf("online players = %d, want 0", resp.Players.Online)
	}
	if resp.Version.Protocol != protocol.Version {
		t.Errorf("protocol = %d, want %d", resp.Version.Protocol, protocol.Version)
	}

	c.Send(protocol.IDStatusPing, func(b *protocol.Buffer) { b.PutI64(123456789) })
	pong := protocol.NewReader(c.Expect(protocol.IDStatusPong))
	if got := pong.ReadI64(); got != 123456789 {
		t.Errorf("pong = %d, want 123456789", got)
	}
}

func TestPlayerJoins(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	srv, c := servertest.Dial(t, cfg)
	playerUUID := c.Login("Testificate")
	if want := protocol.OfflineUUID("Testificate"); playerUUID != want {
		t.Errorf("offline UUID = %s, want %s", playerUUID, want)
	}
	c.WatchBook = true
	c.Configure()

	// Login (play) describes the world the player is joining: the three
	// dimensions this server runs, the overworld first.
	r := protocol.NewReader(c.Expect(protocol.IDPlayLogin))
	entityID := r.ReadI32()
	r.ReadBool() // hardcore
	if got := r.ReadVarInt(); got != 3 {
		t.Errorf("dimension count = %d, want 3", got)
	}
	for _, want := range []string{"minecraft:overworld", "minecraft:the_nether", "minecraft:the_end"} {
		if got := r.ReadString(32767); got != want {
			t.Errorf("dimension = %q, want %q", got, want)
		}
	}
	if err := r.Err(); err != nil {
		t.Fatalf("login (play): %v", err)
	}
	if entityID == 0 {
		t.Error("entity ID = 0, want a real entity ID")
	}

	// The world's difficulty comes next, before anything about the player
	// themselves: easy, which is what a vanilla server.properties starts
	// with and what a world this server creates carries.
	r = protocol.NewReader(c.Expect(protocol.IDPlayDifficulty))
	if got := r.ReadU8(); got != 1 {
		t.Errorf("difficulty = %d, want 1 (easy)", got)
	}
	if got := r.ReadBool(); got {
		t.Error("difficulty is locked, want it free")
	}

	// A player joins in survival, the default of server.properties: nothing
	// about them can fly, and they have everything to lose.
	r = protocol.NewReader(c.Expect(protocol.IDPlayAbilities))
	if got := r.ReadI8(); got != 0 {
		t.Errorf("abilities flags = %#02x, want 0 (survival)", got)
	}

	// The hotbar selection goes out before there is anything to select, which
	// is where the vanilla server sends it, and the experience beside it:
	// an empty bar, level nought and no total, for a player who has none.
	if got := protocol.NewReader(c.Expect(protocol.IDPlayHeldItem)).ReadVarInt(); got != 0 {
		t.Errorf("held hotbar slot = %d, want 0", got)
	}
	r = protocol.NewReader(c.Expect(protocol.IDPlaySetExperience))
	if progress := r.ReadF32(); progress != 0 {
		t.Errorf("experience progress = %g, want 0", progress)
	}
	if level := r.ReadVarInt(); level != 0 {
		t.Errorf("experience level = %d, want 0", level)
	}
	if total := r.ReadVarInt(); total != 0 {
		t.Errorf("experience total = %d, want 0", total)
	}
	if err := r.Err(); err != nil {
		t.Fatalf("set experience: %v", err)
	}

	// What the recipe book is made of comes next, before anything about the
	// player: the nine item groups its filters test against and the
	// stonecutter's recipes, both as the vanilla server sends them.
	andesite, ok := registry.ItemID("minecraft:andesite")
	if !ok {
		t.Fatal("no andesite in the item registry")
	}
	slab, _ := registry.ItemID("minecraft:andesite_slab")
	r = protocol.NewReader(c.Expect(protocol.IDPlayUpdateRecipes))
	sets := r.ReadVarInt()
	if sets != 9 {
		t.Errorf("recipe property sets = %d, want 9", sets)
	}
	for range sets {
		if got := r.ReadString(32767); !slices.Contains([]string{
			"minecraft:blast_furnace_input", "minecraft:campfire_input",
			"minecraft:smithing_base", "minecraft:smithing_template",
			"minecraft:furnace_input", "minecraft:smoker_input",
			"minecraft:smithing_addition",
			"minecraft:brewing_input", "minecraft:brewing_reagent",
		}, got) {
			t.Errorf("recipe property set = %q, want one of the vanilla nine", got)
		}
		for range r.ReadVarInt() {
			r.ReadVarInt()
		}
	}
	cuts := r.ReadVarInt()
	if cuts != 351 {
		t.Errorf("stonecutter recipes = %d, want 351", cuts)
	}
	// The first cut is andesite's: one input item, and a stack of two slabs
	// as an item stack display whose component patch carries nothing.
	if got := r.ReadVarInt(); got != 2 {
		t.Errorf("first stonecutter input set type = %d, want 2 (one item)", got)
	}
	if got := r.ReadVarInt(); got != andesite {
		t.Errorf("first stonecutter input = %d, want andesite", got)
	}
	if got := r.ReadVarInt(); got != 5 {
		t.Errorf("first stonecutter result display type = %d, want 5 (item stack)", got)
	}
	if got := r.ReadVarInt(); got != slab {
		t.Errorf("first stonecutter result = %d, want andesite slab", got)
	}
	if got := r.ReadVarInt(); got != 2 {
		t.Errorf("first stonecutter result count = %d, want 2", got)
	}
	if got := r.ReadVarInt(); got != 0 {
		t.Errorf("first stonecutter result added components = %d, want none", got)
	}
	if got := r.ReadVarInt(); got != 0 {
		t.Errorf("first stonecutter result removed components = %d, want none", got)
	}
	// The rest of the cuts read the same shape, so the whole packet is
	// accounted for by the time the next packet is expected.
	for range cuts - 1 {
		r.ReadVarInt() // the input set's type
		r.ReadVarInt() // the input item
		for range 5 {
			r.ReadVarInt() // display type, item, count, and the patch's two counts
		}
	}
	if err := r.Err(); err != nil {
		t.Fatalf("update recipes: %v", err)
	}
	if got := r.Remaining(); got != 0 {
		t.Errorf("update recipes has %d bytes left over", got)
	}

	// The permission level the player holds rides in as an entity event, one
	// of the statuses that are a player's alone. With no ops.json the server
	// has no operators: the joining player holds level 0, and 24 is that
	// level's status.
	r = protocol.NewReader(c.Expect(protocol.IDPlayEntityEvent))
	if got := r.ReadI32(); got != entityID {
		t.Errorf("entity event for %d, want %d", got, entityID)
	}
	if got := r.ReadU8(); got != 24 {
		t.Errorf("entity event status = %d, want %d (permission level 0)", got, 24)
	}

	c.Expect(protocol.IDPlayCommands)

	// The recipe book's settings come with the command tree: every switch a
	// new player has not touched sits at off.
	r = protocol.NewReader(c.Expect(protocol.IDPlayRecipeBookSettings))
	for i := range 8 {
		if got := r.ReadBool(); got {
			t.Errorf("recipe book setting %d is on, want it off", i)
		}
	}
	if err := r.Err(); err != nil {
		t.Fatalf("recipe book settings: %v", err)
	}
	if got := r.Remaining(); got != 0 {
		t.Errorf("recipe book settings has %d bytes left over", got)
	}

	// The one recipe a new player knows is the crafting table: shaped two by
	// two of the planks tag, with the table both as its result and as the
	// station it is made at, and the same four squares as its crafting
	// requirements.
	craftingTable, ok := registry.ItemID("minecraft:crafting_table")
	if !ok {
		t.Fatal("no crafting_table in the item registry")
	}
	r = protocol.NewReader(c.Expect(protocol.IDPlayRecipeBookAdd))
	if got := r.ReadVarInt(); got != 1 {
		t.Errorf("recipe book entries = %d, want 1", got)
	}
	if got, want := r.ReadVarInt(), registry.CraftingDisplayOfName("minecraft:crafting_table").ID; got != want {
		t.Errorf("recipe book entry ID = %d, want %d", got, want)
	}
	if got := r.ReadVarInt(); got != 1 {
		t.Errorf("recipe display type = %d, want 1 (shaped)", got)
	}
	width, height := r.ReadVarInt(), r.ReadVarInt()
	if width != 2 || height != 2 {
		t.Errorf("recipe shape = %dx%d, want 2x2", width, height)
	}
	if got := r.ReadVarInt(); got != 4 {
		t.Errorf("recipe squares = %d, want 4", got)
	}
	for i := range 4 {
		if got := r.ReadVarInt(); got != 6 {
			t.Errorf("square %d display type = %d, want 6 (tag)", i, got)
		}
		if got := r.ReadVarInt(); got != 0 {
			t.Errorf("square %d HolderSet marker = %d, want 0 (tag name)", i, got)
		}
		if got := r.ReadString(32767); got != "minecraft:planks" {
			t.Errorf("square %d asks for %q, want minecraft:planks", i, got)
		}
	}
	if got := r.ReadVarInt(); got != 5 {
		t.Errorf("result display type = %d, want 5 (item stack)", got)
	}
	if got := r.ReadVarInt(); got != craftingTable {
		t.Errorf("result = %d, want the crafting table", got)
	}
	if got := r.ReadVarInt(); got != 1 {
		t.Errorf("result count = %d, want 1", got)
	}
	if got := r.ReadVarInt(); got != 0 {
		t.Errorf("result added components = %d, want none", got)
	}
	if got := r.ReadVarInt(); got != 0 {
		t.Errorf("result removed components = %d, want none", got)
	}
	if got := r.ReadVarInt(); got != 4 {
		t.Errorf("station display type = %d, want 4 (item)", got)
	}
	if got := r.ReadVarInt(); got != craftingTable {
		t.Errorf("station = %d, want the crafting table", got)
	}
	if got := r.ReadVarInt(); got != 0 {
		t.Errorf("recipe group = %d, want none", got)
	}
	if got := r.ReadVarInt(); got != 3 {
		t.Errorf("recipe category = %d, want 3 (crafting_misc)", got)
	}
	if got := r.ReadBool(); !got {
		t.Error("recipe has no crafting requirements, want four")
	} else {
		if got := r.ReadVarInt(); got != 4 {
			t.Errorf("crafting requirements = %d, want 4", got)
		}
		for i := range 4 {
			if got := r.ReadVarInt(); got != 0 {
				t.Errorf("requirement %d set type = %d, want 0 (tag)", i, got)
			}
			if got := r.ReadString(32767); got != "minecraft:planks" {
				t.Errorf("requirement %d asks for %q, want minecraft:planks", i, got)
			}
		}
	}
	if got := r.ReadU8(); got != 0x02 {
		t.Errorf("recipe flags = %#02x, want 0x02 (highlighted as new)", got)
	}
	if got := r.ReadBool(); !got {
		t.Error("recipe does not replace the book, want it to")
	}
	if err := r.Err(); err != nil {
		t.Fatalf("recipe book add: %v", err)
	}
	if got := r.Remaining(); got != 0 {
		t.Errorf("recipe book add has %d bytes left over", got)
	}

	// The player is put where the server thinks they are before anyone else
	// is told about them.
	// The harness accepts the teleport as it reads it.
	c.Expect(protocol.IDPlaySyncPosition)

	// What the server calls itself comes with the teleport: the MOTD the
	// server list showed, and no icon, since there is no server-icon.png.
	var wantServerData protocol.Buffer
	wantServerData.PutText(protocol.Text{Text: cfg.MOTD})
	wantServerData.PutBool(false) // no icon
	if data := c.Expect(protocol.IDPlayServerData); !bytes.Equal(data, wantServerData.Bytes()) {
		t.Errorf("server data = % X, want % X", data, wantServerData.Bytes())
	}

	// The player shows up on their own tab list, themselves first.
	info := protocol.NewReader(c.Expect(protocol.IDPlayPlayerInfoUpdate))
	info.ReadU8() // actions
	if got := info.ReadVarInt(); got != 1 {
		t.Errorf("player info entries = %d, want 1", got)
	}
	if got := info.ReadUUID(); got != playerUUID {
		t.Errorf("player info UUID = %s, want %s", got, playerUUID)
	}

	// The client is told what its own player looks like, even though it made
	// that body itself: the skin layers it asked for are what it draws itself
	// with in third person, and it has to hear them back from the server.
	own := c.ExpectMetadata()
	if own.EntityID != entityID {
		t.Errorf("own metadata is for entity %d, want %d", own.EntityID, entityID)
	}
	if own.SkinParts != 0x7F {
		t.Errorf("own skin parts = %#02x, want 0x7F", own.SkinParts)
	}

	// Then the attributes it starts from: the three a vanilla server sends,
	// the armour absent until equipment puts a modifier on.
	c.SeePlayerAttributes = true
	attrID, attrs := servertest.ReadAttributes(t, c.Expect(protocol.IDPlayUpdateAttributes))
	if attrID != entityID {
		t.Errorf("attributes for entity %d, want %d", attrID, entityID)
	}
	want := map[int32]float64{servertest.AttrBlockReach: 4.5, servertest.AttrEntityReach: 3,
		servertest.AttrMovementSpeed: float64(float32(0.1))}
	if len(attrs) != len(want) {
		t.Errorf("join attributes = %+v, want %v", attrs, want)
	}
	for id, base := range want {
		if e, ok := attrs[id]; !ok || e.Base != base || len(e.Modifiers) != 0 {
			t.Errorf("join attribute %d = %+v, want base %v and no modifiers", id, e, base)
		}
	}

	// The clock and the world spawn follow the tab list, with the border
	// between the two, then the game event that starts the wait for chunks, the
	// pace of the world's clock, and the chunk stream.
	r = protocol.NewReader(c.Expect(protocol.IDPlayInitializeBorder))
	if got := r.ReadF64(); got != 0 {
		t.Errorf("border centre X = %v, want 0", got)
	}
	if got := r.ReadF64(); got != 0 {
		t.Errorf("border centre Z = %v, want 0", got)
	}
	if got := r.ReadF64(); got != 59999968 {
		t.Errorf("border old diameter = %v, want 59999968", got)
	}
	if got := r.ReadF64(); got != 59999968 {
		t.Errorf("border new diameter = %v, want 59999968", got)
	}
	if got := r.ReadVarLong(); got != 0 {
		t.Errorf("border speed = %d, want 0 (not moving)", got)
	}
	if got := r.ReadVarInt(); got != 29999984 {
		t.Errorf("border portal boundary = %d, want 29999984", got)
	}
	if got := r.ReadVarInt(); got != 5 {
		t.Errorf("border warning blocks = %d, want 5", got)
	}
	if got := r.ReadVarInt(); got != 300 {
		t.Errorf("border warning time = %d, want 300", got)
	}
	if err := r.Err(); err != nil {
		t.Fatalf("initialize world border: %v", err)
	}
	if got := r.Remaining(); got != 0 {
		t.Errorf("initialize world border has %d bytes left over", got)
	}

	c.Expect(protocol.IDPlayUpdateTime)
	c.Expect(protocol.IDPlaySpawnPosition)
	c.Expect(protocol.IDPlayGameEvent)

	// The world's clock runs at the twenty ticks a second the server does
	// and is never frozen, and the step count it carries with it is zero.
	r = protocol.NewReader(c.Expect(protocol.IDPlayTickingState))
	if got := r.ReadF32(); got != 20 {
		t.Errorf("tick rate = %v, want 20", got)
	}
	if got := r.ReadBool(); got {
		t.Error("the world is frozen, want it running")
	}
	if got := protocol.NewReader(c.Expect(protocol.IDPlayTickingStep)).ReadVarInt(); got != 0 {
		t.Errorf("tick steps = %d, want 0", got)
	}

	// The inventory window and the health go out with the join, ahead of
	// the chunk stream: a client that has begun to see chunks has left its
	// loading screen, so they cannot wait for the world to arrive. A new
	// player carries nothing but a full window is still sent, and a new
	// player is full of health, food and the saturation a spawn grants.
	content := protocol.NewReader(c.Expect(protocol.IDPlayContainerContent))
	if got := content.ReadVarInt(); got != 0 {
		t.Errorf("window ID = %d, want 0 (the player's own inventory)", got)
	}
	content.ReadVarInt() // state ID
	if got := content.ReadVarInt(); got != 46 {
		t.Errorf("inventory slots = %d, want 46", got)
	}
	for slot := range 46 {
		if count := content.ReadVarInt(); count != 0 {
			t.Errorf("slot %d holds %d items, want an empty inventory", slot, count)
		}
	}

	health := protocol.NewReader(c.Expect(protocol.IDPlaySetHealth))
	if got := health.ReadF32(); got != 20 {
		t.Errorf("health = %v, want 20", got)
	}
	if got := health.ReadVarInt(); got != 20 {
		t.Errorf("food = %d, want 20", got)
	}
	if got := health.ReadF32(); got != 5 {
		t.Errorf("saturation = %v, want 5", got)
	}
	if err := health.Err(); err != nil {
		t.Fatalf("set health: %v", err)
	}

	c.Expect(protocol.IDPlayCenterChunk)
	c.Expect(protocol.IDPlayChunkBatchStart)

	// The chunks that follow, which the client asked a render distance of
	// four for, so it got a 9x9 area of them - in batches the client's
	// acknowledgements pace, none bigger than the nine chunks per tick
	// the server opens at.
	var chunks int32
	for packetID, _ := range c.Packets(servertest.JoinTimeout) {
		switch packetID {
		case protocol.IDPlayChunkBatchStart:
			// Each batch of the stream opens with one.
		case protocol.IDPlayChunkBatchFinished:
		case protocol.IDPlayUpdateTime:
			// The loop's own broadcast rides anywhere.
		case protocol.IDPlayChunkData:
			chunks++
		default:
			t.Fatalf("unexpected packet 0x%02X while reading chunks", packetID)
		}
		if chunks == 9*9 {
			break
		}
	}
	if want := int32(9 * 9); chunks != want {
		t.Errorf("received %d chunks, want %d", chunks, want)
	}

	if got := srv.PlayerCount(); got != 1 {
		t.Errorf("player count = %d, want 1", got)
	}
}

// TestJoinSendsInventoryAndHealthAheadOfTheChunks checks that the join's
// container content and health packets go out ahead of the chunk stream —
// the inventory in the join run, the health on the player's first tick,
// both before the first chunk batch. The client leaves its loading screen
// as the chunks begin to arrive, so a join that held them back for the
// first chunk would stand a player in the world with an empty hotbar and
// a full bar of health.
//
// This join's first chunk is held back: the packets must not wait behind
// it.
func TestJoinSendsInventoryAndHealthAheadOfTheChunks(t *testing.T) {
	t.Parallel()
	var releaseChunks func()
	_, c := servertest.Dial(t, servertest.SurvivalTestConfig(), func(s *server.Server) {
		releaseChunks = s.HoldFirstChunk()
	})
	defer releaseChunks()

	c.Login("Testificate")
	c.Configure()
	c.AwaitPlay(protocol.IDPlayGameEvent)
	c.Expect(protocol.IDPlayTickingState)
	c.Expect(protocol.IDPlayTickingStep)

	// With the first chunk still held back, the window and the health
	// arrive ahead of the chunk stream rather than behind its first
	// packet.
	content := protocol.NewReader(c.Expect(protocol.IDPlayContainerContent))
	if got := content.ReadVarInt(); got != 0 {
		t.Errorf("window ID = %d, want 0 (the player's own inventory)", got)
	}
	content.ReadVarInt() // state ID
	if got := content.ReadVarInt(); got != 46 {
		t.Errorf("inventory slots = %d, want 46", got)
	}
	for slot := range 46 {
		if count := content.ReadVarInt(); count != 0 {
			t.Errorf("slot %d holds %d items, want an empty inventory", slot, count)
		}
	}

	health := protocol.NewReader(c.Expect(protocol.IDPlaySetHealth))
	if got := health.ReadF32(); got != 20 {
		t.Errorf("health = %v, want 20", got)
	}
	if got := health.ReadVarInt(); got != 20 {
		t.Errorf("food = %d, want 20", got)
	}
	if got := health.ReadF32(); got != 5 {
		t.Errorf("saturation = %v, want 5", got)
	}

	// The held back chunk is the first of the batch that has begun.
	c.Expect(protocol.IDPlayCenterChunk)
	c.Expect(protocol.IDPlayChunkBatchStart)

	// The ground may arrive now: a nine-by-nine area for the render
	// distance of four the client asked for, in batches the client's
	// acknowledgements pace.
	releaseChunks()
	var chunks int32
	finished := false
	for packetID, _ := range c.Packets(servertest.JoinTimeout) {
		switch packetID {
		case protocol.IDPlayChunkBatchStart:
			// Each batch of the stream opens with one.
		case protocol.IDPlayChunkBatchFinished:
			finished = true
		case protocol.IDPlayChunkData:
			chunks++
		case protocol.IDPlayUpdateTime:
			// The loop's own broadcast rides anywhere.
		default:
			t.Fatalf("unexpected packet 0x%02X while reading chunks", packetID)
		}
		if int(chunks) == 9*9 {
			break
		}
	}
	if !finished {
		t.Fatalf("no batch finished within %s", servertest.JoinTimeout)
	}
	if want := int32(9 * 9); chunks != want {
		t.Errorf("received %d chunks, want %d", chunks, want)
	}
}

// TestLoginRejectsOtherProtocolVersion checks the version mismatch kick: a
// client below protocol 754 is told it is outdated, a newer one is called
// incompatible, and both name this server's version.
func TestLoginRejectsOtherProtocolVersion(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	_, connect := servertest.Start(t, cfg)
	for _, tc := range []struct {
		protocol int32
		want     string
	}{
		{47, `{"translate":"multiplayer.disconnect.outdated_client","with":["` + registry.Version + `"]}`}, // 1.8
		{775, `{"translate":"multiplayer.disconnect.incompatible","with":["` + registry.Version + `"]}`},
	} {
		c := connect()
		c.Send(protocol.IDHandshake, func(b *protocol.Buffer) {
			b.PutVarInt(tc.protocol)
			b.PutString("localhost")
			b.PutI16(25565)
			b.PutVarInt(protocol.NextStateLogin)
		})
		c.Send(protocol.IDLoginStart, func(b *protocol.Buffer) {
			b.PutString("Testificate")
			b.PutUUID(uuid.Nil)
		})

		if got := protocol.NewReader(c.Expect(protocol.IDLoginDisconnect)).ReadString(32767); got != tc.want {
			t.Errorf("protocol %d disconnect = %s, want %s", tc.protocol, got, tc.want)
		}
	}
}

// TestLoginKicksOtherProtocolBeforeLoginStart checks the version kick comes
// with the handshake: no Login Start is sent, and an invalid name does not
// replace it.
func TestLoginKicksOtherProtocolBeforeLoginStart(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	_, connect := servertest.Start(t, cfg)
	for _, name := range []string{"", "bad name!"} {
		c := connect()
		c.Send(protocol.IDHandshake, func(b *protocol.Buffer) {
			b.PutVarInt(47)
			b.PutString("localhost")
			b.PutI16(25565)
			b.PutVarInt(protocol.NextStateLogin)
		})
		if name != "" {
			c.Send(protocol.IDLoginStart, func(b *protocol.Buffer) {
				b.PutString(name)
				b.PutUUID(uuid.Nil)
			})
		}

		want := `{"translate":"multiplayer.disconnect.outdated_client","with":["` + registry.Version + `"]}`
		if got := protocol.NewReader(c.Expect(protocol.IDLoginDisconnect)).ReadString(32767); got != want {
			t.Errorf("name %q: disconnect = %s, want %s", name, got, want)
		}
	}
}

// TestTransferIntentRefusedWhenTransfersOff checks the handshake's Transfer
// intent is refused while accepts-transfers is off, which is the default: the
// client gets the transfers_disabled login Disconnect instead of a login.
func TestTransferIntentRefusedWhenTransfersOff(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	_, c := servertest.Dial(t, cfg)
	c.Handshake(protocol.NextStateTransfer)
	c.Send(protocol.IDLoginStart, func(b *protocol.Buffer) {
		b.PutString("Testificate")
		b.PutUUID(uuid.Nil)
	})

	got := protocol.NewReader(c.Expect(protocol.IDLoginDisconnect)).ReadString(32767)
	want := `{"translate":"multiplayer.disconnect.transfers_disabled"}`
	if got != want {
		t.Errorf("transfer refusal = %s, want %s", got, want)
	}
}

// TestTransferIntentAcceptedWhenTransfersOn checks accepts-transfers lets a
// Transfer intent through the login, the way the Login intent does.
func TestTransferIntentAcceptedWhenTransfersOn(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.AcceptsTransfers = true

	_, c := servertest.Dial(t, cfg)
	playerUUID := c.LoginWithIntent("Testificate", protocol.NextStateTransfer)
	if want := protocol.OfflineUUID("Testificate"); playerUUID != want {
		t.Errorf("offline UUID = %s, want %s", playerUUID, want)
	}
}

// TestTransferredClientGetsNoServerData checks Server Data is left out of the
// join of a client that arrived on the Transfer intent and still sent to one
// that logged in normally.
func TestTransferredClientGetsNoServerData(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		intent int32
		want   bool
	}{
		{"login", protocol.NextStateLogin, true},
		{"transfer", protocol.NextStateTransfer, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := config.Default()
			cfg.OnlineMode = false
			cfg.AcceptsTransfers = true

			_, c := servertest.Dial(t, cfg)
			c.LoginWithIntent("Testificate", tc.intent)
			c.Configure()

			got := false
			c.ReadJoin(func(packetID int32, _ []byte) {
				if packetID == protocol.IDPlayServerData {
					got = true
				}
			})
			if got != tc.want {
				t.Errorf("server data sent = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestConfigurationSendsTheBugReportLink checks what bug-report-link names:
// with it set, the client receives one Server Links packet during
// configuration — a single entry of the built-in bug_report kind with the
// URL — ahead of Finish Configuration, right behind the brand where a
// vanilla server puts it.
func TestConfigurationSendsTheBugReportLink(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.BugReportLink = "https://example.org/x"

	_, c := servertest.Dial(t, cfg)
	c.Login("Testificate")

	// Brand, links, then the Known Packs request.
	c.Expect(protocol.IDConfigPluginMessage)
	data := c.Expect(protocol.IDConfigServerLinks)
	c.Expect(protocol.IDKnownPacks)

	r := protocol.NewReader(data)
	if count := r.ReadVarInt(); count != 1 {
		t.Fatalf("server links count = %d, want 1", count)
	}
	if !r.ReadBool() {
		t.Error("link label is the custom kind, want the built-in enum")
	}
	if kind := r.ReadVarInt(); kind != protocol.ServerLinkBugReport {
		t.Errorf("link kind = %d, want the bug_report %d", kind, protocol.ServerLinkBugReport)
	}
	if url := r.ReadString(32767); url != "https://example.org/x" {
		t.Errorf("link URL = %q, want %q", url, "https://example.org/x")
	}
	if r.Remaining() != 0 || r.Err() != nil {
		t.Errorf("server links leftover=%d err=%v", r.Remaining(), r.Err())
	}

	// The phase carries on to Finish Configuration, with no second links
	// packet in it.
	c.Send(protocol.IDKnownPacksIn, func(b *protocol.Buffer) {
		b.PutVarInt(1)
		b.PutString("minecraft")
		b.PutString("core")
		b.PutString(registry.Version)
	})
	finished := false
	for packetID := range c.Packets(servertest.ExpectTimeout) {
		switch packetID {
		case protocol.IDFeatureFlags, protocol.IDRegistryData, protocol.IDConfigUpdateTags:
		case protocol.IDConfigServerLinks:
			t.Error("a second Server Links packet arrived")
		case protocol.IDConfigFinish:
			c.Send(protocol.IDConfigFinishAck, nil)
			finished = true
		default:
			t.Fatalf("unexpected configuration packet 0x%02X", packetID)
		}
		if finished {
			return
		}
	}
	t.Fatal("configuration did not finish")
}

// TestNoServerLinksWithoutTheBugReportLink checks the default: with
// bug-report-link unset, no Server Links packet is sent. The whole phase is
// read by configure, which fails the test on any packet the phase's own
// packets do not include.
func TestNoServerLinksWithoutTheBugReportLink(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	_, c := servertest.Dial(t, cfg)
	c.Login("Testificate")
	c.Configure()
}

func TestStatusPingWithoutRequest(t *testing.T) {
	t.Parallel()
	_, c := servertest.Dial(t, config.Default())
	c.Handshake(protocol.NextStateStatus)
	c.Send(protocol.IDStatusPing, func(b *protocol.Buffer) { b.PutI64(987654321) })
	pong := protocol.NewReader(c.Expect(protocol.IDStatusPong))
	if got := pong.ReadI64(); got != 987654321 {
		t.Errorf("pong = %d, want 987654321", got)
	}
}

func TestStatusSecondRequestCloses(t *testing.T) {
	t.Parallel()
	_, c := servertest.Dial(t, config.Default())
	c.Handshake(protocol.NextStateStatus)
	c.Send(protocol.IDStatusRequest, nil)
	c.Expect(protocol.IDStatusResponse)
	c.Send(protocol.IDStatusRequest, nil)
	if id, _, err := c.Read(); err == nil {
		t.Fatalf("got packet 0x%02X after a second status request, want the connection closed", id)
	}
}

func TestStatusEnforcesSecureChat(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name            string
		online, enforce bool
		want            bool
	}{
		{"default", true, true, true},
		{"property off", true, false, false},
		{"offline mode", false, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := config.Default()
			cfg.OnlineMode = tc.online
			cfg.EnforceSecureProfile = tc.enforce
			_, c := servertest.Dial(t, cfg)
			c.Handshake(protocol.NextStateStatus)
			c.Send(protocol.IDStatusRequest, nil)
			r := protocol.NewReader(c.Expect(protocol.IDStatusResponse))
			var raw map[string]any
			if err := json.Unmarshal([]byte(r.ReadString(32767)), &raw); err != nil {
				t.Fatalf("unmarshal status: %v", err)
			}
			if got, ok := raw["enforcesSecureChat"].(bool); !ok || got != tc.want {
				t.Errorf("enforcesSecureChat = %v, want %v", raw["enforcesSecureChat"], tc.want)
			}
		})
	}
}

// TestLoginStallGetsSlowLoginDisconnect checks a client that sends Login Start
// and then stalls is disconnected with slow_login once the login deadline
// passes.
func TestLoginStallGetsSlowLoginDisconnect(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false
	cfg.NetworkCompressionThreshold = -1

	_, connect := servertest.Start(t, cfg, func(s *server.Server) { s.SetLoginTimeout(300 * time.Millisecond) })
	c := connect()
	c.Send(protocol.IDHandshake, func(b *protocol.Buffer) {
		b.PutVarInt(protocol.Version)
		b.PutString("localhost")
		b.PutI16(25565)
		b.PutVarInt(protocol.NextStateLogin)
	})
	c.Send(protocol.IDLoginStart, func(b *protocol.Buffer) {
		b.PutString("Testificate")
		b.PutUUID(uuid.Nil)
	})
	c.Expect(protocol.IDLoginSuccess)

	want := `{"translate":"multiplayer.disconnect.slow_login"}`
	if got := protocol.NewReader(c.Expect(protocol.IDLoginDisconnect)).ReadString(32767); got != want {
		t.Errorf("disconnect = %s, want %s", got, want)
	}
}

// TestLoginAcceptsAnyPrintableAsciiName checks names outside [A-Za-z0-9_]
// log in, and names with a space, a control or non-ASCII character
// are refused.
func TestLoginAcceptsAnyPrintableAsciiName(t *testing.T) {
	t.Parallel()
	cfg := config.Default()
	cfg.OnlineMode = false

	_, connect := servertest.Start(t, cfg)
	for _, name := range []string{"Dot.Name", ".Floodgate", "Steve-1"} {
		connect().Login(name)
	}
	for _, name := range []string{"bad name", "café", "tab\t"} {
		c := connect()
		c.Handshake(protocol.NextStateLogin)
		c.Send(protocol.IDLoginStart, func(b *protocol.Buffer) {
			b.PutString(name)
			b.PutUUID(uuid.Nil)
		})
		got := protocol.NewReader(c.Expect(protocol.IDLoginDisconnect)).ReadString(32767)
		if !strings.Contains(got, "Invalid username.") {
			t.Errorf("name %q: disconnect = %s, want Invalid username.", name, got)
		}
	}
}
