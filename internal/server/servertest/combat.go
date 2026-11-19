package servertest

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// AttackSounds names the player attack sounds by sound event ID.
var AttackSounds = func() map[int32]string {
	m := map[int32]string{}
	for _, s := range []string{"crit", "knockback", "nodamage", "strong", "sweep", "weak"} {
		name := "minecraft:entity.player.attack." + s
		id, ok := registry.SoundEventID(name)
		if !ok {
			panic("the sound registry has no " + name)
		}
		m[id] = s
	}
	return m
}()

// BlowHeard is what a client heard of a blow on one target.
type BlowHeard struct {
	Animations []uint8  // Entity Animation packets about the target
	Sounds     []string // the attack sounds, by their last name part
	Particles  []int32  // Level Particles types
}

// HearBlow collects what the client has been sent up to now.
func (c *Client) HearBlow(targetID int32) BlowHeard {
	c.T.Helper()
	var h BlowHeard
	c.SettleAndCollect(func(id int32, data []byte) {
		r := protocol.NewReader(data)
		switch id {
		case protocol.IDPlayEntityAnimation:
			if r.ReadVarInt() == targetID {
				h.Animations = append(h.Animations, r.ReadU8())
			}
		case protocol.IDPlaySoundEffect:
			name, ok := AttackSounds[r.ReadVarInt()-1]
			if !ok {
				return
			}
			h.Sounds = append(h.Sounds, name)
			if cat := r.ReadVarInt(); cat != protocol.SoundCategoryPlayer {
				c.T.Errorf("the %s sound's category = %d, want players", name, cat)
			}
			r.ReadI32()
			r.ReadI32()
			r.ReadI32()
			if vol, pitch := r.ReadF32(), r.ReadF32(); vol != 1 || pitch != 1 {
				c.T.Errorf("the %s sound at volume %v pitch %v, want 1 and 1", name, vol, pitch)
			}
		case protocol.IDPlayLevelParticles:
			h.Particles = append(h.Particles, r.ReadVarInt())
		}
	})
	return h
}

// MustItemID is the registry ID of the named item, failing the test when the
// registry does not know it.
func MustItemID(t *testing.T, name string) int32 {
	t.Helper()
	id, ok := registry.ItemID(name)
	if !ok {
		t.Fatalf("the item registry has no %s", name)
	}
	return id
}

// MetadataEntityID is the entity a Set Entity Metadata packet is about.
func MetadataEntityID(data []byte) int32 {
	return protocol.NewReader(data).ReadVarInt()
}

// ItemStackMetadata reads the item stack a Set Entity Metadata packet names,
// the field a spawned item's introducing packet carries; a packet about some
// other kind of body reads as the empty stack.
func ItemStackMetadata(data []byte) protocol.ItemStack {
	r := protocol.NewReader(data)
	r.ReadVarInt() // the entity, which the stack's own arrival names
	stack := protocol.ItemStack{}
	for r.Err() == nil {
		index := r.ReadU8()
		if index == protocol.MetadataEnd {
			break
		}
		fieldType := r.ReadVarInt()
		if index == protocol.MetadataIndexItemSlot && fieldType == protocol.MetadataSlot {
			stack, _ = r.ReadSlot()
			continue
		}
		StepOverMetadataValue(r, fieldType)
	}
	return stack
}

// BlastLeftItem reports whether an item list holds a stack of the named item.
func BlastLeftItem(items []server.TestItem, name string) bool {
	for _, it := range items {
		if it.Item == name {
			return true
		}
	}
	return false
}

// CombatTestConfig is the configuration the combat tests run in: survival,
// the mode a fight makes sense in.
func CombatTestConfig() *config.Config {
	cfg := config.Default()
	cfg.OnlineMode = false
	return cfg
}

// SwordDamage is what one whole swing of the diamond sword carries: the
// fist's one plus the blade's own.
func SwordDamage() float64 {
	sword, ok := registry.ItemID("minecraft:diamond_sword")
	if !ok {
		panic("the item registry has no diamond sword")
	}
	return 1.0 + float64(registry.AttackDamage(sword))
}

// ExpectMobHealth reads the health the next Set Entity Metadata packet
// carries for the mob named, waiting past the background a fight's client
// hears.
func ExpectMobHealth(c *Client, mobID int32) float32 {
	c.T.Helper()
	var m SpawnedMob
	m.EntityID = mobID
	c.ReadMobMetadataInto(&m, c.ExpectMobPacket(protocol.IDPlayEntityMetadata))
	return m.Health
}

// Strike swings at the mob and reads the hurt it answers with, in the order
// the server says it — the damage event, the flinch of the entity event,
// the tilt of the hurt animation, the shove of the velocity and the health
// left on the metadata — returning that health.
func (c *Client) Strike(mobID, attackerID int32) float32 {
	c.T.Helper()
	c.Attack(mobID)
	c.ExpectMobDamageEvent(mobID, "minecraft:player_attack", attackerID+1)
	hurtAnim := protocol.NewReader(c.ExpectMobPacket(protocol.IDPlayHurtAnimation))
	if got := hurtAnim.ReadVarInt(); got != mobID {
		c.T.Errorf("hurt animation for entity %d, want %d", got, mobID)
	}
	hurtAnim.ReadF32()
	motion := protocol.NewReader(c.ExpectMobPacket(protocol.IDPlaySetEntityMotion))
	if got := motion.ReadVarInt(); got != mobID {
		c.T.Errorf("shove for entity %d, want %d", got, mobID)
	}
	motion.ReadLpVec3()
	return ExpectMobHealth(c, mobID)
}

// StrikeFresh plants a cow of its own at the spot named and strikes it
// once, returning the health the blow left. A blow of its own cow keeps
// every blow's arithmetic against a full ten — no blow in these tests
// kills, so no death's packets thread between the reads.
func (c *Client) StrikeFresh(srv *server.Server, x, z float64, attackerID int32) float32 {
	c.T.Helper()
	mobID := srv.SpawnMob("minecraft:cow", x, 64, z)
	c.ExpectMobSpawn(protocol.EntityTypeCow)
	return c.Strike(mobID, attackerID)
}

