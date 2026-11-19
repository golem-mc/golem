package combat

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/registry"
)

// The firework rockets as the wire carries them: a rocket a player uses on
// a block launches from the face clicked, a gliding player's use launches
// one attached to them, and the burst a rocket's flight ends in tells the
// Entity Event the client draws it from and hurts the bodies around. The
// numbers are vanilla's, read off the FireworkRocketEntity and
// FireworkRocketItem bytecode: the launch at the click location and 0.15 of
// a block along the face's normal, the rise of a twentieth of a block a
// tick, and the life of ten ticks a flight count plus up to eleven more.

// useItemOnFace uses the held item against the block given, aimed at the
// cursor within it the way a client's click is, on the face named.
func useItemOnFace(c *servertest.Client, x, y, z, face int32, cursorX, cursorY, cursorZ float32) {
	c.T.Helper()
	c.Send(protocol.IDPlayUseItemOn, func(b *protocol.Buffer) {
		b.PutVarInt(protocol.HandMain)
		b.PutPosition(x, y, z)
		b.PutVarInt(face)
		b.PutF32(cursorX)
		b.PutF32(cursorY)
		b.PutF32(cursorZ)
		b.PutBool(false) // not inside the block clicked
		b.PutBool(false) // and not against the world border
		b.PutVarInt(1)   // the block change sequence
	})
}

// fireworkMetadata is one Set Entity Metadata packet a firework's ownfield
// or a pose came in.
type fireworkMetadata struct {
	entityID  int32
	item      *protocol.ItemStack
	attached  int32 // the entity the rocket rides, 0 where none was told
	shotAngle bool
	pose      int32
}

// fireworkTraffic is what the wire carried while a rocket lived, as the
// thrown traffic readers collect it plus the firework's own fields.
type fireworkTraffic struct {
	sounds       []servertest.ThrownSound
	spawns       []servertest.ThrownSpawn
	metas        []fireworkMetadata
	healths      []servertest.ThrownHealth
	damageEvents []servertest.ThrownDamageEvent
	hurtAnims    []servertest.ThrownEvent
	teleports    []servertest.ThrownTeleport
	motions      []servertest.ThrownVelocity
	positions    []servertest.ThrownPosition
	events       []servertest.ThrownEvent
	removes      [][]int32
	healthPings  []float64
}

