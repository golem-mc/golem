package explosions

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// The drop-decay rules of a blast: the switch ServerLevel.explode runs on
// the ExplosionInteraction its caller passes. A mob's blast is the
// mob_griefing rule's to veto whole — the world stands, though the
// ray-cast's own count still rides the packet — and the mob decay rule's to
// scale; a primed TNT's the tnt decay rule's; a block's own — the bed where
// beds cannot work — the block decay rule's. A rule on leaves the drops
// under the tables' explosion_decay, one chance in the blast radius; a rule
// off drops everything whole, the loot context carrying no blast radius at
// all. The tests turn each rule off, where the outcome is deterministic:
// every destroyed stone gives its own stack, so the stacks dropped count
// the stones destroyed.

// collectBlast reads a watching client's packets until the blast's own
// packet arrives — every drop it spawned is on the wire ahead of it, the
// sends one runExplosion lays down in order — and hands the drops back with
// it. A spawn that is not an item's is stepped over, and so is any metadata
// that follows nothing of ours; a drop's own metadata is the packet right
// behind its spawn, the two sends one registerItem makes.
func collectBlast(srv *server.Server, c *servertest.Client, n int) ([]servertest.ItemDrop, servertest.Explosion, bool) {
	c.T.Helper()
	var col blastCollector
	c.ReadTicks(srv, n, func(id int32, data []byte) bool {
		return col.handle(c, id, data)
	})
	return col.drops, col.boom, col.exploded
}

// blastCollector is collectBlast's reader of one packet at a time, for a
// caller that pumps the packets itself.
type blastCollector struct {
	drops    []servertest.ItemDrop
	pending  *servertest.ItemDrop
	boom     servertest.Explosion
	exploded bool
}

// handle takes one packet and reports whether it was the blast's own.
func (b *blastCollector) handle(c *servertest.Client, id int32, data []byte) bool {
	switch id {
	case protocol.IDPlaySpawnEntity:
		if b.pending != nil {
			return false // the last drop's metadata is behind us already
		}
		if _, entityType := spawnEntityHead(data); entityType == protocol.EntityTypeItem {
			drop := servertest.ParseItemSpawn(data)
			if drop.EntityID != 0 {
				b.pending = &drop
			}
		}
	case protocol.IDPlayEntityMetadata:
		if b.pending == nil {
			return false
		}
		r := protocol.NewReader(data)
		if got := r.ReadVarInt(); got == b.pending.EntityID {
			if r.ReadU8() == protocol.MetadataIndexItemSlot && r.ReadVarInt() == protocol.MetadataSlot {
				b.pending.Stack, _ = r.ReadSlot()
				b.drops = append(b.drops, *b.pending)
				b.pending = nil
			}
		}
	case protocol.IDPlayExplosion:
		b.boom = servertest.ReadExplosion(c, data)
		b.exploded = true
		return true
	}
	return false
}

// stoneRing is the ring of stone a blast is stood beside to chew: eight
// blocks around the cell a creeper's feet claim, one out to three along the
// line the two of them stand on.
var stoneRing = [][3]int32{
	{1, 64, 0}, {3, 64, 0}, {2, 64, 1}, {2, 64, -1},
	{1, 64, 1}, {1, 64, -1}, {3, 64, 1}, {3, 64, -1},
}

// stoneCells is the map of where stone stood before a blast ran, read over
// a box wide enough to hold every cell the blast's rays reach — the flat
// world's own ground is stone below the grass, so the count of what a blast
// took has to cover the crater's chew into it, not only the blocks a test
// laid out.
type stoneCells map[[3]int32]struct{}

// stoneCellsAround reads the stone cells in the box given through the block
// reader handed in — the overworld's BlockAt, the nether's
// BlockInDimension — the snapshot a blast is measured against.
func stoneCellsAround(read func(x, y, z int32) uint16, x0, x1, y0, y1, z0, z1 int32) stoneCells {
	stone, _ := registry.FindState("minecraft:stone", nil)
	cells := stoneCells{}
	for x := x0; x <= x1; x++ {
		for y := y0; y <= y1; y++ {
			for z := z0; z <= z1; z++ {
				if read(x, y, z) == stone {
					cells[[3]int32{x, y, z}] = struct{}{}
				}
			}
		}
	}
	return cells
}

