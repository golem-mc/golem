package survival

import (
	"github.com/golem-mc/golem/internal/server/servertest"
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/registry"
)

// The powder snow's freeze on the wire: the frozen ticks climb as metadata
// index 7 while a body stands in the snow, the fully frozen one takes a
// freeze hurt every fortieth tick after, and the leather the
// freeze_immune_wearables tag names, the freeze_damage rule and the modes
// that cannot be hurt all hold it off. Each test drives the ticks itself,
// so the freeze is measured in ticks rather than wall-clock time.

// frozenView is what one Set Entity Metadata packet said about a body's
// freeze: the frozen ticks where the packet carried them, and the burns
// and breaths the environment packets ride along with.
type frozenView struct {
	entityID  int32
	flags     uint8
	air       int32
	hasAir    bool
	frozen    int32
	hasFrozen bool
}

// readFrozenMetadata parses one Set Entity Metadata packet for the fields
// the freeze ticks change, stepping over the rest by their declared type.
func readFrozenMetadata(t *testing.T, data []byte) frozenView {
	t.Helper()
	r := protocol.NewReader(data)
	v := frozenView{entityID: r.ReadVarInt()}
	for r.Err() == nil {
		index := r.ReadU8()
		if index == protocol.MetadataEnd {
			break
		}
		fieldType := r.ReadVarInt()
		switch fieldType {
		case protocol.MetadataByte, protocol.MetadataBoolean:
			b := r.ReadU8()
			if index == protocol.MetadataIndexEntityFlags {
				v.flags = b
			}
		case protocol.MetadataVarInt, protocol.MetadataPose, protocol.MetadataHumanoidArm:
			value := r.ReadVarInt()
			switch index {
			case protocol.MetadataIndexAir:
				v.hasAir, v.air = true, value
			case protocol.MetadataIndexTicksFrozen:
				v.hasFrozen, v.frozen = true, value
			}
		case protocol.MetadataFloat:
			r.ReadF32()
		default:
			t.Fatalf("unexpected metadata field %d of type %d", index, fieldType)
		}
	}
	if err := r.Err(); err != nil {
		t.Fatalf("entity metadata: %v", err)
	}
	return v
}

// inPowderSnow stands a joined player in a powder snow cell of the flat
// world, with the tick loop stopped and the survival pass the test's to
// drive; the cell is the z column of the test's own. The chunk is held so
// the powder snow lands in a loaded cell, the join's own stream being
// still on its way out.
func inPowderSnow(t *testing.T, name string, z int32) (*server.Server, *servertest.Client, func(int), int32) {
	t.Helper()
	srv, connect := servertest.Start(t, servertest.EnvironmentConfig())
	tick := servertest.StopTicking(t, srv)
	release := srv.HoldChunk(0, z)
	t.Cleanup(release)
	c := connect()
	entityID := c.JoinAs(name)
	if !srv.SetBlock(0, 64, z, servertest.BlockState(t, "minecraft:powder_snow")) {
		t.Fatal("the powder snow's chunk was not loaded")
	}
	c.MoveToGround(0.5, 64, float64(z)+0.5, true)
	// Two tick ends with no move between clear the join's move, so the body
	// stands still and kicks up no snowflakes.
	c.EndTick()
	c.EndTick()
	c.Settle()
	return srv, c, tick, entityID
}

