package servertest

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"hash/fnv"
	"maps"
	"math/bits"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// CropsSeed is the seed the farm tests pin the dice with.
const CropsSeed = int64(21)

// CropsTickSpeed is the random tick rule the farm tests run at: fifty dice
// a section a pass, against the default three, so the pass counts below are
// tens of hits on the one cell a test watches rather than single ones.
const CropsTickSpeed = 50

// SetFarmBlock lays a block with /setblock carrying the property overrides
// given, the way an operator sets a state exactly, waits for the update
// that says it landed, and has the column's light settled before the test
// moves on.
func SetFarmBlock(t *testing.T, c *Client, srv *server.Server, x, y, z int32, name string, overrides map[string]string) uint16 {
	t.Helper()
	state, ok := registry.FindState(name, overrides)
	if !ok {
		t.Fatalf("the registry has no %s with %v", name, overrides)
	}
	var word strings.Builder
	word.WriteString(strings.TrimPrefix(name, "minecraft:"))
	if len(overrides) > 0 {
		word.WriteString("[")
		first := true
		for k, v := range overrides {
			if !first {
				word.WriteString(",")
			}
			word.WriteString(k + "=" + v)
			first = false
		}
		word.WriteString("]")
	}
	c.ChatCommand("setblock " + strconv.FormatInt(int64(x), 10) + " " +
		strconv.FormatInt(int64(y), 10) + " " + strconv.FormatInt(int64(z), 10) + " " + word.String())
	AwaitBlockUpdate(t, c, x, y, z, state)
	// The update goes out before the placement books its neighbours' visits;
	// only the feedback, sent last, says a following TickPhysics will find
	// them.
	AwaitCommandFeedback(t, c, "commands.setblock.success",
		strconv.FormatInt(int64(x), 10), strconv.FormatInt(int64(y), 10), strconv.FormatInt(int64(z), 10))
	srv.RelightAt(x, z)
	return state
}

// CropsProps reads the properties of a state the test means to judge,
// failing it when the state belongs to another block.
func CropsProps(t *testing.T, state uint16, name string) map[string]string {
	t.Helper()
	got, _, ok := registry.BlockStateName(state)
	if !ok || got != name {
		t.Fatalf("the block state %d is not %s", state, name)
	}
	return maps.Clone(registry.StateProps(state))
}

// CropsStateOf reads the state the registry carries for a block with the
// overrides given, failing the test when there is none.
func CropsStateOf(t *testing.T, name string, overrides map[string]string) uint16 {
	t.Helper()
	state, ok := registry.FindState(name, overrides)
	if !ok {
		t.Fatalf("the registry has no %s with %v", name, overrides)
	}
	return state
}

// GiveFarmItem puts an item in the player's hand by name.
func GiveFarmItem(t *testing.T, c *Client, name string) {
	t.Helper()
	id, ok := registry.ItemID(name)
	if !ok {
		t.Fatalf("the item registry has no %s", name)
	}
	c.SetCreativeSlot(player.HotbarSlot, protocol.ItemStack{ItemID: id, Count: 1})
}

// CropsNameOf reads the block a state belongs to, failing the test when the
// state is one the registry does not name.
func CropsNameOf(t *testing.T, state uint16) string {
	t.Helper()
	name, _, ok := registry.BlockStateName(state)
	if !ok {
		t.Fatalf("the block state %d names no block", state)
	}
	return name
}

// AwaitCropBreak waits out the packets one break settles into and returns
// both what it left on the wire — the air the block became, read at its
// position — and the stacks its loot laid down. One stream is read for
// both: the spawn and the metadata that carry a stack follow the block
// update, and a reader looking for one alone would eat the other. The break
// itself is already driven — the dig or the tick stepped before the call —
// so the wait is only the transport's, at the harness's standard read
// budget.
func AwaitCropBreak(t *testing.T, c *Client, x, y, z int32, want uint16) []protocol.ItemStack {
	t.Helper()
	var drops []protocol.ItemStack
	pending := int32(0) // the item entity whose stack is still awaited
	collect := func(gotID int32, data []byte) {
		switch gotID {
		case protocol.IDPlaySpawnEntity:
			if drop := ParseItemSpawn(data); drop.EntityID != 0 {
				pending = drop.EntityID
			}
		case protocol.IDPlayEntityMetadata:
			r := protocol.NewReader(data)
			if id := r.ReadVarInt(); id != pending {
				return // another body's metadata; the item's is still to come
			}
			if index := r.ReadU8(); index != protocol.MetadataIndexItemSlot {
				return
			}
			if r.ReadVarInt() != protocol.MetadataSlot {
				return
			}
			if stack, ok := r.ReadSlot(); ok {
				pending = 0
				drops = append(drops, stack)
			}
		}
	}
	for gotID, data := range c.Packets(ExpectTimeout) {
		t.Helper()
		for _, ch := range BlockChangesIn(t, gotID, data) {
			if ch.X == x && ch.Y == y && ch.Z == z {
				if ch.State != want {
					t.Fatalf("the block at (%d, %d, %d) = %d, want %d", x, y, z, ch.State, want)
				}
				// The drops spawn after the cell is written, so they follow it.
				c.SettleAndCollect(collect)
				return drops
			}
		}
		collect(gotID, data)
	}
	t.Fatalf("no block update for (%d, %d, %d) arrived", x, y, z)
	return nil
}

