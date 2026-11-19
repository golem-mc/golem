package blocks

import (
	"encoding/binary"
	"github.com/golem-mc/golem/internal/server/servertest"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// The conduit, black-box: the cage a conduit stands in — the water of the
// 3×3×3 about it and the frame cells of the 5×5×5 cage about that — what
// the sixteen-cell ring of the conduit's own level does for a swimmer
// within the reach of its frame, and the attack a complete forty-two-cell
// cage keeps up. Every test drives the ticks by hand with the world's age
// under its own control, so the conduit's forty-tick rhythm is measured
// rather than estimated: the age is set to a multiple of forty first, so
// the refresh lands on the fortieth tick after it.

// conduitRing is the sixteen frame cells of the conduit's own level, the
// 5×5 ring about the 3×3 its water fills: the smallest frame that
// activates a conduit.
// vanilla: ConduitBlockEntity.updateShape
var conduitRing = [][3]int32{
	{-2, 0, -2}, {-2, 0, -1}, {-2, 0, 0}, {-2, 0, 1}, {-2, 0, 2},
	{2, 0, -2}, {2, 0, -1}, {2, 0, 0}, {2, 0, 1}, {2, 0, 2},
	{-1, 0, -2}, {0, 0, -2}, {1, 0, -2},
	{-1, 0, 2}, {0, 0, 2}, {1, 0, 2},
}

// conduitFullCage is the frame a complete cage is built of, the 42 cells of
// the 5×5×5 shell the vanilla scan walks — the ring of the conduit's own
// level among them — listed literally so the test does not re-derive the
// scan it checks.
// vanilla: ConduitBlockEntity.updateShape
var conduitFullCage = [][3]int32{
	{-2, -2, 0}, {-2, -1, 0}, {-2, 0, -2}, {-2, 0, -1}, {-2, 0, 0}, {-2, 0, 1},
	{-2, 0, 2}, {-2, 1, 0}, {-2, 2, 0},
	{-1, -2, 0}, {-1, 0, -2}, {-1, 0, 2}, {-1, 2, 0},
	{0, -2, -2}, {0, -2, -1}, {0, -2, 0}, {0, -2, 1}, {0, -2, 2},
	{0, -1, -2}, {0, -1, 2}, {0, 0, -2}, {0, 0, 2}, {0, 1, -2}, {0, 1, 2},
	{0, 2, -2}, {0, 2, -1}, {0, 2, 0}, {0, 2, 1}, {0, 2, 2},
	{1, -2, 0}, {1, 0, -2}, {1, 0, 2}, {1, 2, 0},
	{2, -2, 0}, {2, -1, 0}, {2, 0, -2}, {2, 0, -1}, {2, 0, 0}, {2, 0, 1},
	{2, 0, 2}, {2, 1, 0}, {2, 2, 0},
}

// buildConduitCage fills the water of the 3×3×3 about x, y, z — the
// conduit's own waterlogging supplies the middle — and the frame cells
// named, so the conduit planted there is the frame the scan reads. In a
// named dimension the blocks land there.
func buildConduitCage(t *testing.T, srv *server.Server, dimension string, x, y, z int32, frame [][3]int32) {
	t.Helper()
	setBlock := func(x, y, z int32, state uint16) {
		t.Helper()
		var ok bool
		var at uint16
		if dimension == "" {
			ok = srv.SetBlock(x, y, z, state)
			at = srv.BlockAt(x, y, z)
		} else {
			ok = srv.SetBlockInDimension(dimension, x, y, z, state)
			at = srv.BlockInDimension(dimension, x, y, z)
		}
		// A write to a cell that already holds it is not a failure; an
		// unloaded chunk, whose read comes back air, is.
		if !ok && at != state {
			t.Fatal("the cage's chunk was not loaded")
		}
	}
	water, prismarine := servertest.BlockState(t, "minecraft:water"), servertest.BlockState(t, "minecraft:prismarine")
	// The 3×3×3 fills with water whole, the middle included: the conduit
	// planted there replaces that water, which is what waterlogs it.
	for dx := int32(-1); dx <= 1; dx++ {
		for dy := int32(-1); dy <= 1; dy++ {
			for dz := int32(-1); dz <= 1; dz++ {
				setBlock(x+dx, y+dy, z+dz, water)
			}
		}
	}
	for _, o := range frame {
		setBlock(x+o[0], y+o[1], z+o[2], prismarine)
	}
}

// placeConduit hands the client a conduit and plants it into the water at
// x, y, z, which waterlogs it, the way a player sets one into the sea. It
// then checks the cell and the block entity the world keeps about it.
func placeConduit(t *testing.T, c *servertest.Client, srv *server.Server, dimension string, x, y, z int32) {
	t.Helper()
	c.SetCreativeSlot(player.HotbarSlot, servertest.ItemStack(t, "minecraft:conduit", 1))
	c.PlaceBlock(x, y, z, protocol.FaceTop)
	c.Settle()
	state := srv.BlockAt(x, y, z)
	if dimension != "" {
		state = srv.BlockInDimension(dimension, x, y, z)
	}
	name, raw, ok := registry.BlockStateName(state)
	if !ok || name != "minecraft:conduit" || registry.ParseStateProps(raw)["waterlogged"] != "true" {
		t.Fatalf("the conduit stands as %s at (%d, %d, %d), want waterlogged", name, x, y, z)
	}
	var kind world.BlockEntityKind
	if dimension == "" {
		kind, ok = srv.BlockEntityKindAt(x, y, z)
	} else {
		kind, ok = srv.BlockEntityKindInDimension(dimension, x, y, z)
	}
	if !ok || kind != world.KindConduit {
		t.Fatalf("the placed conduit's block entity = %q/%v, want a minecraft:conduit", kind, ok)
	}
}

// driveConduitTicks stops the loop and returns the stepper that runs its
// ticks by hand: the world's age one tick forward first, then the
// block-entity pass, the survival pass and the mobs', the age ahead of
// the rest the order the main loop keeps. The conduit refreshes only on
// the age's forty-tick stride, so the age's phase decides which driven
// tick a refresh is.
func driveConduitTicks(t *testing.T, srv *server.Server) func(int) {
	t.Helper()
	srv.StopTick()
	return func(n int) {
		t.Helper()
		for range n {
			age, _ := srv.Time()
			srv.SetWorldAge(age + 1)
			srv.TickBlockEntities()
			srv.TickSurvival()
			srv.TickMobs(1)
		}
	}
}

// phaseConduitAge sets the world's age to a multiple of forty, so the next
// refresh of the stride is the fortieth tick the stepper drives.
// vanilla: ConduitBlockEntity.serverTick, the gameTime % 40 gate
func phaseConduitAge(t *testing.T, srv *server.Server) {
	t.Helper()
	age, _ := srv.Time()
	srv.SetWorldAge(age - age%40)
}

// conduitTell is what the wire said around one conduit refresh.
type conduitTell struct {
	state, strikes        int                    // the state-change and attack sounds heard
	ambient, shortAmbient int                    // the 80-tick pulse and its short partner
	effects               []servertest.MobEffect // the Update Mob Effects
	removes, datas        int                    // effect removals; the conduit's block entity data
	damage                []int32                // the damage types of the victim's damage events
	healths               []float32              // the victim's health metadata
	targets               []uuid.UUID            // the Target the block entity data named
}

// conduitTellRecorder reads one client's packets into one tell, sifted by
// the sounds and the victim it is about.
type conduitTellRecorder struct {
	t                               *testing.T
	tell                            *conduitTell
	stateSound, attackSound         int32 // zero sounds collect none
	ambientSound, shortAmbientSound int32
	victim                          int32 // zero victims collect all
}

func (r conduitTellRecorder) record(id int32, data []byte) {
	r.t.Helper()
	switch id {
	case protocol.IDPlaySoundEffect:
		s := servertest.SoundSee{}
		s.ReadSoundSee(r.t, data)
		if r.stateSound != 0 && s.Sound == r.stateSound {
			r.tell.state++
		}
		if r.attackSound != 0 && s.Sound == r.attackSound {
			r.tell.strikes++
			if s.Category != protocol.SoundCategoryBlock || s.Volume != 1 || s.Pitch != 1 {
				r.t.Errorf("the strike's sound: category=%d volume=%v pitch=%v, want the blocks at full",
					s.Category, s.Volume, s.Pitch)
			}
		}
		if r.ambientSound != 0 && s.Sound == r.ambientSound {
			r.tell.ambient++
		}
		if r.shortAmbientSound != 0 && s.Sound == r.shortAmbientSound {
			r.tell.shortAmbient++
		}
	case protocol.IDPlayUpdateMobEffect:
		reader := protocol.NewReader(data)
		r.tell.effects = append(r.tell.effects, servertest.MobEffect{Entity: reader.ReadVarInt(), Effect: reader.ReadVarInt(),
			Amplifier: reader.ReadVarInt(), Duration: reader.ReadVarInt(), Flags: reader.ReadU8()})
		if err := reader.Err(); err != nil {
			r.t.Fatalf("update mob effect: %v", err)
		}
	case protocol.IDPlayRemoveMobEffect:
		r.tell.removes++
	case protocol.IDPlayBlockEntityData:
		reader := protocol.NewReader(data)
		reader.ReadPosition()
		if reader.ReadVarInt() != world.BlockEntityTypeID("minecraft:conduit") {
			return
		}
		r.tell.datas++
		tag, _, err := nbt.DecodeNetwork(reader.Rest())
		if err != nil {
			r.t.Fatalf("conduit block entity data: %v", err)
		}
		if compound, ok := tag.(nbt.Compound); ok {
			if ids, ok := nbt.Get[nbt.IntArray](compound, "Target"); ok && len(ids) == 4 {
				r.tell.targets = append(r.tell.targets, intArrayUUID(ids))
			}
		}
	case protocol.IDPlayDamageEvent:
		if r.victim == 0 {
			return
		}
		reader := protocol.NewReader(data)
		victim, damageType := reader.ReadVarInt(), reader.ReadVarInt()
		if err := reader.Err(); err != nil {
			r.t.Fatalf("damage event: %v", err)
		}
		if victim == r.victim {
			r.tell.damage = append(r.tell.damage, damageType)
		}
	case protocol.IDPlayEntityMetadata:
		if r.victim == 0 {
			return
		}
		reader := protocol.NewReader(data)
		if reader.ReadVarInt() != r.victim {
			return
		}
		// The hurt tells one health alone; other packets about the body -
		// its pose, its flags - are stepped over whole.
		if index := reader.ReadU8(); index == protocol.MetadataIndexLivingHealth &&
			reader.ReadVarInt() == protocol.MetadataFloat {
			r.tell.healths = append(r.tell.healths, reader.ReadF32())
		}
	}
}

// collectConduitTells drains everything the clients have to hear into one
// tell each, so the phases between the conduit's refreshes read as wholes.
func collectConduitTells(t *testing.T, clients []*servertest.Client, stateSound, attackSound, victim int32) []*conduitTell {
	t.Helper()
	tells := make([]*conduitTell, len(clients))
	for i, c := range clients {
		tell := &conduitTell{}
		tells[i] = tell
		c.SettleAndCollect(conduitTellRecorder{t: t, tell: tell, stateSound: stateSound,
			attackSound: attackSound, victim: victim}.record)
	}
	return tells
}

// conduitTarget reads the UUID the conduit's block entity at x, y, z names
// as its target, or uuid.Nil while the hunt rests.
func conduitTarget(t *testing.T, srv *server.Server, x, y, z int32) uuid.UUID {
	t.Helper()
	tag, ok := srv.BlockEntityTagAt(x, y, z)
	if !ok {
		t.Fatalf("the conduit's block entity at (%d, %d, %d) is gone", x, y, z)
	}
	ids, has := nbt.Get[nbt.IntArray](tag, "Target")
	if !has || len(ids) != 4 {
		return uuid.Nil
	}
	return intArrayUUID(ids)
}

// intArrayUUID decodes the UUID four big-endian ints name.
func intArrayUUID(ids nbt.IntArray) uuid.UUID {
	var id uuid.UUID
	for i, v := range ids[:4] {
		binary.BigEndian.PutUint32(id[i*4:], uint32(v))
	}
	return id
}

// TestAConduitsRingInWaterPowersTheNearbySwimmer checks the conduit's
// activation end to end: the frame — the sixteen-cell ring of its own
// level — the water the 3×3×3 about it holds, and on the refresh's
// fortieth tick the activation sound with Conduit Power for a swimmer
// within the ring's reach, ambient and blended in. A dry body the same
// distance away gets nothing, and the effect wears out on its own once the
// frame falls a cell short: the deactivation sound alone, no refresh and
// no removal.
// vanilla: ConduitBlockEntity.serverTick, applyEffects
func TestAConduitsRingInWaterPowersTheNearbySwimmer(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig())
	tick := driveConduitTicks(t, srv)
	activate := servertest.ConduitSoundID(t, "minecraft:block.conduit.activate")
	deactivate := servertest.ConduitSoundID(t, "minecraft:block.conduit.deactivate")

	swimmer := connect()
	swimmerID := swimmer.JoinAs("Swimmer")
	actor := connect()
	actor.JoinAs("Actor")

	// The cage at the plaza's edge, and beside it the column of water a
	// swimmer fits in; the actor walks up to reach the conduit's middle
	// and back out to stay a dry control.
	const cx, cy, cz = 5, 64, 0
	buildConduitCage(t, srv, "", cx, cy, cz, conduitRing)
	water := servertest.BlockState(t, "minecraft:water")
	if !srv.SetBlock(10, 64, -1, water) || !srv.SetBlock(10, 65, -1, water) {
		t.Fatal("the swimmer's chunk was not loaded")
	}
	actor.MoveToGround(cx+0.5, 64, cz+3.5, true)
	placeConduit(t, actor, srv, "", cx, cy, cz)
	actor.MoveToGround(0.5, 64, 0.5, true)
	swimmer.MoveToGround(10.5, 64, -0.5, true)
	swimmer.Settle()

	// Thirty-nine driven ticks: the conduit has said nothing, the refresh
	// being what the fortieth is.
	phaseConduitAge(t, srv)
	var before conduitTell
	for range 39 {
		tick(1)
		for _, c := range []*servertest.Client{swimmer, actor} {
			c.SettleAndCollect(conduitTellRecorder{t: t, tell: &before, stateSound: activate}.record)
		}
	}
	if before.state != 0 || before.strikes != 0 || len(before.effects) != 0 || before.removes != 0 {
		t.Fatalf("the conduit spoke before the refresh tick: %+v", before)
	}

	// The fortieth: the coming-on sound the whole plaza hears, and the
	// effect on the swimmer alone.
	tick(1)
	tells := collectConduitTells(t, []*servertest.Client{swimmer, actor}, activate, 0, 0)
	for i, tell := range tells {
		if tell.removes != 0 || tell.strikes != 0 {
			t.Errorf("client %d heard %d strikes and %d removals, want none", i, tell.strikes, tell.removes)
		}
	}
	if tells[0].state != 1 || tells[1].state != 1 {
		t.Errorf("the refresh's activation sound reached %d and %d of the two, want one each",
			tells[0].state, tells[1].state)
	}
	if len(tells[1].effects) != 0 {
		t.Errorf("the dry actor was sent %d effects, want none", len(tells[1].effects))
	}
	if len(tells[0].effects) != 1 {
		t.Fatalf("the swimmer was sent %d effects, want one", len(tells[0].effects))
	}
	// Conduit Power thirteen seconds long, of the first level, ambient with
	// its particles and icon, blended in as a new effect: the duration is
	// the 260 ticks vanilla hands out, which outreaches the forty between
	// its refreshes.
	got := tells[0].effects[0]
	want := servertest.MobEffect{
		Entity:    swimmerID,
		Effect:    registry.MustMobEffect("minecraft:conduit_power"),
		Amplifier: 0,
		Duration:  260,
		Flags:     0x01 | 0x02 | 0x04 | 0x08,
	}
	if got != want {
		t.Errorf("the swimmer's effect = %+v, want %+v", got, want)
	}

	// Forty ticks and a frame block fewer: the conduit goes dark on its
	// next refresh — the deactivation sound alone, the swimmer's effect
	// neither refreshed nor taken off.
	srv.SetBlock(cx-2, cy, cz, 0)
	tick(40)
	tells = collectConduitTells(t, []*servertest.Client{swimmer, actor}, deactivate, 0, 0)
	for i, tell := range tells {
		if tell.state != 1 || tell.removes != 0 || len(tell.effects) != 0 {
			t.Errorf("client %d heard %d state sounds, %d effects and %d removals over the deactivation, want one sound alone",
				i, tell.state, len(tell.effects), tell.removes)
		}
	}
}