// readFireworkTraffic collects everything the wire carries while the given
// ticks run, the packets a firework's life and the hurts of its burst
// are made of. The metadata fields a rocket carries — its item slot, the
// entity it is attached to, the pose of a gliding player — are read whole;
// anything else a packet is stepped over by the shape its type names.
func readFireworkTraffic(c *servertest.Client, srv *server.Server, ticks int) *fireworkTraffic {
	c.T.Helper()
	traffic := &fireworkTraffic{}
	c.ReadTicks(srv, ticks, func(gotID int32, data []byte) bool {
		switch gotID {
		case protocol.IDPlaySoundEffect:
			r := protocol.NewReader(data)
			wireID := r.ReadVarInt()
			category := r.ReadVarInt()
			x := float64(r.ReadI32()) / 8
			y := float64(r.ReadI32()) / 8
			z := float64(r.ReadI32()) / 8
			volume := r.ReadF32()
			pitch := r.ReadF32()
			name := ""
			if wireID > 0 {
				if n, ok := servertest.SoundEventName(wireID - 1); ok {
					name = n
				}
			}
			traffic.sounds = append(traffic.sounds, servertest.ThrownSound{
				Name: name, Category: category,
				X: x, Y: y, Z: z, Volume: volume, Pitch: pitch,
			})
		case protocol.IDPlaySetHealth:
			r := protocol.NewReader(data)
			health := r.ReadF32()
			traffic.healthPings = append(traffic.healthPings, float64(health))
		case protocol.IDPlaySpawnEntity:
			traffic.spawns = append(traffic.spawns, servertest.ReadThrownSpawn(c, data))
		case protocol.IDPlayEntityMetadata:
			r := protocol.NewReader(data)
			id := r.ReadVarInt()
			m := fireworkMetadata{entityID: id, pose: -1, attached: -1}
			for r.Err() == nil {
				index := r.ReadU8()
				if index == protocol.MetadataEnd {
					break
				}
				fieldType := r.ReadVarInt()
				switch {
				case index == protocol.MetadataIndexItemSlot && fieldType == protocol.MetadataSlot:
					stack, ok := r.ReadSlot()
					if !ok {
						c.T.Fatalf("entity %d's item slot did not read back", id)
					}
					m.item = &stack
				case index == protocol.MetadataIndexFireworkAttachedTo && fieldType == protocol.MetadataOptionalVarInt:
					m.attached = r.ReadVarInt() - 1
				case index == protocol.MetadataIndexFireworkShotAtAngle && fieldType == protocol.MetadataBoolean:
					m.shotAngle = r.ReadBool()
				case index == protocol.MetadataIndexPose && fieldType == protocol.MetadataPose:
					m.pose = r.ReadVarInt()
				case index == protocol.MetadataIndexLivingHealth && fieldType == protocol.MetadataFloat:
					traffic.healths = append(traffic.healths, servertest.ThrownHealth{Id: id, Health: r.ReadF32()})
				default:
					servertest.SkipMetadataField(c, r, fieldType)
				}
			}
			if r.Err() != nil {
				c.T.Fatalf("entity metadata: %v", r.Err())
			}
			traffic.metas = append(traffic.metas, m)
		case protocol.IDPlayDamageEvent:
			r := protocol.NewReader(data)
			id := r.ReadVarInt()
			damageType := r.ReadVarInt()
			traffic.damageEvents = append(traffic.damageEvents, servertest.ThrownDamageEvent{Id: id, DamageType: damageType})
		case protocol.IDPlayHurtAnimation:
			r := protocol.NewReader(data)
			id := r.ReadVarInt()
			traffic.hurtAnims = append(traffic.hurtAnims, servertest.ThrownEvent{Id: id})
		case protocol.IDPlayTeleportEntity:
			r := protocol.NewReader(data)
			id := r.ReadVarInt()
			r.ReadVarInt() // position path
			x, y, z := r.ReadF64(), r.ReadF64(), r.ReadF64()
			traffic.teleports = append(traffic.teleports, servertest.ThrownTeleport{Id: id, X: x, Y: y, Z: z})
		case protocol.IDPlayEntityPosition:
			r := protocol.NewReader(data)
			id := r.ReadVarInt()
			onGround := r.ReadVarInt()&1 != 0
			dx, dy, dz := r.ReadI16(), r.ReadI16(), r.ReadI16()
			traffic.positions = append(traffic.positions, servertest.ThrownPosition{
				Id: id, Dx: int32(dx), Dy: int32(dy), Dz: int32(dz), OnGround: onGround,
			})
		case protocol.IDPlayEntityPositionRot:
			r := protocol.NewReader(data)
			id := r.ReadVarInt()
			onGround := r.ReadVarInt()&1 != 0
			dx, dy, dz := r.ReadI16(), r.ReadI16(), r.ReadI16()
			r.ReadU8() // yaw
			r.ReadU8() // pitch
			traffic.positions = append(traffic.positions, servertest.ThrownPosition{
				Id: id, Dx: int32(dx), Dy: int32(dy), Dz: int32(dz), OnGround: onGround,
			})
		case protocol.IDPlaySetEntityMotion:
			r := protocol.NewReader(data)
			id := r.ReadVarInt()
			vx, vy, vz := r.ReadLpVec3()
			traffic.motions = append(traffic.motions, servertest.ThrownVelocity{Id: id, Vx: vx, Vy: vy, Vz: vz})
		case protocol.IDPlayEntityEvent:
			r := protocol.NewReader(data)
			id := r.ReadI32()
			status := r.ReadU8()
			traffic.events = append(traffic.events, servertest.ThrownEvent{Id: id, Status: status})
		case protocol.IDPlayRemoveEntities:
			r := protocol.NewReader(data)
			count := r.ReadVarInt()
			var ids []int32
			for range count {
				ids = append(ids, r.ReadVarInt())
			}
			traffic.removes = append(traffic.removes, ids)
		}
		return false
	})
	return traffic
}