// AwaitBlockEventually reads packets until a Block Update for the position
// given carries the state wanted, stepping over the updates at that
// position that carry an earlier state of a step-by-step change — a
// farmland's drying passes through every moisture on its way to dirt, and
// each of those is its own update on the wire.
func AwaitBlockEventually(t *testing.T, c *Client, x, y, z int32, want uint16) {
	t.Helper()
	for packetID, data := range c.Packets(10 * time.Second) {
		if _, ok := c.takeCell(packetID, data, func(ch BlockChange) bool {
			return ch.X == x && ch.Y == y && ch.Z == z && ch.State == want
		}); ok {
			return
		}
	}
	t.Fatalf("no block update for (%d, %d, %d) arrived", x, y, z)
}

// AwaitBlockState reads packets until a Block Update for the position given
// arrives, and reports the state it carried, whatever that is — for a
// change whose exact outcome the test judges after the fact rather than
// predicts.
func AwaitBlockState(t *testing.T, c *Client, x, y, z int32) uint16 {
	t.Helper()
	for packetID, data := range c.Packets(10 * time.Second) {
		if ch, ok := c.takeCell(packetID, data, func(ch BlockChange) bool {
			return ch.X == x && ch.Y == y && ch.Z == z
		}); ok {
			return ch.State
		}
	}
	t.Fatalf("no block update for (%d, %d, %d) arrived", x, y, z)
	return 0
}

// StartOreWorld stands a server on the overworld of seed 12345, the seed
// these tests' wants are measured against, with optional configuration hooks.
func StartOreWorld(t *testing.T, configure ...func(*config.Config)) *server.Server {
	t.Helper()

	cfg := config.Default()
	cfg.OnlineMode = false
	// The unprefixed "normal" rather than "minecraft:normal": the harness
	// swaps the default spelling of it for its flat test ground.
	cfg.LevelType = "normal"
	cfg.LevelSeed = "12345"
	for _, setup := range configure {
		setup(cfg)
	}

	srv, _ := Start(t, cfg)
	return srv
}

// SaplingSeed is the seed the sapling test pins the dice with.
const SaplingSeed = int64(21)

// RootSeed derives the root seed of a test server's random sources from the
// test's name, so its dice fall the same every run, and pins cfg's level
// seed to it where the test left that unset. The flat test world is left
// unpinned: it keeps a level seed, and without one stays at seed 0.
func RootSeed(t *testing.T, cfg *config.Config) int64 {
	seed := nameSeed(t)
	if cfg.LevelSeed == "" && cfg.LevelType != server.TestFlatLevelType {
		cfg.LevelSeed = strconv.FormatInt(seed, 10)
	}
	return seed
}

// nameSeed is the seed a test's name hashes to.
func nameSeed(t *testing.T) int64 {
	h := fnv.New64a()
	h.Write([]byte(t.Name()))
	return int64(h.Sum64())
}

// VanillaSaves is a setup for a test of what reaches disk: it turns the
// harness's drop of untouched chunks back off, so a generated chunk is saved
// once as a real server saves it.
func VanillaSaves(s *server.Server) { s.SetDropUntouchedChunks(false) }

// CrossingTimeout bounds the wait for one crossing's Respawn and arrival.
const CrossingTimeout = 20 * time.Second