// TestAConduitSkipsASpectatorInReach checks that a spectator swimming in
// the same water as a survival player gets no Conduit Power.
// vanilla: ConduitBlockEntity.applyEffects, EntitySelector.NO_SPECTATORS
func TestAConduitSkipsASpectatorInReach(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig(), servertest.OpsListing(t, "Ghost"))
	tick := driveConduitTicks(t, srv)

	ghost := connect()
	ghost.JoinAs("Ghost")
	swimmer := connect()
	swimmer.JoinAs("Swimmer")
	actor := connect()
	actor.JoinAs("Actor")

	const cx, cy, cz = 5, 64, 0
	buildConduitCage(t, srv, "", cx, cy, cz, conduitRing)
	water := servertest.BlockState(t, "minecraft:water")
	if !srv.SetBlock(10, 64, -1, water) || !srv.SetBlock(10, 65, -1, water) {
		t.Fatal("the swimmers' chunk was not loaded")
	}
	actor.MoveToGround(cx+0.5, 64, cz+3.5, true)
	placeConduit(t, actor, srv, "", cx, cy, cz)
	actor.MoveToGround(0.5, 64, 0.5, true)
	swimmer.MoveToGround(10.5, 64, -0.5, true)
	ghost.MoveToGround(10.5, 64, -0.5, true)
	ghost.ChatCommand("gamemode spectator")
	servertest.WaitFor(t, srv, 100, func() bool {
		return srv.PlayerGameMode("Ghost") == protocol.GameModeSpectator
	})
	ghost.Settle()
	swimmer.Settle()

	phaseConduitAge(t, srv)
	tick(40)
	tells := collectConduitTells(t, []*servertest.Client{swimmer, ghost}, 0, 0, 0)
	if len(tells[0].effects) != 1 {
		t.Errorf("the survival swimmer was sent %d effects, want one", len(tells[0].effects))
	}
	if len(tells[1].effects) != 0 {
		t.Errorf("the spectator was sent %d effects, want none", len(tells[1].effects))
	}
}