// destroyedNow counts how many of the snapshot's stones the world now reads
// as air.
func (cells stoneCells) destroyedNow(read func(x, y, z int32) uint16) int {
	gone := 0
	for pos := range cells {
		if read(pos[0], pos[1], pos[2]) == 0 {
			gone++
		}
	}
	return gone
}

// cobbleDropped counts the cobblestone the drops carry, stack by stack.
func cobbleDropped(t *testing.T, drops []servertest.ItemDrop) int {
	t.Helper()
	cobble, ok := registry.ItemID("minecraft:cobblestone")
	if !ok {
		t.Fatal("the item registry has no cobblestone")
	}
	got := 0
	for _, d := range drops {
		if d.Stack.ItemID == cobble {
			got += int(d.Stack.Count)
		}
	}
	return got
}

// TestMobGriefingKeepsTheWorldFromTheCreeperBlast turns mob_griefing off
// and stands a creeper beside a ring of stone: the blast is still the
// creeper's — the packet arrives, the body is taken out — but the world
// stands, no block chewed, nothing dropped, and the ray-cast's own count
// still rides the packet the way the vanilla explode reports it.
func TestMobGriefingKeepsTheWorldFromTheCreeperBlast(t *testing.T) {
	t.Parallel()
	cfg := servertest.HostileTestConfig()

	srv, c := servertest.Join(t, cfg, "Bystander")

	// The veto set before anything is stood up: the interaction is read
	// when the blast is built, and the world it leaves is the rule's own.
	srv.SetGameRule("mob_griefing", false)

	stone, ok := registry.FindState("minecraft:stone", nil)
	if !ok {
		t.Fatal("the block registry has no stone")
	}
	for _, pos := range stoneRing {
		srv.SetBlock(pos[0], pos[1], pos[2], stone)
	}
	// One corner is a TNT block instead: a blast that touches the world
	// primes it, one that keeps it leaves the block standing.
	tnt, ok := registry.FindState("minecraft:tnt", nil)
	if !ok {
		t.Fatal("the block registry has no tnt")
	}
	srv.SetBlock(3, 64, -1, tnt)

	creeperID := srv.SpawnMob("minecraft:creeper", 2.5, 64, 0.5)
	if creeperID == 0 {
		t.Fatal("the creeper never stood up")
	}

	drops, boom, exploded := collectBlast(srv, c, servertest.HostileTicks)
	if !exploded {
		t.Fatal("the creeper never exploded")
	}
	if boom.Radius != 3.0 {
		t.Errorf("servertest.Explosion radius = %v, want the creeper's 3", boom.Radius)
	}
	for _, pos := range stoneRing {
		if srv.BlockAt(pos[0], pos[1], pos[2]) == 0 {
			t.Errorf("the blast took the block at (%d,%d,%d) with mob_griefing off, want the world kept",
				pos[0], pos[1], pos[2])
		}
	}
	if len(drops) != 0 {
		t.Errorf("a blast that keeps the world dropped %d stacks, want none", len(drops))
	}
	if boom.BlockCount <= 0 {
		t.Errorf("servertest.Explosion blockCount = %d, want the ray-cast's own count, which a kept blast still reports", boom.BlockCount)
	}
}

// TestTheMobDecayRuleDropsWhole leaves mob_griefing on and turns the mob
// decay rule off: the creeper's blast takes the stones it reaches and every
// one of them gives its own stack whole, the loot context carrying no blast
// radius for the tables' explosion_decay to roll against.
func TestTheMobDecayRuleDropsWhole(t *testing.T) {
	t.Parallel()
	cfg := servertest.HostileTestConfig()

	srv, c := servertest.Join(t, cfg, "Bystander")

	srv.SetGameRule("mob_explosion_drop_decay", false)

	stone, ok := registry.FindState("minecraft:stone", nil)
	if !ok {
		t.Fatal("the block registry has no stone")
	}
	for _, pos := range stoneRing {
		srv.SetBlock(pos[0], pos[1], pos[2], stone)
	}
	// The stones before the blast ran, the crater's chew into the flat
	// world's own stone among them.
	snapshot := stoneCellsAround(srv.BlockAt, -1, 6, 61, 67, -3, 4)

	creeperID := srv.SpawnMob("minecraft:creeper", 2.5, 64, 0.5)
	if creeperID == 0 {
		t.Fatal("the creeper never stood up")
	}

	drops, boom, exploded := collectBlast(srv, c, servertest.HostileTicks)
	if !exploded {
		t.Fatal("the creeper never exploded")
	}
	if boom.Radius != 3.0 {
		t.Errorf("servertest.Explosion radius = %v, want the creeper's 3", boom.Radius)
	}
	destroyed := snapshot.destroyedNow(srv.BlockAt)
	if destroyed < 3 {
		t.Fatalf("the blast took %d stones, want at least the three the rays next door chew", destroyed)
	}
	if got := cobbleDropped(t, drops); got != destroyed {
		t.Errorf("the decayless blast dropped %d cobblestone for %d stones destroyed, want every stone's own stack",
			got, destroyed)
	}
}