// fireworkOf finds the one rocket that came into the world.
func (tr *fireworkTraffic) fireworkOf() *servertest.ThrownSpawn {
	for i := range tr.spawns {
		if tr.spawns[i].EntityType == protocol.EntityTypeFireworkRocket {
			return &tr.spawns[i]
		}
	}
	return nil
}

// metasOf keeps the firework's own metadata packets one body was told.
func (tr *fireworkTraffic) metasOf(id int32) []fireworkMetadata {
	var out []fireworkMetadata
	for _, m := range tr.metas {
		if m.entityID == id {
			out = append(out, m)
		}
	}
	return out
}

// posesOf keeps the poses a body's metadata arrived in.
func (tr *fireworkTraffic) posesOf(id int32) []int32 {
	var out []int32
	for _, m := range tr.metas {
		if m.entityID == id && m.pose >= 0 {
			out = append(out, m.pose)
		}
	}
	return out
}

// removedOnce reports the body taken out of the world exactly once.
func (tr *fireworkTraffic) removedOnce(id int32) bool {
	times := 0
	for _, ids := range tr.removes {
		for _, gone := range ids {
			if gone == id {
				times++
			}
		}
	}
	return times == 1
}

// eventsOf keeps the entity events one body was named in.
func (tr *fireworkTraffic) eventsOf(id int32) []servertest.ThrownEvent {
	var out []servertest.ThrownEvent
	for _, e := range tr.events {
		if e.Id == id {
			out = append(out, e)
		}
	}
	return out
}

// healthsOf keeps the health floats one living body was told of.
func (tr *fireworkTraffic) healthsOf(id int32) []float32 {
	var out []float32
	for _, h := range tr.healths {
		if h.Id == id {
			out = append(out, h.Health)
		}
	}
	return out
}