// TestPowderSnowFreezesAPlayer is the freeze whole: a hundred and forty
// ticks standing in powder snow count the frozen ticks up one a tick — the
// first tick saying one, the hundred and fortieth the whole of it — and no
// hurt arrives before the counter is full. A fully frozen body takes one
// half-heart on every fortieth tick of its own age — the first being the
// one hundred and sixtieth, twenty past the freeze — and keeps that
// rhythm.
func TestPowderSnowFreezesAPlayer(t *testing.T) {
	t.Parallel()
	srv, c, tick, entityID := inPowderSnow(t, "Frozone", 80)
	// The fed body's own regeneration would heal between the bites and
	// blur the rhythm; a bar below the regeneration line keeps the bites
	// alone.
	srv.SetFood("Frozone", 17)

	tick(140)
	first, frozen, bitten := int32(-1), int32(-1), 0
	c.SettleAndCollect(func(id int32, data []byte) {
		switch id {
		case protocol.IDPlayEntityMetadata:
			if m := readFrozenMetadata(t, data); m.entityID == entityID && m.hasFrozen {
				if first < 0 {
					first = m.frozen
				}
				frozen = m.frozen
			}
		case protocol.IDPlayDamageEvent:
			bitten++
		}
	})
	if first != 1 {
		t.Errorf("the first frozen metadata said %d, want the 1 of the first tick in the snow", first)
	}
	if frozen != 140 {
		t.Errorf("the frozen metadata said %d after a hundred and forty ticks in the snow, want 140", frozen)
	}
	if bitten != 0 {
		t.Errorf("%d damage events arrived before the freeze was full, want none", bitten)
	}

	// The first hurt is the counter's next fortieth tick, twenty past the
	// freeze itself: one half-heart naming the freeze and nobody behind
	// it.
	tick(19)
	if n := servertest.DrainDamage(c); n != 0 {
		t.Errorf("%d damage events arrived before the freeze's sixtieth, want none", n)
	}
	tick(1)
	if got := c.ExpectHealth(); got.Health != 19 {
		t.Errorf("the first freeze hurt took the health to %v, want 19", got.Health)
	}
	c.ExpectDamageEvent(entityID, "minecraft:freeze", 0)

	// Forty ticks of counter later the rhythm repeats: nothing in between,
	// one bite at the fortieth.
	tick(39)
	if n := servertest.DrainDamage(c); n != 0 {
		t.Errorf("%d damage events arrived between the freeze's bites, want none", n)
	}
	tick(1)
	if got := c.ExpectHealth(); got.Health != 18 {
		t.Errorf("the second freeze hurt took the health to %v, want 18", got.Health)
	}
	c.ExpectDamageEvent(entityID, "minecraft:freeze", 0)
}

// TestAPlayerThawsOutOfPowderSnow checks the way out of the snow: the
// frozen ticks come back two a tick — the first tick out of the snow
// saying one hundred and thirty-eight — and the thaw carries no hurt, the
// counter passing below the full freeze on its way down to nothing.
func TestAPlayerThawsOutOfPowderSnow(t *testing.T) {
	t.Parallel()
	srv, c, tick, entityID := inPowderSnow(t, "Thawed", 82)
	srv.SetFood("Thawed", 17)

	tick(140)
	frozen, bitten := int32(-1), 0
	c.SettleAndCollect(func(id int32, data []byte) {
		switch id {
		case protocol.IDPlayEntityMetadata:
			if m := readFrozenMetadata(t, data); m.entityID == entityID && m.hasFrozen {
				frozen = m.frozen
			}
		case protocol.IDPlayDamageEvent:
			bitten++
		}
	})
	if frozen != 140 {
		t.Fatalf("the frozen metadata said %d after the stand in the snow, want 140", frozen)
	}
	if bitten != 0 {
		t.Fatalf("%d damage events arrived while the freeze filled, want none", bitten)
	}

	// Out of the snow, into the dry cell beside it: two frozen ticks come
	// off a tick, the seventy of them landing the counter at nothing.
	c.MoveToGround(0.5, 64, 83.5, true)
	c.Settle()
	tick(70)
	first, frozen := int32(-1), int32(-1)
	bitten = 0
	c.SettleAndCollect(func(id int32, data []byte) {
		switch id {
		case protocol.IDPlayEntityMetadata:
			if m := readFrozenMetadata(t, data); m.entityID == entityID && m.hasFrozen {
				if first < 0 {
					first = m.frozen
				}
				frozen = m.frozen
			}
		case protocol.IDPlayDamageEvent:
			bitten++
		}
	})
	if first != 138 {
		t.Errorf("the first thawing metadata said %d, want the 138 of two off the 140", first)
	}
	if frozen != 0 {
		t.Errorf("the thaw's last metadata said %d, want the counter at nothing", frozen)
	}
	if bitten != 0 {
		t.Errorf("%d damage events arrived through the thaw, want none", bitten)
	}

	// The counter empty, nothing more is sent and nothing bites.
	tick(30)
	frozenArrived := false
	c.SettleAndCollect(func(id int32, data []byte) {
		switch id {
		case protocol.IDPlayEntityMetadata:
			if m := readFrozenMetadata(t, data); m.entityID == entityID && m.hasFrozen {
				frozenArrived = true
			}
		case protocol.IDPlayDamageEvent:
			bitten++
		}
	})
	if frozenArrived {
		t.Error("frozen metadata arrived after the thaw, want the counter left alone at nothing")
	}
	if bitten != 0 {
		t.Errorf("%d damage events arrived after the thaw, want none", bitten)
	}
}