// TestTheTNTDecayRuleDropsWhole turns the tnt decay rule off and drops a
// primed body onto a patch of stone: the blast takes what it reaches and
// every stone of it drops whole, the way DESTROY — no radius in the loot
// context — leaves the tables' own decay unrolled.
func TestTheTNTDecayRuleDropsWhole(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig() // creative, so the blast's drops stay behind

	srv, c := servertest.Join(t, cfg, "Watcher")

	srv.SetGameRule("tnt_explosion_drop_decay", false)

	stone, ok := registry.FindState("minecraft:stone", nil)
	if !ok {
		t.Fatal("the block registry has no stone")
	}
	// The ring around the cell the body lands in: it falls from seventy
	// onto the flat world's surface, feet at sixty-four.
	ring := [][3]int32{
		{3, 64, 0}, {5, 64, 0}, {4, 64, 1}, {4, 64, -1},
		{3, 64, 1}, {3, 64, -1}, {5, 64, 1}, {5, 64, -1},
	}
	for _, pos := range ring {
		srv.SetBlock(pos[0], pos[1], pos[2], stone)
	}
	snapshot := stoneCellsAround(srv.BlockAt, 0, 9, 60, 68, -4, 5)

	if srv.SpawnTNT(4.5, 70, 0.5, 40) == 0 {
		t.Fatal("the primed body never stood up")
	}

	drops, boom, exploded := collectBlast(srv, c, servertest.HostileTicks)
	if !exploded {
		t.Fatal("the fuse never ran out")
	}
	if boom.Radius != 4.0 {
		t.Errorf("servertest.Explosion radius = %v, want the primed TNT's 4", boom.Radius)
	}
	destroyed := snapshot.destroyedNow(srv.BlockAt)
	if destroyed < 3 {
		t.Fatalf("the blast took %d stones, want at least the three the rays next door chew", destroyed)
	}
	if got := cobbleDropped(t, drops); got != destroyed {
		t.Errorf("the decayless blast dropped %d cobblestone for %d stones destroyed, want every stone's own stack",
			got, destroyed)
	}
}

func TestPrimedTNTObeysTNTExplodesAtFuseExpiry(t *testing.T) {
	t.Parallel()
	for _, disableAfterSummon := range []bool{false, true} {
		name := "disabled_before_summon"
		if disableAfterSummon {
			name = "disabled_while_burning"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Keeper", servertest.OpsListing(t, "Keeper"))
			servertest.HoldTicks(t, srv)
			dirt := registry.BlockStates["minecraft:dirt"]
			srv.SetBlock(7, 64, 6, dirt)
			if !disableAfterSummon {
				srv.SetGameRule("tnt_explodes", false)
			}
			c.ChatCommand("summon minecraft:tnt 6.5 64 6.5 {fuse:5}")
			tnt := c.AwaitSpawn(protocol.EntityTypeTnt)
			if disableAfterSummon {
				servertest.RunTicks(t, srv, 1)
				srv.SetGameRule("tnt_explodes", false)
			}

			removed, exploded := false, false
			c.ReadTicks(srv, 10, func(id int32, data []byte) bool {
				switch id {
				case protocol.IDPlayRemoveEntities:
					r := protocol.NewReader(data)
					for range r.ReadVarInt() {
						if r.ReadVarInt() == tnt.EntityID {
							removed = true
						}
					}
				case protocol.IDPlayExplosion:
					exploded = true
				}
				return false
			})
			if !removed {
				t.Error("TNT was not removed after its fuse expired")
			}
			if exploded {
				t.Error("received an Explosion packet with tnt_explodes false")
			}
			if got := srv.BlockAt(7, 64, 6); got != dirt {
				t.Errorf("block beside TNT = %d, want dirt (%d)", got, dirt)
			}
		})
	}
}