// SwingRecovered is the AttackTicks a whole swing's recovery has wound past:
// a sword's twelve and a half ticks, so the count at thirteen reads the
// swing wound up.
const SwingRecovered = 13

// AwaitFullSwing waits until the named player's attack has recovered past
// the slowest weapon here, a sword's twelve and a half ticks, so the next
// blow lands at full strength - the wait a fresh join or a held item
// change starts from the bottom of, timed on the server's own ticks
// rather than the wall clock.
func AwaitFullSwing(t *testing.T, srv *server.Server, name string) {
	t.Helper()
	// Two whole ticks first: the tick that sees a blade just given and
	// spends the recovery for it may not have run yet, and a count read
	// before it would let a hurried blow through on a slow machine.
	since := srv.TicksRun()
	WaitFor(t, srv, 200, func() bool {
		return srv.TicksRun() >= since+2 && srv.AttackTicks(name) >= SwingRecovered
	})
}

// RecoverByHand runs the stopped loop's survival and mob passes until the
// named player's swing has wound back up, so the mob being struck moves only
// in the ticks a recovery takes. At least one pass runs: only a survival pass
// sees a weapon just given, resetting the swing and applying its attack
// damage, and a slow join can leave the count past a swing already.
func RecoverByHand(t *testing.T, srv *server.Server, name string) {
	t.Helper()
	for i := 0; i == 0 || srv.AttackTicks(name) < SwingRecovered; i++ {
		if i == 2*SwingRecovered {
			t.Fatalf("twenty-six hand ticks left %s's swing at %d, want it wound past %d",
				name, srv.AttackTicks(name), SwingRecovered)
		}
		srv.TickSurvival()
		srv.TickMobs(1)
	}
}

// SwingLanded waits for the server to have taken the blow just sent: the
// attack spends the recovery AwaitFullSwing wound up, and the ticker's drop
// below a whole swing is the sign. It is for a stopped loop, where nothing
// winds the swing back up before the look; on a running one, AttackLanded.
func SwingLanded(t *testing.T, srv *server.Server, name string) {
	t.Helper()
	WaitFor(t, srv, 200, func() bool { return srv.AttackTicks(name) < SwingRecovered })
}

// AttackLanded sends c's attack on target, after AwaitFullSwing, and waits
// for the server to have taken the blow, on a running loop: a free loop can
// run the thirteen ticks that wind a spent swing whole again before
// SwingLanded's first look. Unspent, the recovery winds one a tick in step
// with the tick count, so its lead over the count only falls when a blow
// resets it.
func (c *Client) AttackLanded(srv *server.Server, name string, target int32) {
	c.T.Helper()
	ticks0, swing0 := swingClock(srv, name)
	lead0 := int64(swing0) - int64(ticks0)
	c.Attack(target)
	// Handled before the ticks are counted: on a held loop they run faster
	// than a loaded connection reads.
	c.Settle()
	WaitFor(c.T, srv, 200, func() bool {
		ticks, swing := swingClock(srv, name)
		// A reading is within one of the steady lead (the recovery winds
		// in the tick, the count at its end), and a reset drops the lead
		// by the whole recovery wound before it, at least SwingRecovered.
		return int64(swing)-int64(ticks) < lead0-2
	})
}

// swingClock reads the ticks run and the named player's AttackTicks as one
// pair, read again if a tick ended between the two.
func swingClock(srv *server.Server, name string) (ticks uint64, swing int32) {
	for {
		ticks = srv.TicksRun()
		swing = srv.AttackTicks(name)
		if srv.TicksRun() == ticks {
			return ticks, swing
		}
	}
}

// MonsterMetadata is one Set Entity Metadata packet's reading of a
// monster's state: the health of the body, the Mob flags, the spider's
// climbing byte and the creeper's swell direction and its charged and
// ignited booleans, each only where the packet carried them.
type MonsterMetadata struct {
	EntityID   int32
	health     float32
	hasHealth  bool
	Flags      uint8
	HasFlags   bool
	Climbing   bool
	HasClimb   bool
	Swell      int32
	HasSwell   bool
	Ignited    bool
	HasIgnite  bool
	powered    bool
	hasPowered bool
}

// ReadMonsterMetadata parses a Set Entity Metadata packet, taking the
// fields a spider or a creeper carries.
func ReadMonsterMetadata(c *Client, data []byte) MonsterMetadata {
	c.T.Helper()
	r := protocol.NewReader(data)
	m := MonsterMetadata{EntityID: r.ReadVarInt()}
	for r.Err() == nil {
		index := r.ReadU8()
		if index == protocol.MetadataEnd {
			break
		}
		fieldType := r.ReadVarInt()
		switch {
		case index == protocol.MetadataIndexLivingHealth:
			if fieldType != protocol.MetadataFloat {
				c.T.Fatalf("health field type = %d, want %d (a float)", fieldType, protocol.MetadataFloat)
			}
			m.health = r.ReadF32()
			m.hasHealth = true
		case index == protocol.MetadataIndexMobFlags:
			if fieldType != protocol.MetadataByte {
				c.T.Fatalf("mob flags field type = %d, want %d (a byte)", fieldType, protocol.MetadataByte)
			}
			m.Flags = r.ReadU8()
			m.HasFlags = true
		case index == protocol.MetadataIndexSpiderClimbing && fieldType == protocol.MetadataByte:
			// The spider's climbing byte, the index the creeper's swell
			// shares with a different type behind it.
			m.Climbing = r.ReadU8()&protocol.EntityFlagClimbing != 0
			m.HasClimb = true
		case index == protocol.MetadataIndexCreeperSwell && fieldType == protocol.MetadataVarInt:
			m.Swell = r.ReadVarInt()
			m.HasSwell = true
		case index == protocol.MetadataIndexCreeperPowered && fieldType == protocol.MetadataBoolean:
			m.powered = r.ReadBool()
			m.hasPowered = true
		case index == protocol.MetadataIndexCreeperIgnited && fieldType == protocol.MetadataBoolean:
			m.Ignited = r.ReadU8() != 0
			m.HasIgnite = true
		default:
			c.T.Fatalf("unexpected metadata field %d of type %d", index, fieldType)
		}
	}
	if err := r.Err(); err != nil {
		c.T.Fatalf("monster metadata: %v", err)
	}
	if r.Remaining() != 0 {
		c.T.Errorf("monster metadata has %d bytes left over", r.Remaining())
	}
	return m
}