// TestAFireworkRocketLaunchesFromTheFaceItIsUsedOn uses a rocket on the
// ground and reads the launch whole: the Spawn Entity naming the launcher
// in its data field, the rocket born at the click point and 0.15 of a block
// off the face's normal, rising at the vanilla twentieth of a block a tick,
// the launch sound the rocket's first tick plays, the stack the metadata
// says it is — and, when its flight duration runs out, the burst's event
// and the body out of the world. A survival hand pays one rocket for the
// launch.
func TestAFireworkRocketLaunchesFromTheFaceItIsUsedOn(t *testing.T) {
	t.Parallel()
	srv, c, playerID := servertest.JoinAs(t, servertest.BowTestConfig(), "Launcher")
	srv.GiveSlot("Launcher", 36, protocol.ItemStack{
		ItemID: servertest.MustItem("minecraft:firework_rocket"), Count: 2})

	// The plain rocket: clicked on the top of a block beside the player,
	// at the cursor's own place within the block, the face's normal 0.15
	// of a block over. The item's own default firework is the
	// one flight duration, so the whole life is under thirty-one ticks.
	servertest.HoldTicks(t, srv)
	useItemOnFace(c, 3, 63, 1, protocol.FaceTop, 0.5, 0.9, 0.5)

	traffic := readFireworkTraffic(c, srv, 60)
	rocket := traffic.fireworkOf()
	if rocket == nil {
		t.Fatal("no firework rocket came into the world for the use")
	}
	if len(traffic.spawns) != 1 {
		t.Fatalf("the use spawned %d entities, want the one rocket", len(traffic.spawns))
	}
	if rocket.OwnerID != playerID {
		t.Errorf("the rocket's data field names entity %d, want the launcher's %d", rocket.OwnerID, playerID)
	}
	// The birth: the click point and 0.15 off the clicked face.
	if d := rocket.X - 3.5; d < -0.01 || d > 0.01 {
		t.Errorf("the rocket was born at x=%v, want the cursor's 3.5", rocket.X)
	}
	if d := rocket.Y - (63 + 0.9 + 0.15); d < -0.01 || d > 0.01 {
		t.Errorf("the rocket was born at y=%v, want the click and the 0.15 off the top face", rocket.Y)
	}
	if d := rocket.Z - 1.5; d < -0.01 || d > 0.01 {
		t.Errorf("the rocket was born at z=%v, want the cursor's 1.5", rocket.Z)
	}
	// The birth's velocity: the vanilla twentieth of a block a tick up,
	// and the two random fifths of a hundredth sideways at the most.
	if d := rocket.Vy - 0.05; d < -0.001 || d > 0.001 {
		t.Errorf("the rocket rises at vy=%v, want the vanilla twentieth of a block", rocket.Vy)
	}
	if rocket.Vx > 0.005 || rocket.Vx < -0.005 || rocket.Vz > 0.005 || rocket.Vz < -0.005 {
		t.Errorf("the rocket leans sideways at (%v %v), want the triangle's own small way", rocket.Vx, rocket.Vz)
	}
	// The launch sound the rocket's own first tick plays: the rocket's
	// own place — within the eighth of a block the wire's sound
	// coordinates floor to — three blocks loud, the ambient source, the
	// pitch of one.
	var launchSound *servertest.ThrownSound
	for i := range traffic.sounds {
		if traffic.sounds[i].Name == "minecraft:entity.firework_rocket.launch" {
			launchSound = &traffic.sounds[i]
			break
		}
	}
	if launchSound == nil {
		t.Fatal("no firework launch sound was played")
	}
	if launchSound.Category != protocol.SoundCategoryAmbient {
		t.Errorf("the launch sound's category = %d, want the ambient", launchSound.Category)
	}
	if d := launchSound.X - 3.5; d < -0.13 || d > 0.13 {
		t.Errorf("the launch sound played from x=%v, want the rocket's own place", launchSound.X)
	}
	if d := launchSound.Y - 64.05; d < -0.3 || d > 0.3 {
		t.Errorf("the launch sound played from y=%v, want the rocket's first tick of rise", launchSound.Y)
	}
	if launchSound.Volume != 3.0 {
		t.Errorf("the launch sound's volume = %v, want three blocks", launchSound.Volume)
	}
	if launchSound.Pitch != 1.0 {
		t.Errorf("the launch sound's pitch = %v, want the one", launchSound.Pitch)
	}

	// The stack the rocket is: one rocket, told with the spawn, attached
	// to nobody and no crossbow's shot at an angle.
	var slot *protocol.ItemStack
	attached := int32(-1)
	shot := false
	for _, m := range traffic.metasOf(rocket.EntityID) {
		if m.item != nil {
			slot = m.item
		}
		if m.attached >= 0 {
			attached = m.attached
		}
		shot = shot || m.shotAngle
	}
	if slot == nil {
		t.Fatal("no metadata told the rocket's item")
	}
	if slot.Count != 1 || slot.ItemID != servertest.MustItem("minecraft:firework_rocket") {
		t.Errorf("the rocket's stack is %d of item %d, want one firework rocket", slot.Count, slot.ItemID)
	}
	if attached != -1 {
		t.Errorf("the rocket's metadata named entity %d as attached, want nobody", attached)
	}
	if shot {
		t.Error("the launched rocket was told as shot at an angle")
	}

	// The burst: the event the client draws the bang from, the body out
	// of the world, each the once.
	events := traffic.eventsOf(rocket.EntityID)
	if len(events) != 1 || events[0].Status != protocol.EntityStatusFireworkBurst {
		t.Fatalf("the rocket's entity events = %#v, want the burst's one of status seventeen", events)
	}
	if !traffic.removedOnce(rocket.EntityID) {
		t.Error("the rocket was not taken out of the world exactly once")
	}
	// It flew before it went: the flight told the client of at least
	// once, in a position packet or a teleport.
	told := false
	for _, p := range traffic.positions {
		if p.Id == rocket.EntityID {
			told = true
		}
	}
	for _, p := range traffic.teleports {
		if p.Id == rocket.EntityID {
			told = true
		}
	}
	if !told {
		t.Error("the rocket's flight was never told to anyone")
	}

	// The survival hand paid one rocket of its two.
	if left := srv.InventorySlot("Launcher", 36); left.Count != 1 {
		t.Errorf("the hotbar holds %d rockets after the launch, want the one it left", left.Count)
	}
}