// TestIgnitionClearsTheFreeze checks the thaw a flame brings and the cooldown
// it waits on: a fire block lights a counter only at nought or above, so a
// frozen body still counting a douse is not thawed at once, while once the
// flame takes whole the freeze is cleared outright.
// vanilla: BaseFireBlock.fireIgnite, Entity.igniteForTicks
func TestIgnitionClearsTheFreeze(t *testing.T) {
	t.Parallel()
	srv, c, tick, entityID := inPowderSnow(t, "Thawing", 94)
	srv.SetFood("Thawing", 17)

	tick(140)
	frozen := int32(-1)
	c.SettleAndCollect(func(id int32, data []byte) {
		if id == protocol.IDPlayEntityMetadata {
			if m := readFrozenMetadata(t, data); m.entityID == entityID && m.hasFrozen {
				frozen = m.frozen
			}
		}
	})
	if frozen != 140 {
		t.Fatalf("the frozen metadata before the flame said %d, want 140", frozen)
	}

	// The snow cell becomes fire. The counter stands in the douse cooldown
	// from standing outside a flame, so the first tick does not light it and
	// only the thaw's two come off the freeze.
	if !srv.SetBlock(0, 64, 94, servertest.BlockState(t, "minecraft:fire")) {
		t.Fatal("the fire's chunk was not loaded")
	}
	c.Settle()
	tick(1)
	frozen = -1
	c.SettleAndCollect(func(id int32, data []byte) {
		if id == protocol.IDPlayEntityMetadata {
			if m := readFrozenMetadata(t, data); m.entityID == entityID && m.hasFrozen {
				frozen = m.frozen
			}
		}
	})
	if frozen != 138 {
		t.Errorf("the frozen metadata on the douse's first flame tick said %d, want the 138 of the thaw alone", frozen)
	}

	// Once the fire block has walked the counter back to nought it takes,
	// and the freeze is cleared whole.
	tick(19)
	frozen = -1
	c.SettleAndCollect(func(id int32, data []byte) {
		if id == protocol.IDPlayEntityMetadata {
			if m := readFrozenMetadata(t, data); m.entityID == entityID && m.hasFrozen {
				frozen = m.frozen
			}
		}
	})
	if frozen != 0 {
		t.Errorf("the frozen metadata once the flame took said %d, want the cleared nought", frozen)
	}
}

// TestPowderSnowSlowsTheFrozen checks the movement slow the powder snow puts
// on a body: a fully frozen body's client is sent the movement_speed with the
// transient minecraft:powder_snow modifier of -0.05, half of the player's
// base pace at the full freeze.
// vanilla: LivingEntity.tryAddFrost
func TestPowderSnowSlowsTheFrozen(t *testing.T) {
	t.Parallel()
	_, c, tick, entityID := inPowderSnow(t, "Slowpoke", 96)
	tick(140)

	full := func(e servertest.AttrEntry) bool {
		for _, m := range e.Modifiers {
			if m.Id == "minecraft:powder_snow" && math.Abs(m.Amount+0.05) < 1e-9 {
				return true
			}
		}
		return false
	}
	e := c.AwaitAttributes(entityID, servertest.AttrMovementSpeed, full)[servertest.AttrMovementSpeed]
	// The base 0.1 that movement_speed starts at, less the 0.05 of the full
	// freeze's slow. The base is a widened float32, hence the slack.
	if got := e.Value(); math.Abs(got-0.05) > 1e-6 {
		t.Errorf("the frozen movement speed = %v, want the 0.05 of a full freeze", got)
	}
}