// Explosion is one Explosion packet's reading, the fields this server sends
// and vanilla's own carry.
type Explosion struct {
	X, Y, Z    float64
	Radius     float32
	BlockCount int32
	Pushed     bool
	KX, KY, KZ float64
	// PlaySound is the packet's trailing flag.
	PlaySound bool
}

// ReadExplosion parses an Explosion packet by this repository's layout,
// leftovers and all.
func ReadExplosion(c *Client, data []byte) Explosion {
	c.T.Helper()
	r := protocol.NewReader(data)
	e := Explosion{X: r.ReadF64(), Y: r.ReadF64(), Z: r.ReadF64(), Radius: r.ReadF32()}
	e.BlockCount = r.ReadI32()
	e.Pushed = r.ReadBool()
	if e.Pushed {
		e.KX, e.KY, e.KZ = r.ReadF64(), r.ReadF64(), r.ReadF64()
	}
	// The particle the radius picks: the small blast's one burst below
	// two, the emitter from there. vanilla: ServerExplosion.isSmall
	wantParticle := int32(protocol.ParticleExplosionEmitter)
	if e.Radius < 2 {
		wantParticle = protocol.ParticleExplosion
	}
	if got := r.ReadVarInt(); got != wantParticle {
		c.T.Errorf("explosion particle = %d, want the radius's %d", got, wantParticle)
	}
	if got := r.ReadVarInt(); got != protocol.SoundEventGenericExplode {
		c.T.Errorf("explosion sound = %d, want the generic explode ID %d", got, protocol.SoundEventGenericExplode)
	}
	if got := r.ReadVarInt(); got != 2 {
		c.T.Errorf("block particle alternatives = %d, want the vanilla two", got)
	}
	for range 2 {
		r.ReadVarInt() // particle ID
		r.ReadF32()    // scaling
		r.ReadF32()    // Speed
		r.ReadVarInt() // weight
	}
	e.PlaySound = r.ReadBool()
	if err := r.Err(); err != nil {
		c.T.Fatalf("explosion packet: %v", err)
	}
	if r.Remaining() != 0 {
		c.T.Errorf("explosion packet has %d bytes left over", r.Remaining())
	}
	return e
}

// FishingMeta is one Set Entity Metadata packet the hook's own two fields
// came in: the body it is caught in — one on the wire past the ID — and
// the bite, each told only where the packet carried it.
type FishingMeta struct {
	Id         int32
	Hooked     int32
	HookedTold bool
	Biting     bool
	BitingTold bool
}

// FishingRead collects the wire for n ticks, keeping the traffic a
// hook's life is made of — the sounds, the spawns, the metadata of the
// two fields, the motions, the entity events and the removals — and
// stepping over the rest.
func FishingRead(c *Client, srv *server.Server, n int) (sounds []ThrownSound, spawns []ThrownSpawn,
	metas []FishingMeta, motions []ThrownVelocity, events []ThrownEvent, removes [][]int32) {
	c.T.Helper()
	c.ReadTicks(srv, n, func(gotID int32, data []byte) bool {
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
				if n, ok := SoundEventName(wireID - 1); ok {
					name = n
				}
			}
			sounds = append(sounds, ThrownSound{
				Name: name, Category: category,
				X: x, Y: y, Z: z, Volume: volume, Pitch: pitch,
			})
		case protocol.IDPlaySpawnEntity:
			spawns = append(spawns, ReadThrownSpawn(c, data))
		case protocol.IDPlayEntityMetadata:
			r := protocol.NewReader(data)
			id := r.ReadVarInt()
			meta := FishingMeta{Id: id}
			for r.Err() == nil {
				index := r.ReadU8()
				if index == protocol.MetadataEnd {
					break
				}
				fieldType := r.ReadVarInt()
				switch {
				case index == protocol.MetadataIndexHookedEntity && fieldType == protocol.MetadataVarInt:
					meta.Hooked = r.ReadVarInt() - 1
					meta.HookedTold = true
				case index == protocol.MetadataIndexBite && fieldType == protocol.MetadataBoolean:
					meta.Biting = r.ReadBool()
					meta.BitingTold = true
				default:
					SkipMetadataField(c, r, fieldType)
				}
			}
			metas = append(metas, meta)
		case protocol.IDPlaySetEntityMotion:
			r := protocol.NewReader(data)
			id := r.ReadVarInt()
			vx, vy, vz := r.ReadLpVec3()
			motions = append(motions, ThrownVelocity{Id: id, Vx: vx, Vy: vy, Vz: vz})
		case protocol.IDPlayEntityEvent:
			r := protocol.NewReader(data)
			id := r.ReadI32()
			status := r.ReadU8()
			events = append(events, ThrownEvent{Id: id, Status: status})
		case protocol.IDPlayRemoveEntities:
			r := protocol.NewReader(data)
			count := r.ReadVarInt()
			var ids []int32
			for range count {
				ids = append(ids, r.ReadVarInt())
			}
			removes = append(removes, ids)
		}
		return false
	})
	return sounds, spawns, metas, motions, events, removes
}

// SpawnsOf keeps the spawns of the entity type named.
func SpawnsOf(spawns []ThrownSpawn, entityType int32) []ThrownSpawn {
	var out []ThrownSpawn
	for _, s := range spawns {
		if s.EntityType == entityType {
			out = append(out, s)
		}
	}
	return out
}

// HearShieldSound reads a Sound Effect down to its events and reports
// whether one of them was the named sound, with the volume and pitch it
// played at.
func (c *Client) HearShieldSound(data []byte, id int32) (heard bool, volume, pitch float32) {
	c.T.Helper()
	r := protocol.NewReader(data)
	if got := r.ReadVarInt(); got != id+1 {
		return false, 0, 0
	}
	_ = r.ReadVarInt() // Category
	for range 3 {
		r.ReadI32()
	}
	return true, r.ReadF32(), r.ReadF32()
}