// ContainerSlots decodes a Set Container Content packet into the slots it
// carries.
func ContainerSlots(t *testing.T, data []byte) []protocol.ItemStack {
	t.Helper()
	r := protocol.NewReader(data)
	r.ReadVarInt() // Window ID
	r.ReadVarInt() // State ID
	slots := make([]protocol.ItemStack, 0, r.ReadVarInt())
	for cap(slots) > len(slots) {
		stack, ok := r.ReadSlot()
		if !ok {
			t.Fatalf("container content slot %d did not read back", len(slots))
		}
		slots = append(slots, stack)
	}
	r.ReadSlot() // the carried slot
	if err := r.Err(); err != nil {
		t.Fatalf("set container content: %v", err)
	}
	if r.Remaining() != 0 {
		t.Errorf("set container content has %d bytes left over", r.Remaining())
	}
	return slots
}

// TheThingsGiven is what the inventory-across-dimensions test gives its
// player, keyed by the slot of the player's own window each rides in: one
// thing in the hotbar, one in the main inventory, a chestplate worn and one
// in the off hand.
func TheThingsGiven(t *testing.T) map[int]protocol.ItemStack {
	t.Helper()
	return map[int]protocol.ItemStack{
		player.HotbarSlot:    {ItemID: MustItem("minecraft:diamond"), Count: 5},
		player.MainSlot:      {ItemID: MustItem("minecraft:bread"), Count: 2},
		player.ArmorSlot + 1: {ItemID: MustItem("minecraft:iron_chestplate"), Count: 1},
		player.OffHandSlot:   {ItemID: MustItem("minecraft:shield"), Count: 1},
	}
}

// CheckTheThingsGiven fails the test for any slot of a crossing's window
// packet that does not hold what was given, or holds something where
// nothing was.
func CheckTheThingsGiven(t *testing.T, slots []protocol.ItemStack, given map[int]protocol.ItemStack) {
	t.Helper()
	if len(slots) != 46 {
		t.Fatalf("the crossing's window carried %d slots, want 46", len(slots))
	}
	for slot, want := range given {
		got := slots[slot]
		if got.ItemID != want.ItemID || got.Count != want.Count {
			t.Errorf("slot %d after the crossing holds item %d x%d, want item %d x%d",
				slot, got.ItemID, got.Count, want.ItemID, want.Count)
		}
	}
	for slot, got := range slots {
		if _, carried := given[slot]; !carried && !got.Empty() {
			t.Errorf("slot %d after the crossing holds item %d x%d, want it empty", slot, got.ItemID, got.Count)
		}
	}
}

// SyncPositionFlags reads the flags of a Synchronize Player Position packet's
// data, the bit field behind the rotation values.
func SyncPositionFlags(data []byte) int32 {
	r := protocol.NewReader(data)
	r.ReadVarInt() // teleport ID
	r.ReadF64()    // X, y, z
	r.ReadF64()
	r.ReadF64()
	r.ReadF64() // velocity
	r.ReadF64()
	r.ReadF64()
	r.ReadF32() // Yaw
	r.ReadF32() // Pitch
	return r.ReadI32()
}

// EnvironmentConfig is the configuration the environment tests run in:
// offline, survival unless a test says otherwise, the flat world the rest
// of the survival tests share.
func EnvironmentConfig() *config.Config {
	cfg := config.Default()
	cfg.OnlineMode = false
	return cfg
}

// StopTicking stops the main loop and returns the way to drive its survival
// pass by hand: one call, one tick. Everything else the loop would do - the
// fluids, the mobs, the keep-alives - stays still while it is stopped, which
// is what keeps the blocks a test has placed where it placed them.
func StopTicking(t *testing.T, srv *server.Server) func(int) {
	t.Helper()
	srv.StopTick()
	return func(n int) {
		t.Helper()
		for range n {
			srv.TickSurvival()
		}
	}
}

// BurningMetadataView is what a Set Entity Metadata packet says about a
// body's burn and its breath, the two entries the environment changes: the
// shared flags with the on-fire bit among them, and the air when the packet
// carried it.
type BurningMetadataView struct {
	EntityID int32
	Flags    uint8
	HasAir   bool
	Air      int32
}

// ReadBurningMetadata parses one Set Entity Metadata packet for the entries
// the environment changes, stepping over the ones it does not: the pose,
// the skin layers, the hand state.
func ReadBurningMetadata(t *testing.T, data []byte) BurningMetadataView {
	t.Helper()
	r := protocol.NewReader(data)
	m := BurningMetadataView{EntityID: r.ReadVarInt()}
	for r.Err() == nil {
		index := r.ReadU8()
		if index == protocol.MetadataEnd {
			break
		}
		fieldType := r.ReadVarInt()
		switch index {
		case protocol.MetadataIndexEntityFlags:
			m.Flags = r.ReadU8()
		case protocol.MetadataIndexAir:
			m.HasAir = true
			m.Air = r.ReadVarInt()
		case protocol.MetadataIndexPose, protocol.MetadataIndexSkinParts,
			protocol.MetadataIndexHandState, protocol.MetadataIndexMainHand:
			r.ReadVarInt()
		default:
			t.Fatalf("unexpected metadata field %d of type %d", index, fieldType)
		}
	}
	if err := r.Err(); err != nil {
		t.Fatalf("entity metadata: %v", err)
	}
	return m
}