// buildConduitPool builds a two by two, two deep pool walled in stone above
// the ground, so nothing held in it swims out of the beam's reach.
func buildConduitPool(t *testing.T, srv *server.Server) {
	t.Helper()
	stone, water := servertest.BlockState(t, "minecraft:stone"), servertest.BlockState(t, "minecraft:water")
	for x := int32(10); x <= 11; x++ {
		for z := int32(4); z <= 5; z++ {
			if !srv.SetBlock(x, 63, z, water) || !srv.SetBlock(x, 64, z, water) {
				t.Fatal("the pool's chunk was not loaded")
			}
		}
	}
	for x := int32(9); x <= 12; x++ {
		for z := int32(3); z <= 6; z++ {
			if x >= 10 && x <= 11 && z >= 4 && z <= 5 {
				continue
			}
			if !srv.SetBlock(x, 64, z, stone) || !srv.SetBlock(x, 65, z, stone) {
				t.Fatal("the pool wall's chunk was not loaded")
			}
		}
	}
}

// TestACompleteConduitCageStrikesTheWaterborneZombie checks the attack a
// complete cage keeps up: against a zombie held in a pool within the
// beam's eight blocks the conduit picks its target on the refresh tick,
// strikes it with magic every forty ticks after, and lets it go the moment
// the frame falls a cell short — the target cleared and the clients told,
// no strike to follow.
// vanilla: ConduitBlockEntity.updateAndAttackTarget
func TestACompleteConduitCageStrikesTheWaterborneZombie(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig())
	tick := driveConduitTicks(t, srv)
	activate := servertest.ConduitSoundID(t, "minecraft:block.conduit.activate")
	attack := servertest.ConduitSoundID(t, "minecraft:block.conduit.attack.target")
	magic := registry.MustID("minecraft:damage_type", "minecraft:magic")

	c := connect()
	c.JoinAs("Actor")

	cage := conduitFullCage
	if len(cage) != 42 {
		t.Fatalf("the cage is built of %d cells, want 42", len(cage))
	}
	const cx, cy, cz = 5, 64, 0
	buildConduitCage(t, srv, "", cx, cy, cz, cage)

	buildConduitPool(t, srv)
	// One zombie in the pool; its own pass makes its water contact current
	// before the hunt reads it.
	zombie := srv.SpawnMob("minecraft:zombie", 10.5, 63, 4.5)
	if zombie == 0 {
		t.Fatal("the zombie never spawned")
	}
	zombieUUID, ok := srv.MobUUID(zombie)
	if !ok {
		t.Fatal("the zombie walks under no UUID")
	}

	// The conduit into the cage's middle, the actor back out of the
	// plaza's way afterwards.
	c.MoveToGround(cx+0.5, 64, cz+3.5, true)
	placeConduit(t, c, srv, "", cx, cy, cz)
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()

	// Thirty-nine driven ticks: the hunt has not begun.
	phaseConduitAge(t, srv)
	var before conduitTell
	for range 39 {
		tick(1)
		c.SettleAndCollect(conduitTellRecorder{t: t, tell: &before, stateSound: activate,
			attackSound: attack, victim: zombie}.record)
	}
	if before.state != 0 || before.strikes != 0 || len(before.damage) != 0 || before.datas != 0 {
		t.Fatalf("the conduit spoke before the refresh tick: %+v", before)
	}

	// The fortieth tick: the target picked and the beam struck — four of
	// magic, which vanilla's bypasses-armor list lets walk straight through
	// the zombie's own two points — and the beam's target named to the
	// clients through the conduit's block entity.
	tick(1)
	tells := collectConduitTells(t, []*servertest.Client{c}, activate, attack, zombie)
	tell := tells[0]
	if tell.state != 1 {
		t.Errorf("the refresh's activation sound: %d heard, want 1", tell.state)
	}
	if tell.strikes != 1 {
		t.Errorf("the first refresh struck %d times, want 1", tell.strikes)
	}
	if len(tell.damage) != 1 || tell.damage[0] != magic {
		t.Errorf("the first refresh's damage events = %v (magic is %d), want one magic", tell.damage, magic)
	}
	if len(tell.healths) != 1 || tell.healths[0] != 16 {
		t.Errorf("the zombie's health after the first strike, from metadata %v, want [16]", tell.healths)
	}
	if tell.datas != 1 {
		t.Errorf("the beam's target was told to the clients %d times, want 1", tell.datas)
	}
	if len(tell.targets) != 1 || tell.targets[0] != uuid.MustParse(zombieUUID) {
		t.Errorf("the block entity data named target %v, want the zombie's %v", tell.targets, zombieUUID)
	}
	if target := conduitTarget(t, srv, cx, cy, cz); target != uuid.MustParse(zombieUUID) {
		t.Errorf("the conduit's block entity Target = %v, want the zombie's %v", target, zombieUUID)
	}

	// Forty more: the strike again, the target kept — no new block entity
	// update, for the beam still names this zombie.
	tick(40)
	tells = collectConduitTells(t, []*servertest.Client{c}, activate, attack, zombie)
	if tell = tells[0]; tell.strikes != 1 || len(tell.damage) != 1 || len(tell.healths) != 1 || tell.healths[0] != 12 {
		t.Errorf("the second refresh: %d strikes, damage %v, health %v, want one strike leaving 12",
			tell.strikes, tell.damage, tell.healths)
	}
	if tell.datas != 0 {
		t.Errorf("the kept target was told again: %d block entity updates, want 0", tell.datas)
	}

	// A frame block fewer and forty ticks on: the hunt is over — no
	// strike lands and one more block entity update names nobody.
	srv.SetBlock(cx-2, cy, cz, 0)
	tick(40)
	tells = collectConduitTells(t, []*servertest.Client{c}, activate, attack, zombie)
	if tell = tells[0]; tell.strikes != 0 || len(tell.damage) != 0 {
		t.Errorf("the frame's missing cell still struck: %d attack sounds, damage %v", tell.strikes, tell.damage)
	}
	if tell.datas != 1 {
		t.Errorf("the ended hunt told %d beam updates, want 1 (the target cleared)", tell.datas)
	}
	if target := conduitTarget(t, srv, cx, cy, cz); target != uuid.Nil {
		t.Errorf("the cleared hunt still names a target: %v", target)
	}
}

