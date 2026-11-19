package commands

import (
	"math"
	"strconv"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
)

// coalescedKinds are the bodies the generic tracker tells teleports of, each
// summoned still (no gravity) a few blocks from the watcher. A thrown body is
// left out: its own flight schedule tells it absolute every few ticks.
var coalescedKinds = []struct {
	name, typ string
	spawn     int32
	summon    string
}{
	{"item", "minecraft:item", protocol.EntityTypeItem,
		`{Item:{id:"minecraft:stone",count:1},NoGravity:1b,PickupDelay:32767s}`},
	{"orb", "minecraft:experience_orb", protocol.EntityTypeExperienceOrb, `{Value:1,NoGravity:1b}`},
	{"arrow", "minecraft:arrow", protocol.EntityTypeArrow, `{NoGravity:1b}`},
	{"falling block", "minecraft:falling_block", protocol.EntityTypeFallingBlock,
		`{BlockState:{id:"minecraft:sand"},NoGravity:1b,Time:1}`},
	{"tnt", "minecraft:tnt", protocol.EntityTypeTnt, `{fuse:32000s,NoGravity:1b}`},
	{"cloud", "minecraft:area_effect_cloud", protocol.EntityTypeAreaEffectCloud, ``},
	{"boat", "minecraft:oak_boat", protocol.EntityTypeOakBoat, ``},
	{"minecart", "minecraft:minecart", 0, ``},
}

// TestTeleportsOfEveryTrackedKindAreToldOncePerTick checks that teleports of
// an item, orb, arrow, falling block, primed TNT, cloud, boat and
// minecart reach a watcher through the tracker's own tick: twenty that leave
// the body where it stood send no Teleport Entity, and twenty to new places
// in one tick send one, carrying the last place. A frozen world's tracking
// tells them the same way.
func TestTeleportsOfEveryTrackedKindAreToldOncePerTick(t *testing.T) {
	t.Parallel()
	for _, frozen := range []bool{false, true} {
		name := "running"
		if frozen {
			name = "frozen"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			testKindTeleportsToldOncePerTick(t, frozen)
		})
	}
}

func testKindTeleportsToldOncePerTick(t *testing.T, frozen bool) {
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Testificate", servertest.OpsListing(t, "Testificate"))
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()

	ids := map[int32]string{}
	for i, k := range coalescedKinds {
		x := float64(4+i) + 0.5
		if k.name == "cloud" {
			srv.SpawnCloudEntity(x, 64, 30.5) // /summon has no cloud
		} else {
			// Trailing whitespace behind the last argument is a parse
			// error, so the kinds without a tag trim theirs away.
			c.ChatCommand(strings.TrimSpace(
				"summon " + k.typ + " " + strconv.FormatFloat(x, 'f', 1, 64) + " 64 30.5 " + k.summon))
		}
		if k.spawn == 0 {
			k.spawn = servertest.CartMinecartType(t)
		}
		ids[c.AwaitSpawn(k.spawn).EntityID] = k.name
	}

	servertest.HoldTicks(t, srv)
	if frozen {
		srv.RunLine("tick freeze")
	}
	// The summons' own first movement ticks pass before anything is counted.
	c.ReadTicks(srv, 40, func(int32, []byte) bool { return false })

	var selected []command.Entity
	for _, k := range coalescedKinds {
		got := srv.SelectedEntities(k.typ)
		if len(got) != 1 {
			t.Fatalf("selected %d %s bodies, want 1", len(got), k.name)
		}
		selected = append(selected, got...)
	}

	// Twenty teleports onto the spot each body already stands on.
	for _, e := range selected {
		for range 20 {
			e.Teleport(command.Move{Pos: e.Position()}, world.DimensionName, "test")
		}
	}
	idle := map[int32]int{}
	c.ReadTicks(srv, 40, func(id int32, data []byte) bool {
		if id == protocol.IDPlayTeleportEntity {
			idle[protocol.NewReader(data).ReadVarInt()]++
		}
		return false
	})
	for id, name := range ids {
		if idle[id] != 0 {
			t.Errorf("%s was sent %d Teleport Entity packets for twenty teleports onto its own spot, want 0", name, idle[id])
		}
	}

	// Twenty teleports to new places in one tick.
	want := map[string]float64{}
	for i, e := range selected {
		at := e.Position()
		for j := range 20 {
			e.Teleport(command.Move{Pos: player.Position{X: at.X + float64(j+1)*0.25, Y: at.Y, Z: at.Z}},
				world.DimensionName, "test")
		}
		want[coalescedKinds[i].name] = at.X + 5
	}
	told := map[int32]int{}
	c.ReadTicks(srv, 40, func(id int32, data []byte) bool {
		if id != protocol.IDPlayTeleportEntity {
			return false
		}
		r := protocol.NewReader(data)
		eid := r.ReadVarInt()
		name, ours := ids[eid]
		if !ours {
			return false
		}
		told[eid]++
		r.ReadVarInt() // position path
		if x := r.ReadF64(); x != want[name] {
			t.Errorf("%s told at x %v, want the last teleport's %v", name, x, want[name])
		}
		return false
	})
	for id, name := range ids {
		if told[id] != 1 {
			t.Errorf("%s was sent %d Teleport Entity packets for twenty teleports in one tick, want 1", name, told[id])
		}
	}
	c.Disconnect()
}