// StrikeMobVelocity swings at the mob and returns the x and z of the shove
// the hit broadcasts, read from the Set Entity Motion.
func (c *Client) StrikeMobVelocity(mobID, attackerID int32) (float64, float64) {
	c.T.Helper()
	c.Attack(mobID)
	c.ExpectMobDamageEvent(mobID, "minecraft:player_attack", attackerID+1)
	c.ExpectMobPacket(protocol.IDPlayHurtAnimation)
	r := protocol.NewReader(c.ExpectMobPacket(protocol.IDPlaySetEntityMotion))
	r.ReadVarInt() // the entity the shove names
	vx, _, vz := r.ReadLpVec3()
	ExpectMobHealth(c, mobID)
	return vx, vz
}

// StrikeMobMotion is StrikeMobVelocity's horizontal speed alone.
func (c *Client) StrikeMobMotion(mobID, attackerID int32) float64 {
	c.T.Helper()
	vx, vz := c.StrikeMobVelocity(mobID, attackerID)
	return math.Hypot(vx, vz)
}

// NextPlayerMotion reads the victim's next Set Entity Motion and returns its
// horizontal speed, the shove's own x and z.
func (c *Client) NextPlayerMotion() float64 {
	c.T.Helper()
	for id, data := range c.Packets(ExpectTimeout) {
		if id != protocol.IDPlaySetEntityMotion {
			continue
		}
		r := protocol.NewReader(data)
		r.ReadVarInt() // the entity the shove names
		vx, _, vz := r.ReadLpVec3()
		return math.Hypot(vx, vz)
	}
	return -1
}

// SweepHeard is what one client was told about the bodies one blow landed
// on: the damage events by the body each named, the hurt statuses, the
// shoves, and the health a hurting body's metadata left it at.
type SweepHeard struct {
	DamageEvents map[int32]int32 // victim → cause ID (attacker + 1)
	HurtEvents   map[int32]bool
	Motions      map[int32][]float64
	Healths      map[int32]float32
}

// HearBodies collects the whole blow's tells — the stricken victim's own
// and every swept bystander's — into one SweepHeard. The metadata the
// health is read off is recognized by its leading field; a spawn's own
// metadata starts anywhere else and is stepped over.
func (c *Client) HearBodies() SweepHeard {
	c.T.Helper()
	heard := SweepHeard{
		DamageEvents: map[int32]int32{},
		HurtEvents:   map[int32]bool{},
		Motions:      map[int32][]float64{},
		Healths:      map[int32]float32{},
	}
	c.SettleAndCollect(func(id int32, data []byte) {
		c.T.Helper()
		r := protocol.NewReader(data)
		switch id {
		case protocol.IDPlayDamageEvent:
			victim := r.ReadVarInt()
			r.ReadVarInt() // the damage type, a player attack everywhere here
			heard.DamageEvents[victim] = r.ReadVarInt()
			heard.HurtEvents[victim] = true
		case protocol.IDPlayHurtAnimation:
			r.ReadVarInt() // the hurt body, whose tilt nothing here asks after
		case protocol.IDPlaySetEntityMotion:
			body := r.ReadVarInt()
			x, y, z := r.ReadLpVec3()
			heard.Motions[body] = []float64{x, y, z}
		case protocol.IDPlayEntityMetadata:
			body := r.ReadVarInt()
			if r.ReadU8() != protocol.MetadataIndexLivingHealth {
				return
			}
			if got := r.ReadVarInt(); got != protocol.MetadataFloat {
				c.T.Fatalf("health field type = %d, want %d (a float)", got, protocol.MetadataFloat)
			}
			heard.Healths[body] = r.ReadF32()
		}
	})
	return heard
}

// ThrownTraffic is what the wire carried while a thrown body lived, in the
// order it arrived: the sounds, the spawns, the metadata packets, the
// position packets, the entity events and the removals, each read into the
// fields a test asks after.
type ThrownTraffic struct {
	Sounds       []ThrownSound
	Spawns       []ThrownSpawn
	metas        []ThrownMeta
	healths      []ThrownHealth
	positions    []ThrownPosition
	Motions      []ThrownVelocity
	Teleports    []ThrownTeleport
	Events       []ThrownEvent
	removes      [][]int32
	DamageEvents []ThrownDamageEvent
	hurtAnims    []ThrownEvent
}

// ThrownSpawn is one Spawn Entity packet read whole: the body's identity
// and the type the registry carries it under, where it stands and how fast
// it moves, and the owner the data field names.
type ThrownSpawn struct {
	EntityID   int32
	Uuid       uuid.UUID
	EntityType int32
	X, Y, Z    float64
	Vx, Vy, Vz float64
	yaw, Pitch float32
	OwnerID    int32
}

// ThrownSound is one Sound Effect packet: the registry name the sound's ID
// names — the wire's own IDs run one above the registry's, the way
// soundEffectAt sends them — the category, the place and the volume and
// pitch it travelled with.
type ThrownSound struct {
	Name     string
	Category int32
	X, Y, Z  float64
	Volume   float32
	Pitch    float32
}

// ThrownMeta is one Set Entity Metadata packet an item-slot field came in:
// the body it names, the count of the stack and the item it is a stack of.
type ThrownMeta struct {
	Id    int32
	Count int32
	Item  int32
}

// ThrownHealth is one Set Entity Metadata packet a health float came in:
// the living body it names and the health it was told of.
type ThrownHealth struct {
	Id     int32
	Health float32
}

// ThrownDamageEvent is one Damage Event packet: the body hurt and the
// damage type the registry carries the hurting kind under.
type ThrownDamageEvent struct {
	Id         int32
	DamageType int32
}

// ThrownPosition is one Update Entity Position packet: the deltas it
// carries and the ground it says the body stands on.
type ThrownPosition struct {
	Id         int32
	Dx, Dy, Dz int32
	OnGround   bool
}

// ThrownVelocity is one Set Entity Motion packet: the body it names and the
// velocity the packed form carries.
type ThrownVelocity struct {
	Id         int32
	Vx, Vy, Vz float64
}