// TestAConduitLeavesAZombieNautilusAlone checks that the beam picks Enemy
// mobs only: a zombie nautilus in the pool is in the monster category but
// not an Enemy, while a zombie beside it is struck on the next refresh.
// vanilla: ConduitBlockEntity.selectNewTarget
func TestAConduitLeavesAZombieNautilusAlone(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig())
	tick := driveConduitTicks(t, srv)
	activate := servertest.ConduitSoundID(t, "minecraft:block.conduit.activate")
	attack := servertest.ConduitSoundID(t, "minecraft:block.conduit.attack.target")

	c := connect()
	c.JoinAs("Actor")

	const cx, cy, cz = 5, 64, 0
	buildConduitCage(t, srv, "", cx, cy, cz, conduitFullCage)
	buildConduitPool(t, srv)
	nautilus := srv.SpawnMob("minecraft:zombie_nautilus", 10.5, 63, 4.5)
	if nautilus == 0 {
		t.Fatal("the zombie nautilus never spawned")
	}

	c.MoveToGround(cx+0.5, 64, cz+3.5, true)
	placeConduit(t, c, srv, "", cx, cy, cz)
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()

	phaseConduitAge(t, srv)
	var quiet conduitTell
	for range 3 * 40 {
		tick(1)
		c.SettleAndCollect(conduitTellRecorder{t: t, tell: &quiet, stateSound: activate,
			attackSound: attack}.record)
	}
	if quiet.strikes != 0 || len(quiet.damage) != 0 || len(quiet.healths) != 0 {
		t.Fatalf("the beam hit the zombie nautilus: %d strikes, damage %v, health %v",
			quiet.strikes, quiet.damage, quiet.healths)
	}

	zombie := srv.SpawnMob("minecraft:zombie", 11.5, 63, 5.5)
	if zombie == 0 {
		t.Fatal("the zombie never spawned")
	}
	tick(40)
	tells := collectConduitTells(t, []*servertest.Client{c}, activate, attack, zombie)
	if tells[0].strikes != 1 {
		t.Errorf("the refresh after the zombie arrived struck %d times, want 1", tells[0].strikes)
	}
}