// DrainDamage counts the Damage Events the hand-driven ticks sent, the way a
// test proves a run of ticks hurt nobody.
func DrainDamage(c *Client) int {
	c.T.Helper()
	count := 0
	c.SettleAndCollect(func(id int32, data []byte) {
		if id == protocol.IDPlayDamageEvent {
			count++
		}
	})
	return count
}

// BlockState is the default state ID of the named block, which is what a
// test places with. Blocks that carry no item form — water, lava, fire —
// are not in the placeable table, so the lookup falls to every state the
// block has, the default first. A name the registry does not know fails the
// test, since a test that placed nothing would prove nothing.
func BlockState(t *testing.T, name string) uint16 {
	t.Helper()
	if state, ok := registry.BlockStates[name]; ok {
		return state
	}
	states, ok := registry.AllStates[name]
	if !ok || len(states) == 0 {
		t.Fatalf("no block state for %s", name)
	}
	return states[0].ID
}

// BlockStateWith is the state ID of the named block carrying exactly the
// properties given, which is what a test places when the default state
// would not do: a berry bush past its first age is the only kind that
// pricks.
func BlockStateWith(t *testing.T, name, props string) uint16 {
	t.Helper()
	for _, state := range registry.AllStates[name] {
		if state.Props == props {
			return state.ID
		}
	}
	t.Fatalf("no %s state with %s", name, props)
	return 0
}

// ChunkAt reads through the join and returns the Chunk Data packet for the
// chunk at (cx, cz).
func ChunkAt(t *testing.T, c *Client, cx, cz int32) []byte {
	t.Helper()
	var found []byte
	c.ReadJoin(func(id int32, data []byte) {
		if id != protocol.IDPlayChunkData || found != nil {
			return
		}
		r := protocol.NewReader(data)
		if r.ReadI32() == cx && r.ReadI32() == cz {
			found = data
		}
	})
	if found == nil {
		t.Fatalf("no chunk data for (%d, %d) in the join", cx, cz)
	}
	return found
}

// ParseHeightmaps reads the prefixed heightmap array at the head of a chunk
// packet and returns each type's 256 column values, leaving r after them.
func ParseHeightmaps(t *testing.T, packet []byte) (map[int32][]int32, *protocol.Reader) {
	t.Helper()
	r := protocol.NewReader(packet)
	r.ReadI32() // chunk x
	r.ReadI32() // chunk z
	count := r.ReadVarInt()
	if count != 3 {
		t.Fatalf("heightmap count = %d, want 3", count)
	}
	bitsPerEntry := bits.Len(uint(384)) // the overworld's 384 blocks
	perLong := 64 / bitsPerEntry
	maps := make(map[int32][]int32, 3)
	for range count {
		typ := r.ReadVarInt()
		longs := r.ReadVarInt()
		if want := int32((256 + perLong - 1) / perLong); longs != want {
			t.Errorf("heightmap %d long count = %d, want %d", typ, longs, want)
		}
		packed := make([]uint64, longs)
		for i := range packed {
			packed[i] = uint64(r.ReadI64())
		}
		if err := r.Err(); err != nil {
			t.Fatalf("heightmap %d: %v", typ, err)
		}
		mask := uint64(1)<<bitsPerEntry - 1
		values := make([]int32, 256)
		for i := range values {
			values[i] = int32(packed[i/perLong] >> ((i % perLong) * bitsPerEntry) & mask)
		}
		maps[typ] = values
	}
	return maps, r
}

// ReadTestBitSet reads a BitSet field and returns its low 64 bits.
func ReadTestBitSet(t *testing.T, r *protocol.Reader) uint64 {
	t.Helper()
	v := r.ReadBitSet()
	if err := r.Err(); err != nil {
		t.Fatalf("BitSet: %v", err)
	}
	return v
}

