package mobs

import (
	"github.com/golem-mc/golem/internal/server/servertest"

	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/registry"
)

// The hostile targeting through a target that is not fully visible. The
// targeting conditions cut the follow range by the target's own visibility
// percent before the hunt may name it — a sneaking body reads 0.8, an
// invisible one 0.7 by its armour cover (a quarter a worn piece, floored at
// a tenth), and the head matching the hunting kind halves it again — while
// the hunt that has opened keeps its prey by the whole range. The zombie's
// follow range is the vanilla 35, so each shape has its own reach: 28 for
// sneaking, 2.45 for bare invisibility, 6.125 invisible under one helmet,
// 17.5 under the zombie's own head.

// visibilityWatchTicks is how long the wire watches a fresh zombie for its
// hunt, counted from its spawn: the nearest scan runs every few ticks, so a
// prey the zombie may take is taken early, and each shape it may not has
// this long to say so.
const visibilityWatchTicks = 200

// sneakingPrey presses the sneak key the way a client's input packet does,
// which is what the server takes a sneaking body's state from.
func sneakingPrey(_ *server.Server, c *servertest.Client) {
	c.Send(protocol.IDPlayPlayerInput, func(b *protocol.Buffer) { b.PutU8(protocol.InputSneak) })
	c.Settle()
}

// invisiblePrey gives the prey the Invisibility effect, long enough to
// outlast the watch however slowly the loop runs its ticks.
func invisiblePrey(srv *server.Server, _ *servertest.Client) {
	srv.GiveEffect("Prey", "minecraft:invisibility", 1200, 0, false)
}

// helmetedInvisiblePrey doubles invisibility with one worn piece: a quarter
// of the armour cover vanilla counts a helmet as.
func helmetedInvisiblePrey(srv *server.Server, c *servertest.Client) {
	helmet, ok := registry.ItemID("minecraft:iron_helmet")
	if !ok {
		c.T.Fatal("the item registry has no iron helmet")
	}
	srv.GiveEffect("Prey", "minecraft:invisibility", 1200, 0, false)
	srv.GiveSlot("Prey", player.ArmorSlot, protocol.ItemStack{ItemID: helmet, Count: 1})
}

// zombieHeadedPrey puts the zombie's own head on, the piece that halves
// the prey's visibility to a zombie and to no exact other kind.
func zombieHeadedPrey(srv *server.Server, c *servertest.Client) {
	head, ok := registry.ItemID("minecraft:zombie_head")
	if !ok {
		c.T.Fatal("the item registry has no zombie head")
	}
	srv.GiveSlot("Prey", player.ArmorSlot, protocol.ItemStack{ItemID: head, Count: 1})
}

// leatherHelmetPrey wears a leather helmet carrying the given mob_visibility
// against zombies, or a plain one when plain is set.
func leatherHelmetPrey(share float32, plain bool) func(*server.Server, *servertest.Client) {
	return func(srv *server.Server, c *servertest.Client) {
		helmet := protocol.ItemStack{ItemID: registry.MustItem("minecraft:leather_helmet"), Count: 1}
		if !plain {
			zombie, _ := registry.EntityTypeID("minecraft:zombie")
			helmet.SetRecord(protocol.DataComponentMobVisibility, []any{protocol.EntityTypeSet{Present: true, IDs: []int32{zombie}}, share})
		}
		srv.GiveSlot("Prey", player.ArmorSlot, helmet)
	}
}

// visibilityWatch is one watched encounter: the server it ran in, the
// zombie's entity ID, and whether the hunt opened inside the window.
type visibilityWatch struct {
	srv    *server.Server
	zombie int32
	hunted bool
}