// TestAConduitPowersSwimmersInTheEnd checks the conduit beyond the
// overworld: the same cage planted on the end's island, the swimmer
// standing in a column of water beside it, and Conduit Power arriving on
// the refresh tick the same way it does at home.
func TestAConduitPowersSwimmersInTheEnd(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig())
	tick := driveConduitTicks(t, srv)
	activate := servertest.ConduitSoundID(t, "minecraft:block.conduit.activate")

	c := connect()
	id := c.JoinAs("Camper")

	// The move across and the chunk stream read; the island's ground is
	// under the arrival.
	arrival := world.EndArrival()
	srv.MovePlayerToDimension("Camper", world.EndName,
		float64(arrival.X)+0.5, float64(arrival.Y), float64(arrival.Z)+0.5)
	c.Settle()

	// The cage one cell up and three south of the arrival — waiting for
	// the island's chunks first — and the swimmer the arrival itself,
	// standing in the water its own two cells were given.
	water := servertest.BlockState(t, "minecraft:water")
	servertest.WaitFor(t, srv, 200, func() bool {
		return srv.SetBlockInDimension(world.EndName, arrival.X, arrival.Y, arrival.Z+3, water)
	})
	buildConduitCage(t, srv, world.EndName, arrival.X, arrival.Y+1, arrival.Z+3, conduitRing)
	if !srv.SetBlockInDimension(world.EndName, arrival.X, arrival.Y, arrival.Z, water) ||
		!srv.SetBlockInDimension(world.EndName, arrival.X, arrival.Y+1, arrival.Z, water) {
		t.Fatal("the swimmer's water was not placed in the end")
	}

	// The swimmer into their column, the conduit into the cage's middle —
	// close enough for the client's own reach.
	c.MoveToGround(float64(arrival.X)+0.5, float64(arrival.Y), float64(arrival.Z)+0.5, true)
	placeConduit(t, c, srv, world.EndName, arrival.X, arrival.Y+1, arrival.Z+3)
	c.Settle()

	// Thirty-nine driven ticks of nothing, and the fortieth the same
	// activation and effect the overworld's got.
	phaseConduitAge(t, srv)
	var before conduitTell
	for range 39 {
		tick(1)
		c.SettleAndCollect(conduitTellRecorder{t: t, tell: &before, stateSound: activate}.record)
	}
	if before.state != 0 || len(before.effects) != 0 {
		t.Fatalf("the end's conduit spoke before the refresh tick: %+v", before)
	}
	tick(1)
	tells := collectConduitTells(t, []*servertest.Client{c}, activate, 0, 0)
	if tell := tells[0]; tell.state != 1 || len(tell.effects) != 1 || tell.effects[0] != (servertest.MobEffect{
		Entity:    id,
		Effect:    registry.MustMobEffect("minecraft:conduit_power"),
		Amplifier: 0,
		Duration:  260,
		Flags:     0x01 | 0x02 | 0x04 | 0x08,
	}) {
		t.Errorf("the end's refresh: %d state sounds, effects %+v, want the activation and one conduit power",
			tell.state, tell.effects)
	}
}