// TestTeleportsOfHangingBodiesAreToldOncePerTick checks that a painting, item
// frame and leash knot are told a teleport through the tracker's tick: twenty
// onto the cell they hang in send no Teleport Entity, and twenty to other
// cells in one tick send one, carrying the painting's anchor or the other
// bodies' box centre. A frozen world's tracking tells them the same way.
func TestTeleportsOfHangingBodiesAreToldOncePerTick(t *testing.T) {
	t.Parallel()
	for _, frozen := range []bool{false, true} {
		name := "running"
		if frozen {
			name = "frozen"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			testHangingTeleportsToldOncePerTick(t, frozen)
		})
	}
}

func testHangingTeleportsToldOncePerTick(t *testing.T, frozen bool) {
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Testificate", servertest.OpsListing(t, "Testificate"))
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()

	// The knot's cells, every one it is sent to, are fences, or its survival
	// poll would take it down.
	fence := servertest.BlockState(t, "minecraft:oak_fence")
	for z := int32(30); z < 52; z++ {
		srv.SetBlock(8, 64, z, fence)
	}
	kinds := []struct {
		name, typ string
		spawn     int32
		id        int32
	}{
		{"painting", "minecraft:painting", protocol.EntityTypePainting, srv.SpawnPaintingEntity(4, 64, 30, 0).ID},
		{"item frame", "minecraft:item_frame", protocol.EntityTypeItemFrame, srv.SpawnFrameEntity(6, 64, 30).ID},
		{"leash knot", "minecraft:leash_knot", protocol.EntityTypeLeashKnot, srv.SpawnKnotEntity(8, 64, 30).ID},
	}
	if frozen {
		// A frozen world's tick tracks paintings only; frames and knots wait
		// for it to run.
		kinds = kinds[:1]
	}
	ids := map[int32]string{}
	for _, k := range kinds {
		ids[k.id] = k.name
	}
	servertest.HoldTicks(t, srv)
	if frozen {
		srv.RunLine("tick freeze")
	}
	c.ReadTicks(srv, 40, func(int32, []byte) bool { return false })

	var selected []command.Entity
	for _, k := range kinds {
		got := srv.SelectedEntities(k.typ)
		if len(got) != 1 {
			t.Fatalf("selected %d %s bodies, want 1", len(got), k.name)
		}
		selected = append(selected, got...)
	}
	type pose struct{ x, y, z float64 }
	poses := map[int32]pose{}
	count := func() map[int32]int {
		told := map[int32]int{}
		c.ReadTicks(srv, 40, func(id int32, data []byte) bool {
			if id == protocol.IDPlayTeleportEntity {
				r := protocol.NewReader(data)
				eid := r.ReadVarInt()
				told[eid]++
				r.ReadVarInt() // position path
				poses[eid] = pose{r.ReadF64(), r.ReadF64(), r.ReadF64()}
			}
			return false
		})
		return told
	}

	for i, e := range selected {
		for range 20 {
			e.Teleport(command.Move{Pos: player.Position{X: float64(4+2*i) + 0.5, Y: 64.5, Z: 30.5}},
				world.DimensionName, "test")
		}
	}
	idle := count()
	for id, name := range ids {
		if idle[id] != 0 {
			t.Errorf("%s was sent %d Teleport Entity packets for twenty teleports onto its own cell, want 0", name, idle[id])
		}
	}

	for i, e := range selected {
		for j := range 20 {
			e.Teleport(command.Move{Pos: player.Position{X: float64(4+2*i) + 0.5, Y: 64.5, Z: 31.5 + float64(j)}},
				world.DimensionName, "test")
		}
	}
	told := count()
	for id, name := range ids {
		if told[id] != 1 {
			t.Errorf("%s was sent %d Teleport Entity packets for twenty teleports in one tick, want 1", name, told[id])
		}
	}

	// The last teleport's cell is z 50; paintings track their anchor.
	box := func(minX, minY, minZ, maxX, maxY, maxZ float64) pose {
		return pose{(minX + maxX) / 2, (minY + maxY) / 2, (minZ + maxZ) / 2}
	}
	wantPose := map[string]pose{
		"painting":   {4, 64, 50},
		"item frame": box(rules.ItemFrameBox(6, 64, 50, 3, false)),
		"leash knot": {8.5, 64.375, 50.5},
	}
	for id, name := range ids {
		got, want := poses[id], wantPose[name]
		if math.Abs(got.x-want.x) > 1e-9 || math.Abs(got.y-want.y) > 1e-9 || math.Abs(got.z-want.z) > 1e-9 {
			t.Errorf("%s told at %v, want %v", name, got, want)
		}
	}
	c.Disconnect()
}