// TestAGlidingPlayerUsesARocketAttachedToThemselves puts the player in
// the elytra's fall flying state in the air and uses a rocket there: the
// rocket spawns at the player's own position attached to them — the
// metadata's optional entity naming the player — and its burst, when the
// flight runs out, hurts the gliding player their full five and two a
// star and the cow standing below by the distance's falloff share, both
// with the fireworks damage type behind the hurt.
func TestAGlidingPlayerUsesARocketAttachedToThemselves(t *testing.T) {
	t.Parallel()
	srv, c, playerID := servertest.JoinAs(t, servertest.BowTestConfig(), "Glider")
	cowID := srv.SpawnMob("minecraft:cow", 2, 64, 0.5)
	srv.SetMobNoAI(cowID)

	// The wings go on from the hand: the use that equips them is answered
	// by the slot syncs before the hotbar slot they leave is given over
	// to the rocket.
	srv.GiveSlot("Glider", 36, protocol.ItemStack{ItemID: servertest.MustItem("minecraft:elytra"), Count: 1})
	servertest.UseHeld(c, -90, 0)
	c.AwaitPlay(protocol.IDPlayContainerSlot)
	c.AwaitPlay(protocol.IDPlayContainerSlot)
	srv.GiveSlot("Glider", 36, servertest.FireworkStack(0, 1))

	// A flight-duration rocket of one explosion: the life is ten ticks
	// and up to eleven more, and the burst asks five and two a star.
	c.MoveToGrounded(0.5, 66, 0.5, -90, 10, false)
	servertest.HoldTicks(t, srv)
	servertest.StartFallFlying(c)
	servertest.UseHeld(c, -90, 10)

	traffic := readFireworkTraffic(c, srv, 60)
	// The player's wings opened: the pose metadata told the fall flying
	// pose among its own.
	gliding := false
	for _, pose := range traffic.posesOf(playerID) {
		if pose == protocol.PoseFallFlying {
			gliding = true
		}
	}
	if !gliding {
		t.Error("the player was never told into the fall flying pose")
	}

	rocket := traffic.fireworkOf()
	if rocket == nil {
		t.Fatal("no firework rocket came into the world for the gliding use")
	}
	if rocket.OwnerID != playerID {
		t.Errorf("the rocket's data field names entity %d, want the glider's %d", rocket.OwnerID, playerID)
	}
	// The boost rocket is born at the body it attaches to.
	if d := rocket.X - 0.5; d < -0.01 || d > 0.01 {
		t.Errorf("the rocket was born at x=%v, want the glider's own", rocket.X)
	}
	if d := rocket.Y - 66; d < -0.01 || d > 0.01 {
		t.Errorf("the rocket was born at y=%v, want the glider's own", rocket.Y)
	}
	if d := rocket.Z - 0.5; d < -0.01 || d > 0.01 {
		t.Errorf("the rocket was born at z=%v, want the glider's own", rocket.Z)
	}
	// The attachment the metadata tells: the rocket rides the player.
	attached := int32(-1)
	for _, m := range traffic.metasOf(rocket.EntityID) {
		if m.attached >= 0 {
			attached = m.attached
		}
	}
	if attached != playerID {
		t.Errorf("the rocket's attachment names entity %d, want the glider's %d", attached, playerID)
	}

	// The burst: the event, the removal, and the hurts. The glider takes
	// the full seven; the cow, the share its distance leaves it.
	events := traffic.eventsOf(rocket.EntityID)
	if len(events) != 1 || events[0].Status != protocol.EntityStatusFireworkBurst {
		t.Fatalf("the rocket's entity events = %#v, want the burst's one", events)
	}
	if !traffic.removedOnce(rocket.EntityID) {
		t.Error("the rocket was not taken out of the world exactly once")
	}
	// The glider's hurt: the seven the burst asks, read from the Set
	// Health the hurt sends. What the vitals read afterwards may have
	// regenerated some of it back within the read, so it is the packet
	// the burst answers with that carries the number.
	lowest := -1.0
	for _, h := range traffic.healthPings {
		if lowest < 0 || h < lowest {
			lowest = h
		}
	}
	if lowest < 0 {
		t.Fatal("the glider's own health was never told")
	}
	if d := lowest - 13; d > 0.01 || d < -0.01 {
		t.Errorf("the glider's lowest health = %v, want the thirteen the seven burst left of twenty", lowest)
	}
	if h := srv.PlayerHealth("Glider"); h < 13 {
		t.Errorf("the glider's health = %v, want nothing below the burst's own thirteen", h)
	}
	if healths := traffic.healthsOf(cowID); len(healths) == 0 {
		t.Error("the cow's health was never told after the burst")
	} else {
		if last := healths[len(healths)-1]; last >= 10 {
			t.Errorf("the cow's latest health was told %v, want the falloff's share off it", last)
		}
	}
	fireworksType := registry.MustID("minecraft:damage_type", "minecraft:fireworks")
	for _, victim := range []struct {
		id  int32
		who string
	}{{playerID, "the glider"}, {cowID, "the cow"}} {
		hurt := false
		for _, ev := range traffic.damageEvents {
			if ev.Id == victim.id && ev.DamageType == fireworksType {
				hurt = true
			}
		}
		if !hurt {
			t.Errorf("no fireworks damage event named %s", victim.who)
		}
	}
	// The rocket the survival hand used was spent.
	if left := srv.InventorySlot("Glider", 36); left.Count != 0 {
		t.Errorf("the hotbar still holds %d rockets, want the one use to spend it", left.Count)
	}
}