// walledPool fills a two-by-two, two-deep water pool at x, z and walls it in
// stone above the ground, so a mob put in it stays a waterborne body where it
// was spawned.
func walledPool(t *testing.T, srv *server.Server, x, z int32) {
	t.Helper()
	stone, water := servertest.BlockState(t, "minecraft:stone"), servertest.BlockState(t, "minecraft:water")
	for px := x; px <= x+1; px++ {
		for pz := z; pz <= z+1; pz++ {
			if !srv.SetBlock(px, 63, pz, water) || !srv.SetBlock(px, 64, pz, water) {
				t.Fatal("the pool's chunk was not loaded")
			}
		}
	}
	for px := x - 1; px <= x+2; px++ {
		for pz := z - 1; pz <= z+2; pz++ {
			if px >= x && px <= x+1 && pz >= z && pz <= z+1 {
				continue
			}
			if !srv.SetBlock(px, 64, pz, stone) || !srv.SetBlock(px, 65, pz, stone) {
				t.Fatal("the pool wall's chunk was not loaded")
			}
		}
	}
}

// TestAConduitHuntsTheCornerOfItsCube checks that the hunt draws its fresh
// target from the eight-block cube, not the sphere a kept target must stay
// inside: a zombie seven blocks out on x and z, past the sphere but inside
// the box, is picked and struck.
// vanilla: ConduitBlockEntity.selectNewTarget, AABB(pos).inflate(8)
func TestAConduitHuntsTheCornerOfItsCube(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig())
	tick := driveConduitTicks(t, srv)
	attack := servertest.ConduitSoundID(t, "minecraft:block.conduit.attack.target")

	c := connect()
	c.JoinAs("Actor")

	const cx, cy, cz = 5, 64, 0
	buildConduitCage(t, srv, "", cx, cy, cz, conduitFullCage)
	// The pool seven blocks out on x and z: the zombie's box meets the
	// cube, whose edge stands eight blocks off the conduit's own box, while
	// the sphere the kept target is measured against does not reach it.
	walledPool(t, srv, 12, 7)
	zombie := srv.SpawnMob("minecraft:zombie", 12.5, 63, 7.5)
	if zombie == 0 {
		t.Fatal("the zombie never spawned")
	}
	zombieUUID, ok := srv.MobUUID(zombie)
	if !ok {
		t.Fatal("the zombie walks under no UUID")
	}
	if rules.ConduitReaches(cx, cy, cz, 12.5, 63, 7.5, rules.ConduitHuntRange) {
		t.Fatal("the corner zombie stands within the sphere; the test no longer proves the cube")
	}

	c.MoveToGround(cx+0.5, 64, cz+3.5, true)
	placeConduit(t, c, srv, "", cx, cy, cz)
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()

	phaseConduitAge(t, srv)
	tick(40)
	tell := collectConduitTells(t, []*servertest.Client{c}, 0, attack, zombie)[0]
	if tell.strikes != 1 {
		t.Errorf("the refresh struck the corner zombie %d times, want 1", tell.strikes)
	}
	if target := conduitTarget(t, srv, cx, cy, cz); target != uuid.MustParse(zombieUUID) {
		t.Errorf("the hunt's target = %v, want the corner zombie's %v", target, zombieUUID)
	}
}