// TestAZombieWeighsItsPreysVisibility plants a fresh zombie a measured
// distance east of a survival prey dressed by one of the shapes above, and
// holds the wire to the visibility cut: the hunt the shape's own reach
// permits either opens or never does.
func TestAZombieWeighsItsPreysVisibility(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		// distance is how many blocks east of the prey the zombie stands.
		distance float64
		// prepare dresses the prey into the shape the case weighs.
		prepare func(*server.Server, *servertest.Client)
		// want names whether the hunt opens inside the watch.
		want bool
		// refuseBeyond is the reach the case's own shape leaves: a refused
		// zombie must still stand beyond it at the end, or the watch
		// measured a stroll and not the search.
		refuseBeyond float64
	}{
		{"standing thirty blocks out", 30, nil, true, 0},
		{"sneaking twenty blocks out", 20, sneakingPrey, true, 0},
		{"sneaking thirty-three blocks out", 33, sneakingPrey, false, 28},
		{"invisible with no armour twenty-four and a half blocks out", 24.5, invisiblePrey, false, 2.45},
		{"invisible in a helmet ten blocks out", 10, helmetedInvisiblePrey, false, 6.125},
		{"wearing the zombie's head thirty-one blocks out", 31, zombieHeadedPrey, false, 17.5},
		// A stack's own mob_visibility: follow range 35 * 0 floors at 2.
		{"in a helmet whose own mob_visibility is zero, twenty blocks out", 20, leatherHelmetPrey(0, false), false, 2},
		{"in a plain leather helmet twenty blocks out", 20, leatherHelmetPrey(0, true), true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			watch := zombieWatchesDressedPrey(t, tc.distance, tc.prepare)
			if watch.hunted != tc.want {
				if tc.want {
					t.Errorf("the zombie never hunted a prey %v blocks out, want the reach to take them", tc.distance)
				} else {
					t.Errorf("the zombie hunted a prey %v blocks out, want the shape's reach too short for that", tc.distance)
				}
			}
			if tc.want || watch.hunted {
				return
			}
			// A refused watch is only the search's own word if the zombie
			// never wandered inside the reach the shape leaves; the world
			// is the same every run, so this says the geometry was sound.
			x, _, z, ok := watch.srv.MobPos(watch.zombie)
			if !ok {
				t.Fatal("the zombie left the world before the watch ended")
			}
			if away := math.Hypot(x-0.5, z-0.5); away <= tc.refuseBeyond {
				t.Fatalf("the zombie strolled to %v blocks of the prey, inside the %v the shape's reach leaves; the geometry measured nothing", away, tc.refuseBeyond)
			}
		})
	}
}

// zombieWatchesDressedPrey stands a fresh zombie exactly distance blocks
// east of a survival prey dressed by prepare, and watches the wire for the
// hunt the shapes permit: the aggressive bit of the Mob flags rising.
func zombieWatchesDressedPrey(t *testing.T, distance float64, prepare func(*server.Server, *servertest.Client)) visibilityWatch {
	t.Helper()
	srv, connect := servertest.Start(t, servertest.HostileTestConfig())
	srv.SetDayTime(18000) // midnight keeps the daylight burn out of the watch
	c := connect()
	c.JoinAs("Prey")
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()
	if prepare != nil {
		prepare(srv, c)
	}
	c.Settle() // the shape's packets are read past before the world is watched

	// Held, the zombie lives exactly the watch from its spawn: left free, a
	// starved reader stretches the window, and past about tick 320 the
	// sneaking case's zombie strolls within 23 blocks and may hunt.
	servertest.HoldTicks(t, srv)
	spawned := srv.TicksRun()
	zombieID := srv.SpawnMob("minecraft:zombie", 0.5+distance, 64, 0.5)
	awaitZombieSpawn(srv, c, zombieID)

	watch := visibilityWatch{srv: srv, zombie: zombieID}
	c.PumpTicksSince(srv, spawned, visibilityWatchTicks, func(id int32, data []byte) bool {
		if id != protocol.IDPlayEntityMetadata {
			return false
		}
		// The prey's own body arrives here too — its effects say so — and
		// the strict zombie reader takes no other body's fields.
		r := protocol.NewReader(data)
		if got := r.ReadVarInt(); r.Err() != nil || got != zombieID {
			return false
		}
		m := servertest.ReadZombieMetadata(c, data)
		if !m.HasFlags {
			return false
		}
		watch.hunted = m.Flags&protocol.EntityFlagAggressive != 0
		return watch.hunted
	})
	return watch
}

// awaitZombieSpawn reads the wire up to the zombie's Spawn Entity, checking
// the entity ID and the kind, without the strict reader the spawned-mob
// shape tests use: a spawn's own rolls — a baby's pace modifier in its
// attributes — are none of the visibility's business.
func awaitZombieSpawn(srv *server.Server, c *servertest.Client, zombieID int32) {
	c.T.Helper()
	if !c.ReadTicks(srv, servertest.HostileTicks, func(id int32, data []byte) bool {
		if id != protocol.IDPlaySpawnEntity {
			return false
		}
		r := protocol.NewReader(data)
		if got := r.ReadVarInt(); got != zombieID {
			c.T.Fatalf("the zombie's spawn carries entity ID %d, want the one SpawnMob reported (%d)", got, zombieID)
		}
		r.ReadUUID()
		if got := r.ReadVarInt(); got != protocol.EntityTypeZombie {
			c.T.Fatalf("the zombie's spawn carries type %d, want the zombie's %d", got, protocol.EntityTypeZombie)
		}
		return true
	}) {
		c.T.Fatal("the zombie's spawn never arrived")
	}
}