// WaitForBlock runs ticks until the block at the position satisfies ok,
// failing once a few seconds' worth have passed. Physics runs on the main loop, so a
// change it makes lands some ticks after whatever caused it.
func WaitForBlock(t *testing.T, srv *server.Server, x, y, z int32, ok func(uint16) bool) uint16 {
	t.Helper()
	UntilTicks(t, srv, 100, fmt.Sprintf("block at (%d, %d, %d)", x, y, z),
		func() bool { return ok(srv.BlockAt(x, y, z)) })
	return srv.BlockAt(x, y, z)
}

// The frame a portal is lit in, in the flat world every test server runs on:
// obsidian around a 2x3 interior running along X, standing a block out of the
// spawn. The tests place the obsidian directly — the placement code has its
// own tests — and light the frame the way a player would.
const (
	PortalFloorY = 64 // the first air above the flat world's stone

	PortalInnerX0 = 4 // the interior spans x 4..5, y 64..66, z 4
	PortalInnerX1 = 5
	PortalInnerZ  = 4
)

// PortalStates resolves the blocks the portal tests speak of.
func PortalStates(t *testing.T) (obsidian, portalX, portalZ, bedrock uint16) {
	t.Helper()
	obsidian, ok := registry.FindState("minecraft:obsidian", nil)
	if !ok {
		t.Fatal("the block registry has no minecraft:obsidian")
	}
	portalX, ok = registry.FindState("minecraft:nether_portal", map[string]string{"axis": "x"})
	if !ok {
		t.Fatal("the block registry has no minecraft:nether_portal")
	}
	portalZ, ok = registry.FindState("minecraft:nether_portal", map[string]string{"axis": "z"})
	if !ok {
		t.Fatal("the block registry has no minecraft:nether_portal with axis=z")
	}
	bedrock, ok = registry.FindState("minecraft:bedrock", nil)
	if !ok {
		t.Fatal("the block registry has no minecraft:bedrock")
	}
	return obsidian, portalX, portalZ, bedrock
}

// BuildFrame places the obsidian of a 2x3 frame without broadcasting, the
// way a test builds anything: directly into the world, chunk already loaded
// by the players it stands in front of.
func BuildFrame(t *testing.T, srv *server.Server) {
	t.Helper()
	obsidian, _, _, _ := PortalStates(t)
	frame := [][3]int32{
		// The floor and the ceiling of the interior.
		{PortalInnerX0, PortalFloorY - 1, PortalInnerZ},
		{PortalInnerX1, PortalFloorY - 1, PortalInnerZ},
		{PortalInnerX0, PortalFloorY + 3, PortalInnerZ},
		{PortalInnerX1, PortalFloorY + 3, PortalInnerZ},
		// The two sides, floor to ceiling.
		{PortalInnerX0 - 1, PortalFloorY, PortalInnerZ},
		{PortalInnerX0 - 1, PortalFloorY + 1, PortalInnerZ},
		{PortalInnerX0 - 1, PortalFloorY + 2, PortalInnerZ},
		{PortalInnerX1 + 1, PortalFloorY, PortalInnerZ},
		{PortalInnerX1 + 1, PortalFloorY + 1, PortalInnerZ},
		{PortalInnerX1 + 1, PortalFloorY + 2, PortalInnerZ},
	}
	for _, cell := range frame {
		if !srv.SetBlock(cell[0], cell[1], cell[2], obsidian) {
			t.Fatalf("placing the frame's obsidian at (%d, %d, %d) failed", cell[0], cell[1], cell[2])
		}
	}
}

// FrameInterior lists the cells a valid 2x3 interior takes, so a test can
// assert one of them.
func FrameInterior() [][3]int32 {
	var cells [][3]int32
	for x := PortalInnerX0; x <= PortalInnerX1; x++ {
		for y := PortalFloorY; y <= PortalFloorY+2; y++ {
			cells = append(cells, [3]int32{int32(x), int32(y), PortalInnerZ})
		}
	}
	return cells
}

// Flint sets the client's main hand to a flint and steel, the way a creative
// client decides what it holds.
func Flint(t *testing.T, c *Client) {
	t.Helper()
	id, ok := registry.ItemID("minecraft:flint_and_steel")
	if !ok {
		t.Fatal("the item registry has no minecraft:flint_and_steel")
	}
	c.SetCreativeSlot(36, protocol.ItemStack{ItemID: id, Count: 1})
}

// DropHeld sends what the drop key sends: one item of the held stack thrown
// out in front of the player.
func DropHeld(c *Client, x, y, z int32) {
	c.Send(protocol.IDPlayPlayerAction, func(b *protocol.Buffer) {
		b.PutVarInt(protocol.PlayerActionDropItem)
		b.PutPosition(x, y, z)
		b.PutI8(0)     // face, which the server does not read here
		b.PutVarInt(1) // sequence
	})
}