// TestAStartedGlideSetsTheFallFlyingFlag sends the fall-flying player
// command to a gliding player in the air and reads the shared flags byte
// back: the 0x80 fall-flying bit, which is what opens the wings on the
// client and what vanilla's stopFallFlying clears to put them back.
func TestAStartedGlideSetsTheFallFlyingFlag(t *testing.T) {
	t.Parallel()
	srv, c, playerID := servertest.JoinAs(t, servertest.BowTestConfig(), "Glider")

	srv.GiveSlot("Glider", 36, protocol.ItemStack{ItemID: servertest.MustItem("minecraft:elytra"), Count: 1})
	servertest.UseHeld(c, -90, 0)
	c.AwaitPlay(protocol.IDPlayContainerSlot)
	c.AwaitPlay(protocol.IDPlayContainerSlot)

	c.MoveToGrounded(0.5, 66, 0.5, -90, 10, false)
	servertest.HoldTicks(t, srv)
	servertest.StartFallFlying(c)

	flags := uint8(0)
	seen := false
	c.ReadTicks(srv, 40, func(id int32, data []byte) bool {
		if id != protocol.IDPlayEntityMetadata {
			return false
		}
		m := servertest.ReadBurningMetadata(t, data)
		if m.EntityID == playerID {
			flags = m.Flags
			seen = true
		}
		return false
	})
	if !seen {
		t.Fatal("no metadata for the glider after the wings were asked to open")
	}
	if flags&protocol.EntityFlagFlying == 0 {
		t.Errorf("the glider's shared flags = %#02x, want the fall-flying bit %#02x", flags, protocol.EntityFlagFlying)
	}
}