// TestACompleteConduitCageKillsItsTarget checks that the beam can kill: five
// strikes of four of magic run a zombie's twenty points out, and the refresh
// after finds it dead, clears the target and strikes nobody.
// vanilla: ConduitBlockEntity.updateAndAttackTarget
func TestACompleteConduitCageKillsItsTarget(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig())
	tick := driveConduitTicks(t, srv)
	attack := servertest.ConduitSoundID(t, "minecraft:block.conduit.attack.target")

	c := connect()
	c.JoinAs("Actor")

	const cx, cy, cz = 5, 64, 0
	buildConduitCage(t, srv, "", cx, cy, cz, conduitFullCage)
	walledPool(t, srv, 10, 4)
	zombie := srv.SpawnMob("minecraft:zombie", 10.5, 63, 4.5)
	if zombie == 0 {
		t.Fatal("the zombie never spawned")
	}

	c.MoveToGround(cx+0.5, 64, cz+3.5, true)
	placeConduit(t, c, srv, "", cx, cy, cz)
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()

	// Five refreshes: twenty points of magic leaving none. The mob pass
	// takes the body away, and the sixth refresh finds the target dead.
	phaseConduitAge(t, srv)
	var last conduitTell
	for range 5 {
		tick(40)
		last = *collectConduitTells(t, []*servertest.Client{c}, 0, attack, zombie)[0]
	}
	if last.strikes != 1 || len(last.healths) != 1 || last.healths[0] != 0 {
		t.Fatalf("the killing refresh: %d strikes, health %v, want one strike leaving 0",
			last.strikes, last.healths)
	}
	tick(40)
	tell := collectConduitTells(t, []*servertest.Client{c}, 0, attack, zombie)[0]
	if tell.strikes != 0 || len(tell.damage) != 0 {
		t.Errorf("the hunt struck a dead target: %d attack sounds, damage %v", tell.strikes, tell.damage)
	}
	if target := conduitTarget(t, srv, cx, cy, cz); target != uuid.Nil {
		t.Errorf("the hunt still names the killed target: %v", target)
	}
}

// TestAConduitsTargetSurvivesARestart checks the hunt's Target against the
// save: the UUID it names is written into the chunk and read back by the
// server that opens it again.
// vanilla: ConduitBlockEntity.saveAdditional
func TestAConduitsTargetSurvivesARestart(t *testing.T) {
	t.Parallel()
	cfg := servertest.BlockTestConfig()
	cfg.LevelName = t.TempDir()

	srv, connect := servertest.Start(t, cfg)
	tick := driveConduitTicks(t, srv)

	c := connect()
	c.JoinAs("Actor")

	const cx, cy, cz = 5, 64, 0
	buildConduitCage(t, srv, "", cx, cy, cz, conduitFullCage)
	walledPool(t, srv, 10, 4)
	zombie := srv.SpawnMob("minecraft:zombie", 10.5, 63, 4.5)
	if zombie == 0 {
		t.Fatal("the zombie never spawned")
	}
	zombieUUID, ok := srv.MobUUID(zombie)
	if !ok {
		t.Fatal("the zombie walks under no UUID")
	}

	c.MoveToGround(cx+0.5, 64, cz+3.5, true)
	placeConduit(t, c, srv, "", cx, cy, cz)
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()

	phaseConduitAge(t, srv)
	tick(40)
	if target := conduitTarget(t, srv, cx, cy, cz); target != uuid.MustParse(zombieUUID) {
		t.Fatalf("the hunt named %v before the save, want %v", target, zombieUUID)
	}
	srv.FlushSaves()
	srv.Shutdown()

	restarted := *cfg
	srv2, c2, _ := servertest.JoinAs(t, &restarted, "Actor")
	c2.Settle()
	if target := conduitTarget(t, srv2, cx, cy, cz); target != uuid.MustParse(zombieUUID) {
		t.Errorf("after the restart the conduit's Target = %v, want %v", target, zombieUUID)
	}
}

// TestAnActiveConduitPlaysItsAmbientSound checks the two ambient sounds an
// active frame keeps up: the first short ambient, due as soon as the conduit
// is active, and the pulse on the eighty-tick line.
// vanilla: ConduitBlockEntity.serverTick
func TestAnActiveConduitPlaysItsAmbientSound(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig())
	tick := driveConduitTicks(t, srv)
	ambient := servertest.ConduitSoundID(t, "minecraft:block.conduit.ambient")
	short := servertest.ConduitSoundID(t, "minecraft:block.conduit.ambient.short")

	c := connect()
	c.JoinAs("Actor")

	const cx, cy, cz = 5, 64, 0
	buildConduitCage(t, srv, "", cx, cy, cz, conduitRing)
	c.MoveToGround(cx+0.5, 64, cz+3.5, true)
	placeConduit(t, c, srv, "", cx, cy, cz)
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()

	// The age on an eighty-tick line: the activation refresh forty ticks
	// later plays the first short ambient, the pulse falling on the line
	// eighty ticks on.
	age, _ := srv.Time()
	srv.SetWorldAge(age - age%80)
	recorder := func(tell *conduitTell) conduitTellRecorder {
		return conduitTellRecorder{t: t, tell: tell, ambientSound: ambient, shortAmbientSound: short}
	}

	var shortTell conduitTell
	tick(40)
	c.SettleAndCollect(recorder(&shortTell).record)
	if shortTell.shortAmbient != 1 || shortTell.ambient != 0 {
		t.Errorf("the first refresh: %d pulses, %d shorts, want the short alone",
			shortTell.ambient, shortTell.shortAmbient)
	}
	var pulseTell conduitTell
	tick(40)
	c.SettleAndCollect(recorder(&pulseTell).record)
	if pulseTell.ambient != 1 || pulseTell.shortAmbient != 0 {
		t.Errorf("the second refresh: %d pulses, %d shorts, want the pulse alone",
			pulseTell.ambient, pulseTell.shortAmbient)
	}
}