// EnterTheEndByItsPortal takes a freshly joined player into the end the way a
// placed end portal does, and returns standing on the arrival platform, the
// cell at their feet free for the end's own exit portal.
func EnterTheEndByItsPortal(t *testing.T, srv *server.Server, c *Client, name string) world.SpawnPos {
	t.Helper()
	endPortal := srv.EndPortalState()
	arrival := world.EndArrival()
	obsidian, _, _, _ := PortalStates(t)

	// The end portal in the overworld, a few steps out for the fall into
	// it. The crossing: a Respawn naming the end, and the stream of it
	// read through the way a client does.
	if srv.LoadBlockInDimension(world.DimensionName, 10, PortalFloorY-1, 10) != endPortal {
		if !srv.SetBlock(10, PortalFloorY-1, 10, endPortal) {
			t.Fatal("placing the end portal in the overworld failed")
		}
	}
	c.MoveTo(10.5, PortalFloorY-1, 10.5, 0, 0)
	data, ok := ReadUntil(t, c, ExpectTimeout, protocol.IDPlayRespawn)
	if !ok {
		t.Fatal("no respawn arrived for standing in the end portal")
	}
	r := protocol.NewReader(data)
	r.ReadVarInt() // dimension type
	if got := r.ReadString(32767); got != world.EndName {
		t.Fatalf("respawn dimension = %q, want %q", got, world.EndName)
	}
	if _, ok := ReadUntilViewSent(t, c); !ok {
		t.Fatal("the end never arrived around the crossing")
	}
	WaitFor(t, srv, 200, func() bool {
		return srv.PlayerDimension(name) == world.EndName
	})
	// The platform, whose obsidian goes under the feet.
	WaitFor(t, srv, 200, func() bool {
		return srv.LoadBlockInDimension(world.EndName, arrival.X, arrival.Y-1, arrival.Z) == obsidian
	})
	return arrival
}

// ExpectEndCredits waits up to 400 ticks for the end's first-exit credits.
func ExpectEndCredits(t *testing.T, c *Client, srv *server.Server) {
	t.Helper()
	ExpectEndCreditsWithin(t, c, srv, 400)
}

// ExpectEndCreditsWithin checks the win-game event and value within n game ticks.
// vanilla: ServerPlayer.showEndCredits
func ExpectEndCreditsWithin(t *testing.T, c *Client, srv *server.Server, n int) {
	t.Helper()
	var data []byte
	if !c.ReadTicks(srv, n, func(id int32, p []byte) bool {
		if id != protocol.IDPlayGameEvent || protocol.NewReader(p).ReadU8() != protocol.GameEventWinGame {
			return false
		}
		data = p
		return true
	}) {
		t.Fatal("no win-game event arrived for the first exit from the end, want the credits")
	}
	r := protocol.NewReader(data)
	if got := r.ReadU8(); got != protocol.GameEventWinGame {
		t.Fatalf("game event = %d, want %d (win game)", got, protocol.GameEventWinGame)
	}
	if got := r.ReadF32(); got != 0 {
		t.Errorf("win game value = %v, want 0, the value the reference server sends", got)
	}
}

// PortalFrame fills the tests' 2x3 frame with portal blocks directly, so a
// survival player the flint's reach would not cover can still stand in a lit
// portal.
func PortalFrame(t *testing.T, srv *server.Server) {
	t.Helper()
	_, portalX, _, _ := PortalStates(t)
	BuildFrame(t, srv)
	for _, cell := range FrameInterior() {
		if !srv.SetBlock(cell[0], cell[1], cell[2], portalX) {
			t.Fatalf("placing a portal block at (%d, %d, %d) failed", cell[0], cell[1], cell[2])
		}
	}
}

// EntityRegionPath is the entity region file holding a chunk.
func EntityRegionPath(level string, pos world.ChunkPos) string {
	rx, rz := strconv.Itoa(int(pos.X>>5)), strconv.Itoa(int(pos.Z>>5))
	return filepath.Join(level, "dimensions", "minecraft", "overworld", "entities", "r."+rx+"."+rz+".mca")
}