// inPowderSnowPit stops the loop of a fresh mob-test world, holds the pit's
// chunk and fills a three-by-three pit two deep with powder snow over a
// stone floor: the place a summoned body stands frozen. The clock is held
// at midnight, so a kind the freeze spares is not burning for a second
// reason instead.
func inPowderSnowPit(t *testing.T) (*server.Server, *servertest.Client) {
	t.Helper()
	srv, c := servertest.Join(t, servertest.MobTestConfig(), "Watcher", servertest.OpsListing(t, "Watcher"))
	c.Settle()
	srv.StopTick()
	release := srv.HoldChunk(5, 5)
	t.Cleanup(release)
	srv.SetPaused(true)
	srv.SetDayTime(18000)

	snow := servertest.BlockState(t, "minecraft:powder_snow")
	stone := servertest.BlockState(t, "minecraft:stone")
	for x := int32(4); x <= 6; x++ {
		for z := int32(4); z <= 6; z++ {
			srv.SetBlock(x, 63, z, stone)
			srv.SetBlock(x, 64, z, snow)
			srv.SetBlock(x, 65, z, snow)
		}
	}
	if got := srv.BlockAt(5, 64, 5); got != snow {
		t.Fatal("the pit's chunk was not loaded")
	}
	return srv, c
}

// summonIntoPowderSnow stands a NoAI mob of the kind named into the
// powder snow pit, and reports the entity ID its packets address it by.
func summonIntoPowderSnow(t *testing.T, srv *server.Server, c *servertest.Client, kind string) int32 {
	t.Helper()
	c.ChatCommand("summon minecraft:" + kind + " 5.5 64 5.5 {NoAI:1b}")
	typeID, ok := registry.EntityTypeID("minecraft:" + kind)
	if !ok {
		t.Fatalf("no entity type for minecraft:%s", kind)
	}
	id := c.ExpectSummonedMob(typeID)
	c.Settle()
	return id
}

// TestACowInPowderSnowFreezes checks the mob half of the freeze: a summoned
// cow standing in powder snow sends its frozen ticks up as metadata — the
// first tick one, the hundred and fortieth the whole — and the fully
// frozen body takes the freeze's bite on its own next fortieth tick of age,
// twenty past the freeze.
func TestACowInPowderSnowFreezes(t *testing.T) {
	t.Parallel()
	srv, c := inPowderSnowPit(t)
	cowID := summonIntoPowderSnow(t, srv, c, "cow")

	srv.TickMobs(140)
	first, frozen, bitten := int32(-1), int32(-1), 0
	c.SettleAndCollect(func(id int32, data []byte) {
		switch id {
		case protocol.IDPlayEntityMetadata:
			if m := readFrozenMetadata(t, data); m.entityID == cowID && m.hasFrozen {
				if first < 0 {
					first = m.frozen
				}
				frozen = m.frozen
			}
		case protocol.IDPlayDamageEvent:
			bitten++
		}
	})
	if first != 1 {
		t.Errorf("the cow's first frozen metadata said %d, want 1", first)
	}
	if frozen != 140 {
		t.Errorf("the cow's frozen metadata said %d after a hundred and forty ticks, want 140", frozen)
	}
	if bitten != 0 {
		t.Errorf("%d damage events arrived before the cow's freeze was full, want none", bitten)
	}

	srv.TickMobs(19)
	bitten = 0
	c.SettleAndCollect(func(id int32, data []byte) {
		if id == protocol.IDPlayDamageEvent {
			bitten++
		}
	})
	if bitten != 0 {
		t.Errorf("%d damage events arrived before the cow's freeze's fortieth, want none", bitten)
	}

	// The bite itself: a freeze hurt of one on the cow's body, named as
	// the freeze and nobody behind it.
	srv.TickMobs(1)
	bitten, froze := 0, false
	c.SettleAndCollect(func(id int32, data []byte) {
		if id != protocol.IDPlayDamageEvent {
			return
		}
		r := protocol.NewReader(data)
		if r.ReadVarInt() != cowID {
			return
		}
		bitten++
		if got, want := r.ReadVarInt(), registry.MustID("minecraft:damage_type", "minecraft:freeze"); got == want {
			froze = true
		}
		if got := r.ReadVarInt(); got != 0 {
			t.Errorf("the freeze's cause ID = %d, want nobody behind it", got)
		}
		if got := r.ReadVarInt(); got != 0 {
			t.Errorf("the freeze's direct ID = %d, want nobody behind it", got)
		}
		if r.ReadBool() {
			t.Error("the freeze's event carries a position, which only /damage gives")
		}
	})
	if bitten != 1 || !froze {
		t.Errorf("the frozen cow's bite came to %d events, %v freeze-named, want one freeze hurt", bitten, froze)
	}
}