// ThrownTeleport is one Teleport Entity packet: the body it names and the
// absolute place the packet puts it at.
type ThrownTeleport struct {
	Id      int32
	X, Y, Z float64
}

// ThrownEvent is one packet that names a body and one more field: the
// entity events' status, and the damage events' and hurt animations' own
// records as the hurting ones.
type ThrownEvent struct {
	Id     int32
	Status uint8
}

// ReadThrownTraffic collects everything the wire carries while the given
// ticks run (none: what is already sent; a thrown body's flight is its
// length in ticks), reading the packets a thrown body's
// life is made of and stepping over the rest — the join's own traffic, the
// other bodies' movement packets, the keep-alives a client answers.
func ReadThrownTraffic(c *Client, srv *server.Server, ticks int) *ThrownTraffic {
	c.T.Helper()
	traffic := &ThrownTraffic{}
	c.ReadTicks(srv, ticks, func(gotID int32, data []byte) bool {
		traffic.add(c, gotID, data)
		return false
	})
	return traffic
}

// PearlSync is one Synchronize Player Position packet: the teleport id, where
// it puts the player and the flags it travels under.
type PearlSync struct {
	ID         int32
	X, Y, Z    float64
	Yaw, Pitch float32
	Flags      int32
}

// PearlCooldown is one Set Cooldown packet: the cooldown group it names and the
// ticks it runs for, zero the clear the end sends.
type PearlCooldown struct {
	Group string
	Ticks int32
}

// PearlLive is the thrown body's traffic plus the packets only a pearl's
// owner is sent: the cooldown its throw starts, the teleport its landing makes
// and the health the landing's hurt leaves.
type PearlLive struct {
	*ThrownTraffic
	Syncs     []PearlSync
	Cooldowns []PearlCooldown
	Healths   []float32
}

// CooldownsOf keeps the Set Cooldown packets naming the group.
func (pl *PearlLive) CooldownsOf(group string) []PearlCooldown {
	var out []PearlCooldown
	for _, cd := range pl.Cooldowns {
		if cd.Group == group {
			out = append(out, cd)
		}
	}
	return out
}

// ReadPearlLive collects what the wire carries while the given ticks run: the
// thrown body's packets and the owner's Synchronize Player Position ones.
func ReadPearlLive(c *Client, srv *server.Server, ticks int) *PearlLive {
	c.T.Helper()
	live := &PearlLive{ThrownTraffic: &ThrownTraffic{}}
	c.ReadTicks(srv, ticks, func(gotID int32, data []byte) bool {
		switch gotID {
		case protocol.IDPlayCooldown:
			r := protocol.NewReader(data)
			group := r.ReadString(256)
			live.Cooldowns = append(live.Cooldowns, PearlCooldown{Group: group, Ticks: r.ReadVarInt()})
			return false
		case protocol.IDPlaySetHealth:
			live.Healths = append(live.Healths, protocol.NewReader(data).ReadF32())
			return false
		case protocol.IDPlaySyncPosition:
		default:
			live.add(c, gotID, data)
			return false
		}
		r := protocol.NewReader(data)
		s := PearlSync{ID: r.ReadVarInt()}
		s.X, s.Y, s.Z = r.ReadF64(), r.ReadF64(), r.ReadF64()
		r.ReadF64()
		r.ReadF64()
		r.ReadF64() // the velocity a pearl's teleport clears
		s.Yaw, s.Pitch = r.ReadF32(), r.ReadF32()
		s.Flags = r.ReadI32()
		if err := r.Err(); err != nil {
			c.T.Fatalf("synchronize player position: %v", err)
		}
		live.Syncs = append(live.Syncs, s)
		return false
	})
	return live
}

// add files one packet under the kind of record it carries; a packet of no
// thrown body's is stepped over.
func (t *ThrownTraffic) add(c *Client, gotID int32, data []byte) {
	c.T.Helper()
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
			if n, ok := SoundEventName(wireID - 1); ok {
				name = n
			}
		}
		t.Sounds = append(t.Sounds, ThrownSound{
			Name: name, Category: category,
			X: x, Y: y, Z: z, Volume: volume, Pitch: pitch,
		})
	case protocol.IDPlaySpawnEntity:
		t.Spawns = append(t.Spawns, ReadThrownSpawn(c, data))
	case protocol.IDPlayEntityMetadata:
		r := protocol.NewReader(data)
		id := r.ReadVarInt()
		for r.Err() == nil {
			index := r.ReadU8()
			if index == protocol.MetadataEnd {
				break
			}
			fieldType := r.ReadVarInt()
			if index == protocol.MetadataIndexItemSlot && fieldType == protocol.MetadataSlot {
				stack, ok := r.ReadSlot()
				if !ok {
					c.T.Fatalf("entity %d's item slot did not read back", id)
				}
				t.metas = append(t.metas, ThrownMeta{Id: id, Count: stack.Count, Item: stack.ItemID})
				continue
			}
			if index == protocol.MetadataIndexLivingHealth && fieldType == protocol.MetadataFloat {
				t.healths = append(t.healths, ThrownHealth{Id: id, Health: r.ReadF32()})
				continue
			}
			SkipMetadataField(c, r, fieldType)
		}
	case protocol.IDPlayEntityPosition:
		r := protocol.NewReader(data)
		id := r.ReadVarInt()
		onGround := r.ReadVarInt()&1 != 0
		dx, dy, dz := r.ReadI16(), r.ReadI16(), r.ReadI16()
		t.positions = append(t.positions, ThrownPosition{
			Id: id, Dx: int32(dx), Dy: int32(dy), Dz: int32(dz), OnGround: onGround,
		})
	case protocol.IDPlaySetEntityMotion:
		r := protocol.NewReader(data)
		id := r.ReadVarInt()
		vx, vy, vz := r.ReadLpVec3()
		t.Motions = append(t.Motions, ThrownVelocity{Id: id, Vx: vx, Vy: vy, Vz: vz})
	case protocol.IDPlayTeleportEntity:
		r := protocol.NewReader(data)
		id := r.ReadVarInt()
		r.ReadVarInt() // position path, LINEAR
		x, y, z := r.ReadF64(), r.ReadF64(), r.ReadF64()
		t.Teleports = append(t.Teleports, ThrownTeleport{Id: id, X: x, Y: y, Z: z})
	case protocol.IDPlayEntityEvent:
		r := protocol.NewReader(data)
		id := r.ReadI32()
		status := r.ReadU8()
		t.Events = append(t.Events, ThrownEvent{Id: id, Status: status})
	case protocol.IDPlayHurtAnimation:
		r := protocol.NewReader(data)
		id := r.ReadVarInt()
		t.hurtAnims = append(t.hurtAnims, ThrownEvent{Id: id})
	case protocol.IDPlayDamageEvent:
		r := protocol.NewReader(data)
		id := r.ReadVarInt()
		damageType := r.ReadVarInt()
		t.DamageEvents = append(t.DamageEvents, ThrownDamageEvent{Id: id, DamageType: damageType})
	case protocol.IDPlayRemoveEntities:
		r := protocol.NewReader(data)
		count := r.ReadVarInt()
		var ids []int32
		for range count {
			ids = append(ids, r.ReadVarInt())
		}
		t.removes = append(t.removes, ids)
	}
}