// TestAGlideEndsWhenTheGliderBoardsABoat seats a gliding player in a boat:
// a passenger may not glide, so the next tick clears the fall-flying bit.
// vanilla: LivingEntity.canGlide (isPassenger), LivingEntity.updateFallFlying
func TestAGlideEndsWhenTheGliderBoardsABoat(t *testing.T) {
	t.Parallel()
	srv, c, playerID := servertest.JoinAs(t, servertest.BowTestConfig(), "Glider")

	srv.GiveSlot("Glider", 36, protocol.ItemStack{ItemID: servertest.MustItem("minecraft:elytra"), Count: 1})
	servertest.UseHeld(c, -90, 0)
	c.AwaitPlay(protocol.IDPlayContainerSlot)
	c.AwaitPlay(protocol.IDPlayContainerSlot)

	c.MoveToGrounded(0.5, 66, 0.5, -90, 10, false)
	servertest.HoldTicks(t, srv)
	servertest.StartFallFlying(c)

	isGliding := func(id int32, data []byte) bool {
		if id != protocol.IDPlayEntityMetadata {
			return false
		}
		m := servertest.ReadBurningMetadata(t, data)
		return m.EntityID == playerID && m.Flags&protocol.EntityFlagFlying != 0
	}
	if !c.ReadTicks(srv, 40, isGliding) {
		t.Fatal("the glide never started")
	}

	boat := srv.SpawnBoat(1.5, 64, 0.5, "minecraft:oak_boat")
	c.UseOn(boat)

	flags := uint8(0)
	seen := false
	c.ReadTicks(srv, 40, func(id int32, data []byte) bool {
		if id != protocol.IDPlayEntityMetadata {
			return false
		}
		m := servertest.ReadBurningMetadata(t, data)
		if m.EntityID == playerID {
			flags = m.Flags
			seen = true
		}
		return false
	})
	if got := srv.BoatPassengerIDs(boat); len(got) != 1 || got[0] != playerID {
		t.Fatalf("the boat holds %v, want the glider aboard", got)
	}
	if !seen {
		t.Fatal("no metadata for the glider after boarding")
	}
	if flags&protocol.EntityFlagFlying != 0 {
		t.Errorf("the seated glider's shared flags = %#02x, want the fall-flying bit cleared", flags)
	}
}

// TestAFireworkBurstBreaksAnArmorStandInReach glides past a stand and uses
// a starred rocket: the burst breaks the visible stand outright — the
// fireworks damage type is one of is_explosion — the way a blast does.
func TestAFireworkBurstBreaksAnArmorStandInReach(t *testing.T) {
	t.Parallel()
	srv, c, _ := servertest.JoinAs(t, servertest.BowTestConfig(), "Glider", servertest.OpsListing(t, "Glider"))

	c.ChatCommand("summon minecraft:armor_stand 1.5 64 0.5")
	c.ExpectArmorStandSpawn()

	srv.GiveSlot("Glider", 36, protocol.ItemStack{ItemID: servertest.MustItem("minecraft:elytra"), Count: 1})
	servertest.UseHeld(c, -90, 0)
	c.AwaitPlay(protocol.IDPlayContainerSlot)
	c.AwaitPlay(protocol.IDPlayContainerSlot)
	srv.GiveSlot("Glider", 36, servertest.FireworkStack(0, 1))

	c.MoveToGrounded(0.5, 66, 0.5, -90, 10, false)
	servertest.HoldTicks(t, srv)
	servertest.StartFallFlying(c)
	servertest.UseHeld(c, -90, 10)

	traffic := readFireworkTraffic(c, srv, 60)
	if rocket := traffic.fireworkOf(); rocket == nil {
		t.Fatal("no firework rocket came into the world for the gliding use")
	}
	if n := srv.ArmorStandCount(); n != 0 {
		t.Errorf("%d armor stands remain after the burst, want the one in reach broken", n)
	}
}

// glidingElytraClient logs in a player in mid-air with a fresh elytra on
// the chest and opens the wings.
func glidingElytraClient(t *testing.T, name string, setup ...func(*server.Server)) (*server.Server, *servertest.Client) {
	t.Helper()
	srv, c := elytraClient(t, name, setup...)
	servertest.StartFallFlying(c)
	return srv, c
}