// TestAnActiveConduitRollsItsShortAmbient checks the short ambient's roll: it
// is due as soon as the conduit is active, then sixty to ninety-nine ticks
// on, not on the eighty-tick stride the pulse keeps.
// vanilla: ConduitBlockEntity.serverTick
func TestAnActiveConduitRollsItsShortAmbient(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig())
	tick := driveConduitTicks(t, srv)
	ambient := servertest.ConduitSoundID(t, "minecraft:block.conduit.ambient")
	short := servertest.ConduitSoundID(t, "minecraft:block.conduit.ambient.short")

	c := connect()
	c.JoinAs("Actor")

	const cx, cy, cz = 5, 64, 0
	buildConduitCage(t, srv, "", cx, cy, cz, conduitRing)
	c.MoveToGround(cx+0.5, 64, cz+3.5, true)
	placeConduit(t, c, srv, "", cx, cy, cz)
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()

	// One tick short of an eighty-tick line: the activation refresh lands on
	// the line, where the fixed short-of-the-stride would be silent and only
	// the pulse would play.
	srv.SetWorldAge(79)
	recorder := func(tell *conduitTell) conduitTellRecorder {
		return conduitTellRecorder{t: t, tell: tell, ambientSound: ambient, shortAmbientSound: short}
	}

	var first conduitTell
	tick(1)
	c.SettleAndCollect(recorder(&first).record)
	if first.ambient != 1 || first.shortAmbient != 1 {
		t.Fatalf("the activation tick: %d pulses, %d shorts, want the first short with the pulse",
			first.ambient, first.shortAmbient)
	}

	// The next short is due sixty ticks on at the earliest, so the
	// fifty-nine after this one are silent.
	var quiet conduitTell
	tick(59)
	c.SettleAndCollect(recorder(&quiet).record)
	if quiet.shortAmbient != 0 {
		t.Errorf("a short ambient came inside the sixty-tick floor: %d heard", quiet.shortAmbient)
	}

	// And ninety-nine at the latest: the fortieth tick past that carries it.
	var rolled conduitTell
	tick(40)
	c.SettleAndCollect(recorder(&rolled).record)
	if rolled.shortAmbient != 1 {
		t.Errorf("the next short ambient never came: %d heard within the ninety-nine-tick ceiling", rolled.shortAmbient)
	}
}

// TestAConduitHuntsAMobRainedOnAtTheTopOfItsBox checks the hunt's rain gate
// reads the whole box, vanilla's isInRain: a zombie standing on a slab, its
// feet cell dry under the slab but its head out in the rain, is picked and
// struck.
// vanilla: ConduitBlockEntity.selectNewTarget, Entity.isInWaterOrRain
func TestAConduitHuntsAMobRainedOnAtTheTopOfItsBox(t *testing.T) {
	t.Parallel()
	srv, connect := servertest.Start(t, servertest.BlockTestConfig())
	tick := driveConduitTicks(t, srv)
	attack := servertest.ConduitSoundID(t, "minecraft:block.conduit.attack.target")

	c := connect()
	c.JoinAs("Actor")

	const cx, cy, cz = 5, 64, 0
	buildConduitCage(t, srv, "", cx, cy, cz, conduitFullCage)

	// A slab floor under the zombie, walled two high so it stays: the slab
	// fills the feet cell, so the rain reaches the box's top alone.
	slab, stone := servertest.BlockState(t, "minecraft:oak_slab"), servertest.BlockState(t, "minecraft:stone")
	for x := int32(10); x <= 11; x++ {
		for z := int32(4); z <= 5; z++ {
			if !srv.SetBlock(x, 64, z, slab) {
				t.Fatal("the slab's chunk was not loaded")
			}
		}
	}
	for x := int32(9); x <= 12; x++ {
		for z := int32(3); z <= 6; z++ {
			if x >= 10 && x <= 11 && z >= 4 && z <= 5 {
				continue
			}
			if !srv.SetBlock(x, 65, z, stone) || !srv.SetBlock(x, 66, z, stone) {
				t.Fatal("the wall's chunk was not loaded")
			}
		}
	}
	zombie := srv.SpawnMob("minecraft:zombie", 10.5, 64.5, 4.5)
	if zombie == 0 {
		t.Fatal("the zombie never spawned")
	}
	zombieUUID, ok := srv.MobUUID(zombie)
	if !ok {
		t.Fatal("the zombie walks under no UUID")
	}

	// Rain over the basin, above its threshold before the hunt reads it.
	srv.SetWeatherState(true, false, 100000, 100000, 0)
	for range 20 {
		srv.TickWeather()
	}
	if !srv.Raining() {
		t.Fatal("the world is not raining")
	}

	c.MoveToGround(cx+0.5, 64, cz+3.5, true)
	placeConduit(t, c, srv, "", cx, cy, cz)
	c.MoveToGround(0.5, 64, 0.5, true)
	c.Settle()

	phaseConduitAge(t, srv)
	tick(40)
	tell := collectConduitTells(t, []*servertest.Client{c}, 0, attack, zombie)[0]
	if tell.strikes != 1 {
		t.Errorf("the refresh struck the rained-on zombie %d times, want 1", tell.strikes)
	}
	if target := conduitTarget(t, srv, cx, cy, cz); target != uuid.MustParse(zombieUUID) {
		t.Errorf("the hunt's target = %v, want the rained-on zombie's %v", target, zombieUUID)
	}
}