// TestTeleportsOfFireworkAreToldOncePerTick checks that twenty teleports of a
// rocket in one tick send one Teleport Entity, carrying the last place.
func TestTeleportsOfFireworkAreToldOncePerTick(t *testing.T) {
	t.Parallel()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Testificate", servertest.OpsListing(t, "Testificate"))
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()

	// Held before the rocket exists, so its flight is counted in ticks and
	// a slow machine cannot burn its fuse before the teleports.
	servertest.HoldTicks(t, srv)
	id := srv.SpawnPlayerFirework("Testificate", 4.5, 64, 30.5)
	if !servertest.ASpawnOfWithinTicks(c, srv, 5, protocol.EntityTypeFireworkRocket) {
		t.Fatal("the rocket's spawn did not reach the client within 5 ticks")
	}

	got := srv.SelectedEntities("minecraft:firework_rocket")
	if len(got) != 1 {
		t.Fatalf("selected %d rockets, want 1", len(got))
	}
	for j := range 20 {
		got[0].Teleport(command.Move{Pos: player.Position{X: 4.5 + float64(j+1)*0.25, Y: 64, Z: 30.5}},
			world.DimensionName, "test")
	}
	told := 0
	c.ReadTicks(srv, 5, func(pid int32, data []byte) bool {
		if pid != protocol.IDPlayTeleportEntity {
			return false
		}
		r := protocol.NewReader(data)
		if r.ReadVarInt() != id {
			return false
		}
		told++
		r.ReadVarInt() // position path
		if x := r.ReadF64(); x != 9.5 {
			t.Errorf("rocket told at x %v, want the last teleport's 9.5", x)
		}
		return false
	})
	if told != 1 {
		t.Errorf("rocket was sent %d Teleport Entity packets for twenty teleports in one tick, want 1", told)
	}
	c.Disconnect()
}