// TestTheBlockDecayRuleDropsWhole turns the block decay rule off and sleeps
// in a bed where beds cannot work: the bad-respawn-point blast that takes
// both halves takes the stones around them with every drop whole, the
// block interaction's own rule being the only gate on it.
func TestTheBlockDecayRuleDropsWhole(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.BlockTestConfig(), "Actor")

	srv.SetGameRule("block_explosion_drop_decay", false)

	srv.MovePlayerToDimension("Actor", world.NetherName, 85, 40, 85)
	if _, ok := servertest.ReadUntilViewSent(t, c); !ok {
		t.Fatal("the nether never arrived for the actor")
	}

	stone, ok := registry.FindState("minecraft:stone", nil)
	if !ok {
		t.Fatal("the block registry has no stone")
	}
	read := func(x, y, z int32) uint16 {
		return srv.BlockInDimension(world.NetherName, x, y, z)
	}
	// The patch around the bed, five by five at its own level, the two
	// bed cells the only gap in it — and the stones before the blast ran.
	for x := int32(86); x <= 90; x++ {
		for z := int32(86); z <= 90; z++ {
			if (x == 88 && z == 88) || (x == 88 && z == 89) {
				continue
			}
			if !srv.SetBlockInDimension(world.NetherName, x, 40, z, stone) {
				t.Fatalf("could not stand the stone at (%d,40,%d)", x, z)
			}
		}
	}
	snapshot := stoneCellsAround(read, 84, 93, 36, 44, 84, 93)

	if !srv.SetBlockInDimension(world.NetherName, 88, 40, 88, servertest.BedStateName("red", "foot", "south")) ||
		!srv.SetBlockInDimension(world.NetherName, 88, 40, 89, servertest.BedStateName("red", "head", "south")) {
		t.Fatal("could not stand the bed in the nether")
	}

	c.UseBlock(88, 40, 88)
	drops, boom, exploded := collectBlast(srv, c, servertest.HostileTicks)
	if !exploded {
		t.Fatal("the bed never blew")
	}
	if boom.Radius != 5.0 {
		t.Errorf("servertest.Explosion radius = %v, want the bad respawn point's 5", boom.Radius)
	}
	destroyed := snapshot.destroyedNow(read)
	if destroyed < 10 {
		t.Fatalf("the blast took %d stones, want the wide chew a five-wide blast gives", destroyed)
	}
	if got := cobbleDropped(t, drops); got != destroyed {
		t.Errorf("the decayless blast dropped %d cobblestone for %d stones destroyed, want every stone's own stack",
			got, destroyed)
	}
}

func TestASummonedTNTKeepsItsExplosionPower(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ given, want string }{{"8f", "8.0f"}, {"500f", "128.0f"}} {
		t.Run(c.given, func(t *testing.T) {
			t.Parallel()
			srv, cl := servertest.Join(t, servertest.BlockTestConfig(), "Keeper", servertest.OpsListing(t, "Keeper"))
			servertest.HoldTicks(t, srv)
			cl.ChatCommand("summon minecraft:tnt 6.5 64 6.5 {fuse:1000s,explosion_power:" + c.given + "}")
			cl.AwaitSpawn(protocol.EntityTypeTnt)
			if n := srv.TNTCount(); n != 1 {
				t.Fatalf("TNTCount = %d, want 1", n)
			}
			cl.ChatCommand("data get entity @e[type=minecraft:tnt,limit=1] explosion_power")
			var answer string
			cl.ReadTicks(srv, 3, func(id int32, data []byte) bool {
				if id != protocol.IDPlaySystemChat {
					return false
				}
				text, ok := protocol.ReadText(protocol.NewReader(data))
				if !ok || text.Translate != "commands.data.entity.query" || len(text.With) != 2 {
					return false
				}
				answer = text.With[1].Plain()
				return true
			})
			if answer != c.want {
				t.Errorf("explosion_power read back %q, want %q", answer, c.want)
			}
		})
	}
}