// EditChunkRecord hands the region record of a chunk (its length, type byte
// and payload) to edit, and writes back what edit returns at the end of the
// file, unpadded: the shape of a file vanilla wrote that chunk to last and
// has not closed yet. It returns the header entry and the record written.
func EditChunkRecord(t *testing.T, path string, pos world.ChunkPos, edit func([]byte) []byte) (int, []byte) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	entry := (int(pos.X&31) + int(pos.Z&31)*32) * 4
	start := int(binary.BigEndian.Uint32(raw[entry:])>>8) * 4096
	if start == 0 {
		t.Fatalf("%s holds no chunk (%d,%d)", path, pos.X, pos.Z)
	}
	length := int(binary.BigEndian.Uint32(raw[start:]))
	record := edit(bytes.Clone(raw[start : start+4+length]))
	sector := (len(raw) + 4095) / 4096
	raw = append(raw, make([]byte, sector*4096-len(raw))...)
	raw = append(raw, record...)
	binary.BigEndian.PutUint32(raw[entry:], uint32(sector<<8|(len(record)+4095)/4096))
	if err := os.WriteFile(path, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	return entry, record
}

// WeatherEvent is one Game Event packet, with the event it names and the
// value it carries.
type WeatherEvent struct {
	Event uint8
	Value float32
}

// DrainWeather reads n ticks of packets, gathering every weather event that
// passed.
func DrainWeather(c *Client, srv *server.Server, n int) []WeatherEvent {
	c.T.Helper()
	var events []WeatherEvent
	c.ReadTicks(srv, n, func(packetID int32, data []byte) bool {
		if packetID == protocol.IDPlayGameEvent {
			r := protocol.NewReader(data)
			e := WeatherEvent{Event: r.ReadU8(), Value: r.ReadF32()}
			if e.Event != protocol.GameEventStartWaitingForChunks {
				events = append(events, e)
			}
		}
		return false
	})
	return events
}

// FluidStates returns the state IDs of every state of a fluid block, so a
// test can tell a generated source (the default first state) from a flowing
// state no generation may write.
func FluidStates(t *testing.T, name string) map[uint16]bool {
	t.Helper()
	states, ok := registry.AllStates[name]
	if !ok {
		t.Fatalf("the block registry has no %s", name)
	}
	m := make(map[uint16]bool, len(states))
	for _, s := range states {
		m[s.ID] = true
	}
	return m
}

// MustDefaultState resolves a fluid's default state, which every generated
// cell has to carry.
func MustDefaultState(t *testing.T, name string) uint16 {
	t.Helper()
	state, ok := registry.FindState(name, nil)
	if !ok {
		t.Fatalf("the block registry has no %s", name)
	}
	return state
}

// NothingIn reports whether every slot the block entity read back is empty,
// the shape InventoryAt gives a container that holds nothing.
func NothingIn(slots []protocol.ItemStack) bool {
	for _, s := range slots {
		if !s.Empty() {
			return false
		}
	}
	return true
}

// AutoSaveEvery is the auto-save interval, in ticks, the auto-save tests run on.
const AutoSaveEvery = 4

// AutoSaveEveryTick sets the auto-save to run every AutoSaveEvery ticks,
// counted from the tick the loop was held at.
func AutoSaveEveryTick(srv *server.Server) {
	srv.SetAutoSave(AutoSaveEvery * time.Second / 20)
}

// DefaultKeepAliveTimeout is the timeout a test server serves a client with
// unless it says otherwise; the tests that keep it push only the interval
// out of their way. It outlasts ExpectTimeout: a test waiting out a slow
// read on one client is not reading the others' keep-alives, and a loaded
// machine must not have the server drop them for it.
const DefaultKeepAliveTimeout = 2 * ExpectTimeout

// LoginTail is the tail of a Login (play) packet: its Superflat flag, whether
// a death location follows and its sea level.
type LoginTail struct {
	Superflat bool
	HasDeath  bool
	SeaLevel  int32
}

// ReadLoginTail parses a Login (play) packet down to its tail.
func ReadLoginTail(t *testing.T, data []byte) LoginTail {
	t.Helper()
	r := protocol.NewReader(data)
	r.ReadI32()  // entity ID
	r.ReadBool() // hardcore
	for range r.ReadVarInt() {
		r.ReadString(32767) // dimension name
	}
	r.ReadVarInt()      // max players
	r.ReadVarInt()      // view distance
	r.ReadVarInt()      // simulation distance
	r.ReadBool()        // reduced debug screen
	r.ReadBool()        // respawn screen
	r.ReadBool()        // limited crafting
	r.ReadVarInt()      // dimension type
	r.ReadString(32767) // dimension name
	r.ReadI64()         // hashed seed
	r.ReadVarInt()      // game mode
	r.ReadVarInt()      // previous game mode
	r.ReadBool()        // debug world
	tail := LoginTail{
		Superflat: r.ReadBool(),
		HasDeath:  r.ReadBool(),
	}
	r.ReadVarInt() // portal cooldown
	tail.SeaLevel = r.ReadVarInt()
	if err := r.Err(); err != nil {
		t.Fatalf("login (play): %v", err)
	}
	return tail
}