// ReadThrownSpawn reads one Spawn Entity packet of whatever type it
// carries, against this repository's own field layout: the body's ID and
// UUID and the type the registry carries it under, where it stands and how
// fast it moves, the angles it points, and the owner the data field names.
func ReadThrownSpawn(c *Client, data []byte) ThrownSpawn {
	c.T.Helper()
	r := protocol.NewReader(data)
	s := ThrownSpawn{}
	s.EntityID = r.ReadVarInt()
	if s.EntityID == 0 {
		c.T.Fatal("spawn entity ID = 0, want a real entity ID")
	}
	s.Uuid = r.ReadUUID()
	s.EntityType = r.ReadVarInt()
	s.X, s.Y, s.Z = r.ReadF64(), r.ReadF64(), r.ReadF64()
	s.Vx, s.Vy, s.Vz = r.ReadLpVec3()
	s.Pitch = r.ReadAngle()
	s.yaw = r.ReadAngle()
	r.ReadAngle() // head yaw, which a projectile does not carry
	s.OwnerID = r.ReadVarInt()
	if err := r.Err(); err != nil {
		c.T.Fatalf("spawn entity: %v", err)
	}
	return s
}

// SkipMetadataField steps over one entity metadata field of the type the
// header named, on the reader the field began at: the field types this
// server's own packets carry and no others, a test's loud failure being
// the honest answer to a type the reader does not know.
func SkipMetadataField(c *Client, r *protocol.Reader, fieldType int32) {
	c.T.Helper()
	switch fieldType {
	case protocol.MetadataByte, protocol.MetadataBoolean:
		r.ReadU8()
	case protocol.MetadataVarInt, protocol.MetadataBlockState, protocol.MetadataPose,
		protocol.MetadataCowVariant, protocol.MetadataCowSoundVariant, protocol.MetadataDirection,
		protocol.MetadataPigVariant, protocol.MetadataPigSoundVariant,
		protocol.MetadataChickenVariant, protocol.MetadataChickenSoundVariant,
		protocol.MetadataHumanoidArm, protocol.MetadataZombieNautilusVariant:
		r.ReadVarInt()
	case protocol.MetadataFloat:
		r.ReadF32()
	case protocol.MetadataSlot:
		if _, ok := r.ReadSlot(); !ok {
			c.T.Fatal("a metadata slot field did not read back")
		}
	case protocol.MetadataBlockPos:
		r.ReadPosition()
	case protocol.MetadataOptionalPosition:
		// A sleeper's bed: the traffic readers step over every body's
		// metadata, not only the thrown one's.
		if r.ReadBool() {
			r.ReadPosition()
		}
	default:
		c.T.Fatalf("metadata field type %d is not one the reader knows", fieldType)
	}
	if r.Err() != nil {
		c.T.Fatalf("metadata field of type %d: %v", fieldType, r.Err())
	}
}

// MetasOf keeps the metadata packets one body was told.
func (tr *ThrownTraffic) MetasOf(id int32) []ThrownMeta {
	var out []ThrownMeta
	for _, m := range tr.metas {
		if m.Id == id {
			out = append(out, m)
		}
	}
	return out
}

// PositionsOf keeps the position packets one body was told.
func (tr *ThrownTraffic) PositionsOf(id int32) []ThrownPosition {
	var out []ThrownPosition
	for _, p := range tr.positions {
		if p.Id == id {
			out = append(out, p)
		}
	}
	return out
}

// EventsOf keeps the entity events one body was named in.
func (tr *ThrownTraffic) EventsOf(id int32) []ThrownEvent {
	var out []ThrownEvent
	for _, e := range tr.Events {
		if e.Id == id {
			out = append(out, e)
		}
	}
	return out
}

// RemovalCount is how many times the body was named in a Remove Entities
// packet.
func (tr *ThrownTraffic) RemovalCount(id int32) int {
	n := 0
	for _, ids := range tr.removes {
		for _, one := range ids {
			if one == id {
				n++
			}
		}
	}
	return n
}

// RemovedOnce reports whether the body was taken out of the world exactly
// once.
func (tr *ThrownTraffic) RemovedOnce(id int32) bool {
	return tr.RemovalCount(id) == 1
}

// DamageEventsOf keeps the damage events one body was named the victim of.
func (tr *ThrownTraffic) DamageEventsOf(id int32) []ThrownDamageEvent {
	var out []ThrownDamageEvent
	for _, d := range tr.DamageEvents {
		if d.Id == id {
			out = append(out, d)
		}
	}
	return out
}

// HurtAnimationsOf keeps the hurt animations one body was named in.
func (tr *ThrownTraffic) HurtAnimationsOf(id int32) []ThrownEvent {
	var out []ThrownEvent
	for _, h := range tr.hurtAnims {
		if h.Id == id {
			out = append(out, h)
		}
	}
	return out
}