// heldGlidingElytraClient is glidingElytraClient with the loop held before
// the wings open, so no glide tick runs before the test counts its own.
func heldGlidingElytraClient(t *testing.T, name string, setup ...func(*server.Server)) (*server.Server, *servertest.Client) {
	t.Helper()
	srv, c := elytraClient(t, name, setup...)
	servertest.HoldTicks(t, srv)
	servertest.StartFallFlying(c)
	return srv, c
}

// elytraClient logs in a player in mid-air with a fresh elytra on the chest.
func elytraClient(t *testing.T, name string, setup ...func(*server.Server)) (*server.Server, *servertest.Client) {
	t.Helper()
	srv, c, _ := servertest.JoinAs(t, servertest.BowTestConfig(), name, setup...)
	srv.GiveSlot(name, 36, protocol.ItemStack{ItemID: servertest.MustItem("minecraft:elytra"), Count: 1})
	servertest.UseHeld(c, -90, 0)
	c.AwaitPlay(protocol.IDPlayContainerSlot)
	c.AwaitPlay(protocol.IDPlayContainerSlot)
	c.MoveToGrounded(0.5, 66, 0.5, -90, 10, false)
	return srv, c
}

// vanilla: LivingEntity.updateFallFlying, Entity.checkFallDistanceAccumulation
func TestAGentleElytraLandingDoesNotDealFallDamage(t *testing.T) {
	t.Parallel()
	srv, c := heldGlidingElytraClient(t, "Glider")
	c.MoveToGrounded(0.5, 90, 0.5, -90, 10, false)
	c.Settle()
	for step := 1; step <= 80; step++ {
		x, y := 0.5+float64(step), 90-0.3*float64(step)
		c.MoveToGrounded(x, y, 0.5, -90, 10, false)
		c.Settle()
		servertest.RunTicks(t, srv, 1)
		if !srv.PlayerFallFlying("Glider") {
			t.Fatal("glide ended before landing")
		}
		if distance := srv.PlayerFallDistance("Glider"); distance > 1.3 {
			t.Fatalf("fall distance after step %d = %g, want <= 1.3", step, distance)
		}
	}
	c.MoveToGrounded(80.5, 65.7, 0.5, -90, 10, true)
	c.Settle()
	if health := srv.PlayerHealth("Glider"); health != 20 {
		t.Errorf("health after gentle landing = %g, want 20", health)
	}
}

// TestAGlidingElytraWearsOneEveryTwentyTicks pins the interval: none before
// the first twenty ticks are out, then one point.
// vanilla: LivingEntity.updateFallFlying
func TestAGlidingElytraWearsOneEveryTwentyTicks(t *testing.T) {
	t.Parallel()
	srv, c := heldGlidingElytraClient(t, "Glider")
	since := srv.TicksRun()
	c.PumpTicksSince(srv, since, 10, func(int32, []byte) bool { return false })
	if d := srv.InventorySlot("Glider", 6).Damage; d != 0 {
		t.Errorf("elytra damage after 10 ticks of glide = %d, want 0", d)
	}
	c.PumpTicksSince(srv, since, 30, func(int32, []byte) bool { return false })
	if d := srv.InventorySlot("Glider", 6).Damage; d != 1 {
		t.Errorf("elytra damage after 30 ticks of glide = %d, want 1", d)
	}
}

// TestLevitationEndsAGlide gives Levitation to a gliding player: the wings
// close at the next tick.
// vanilla: LivingEntity.canGlide
func TestLevitationEndsAGlide(t *testing.T) {
	t.Parallel()
	srv, c := glidingElytraClient(t, "Glider")
	c.PumpTicks(srv, 2, func(int32, []byte) bool { return false })
	if !srv.PlayerFallFlying("Glider") {
		t.Fatal("the glide did not start")
	}
	srv.GiveEffect("Glider", "minecraft:levitation", 200, 0, false)
	c.PumpTicks(srv, 3, func(int32, []byte) bool { return false })
	if srv.PlayerFallFlying("Glider") {
		t.Error("the glide went on under Levitation")
	}
}