// CheckRespawn decodes the whole Respawn packet a crossing sends: every field
// from the dimension type to the data kept, in the order the protocol
// reference lays them out.
func CheckRespawn(t *testing.T, data []byte, dimensionType int32, dimension string, seaLevel int32, kept byte, how string) {
	t.Helper()
	r := protocol.NewReader(data)
	if got := r.ReadVarInt(); got != dimensionType {
		t.Errorf("%s respawn dimension type = %d, want %d", how, got, dimensionType)
	}
	if got := r.ReadString(32767); got != dimension {
		t.Errorf("%s respawn dimension = %q, want %q", how, got, dimension)
	}
	r.ReadI64()    // hashed seed
	r.ReadVarInt() // game mode
	r.ReadVarInt() // previous game mode
	r.ReadBool()   // debug world
	if !r.ReadBool() {
		t.Errorf("%s respawn did not flag its superflat world", how)
	}
	if r.ReadBool() {
		t.Errorf("%s respawn carried a death location", how)
	}
	if got := r.ReadVarInt(); got != 0 {
		t.Errorf("%s respawn portal cooldown = %d, want 0", how, got)
	}
	if got := r.ReadVarInt(); got != seaLevel {
		t.Errorf("%s respawn sea level = %d, want %d", how, got, seaLevel)
	}
	if got := r.ReadU8(); got != kept {
		t.Errorf("%s respawn data kept = %#02x, want %#02x", how, got, kept)
	}
	if err := r.Err(); err != nil {
		t.Fatalf("%s respawn: %v", how, err)
	}
	if r.Remaining() != 0 {
		t.Errorf("%s respawn has %d bytes left over", how, r.Remaining())
	}
}

// BecomeSpectator switches the named player to spectator mode and waits for
// the server to take it in, then drains the packets the change leaves.
func BecomeSpectator(t *testing.T, srv *server.Server, c *Client, name string) {
	t.Helper()
	c.ChatCommand("gamemode spectator")
	PollUntil(t, ExpectTimeout, func() bool {
		return srv.PlayerGameMode(name) == protocol.GameModeSpectator
	})
	c.Settle()
}

// CowSummon is the /summon line that stands a stationary cow at the
// coordinates.
func CowSummon(x, z float64) string {
	return fmt.Sprintf("summon minecraft:cow %g 64 %g {NoAI:1b}", x, z)
}

// SummonCowNear summons a stationary cow at the coordinates and returns what
// the wire introduced it as.
func SummonCowNear(t *testing.T, c *Client, x, z float64) CapturedEntity {
	t.Helper()
	c.ChatCommand(CowSummon(x, z))
	cow := c.AwaitSpawn(protocol.EntityTypeCow)
	c.Settle()
	return cow
}

// PendTeleport records pos as a Synchronize Player Position the next
// teleport confirmation has to answer.
func (c *Client) PendTeleport(pos player.Position) { c.pendingTeleport = &pos }

// RespawnPortalCooldown decodes a Respawn packet's portal cooldown, in ticks.
func RespawnPortalCooldown(t *testing.T, data []byte) int32 {
	t.Helper()
	r := protocol.NewReader(data)
	r.ReadVarInt() // dimension type
	r.ReadString(32767)
	r.ReadI64()    // hashed seed
	r.ReadVarInt() // game mode
	r.ReadVarInt() // previous game mode
	r.ReadBool()   // debug world
	r.ReadBool()   // flat world
	if r.ReadBool() {
		r.ReadString(32767) // death dimension
		r.ReadI64()         // death position
	}
	cooldown := r.ReadVarInt()
	if err := r.Err(); err != nil {
		t.Fatalf("respawn: %v", err)
	}
	return cooldown
}

// RespawnPreviousGameMode decodes a Respawn packet's previous game mode: -1
// for none, else the mode's id.
func RespawnPreviousGameMode(t *testing.T, data []byte) int32 {
	t.Helper()
	r := protocol.NewReader(data)
	r.ReadVarInt() // dimension type
	r.ReadString(32767)
	r.ReadI64()
	r.ReadVarInt() // game mode
	prev := r.ReadVarInt() - 1
	if err := r.Err(); err != nil {
		t.Fatalf("respawn: %v", err)
	}
	return prev
}