// HealthsOf keeps the health floats one living body was told of.
func (tr *ThrownTraffic) HealthsOf(id int32) []float32 {
	var out []float32
	for _, h := range tr.healths {
		if h.Id == id {
			out = append(out, h.Health)
		}
	}
	return out
}

// SpawnOfType keeps the first spawn of the entity type named.
func (tr *ThrownTraffic) SpawnOfType(entityType int32) *ThrownSpawn {
	for i := range tr.Spawns {
		if tr.Spawns[i].EntityType == entityType {
			return &tr.Spawns[i]
		}
	}
	return nil
}

// UseHeld uses whatever the hotbar holds the way a client's use does,
// aimed where the yaw and pitch say — the packet the throw rides in on.
func UseHeld(c *Client, yaw, pitch float32) {
	c.T.Helper()
	c.Send(protocol.IDPlayUseItem, func(b *protocol.Buffer) {
		b.PutVarInt(protocol.HandMain)
		b.PutVarInt(1) // block change sequence, whose ack only a dig asks for
		b.PutF32(yaw)
		b.PutF32(pitch)
	})
}

// ExpectThrownSpawn waits for the next thrown body of the entity type given
// to come into the world and reads its spawn whole, stepping over the
// traffic the throw and the other bodies carry.
func (c *Client) ExpectThrownSpawn(srv *server.Server, entityType int32) ThrownSpawn {
	c.T.Helper()
	var spawn ThrownSpawn
	if !c.ReadTicks(srv, SkeletonTicks, func(gotID int32, data []byte) bool {
		if gotID == protocol.IDPlaySpawnEntity {
			if s := ReadThrownSpawn(c, data); s.EntityType == entityType {
				spawn = s
				return true
			}
		}
		return false
	}) {
		c.T.Fatal("no thrown body came into the world")
	}
	return spawn
}

// SpawnEntityHead reads the head of a Spawn Entity packet — the entity ID,
// the UUID it spawned under and the entity type — and gives the rest back
// unread, so a caller can tell whether the arrival is one of its own before
// parsing it whole.
func SpawnEntityHead(data []byte) (int32, int32) {
	r := protocol.NewReader(data)
	id := r.ReadVarInt()
	if r.Err() != nil {
		return 0, 0
	}
	r.ReadUUID()
	if r.Err() != nil {
		return 0, 0
	}
	return id, r.ReadVarInt()
}

// StepOverMetadataValue steps over one metadata value of the type named, the
// ones an item entity's arrival and a custom name carry, so a metadata walk can reach the
// next field without knowing what each one means.
func StepOverMetadataValue(r *protocol.Reader, fieldType int32) {
	switch fieldType {
	case protocol.MetadataByte, protocol.MetadataBoolean:
		r.ReadU8()
	case protocol.MetadataVarInt, protocol.MetadataHumanoidArm, protocol.MetadataZombieNautilusVariant:
		r.ReadVarInt()
	case protocol.MetadataFloat:
		r.ReadF32()
	case protocol.MetadataSlot:
		r.ReadSlot()
	case protocol.MetadataOptionalText:
		if r.ReadBool() {
			protocol.ReadText(r)
		}
	default:
		// Nothing else an item entity carries; the walk stops rather
		// than guessing a length.
		r.Rest()
	}
}

// The flags byte of an Update Mob Effect the totem writes: not ambient,
// with its particles and icon, blended as a freshly added effect.
const TotemEffectAddedFlags = 0x02 | 0x04 | 0x08

// ExpectMobEffectAdded reads the next Update Mob Effect and checks it names
// this body and the effect asked for, at the amplifier and duration, in a
// freshly added state.
func (c *Client) ExpectMobEffectAdded(entityID, effect, amplifier, duration int32) {
	c.T.Helper()
	data := c.ExpectSurvival(protocol.IDPlayUpdateMobEffect)
	r := protocol.NewReader(data)
	if got := r.ReadVarInt(); got != entityID {
		c.T.Errorf("mob effect put on entity %d, want %d", got, entityID)
	}
	if got, want := r.ReadVarInt(), effect; got != want {
		c.T.Errorf("mob effect = %d, want %d", got, want)
	}
	if got, want := r.ReadVarInt(), amplifier; got != want {
		c.T.Errorf("mob effect %d amplifier = %d, want %d", effect, got, want)
	}
	if got, want := r.ReadVarInt(), duration; got != want {
		c.T.Errorf("mob effect %d duration = %d, want %d", effect, got, want)
	}
	if got := r.ReadU8(); got != TotemEffectAddedFlags {
		c.T.Errorf("mob effect %d flags = %#02x, want %#02x (particles, icon, blend)",
			effect, got, TotemEffectAddedFlags)
	}
	if err := r.Err(); err != nil {
		c.T.Fatalf("update mob effect: %v", err)
	}
	if r.Remaining() != 0 {
		c.T.Errorf("update mob effect has %d bytes left over", r.Remaining())
	}
}

// ExpectMobEffectRemoved reads the next Remove Mob Effect and reports which
// effect it took off this body.
func (c *Client) ExpectMobEffectRemoved(entityID int32) int32 {
	c.T.Helper()
	data := c.ExpectSurvival(protocol.IDPlayRemoveMobEffect)
	r := protocol.NewReader(data)
	if got := r.ReadVarInt(); got != entityID {
		c.T.Errorf("mob effect taken off entity %d, want %d", got, entityID)
	}
	effect := r.ReadVarInt()
	if err := r.Err(); err != nil {
		c.T.Fatalf("remove mob effect: %v", err)
	}
	if r.Remaining() != 0 {
		c.T.Errorf("remove mob effect has %d bytes left over", r.Remaining())
	}
	return effect
}

// PotionStack writes a stack of the item given carrying a brewed potion
// and nothing else: the potion by its ID, no custom colour, no custom
// effects and no custom name — the shape of the value the wire's
// potion_contents component holds.
func PotionStack(t *testing.T, item string, potion int32, count int32) protocol.ItemStack {
	t.Helper()
	var value protocol.Buffer
	value.PutBool(true)
	value.PutVarInt(potion)
	value.PutBool(false) // no custom colour
	value.PutVarInt(0)   // no custom effects
	value.PutBool(false) // no custom name
	return protocol.ItemStack{
		ItemID: MustItemID(t, item), Count: count,
		Raw: []protocol.RawComponent{
			{Type: protocol.DataComponentPotionContents, Data: value.Bytes()},
		},
	}
}

// PoisonBrewing is the ID of the plain poison brewing; PoisonEffect the
// status effect it hands out, PoisonFlags the flags the drinking tests'
// packet carries.
var (
	PoisonBrewing = registry.MustPotion("minecraft:poison")
	PoisonEffect  = registry.MustMobEffect("minecraft:poison")
	PoisonFlags   = uint8(0x02 | 0x04 | 0x08)
)

// fireworksComponent is the wire value of a minecraft:fireworks component: the
// flight duration and the prefixed list of explosions, each its shape, two
// colour lists and the trail and twinkle flags. vanilla: Fireworks.STREAM_CODEC
func fireworksComponent(flight, explosions int32) []byte {
	var b protocol.Buffer
	b.PutVarInt(flight)
	b.PutVarInt(explosions)
	for range explosions {
		b.PutVarInt(0) // the shape, small ball
		b.PutVarInt(0) // the colours
		b.PutVarInt(0) // the fade colours
		b.PutBool(false)
		b.PutBool(false)
	}
	return b.Bytes()
}

// FireworkStack is one flight-N rocket of n explosions in the hand.
func FireworkStack(flight, explosions int32) protocol.ItemStack {
	return protocol.ItemStack{
		ItemID: MustItem("minecraft:firework_rocket"),
		Count:  1,
		Raw:    []protocol.RawComponent{{Type: protocol.DataComponentFireworks, Data: fireworksComponent(flight, explosions)}},
	}
}

// PvpOffServer is the combat tests' survival server with the pvp rule off.
func PvpOffServer(t *testing.T) (*server.Server, func() *Client) {
	t.Helper()
	return Start(t, CombatTestConfig(), func(s *server.Server) {
		s.SetGameRule("pvp", false)
	})
}

// NightWorld stops the day cycle at midnight, so the daylight burn no mob of
// the test is due stands out of the way. It is set through the server because
// the player is no operator and a refused /time would leave the noon a zombie
// burns in.
func NightWorld(srv *server.Server, c *Client) {
	srv.SetPaused(true)
	srv.SetDayTime(Midnight)
	c.Settle()
}

// WindBurst is an Explosion packet of a wind charge's burst: the gust
// particle and the burst sound where a blast carries the generic ones.
type WindBurst struct {
	Explosion
	Particle, Sound, Alternatives int32
}

// ReadWindBurst parses the Explosion packet of a wind charge's burst.
func ReadWindBurst(c *Client, data []byte) WindBurst {
	c.T.Helper()
	r := protocol.NewReader(data)
	e := WindBurst{}
	e.X, e.Y, e.Z, e.Radius = r.ReadF64(), r.ReadF64(), r.ReadF64(), r.ReadF32()
	e.BlockCount = r.ReadI32()
	if e.Pushed = r.ReadBool(); e.Pushed {
		e.KX, e.KY, e.KZ = r.ReadF64(), r.ReadF64(), r.ReadF64()
	}
	e.Particle, e.Sound, e.Alternatives = r.ReadVarInt(), r.ReadVarInt(), r.ReadVarInt()
	e.PlaySound = r.ReadBool()
	if err := r.Err(); err != nil || r.Remaining() != 0 {
		c.T.Fatalf("explosion packet: err %v, %d bytes left over", err, r.Remaining())
	}
	return e
}

// BlastSkip is the background a watching client hears while a monster is
// being watched: the world's own traffic. The movement packets are kept —
// the climb test reads a spider's rise out of them — and so are the sound
// effects, the priming of a fuse being one of the things the tests watch
// for.
func BlastSkip(id int32) bool {
	switch id {
	case protocol.IDPlayUpdateTime,
		protocol.IDPlayUpdateLight, protocol.IDPlayBlockUpdate,
		protocol.IDPlayChunkData, protocol.IDPlayChunkBatchStart,
		protocol.IDPlayChunkBatchFinished, protocol.IDPlayCenterChunk,
		protocol.IDPlayUnloadChunk, protocol.IDPlayWorldEvent,
		protocol.IDPlaySetEntityMotion,
		protocol.IDPlayEntityAnimation, protocol.IDPlaySwingAnimation, protocol.IDPlayEntityEquipment,
		protocol.IDPlayContainerSlot, protocol.IDPlayPingResponse:
		return true
	}
	return false
}

// GivePearls puts a stack of sixteen ender pearls in the Thrower's first
// hotbar slot.
func GivePearls(t *testing.T, srv *server.Server) {
	t.Helper()
	srv.GiveSlot("Thrower", 36, protocol.ItemStack{ItemID: MustItem("minecraft:ender_pearl"), Count: 16})
}

// ParticleTail is the part of a Level Particles packet that follows the
// particle and its options.
type ParticleTail struct {
	LongDistance, AlwaysShow bool
	X, Y, Z                  float64
	DX, DY, DZ, Speed        float32
	Count                    int32
}

// ReadParticleTail reads a Level Particles packet's fields after the
// particle's options, checking the three max speeds agree and the
// randomization type is DEFAULT.
func ReadParticleTail(t *testing.T, r *protocol.Reader) ParticleTail {
	t.Helper()
	var p ParticleTail
	p.LongDistance, p.AlwaysShow = r.ReadBool(), r.ReadBool()
	p.X, p.Y, p.Z = r.ReadF64(), r.ReadF64(), r.ReadF64()
	p.DX, p.DY, p.DZ = r.ReadF32(), r.ReadF32(), r.ReadF32()
	p.Speed = r.ReadF32()
	if sy, sz := r.ReadF32(), r.ReadF32(); sy != p.Speed || sz != p.Speed {
		t.Errorf("particle max speeds %v %v %v differ", p.Speed, sy, sz)
	}
	p.Count = r.ReadVarInt()
	if kind := r.ReadVarInt(); kind != 0 {
		t.Errorf("particle randomization type = %d, want 0", kind)
	}
	return p
}
