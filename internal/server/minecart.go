package server

import (
	"cmp"
	"log/slog"
	"math"
	"math/rand"
	"slices"
	"sync"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/plugin"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// Server-side state for the minecarts. The cart itself (the rail ride, the
// rocking) is entity.Minecart; the server holds the identity, the passengers
// and the tracking. The cart lock sits beneath the player list's and alone
// of the entity ones, like the boat's: never held while another of the
// server's locks is taken, every read done in its own phase of the tick. No
// client reports a cart's place, so every viewer, a rider's own client among
// them, is told its movements.

// minecartEntity is one minecart in a world. passengers are the players
// aboard and mobs the mobs; Set Passengers names both.
type minecartEntity struct {
	cart *entity.Minecart
	w    *world.World
	uuid uuid.UUID

	bodyTrack[minecartMeta]

	portal rules.EntityPortal

	// scoresSettled marks a dead cart whose scores the serving pass leaves
	// alone: /kill already reset them, or its UUID lives on in a twin.
	// Guarded by the cart lock.
	scoresSettled bool

	passengers []*client
	mobs       []*mobEntity
	// boarding marks a crossing's twin between its registration and the
	// seating of the riders it carries: the departure sweep leaves it, as an
	// empty cart in a chunk the riders have yet to load would otherwise go
	// to its chunk file, the riders then seated on a cart no longer in the
	// world. Guarded by the cart lock.
	boarding bool
	tags     nbt.StringSet
	custom   customName
	look     entityLook

	// kind is the cart's entity type and item name; cargo is the container
	// of a kind that holds one, guarded by the cart lock.
	kind  registry.EntityType
	cargo vehicleCargo
	// enabled is a hopper cart's switch, flipped by the activator rail it
	// runs over; true for every other kind and unused by them.
	enabled bool
	// tnt and rng are a TNT cart's fuse and explosion settings and its dice,
	// guarded by the cart lock; idle for every other kind, which has no dice.
	tnt entity.TNTCart
	rng *rand.Rand
	// command is a command block cart's command; nil for every other kind.
	command *entity.CommandCart
	// spawner is a spawner cart's BaseSpawner, held outside any chunk; nil for
	// every other kind.
	spawner *world.BlockEntity
	// displayState is the custom display block state ID, 0 for the kind's
	// default; displayOffset is how far the block sits down in the cart.
	displayState  int32
	displayOffset int32

	// carriedPassengers are nested rider compounds of unmodelled kinds,
	// written back under Passengers so they are not lost.
	carriedPassengers []nbt.Tag
}

// minecartTracker tracks the minecarts; a watcher's own state is the
// metadata it was last told. A cart is shown to a joining or arriving client
// by the tick's settle, not by a join send.
// vanilla: ChunkMap.tick, TrackedEntity.updatePlayer
var minecartTracker = trackedKind[*minecartEntity, minecartMeta]{
	invalidatePiston: func(dim *dimension) { dim.pistons.minecarts.built = false },

	rangeChunks: entity.MinecartTrackingRange,
	passengerRange: func(it *minecartEntity) int {
		if len(it.passengers) > 0 {
			return entity.PlayerTrackingRange
		}
		return 0
	},
	interval: entity.MinecartUpdateInterval,
	lock:     func(s *Server) *sync.Mutex { return &s.minecartsMu },
	list:     func(s *Server) *[]*minecartEntity { return &s.minecarts },
	track:    func(it *minecartEntity) *bodyTrack[minecartMeta] { return &it.bodyTrack },
	world:    func(it *minecartEntity) *world.World { return it.w },
	id:       func(it *minecartEntity) int32 { return it.cart.ID },
	dead:     func(it *minecartEntity) bool { return it.cart.Dead },
	portalDimensions: func(*minecartEntity) (float64, float64) {
		return 2 * entity.MinecartHalfWidth, entity.MinecartHeight
	},
	portalCentered: true,
	pose: func(it *minecartEntity) bodyPose {
		m := it.cart
		return bodyPose{x: m.X, y: m.Y, z: m.Z, yaw: m.Yaw, onGround: m.OnGround}
	},
	pairing: minecartPairing,
	own:     minecartMetaOf,
}

// newMinecart builds a cart of the kind given, empty and unregistered.
func (s *Server) newMinecart(w *world.World, kind string, identity uuid.UUID, x, y, z float64) *minecartEntity {
	typ, _ := registry.EntityTypeByName(kind)
	info, _ := entity.MinecartKindForType(typ)
	id := s.nextEntity()
	it := &minecartEntity{
		cart:          entity.NewMinecart(id, x, y, z),
		tnt:           entity.NewTNTCart(),
		uuid:          identity,
		w:             w,
		kind:          typ,
		enabled:       true,
		cargo:         newVehicleCargo(info.Slots),
		displayOffset: info.DisplayOffset,
	}
	if kind == entity.MinecartTNT {
		it.rng = s.newCartRng(id)
	}
	if kind == entity.MinecartFurnace {
		it.cart.Furnace = &entity.FurnaceCart{}
	}
	if kind == entity.MinecartCommand {
		it.command = entity.NewCommandCart()
	}
	if kind == entity.MinecartSpawner {
		it.spawner = world.NewSpawnerState()
	}
	return it
}

// spawnMinecart stands a cart of the kind given in the world at the spot
// given and shows it to everyone in range, the way a laid one begins.
func (s *Server) spawnMinecart(w *world.World, kind string, x, y, z float64) *minecartEntity {
	return s.spawnMinecartFromItem(w, kind, x, y, z, protocol.ItemStack{})
}

// spawnMinecartFromItem applies stack components before the cart is shown.
// vanilla: AbstractMinecart.createMinecart
func (s *Server) spawnMinecartFromItem(w *world.World, kind string, x, y, z float64, stack protocol.ItemStack) *minecartEntity {
	it := s.newMinecart(w, kind, uuid.New(), x, y, z)
	if stack.CustomName != nil || len(stack.Raw) != 0 {
		if tag := itemEntityTag(minecartTag(it).Entries(), stack, it.entityTypeID()); tag != nil {
			applyMinecartTag(it, tag)
		}
	}
	s.registerMinecart(it)
	s.bodySpawned(it.uuid, plugin.SpawnDefault)
	return it
}

// summonMinecart stands a /summon's cart of the kind named up at the position
// given, read through the loader a saved cart's wake uses with the command's
// position the one it stands at. A load that comes to nothing and a tag whose
// UUID is already a live body's are the two refusals.
// vanilla: SummonCommand.createEntity
func (s *Server) summonMinecart(w *world.World, kind string, x, y, z float64, tag nbt.Compound) (*minecartEntity, error) {
	compound := summonTag(tag, kind, x, y, z)
	pos := world.ChunkPos{X: world.ChunkOf(x), Z: world.ChunkOf(z)}
	if woken, fresh := s.wakeChunkOnce(s.dimensionOfWorld(w), pos); fresh {
		s.registerWake(woken)
	}
	w.Acquire(pos)
	it := minecartOfTag(s, w, compound)
	w.Release(pos)
	if it == nil {
		return nil, errNotSimulated
	}
	if s.summonDuplicate(it.uuid) {
		return nil, command.ErrDuplicateUUID
	}
	it.cart.X, it.cart.Y, it.cart.Z = x, y, z
	if err := s.registerSummonedVehicle(w, compound, vehicleSeat{cart: it}); err != nil {
		return nil, err
	}
	return it, nil
}

// registerMinecart puts a built cart into the world and shows it to every
// player within the type's tracking range.
func (s *Server) registerMinecart(it *minecartEntity) {
	minecartTracker.register(s, it)
	s.minecartsMu.Lock()
	if s.cartCols != nil {
		s.cartCols.add(it)
	}
	s.minecartsMu.Unlock()
}

// minecartPairing is what shows a cart: the spawn, the metadata its
// non-default fields carry, and who rides it, in one bundle.
// vanilla: ServerEntity.sendPairingData
func minecartPairing(it *minecartEntity) []outbound {
	packets := pairing(outbound{id: protocol.IDPlaySpawnEntity, data: minecartSpawnPayload(it)},
		outbound{id: protocol.IDPlayEntityMetadata, data: minecartSpawnMetadataPayload(it)})
	if ids := it.passengerIDs(); len(ids) > 0 {
		packets = append(packets, outbound{id: protocol.IDPlaySetPassengers, data: setPassengersPayload(it.cart.ID, ids)})
	}
	return bundle(packets)
}

// passengerIDs is everyone aboard as Set Passengers names them, read under
// the cart lock.
func (it *minecartEntity) passengerIDs() []int32 {
	ids := make([]int32, 0, len(it.passengers)+len(it.mobs))
	for _, p := range it.passengers {
		ids = append(ids, p.player.EntityID)
	}
	for _, ent := range it.mobs {
		ids = append(ids, ent.mob.ID)
	}
	return ids
}

// minecartSpawnPayload builds the Spawn Entity packet that puts a cart in
// the world for a client: entity ID and UUID, the MINECART type, position,
// the motion it keeps and the yaw it faces. There is no type-specific
// data.
func minecartSpawnPayload(it *minecartEntity) []byte {
	m := it.cart
	return spawnEntityPayload(spawnEntity{
		id: m.ID, uuid: it.uuid, typ: it.entityTypeID(),
		x: m.X, y: m.Y, z: m.Z,
		vx: m.VX, vy: m.VY, vz: m.VZ,
		yaw: encodeAngle(m.Yaw),
	})
}

// entityTypeID is the cart's name in the minecraft:entity_type registry,
// as the spawn payload names it.
func (it *minecartEntity) entityTypeID() int32 {
	return int32(it.kind)
}

// kindInfo is what the cart's kind adds to the shared ride.
func (it *minecartEntity) kindInfo() entity.MinecartKind {
	info, _ := entity.MinecartKindForType(it.kind)
	return info
}

// windowKind is the menu a container cart opens: the hopper's five slots for
// the hopper cart, the chest's rows for the rest.
// vanilla: MinecartHopper.createMenu, MinecartChest.createMenu
func (it *minecartEntity) windowKind() windowKind {
	if it.kind == registry.EntityTypeHopperMinecart {
		return windowHopper
	}
	return windowChest
}

// slowdown is the rolling pace a container cart's contents set, 0 for a kind
// whose pace is the rider's.
func (it *minecartEntity) slowdown() float64 {
	if it.cargo.slots == nil {
		return 0
	}
	return entity.ContainerSlowdown(it.cargo.slots, it.cargo.lootTable != "")
}

// tickMinecarts runs one game tick of every cart: where everyone stands is
// read first, with nothing held; the rides run under the cart lock alone;
// the seats the ride leaves riders in are written after it, and the sends
// and the breaks run after every lock is down.
func (s *Server) tickMinecarts() {
	s.mu.RLock()
	clients := s.clientsLocked()
	s.mu.RUnlock()

	pushes := s.minecartPushInputs(clients)
	gate := s.newEntityGate()
	var held []*minecartEntity
	defer func() { minecartTracker.dormantPass(s, held) }()
	s.lockTick(&s.minecartsMu)
	if len(s.minecarts) == 0 {
		s.unlockTick(&s.minecartsMu)
		return
	}

	// A frozen world steps only the carts a player rides.
	// vanilla: TickRateManager.isEntityFrozen
	runs := s.tickRate.Running()
	type seat struct {
		c          *client
		it         *minecartEntity
		x, y, z    float64
		dismount   bool
		dx, dz     int32
		cx, cy, cz float64
	}
	var (
		removals     = make(map[*world.World][]int32)
		splashes     []projectileSplash
		movements    vehicleMovements
		bounces      []bodyBounce
		burns        []burnSpot
		fizzles      []burnSpot
		breaks       []minecartBreak
		seats        []seat
		news         []seatNews
		suckers      []*minecartEntity
		tntFx        tntCartEffects
		falls        []cartFall
		bodyLandings []bodyLanding
		spawners     []*minecartEntity
		gates        []gatewayTouch
		travels      []minecartPortalTravel
		flakes       []snowflakeSpot
		mobSeats     []minecartMobSeat
		pickups      []minecartPickup

		commandRuns []*minecartEntity
	)
	eject := func(it *minecartEntity) {
		if len(it.passengers) == 0 && len(it.mobs) == 0 {
			return
		}
		news = append(news, seatNews{w: it.w, vehicle: it.cart.ID, ids: nil})
		sdx, sdz := it.cart.MotionDir()
		for _, c := range it.passengers {
			news[len(news)-1].event(rules.GameEventEntityDismount, it.cart.X, it.cart.Y, it.cart.Z, c.gameEventContext(0))
			c.minecart.Store(nil)
			seats = append(seats, seat{c: c, dismount: true,
				cx: it.cart.X, cy: it.cart.Y, cz: it.cart.Z, dx: sdx, dz: sdz})
		}
		it.passengers = nil
		for _, ent := range it.mobs {
			news[len(news)-1].event(rules.GameEventEntityDismount, it.cart.X, it.cart.Y, it.cart.Z, rules.GameEventContext{Mob: ent.uuid})
			mobSeats = append(mobSeats, minecartMobSeat{ent: ent, it: it, w: it.w,
				x: it.cart.X, y: it.cart.Y, z: it.cart.Z, dismount: true, dx: sdx, dz: sdz})
		}
		it.mobs = nil
	}
	served := func(it *minecartEntity) {
		removals[it.w] = append(removals[it.w], it.cart.ID)
		if !it.scoresSettled {
			s.entityDestroyed(it.uuid)
		}
		breaks = append(breaks, s.minecartBreakOf(it))
		eject(it)
	}
	alive := s.minecarts[:0]
	for _, it := range s.minecarts {
		if it.cart.Dead {
			// Broken since the last tick — a blow's end, a /kill, the
			// world's bottom — and gone with the drop its break leaves.
			served(it)
			continue
		}
		// A chunk that unloaded after this tick's sweep is the next tick's
		// to step and save. Stepping it now reads air for its ground and
		// drops it a tick's gravity before the sweep comes back.
		if pos := (world.ChunkPos{X: world.ChunkOf(it.cart.X), Z: world.ChunkOf(it.cart.Z)}); !it.w.ChunkLoaded(pos) || !gate.ticks(it.w, it.cart.X, it.cart.Z) {
			held = append(held, it)
			alive = append(alive, it)
			continue
		}
		ridden := len(it.passengers) > 0
		if !runs && !ridden {
			alive = append(alive, it)
			continue
		}
		// The portal pass runs ahead of the ride, as for items. A cart with
		// a player aboard counts but stays: the player's crossing carries it
		// (moveMinecartWithRider).
		state := it.w.BlockAt(rules.BlockOf(it.cart.X), rules.BlockOf(it.cart.Y), rules.BlockOf(it.cart.Z))
		if kind, due := it.portal.Step(state, rules.PortalTransitionTime, vehiclePortalCooldownTicks); due && !ridden {
			travels = append(travels, minecartPortalTravel{it: it, kind: kind})
			continue
		}
		from := entity.NewAABB(it.cart.Box())
		it.cart.Slowdown = it.slowdown()
		it.cart.ShakesOnActivator = it.kind == registry.EntityTypeMinecart
		if it.command != nil {
			it.command.Age++
		}
		pushes.pushedByBodies(it)
		it.cart.IntentX, it.cart.IntentZ = 0, 0
		if ridden {
			it.cart.IntentX, it.cart.IntentZ = it.passengers[0].moveIntent()
		}
		if it.kind == registry.EntityTypeTntMinecart {
			it.cart.StepWithHurt(pushes.bodyWorld(it), ridden || len(it.mobs) > 0, func(amount float64, damageType registry.DamageType, creative bool) bool {
				return s.tntCartHurt(it, amount, creative, entity.TNTCartHit{DamageType: damageType, Ignites: true}, &tntFx)
			})
		} else {
			it.cart.Step(pushes.bodyWorld(it), ridden || len(it.mobs) > 0)
		}
		movements.add(it.w, it.uuid, it.cart.Movement)
		if it.cart.TakeBounceEffect() {
			bounces = append(bounces, bodyBounce{w: it.w, source: it.uuid, x: it.cart.X, y: it.cart.Y, z: it.cart.Z, halfWidth: entity.MinecartHalfWidth})
		}
		for range it.cart.LavaHurts {
			burns = append(burns, burnSpot{it.w, it.cart.X, it.cart.Y, it.cart.Z})
		}
		for range it.cart.Extinguishes {
			fizzles = append(fizzles, burnSpot{it.w, it.cart.X, it.cart.Y, it.cart.Z})
		}
		if sp := it.cart.Splash; sp != nil {
			splashes = append(splashes, projectileSplash{w: it.w, x: sp.X, y: sp.Y, z: sp.Z, volume: sp.Volume, pitch: sp.Pitch})
		}
		for _, y := range it.cart.Snowflakes {
			flakes = append(flakes, snowflakeSpot{it.w, it.cart.X, float64(y + 1), it.cart.Z})
		}
		if it.cart.Furnace != nil && !it.cart.Dead {
			it.cart.Furnace.Tick()
		}
		landed := it.cart.LandedFall
		if it.kind == registry.EntityTypeTntMinecart {
			s.tntCartTick(it, &tntFx)
		}
		it.cart.LandedFall = 0
		if landed > 0 && !it.cart.Dead {
			bodyLandings = append(bodyLandings, bodyLanding{w: it.w, x: it.cart.X, y: it.cart.Y, z: it.cart.Z, halfWidth: entity.MinecartHalfWidth})
		}
		if landed > 0 && !it.cart.Dead && (len(it.passengers) > 0 || len(it.mobs) > 0) {
			falls = append(falls, cartFall{
				w: it.w, landed: landed,
				effect: rules.FallOn(rules.OnPos(it.w, it.cart.X, it.cart.Y, it.cart.Z, 0.49)),
				riders: slices.Clone(it.passengers), mobs: slices.Clone(it.mobs),
			})
		}
		if it.spawner != nil && !it.cart.Dead {
			spawners = append(spawners, it)
		}
		if it.cart.Dead {
			served(it)
			continue
		}
		pushes.pushOut(it)
		if gx, gy, gz, ok := entity.EndGatewayInside(it.w, from, entity.NewAABB(it.cart.Box())); ok {
			gates = append(gates, gatewayTouch{
				w: it.w, x: gx, y: gy, z: gz, mu: &s.minecartsMu,
				portal: &it.portal, cooldown: vehiclePortalCooldownTicks,
				move: func(to player.Position) { s.gatewayMoveMinecart(it, to) },
			})
		}
		// The activator's answer: the riders a lit one casts off are
		// stepped off here, the shake the cart itself now carries.
		if it.cart.Activated {
			it.cart.Activated = false
			eject(it)
		}
		if it.cart.OnActivator {
			it.cart.OnActivator = false
			if it.kind == registry.EntityTypeHopperMinecart {
				it.enabled = !it.cart.ActivatorPowered // vanilla: MinecartHopper.activateMinecart
			}
			if it.command != nil && it.cart.ActivatorPowered && it.command.Activate() {
				commandRuns = append(commandRuns, it)
			}
		}
		if it.kind == registry.EntityTypeHopperMinecart && it.enabled {
			suckers = append(suckers, it)
		}
		if it.kind == registry.EntityTypeMinecart && len(it.passengers) == 0 && len(it.mobs) == 0 {
			if box, ok := it.cart.PickupBox(); ok {
				pickups = append(pickups, minecartPickup{it: it, box: box})
			}
		}
		// The riders ride where the cart holds them. The seat is written
		// after the locks are down, and the write re-checks under the
		// teleport lock that a dismount between the snapshot and the write
		// has not already stepped the rider out.
		for _, c := range it.passengers {
			seats = append(seats, seat{c: c, it: it,
				x: it.cart.X, y: it.cart.Y + entity.MinecartSeatY, z: it.cart.Z})
		}
		for _, ent := range it.mobs {
			mobSeats = append(mobSeats, minecartMobSeat{ent: ent, it: it, w: it.w,
				x: it.cart.X, y: it.cart.Y, z: it.cart.Z})
		}
		alive = append(alive, it)
	}
	s.minecarts = alive
	s.unlockTick(&s.minecartsMu)
	s.applyMobShares(pushes.mobs)
	for _, f := range falls {
		s.fallOntoCartRiders(f)
	}
	s.runBodyLandings(bodyLandings)

	for w, ids := range removals {
		s.broadcastWorld(w, protocol.IDPlayRemoveEntities, removeEntitiesPayload(ids))
	}
	s.sendSeatNews(news)
	movements.apply(s)
	s.runBodyBounces(bounces)
	applyEach(s, splashes, (*Server).splashProjectile)
	applyEach(s, burns, (*Server).burnVehicle)
	applyEach(s, fizzles, (*Server).fizzleVehicle)
	s.rollSnowflakes(flakes)
	for _, st := range seats {
		if st.dismount {
			st.c.dismountMinecartTo(st.cx, st.cy, st.cz, st.dx, st.dz)
			continue
		}
		// The check and the move are one step under the teleport lock, so
		// a dismount landing in between cannot be undone by this write.
		st.c.tp.mu.Lock()
		if st.c.minecart.Load() != st.it {
			st.c.tp.mu.Unlock()
			continue // a dismount between the snapshot and this write
		}
		// The seat moves the rider, not their look, which the rider's move
		// intent turns by. vanilla: Entity.positionRider
		look := st.c.player.Position()
		st.c.moveHeld(player.Position{X: st.x, Y: st.y, Z: st.z, Yaw: look.Yaw, Pitch: look.Pitch}, false)
		st.c.tp.mu.Unlock()
	}
	s.seatMinecartMobs(mobSeats)
	s.pickUpMobs(pickups)
	for _, it := range suckers {
		s.hopperCartSuck(it)
	}
	s.touchGateways(gates)
	for _, it := range commandRuns {
		s.runCommandCart(it)
	}
	s.runTNTCartEffects(tntFx)
	// One lazy host per dimension, so the player snapshot is read once.
	var hosts map[*dimension]*spawnerHost
	for _, it := range spawners {
		dim := s.dimensionOfWorld(it.w)
		if hosts == nil {
			hosts = map[*dimension]*spawnerHost{}
		}
		host := hosts[dim]
		if host == nil {
			host = &spawnerHost{s: s, dim: dim}
			hosts[dim] = host
		}
		s.tickSpawnerCart(it, host)
	}
	for _, br := range breaks {
		s.breakMinecart(br)
	}
	for _, tr := range travels {
		s.finishMinecartTravel(tr.it, tr.kind)
	}
}

// minecartPortalTravel is one cart the portal pass caught.
type minecartPortalTravel struct {
	it   *minecartEntity
	kind rules.PortalKind
}

// finishMinecartTravel sends one cart with no player aboard the portal pass
// caught across its portal, cargo and mob rider with it.
func (s *Server) finishMinecartTravel(it *minecartEntity, kind rules.PortalKind) {
	var turn float32
	minecartTracker.crossPortal(s, it, kind, func(twin *minecartEntity) {
		s.registerMinecart(twin)
		s.carryMinecartMobs(it, twin, turn)
	}, func(to *world.World, at player.Position) (*minecartEntity, bool) {
		twin := minecartOfTag(s, to, minecartTag(it).Entries())
		if twin == nil {
			return nil, false
		}
		twin.carriedPassengers = it.carriedPassengers
		turn = at.Yaw - twin.cart.Yaw
		twin.cart.X, twin.cart.Y, twin.cart.Z, twin.cart.Yaw = at.X, at.Y, at.Z, at.Yaw
		return twin, true
	})
}

// minecartBreak is what a broken cart leaves: its own item, unless the end
// that broke it leaves nothing, which a creative blow's discard and the
// below-world's are, and the contents of a container cart, which spill at
// any end.
type minecartBreak struct {
	w        *world.World
	id       int32
	name     string
	x, y, z  float64
	withLoot bool
	itemName *protocol.Text
	cargo    []protocol.ItemStack
}

// minecartBreakOf reads what a dead cart's break leaves, under the cart
// lock, for the caller to lay after the locks are down.
func (s *Server) minecartBreakOf(it *minecartEntity) minecartBreak {
	// The pending table is rolled as the contents are read out, and the
	// cart keeps none, so a window still open on it shows nothing.
	// vanilla: AbstractMinecartContainer.remove
	s.unpackVehicleLoot(it.w, it.cart.X, it.cart.Y, it.cart.Z, &it.cargo, lootOpener{})
	cargo := it.cargo.slots
	if cargo != nil {
		it.cargo.slots = make([]protocol.ItemStack, len(cargo))
	}
	return minecartBreak{
		w:    it.w,
		id:   it.cart.ID,
		name: cmp.Or(it.kindInfo().Drop, it.kind.Name()),
		x:    it.cart.X, y: it.cart.Y, z: it.cart.Z,
		withLoot: !it.cart.NoLoot,
		itemName: customNameText(it.custom.tag()),
		cargo:    cargo,
	}
}

// breakMinecart lays what a broken cart leaves where it broke — the cart's
// own item at its wheels, unless the do_entity_drops rule has turned the
// leaving off, and a container cart's contents spilled as a broken
// container's are, whatever the rule says.
// vanilla: VehicleEntity.destroy, AbstractMinecartContainer.remove
func (s *Server) breakMinecart(br minecartBreak) {
	s.spillContainer(br.w, int32(math.Floor(br.x)), int32(math.Floor(br.y)), int32(math.Floor(br.z)), br.cargo)
	if !br.withLoot || !br.w.BoolRule(world.RuleEntityDrops) {
		return
	}
	if id, ok := registry.ItemID(br.name); ok {
		s.spawnItemAtFeet(br.w, protocol.ItemStack{ItemID: id, Count: 1, CustomName: br.itemName}, br.x, br.y, br.z)
	}
}

// killMinecart is /kill's discard of a cart: marked dead out of the world's
// loot, so the tick's next pass takes it with nothing laid behind.
// vanilla: Entity.kill, Entity.discard
func (s *Server) killMinecart(it *minecartEntity) {
	s.minecartsMu.Lock()
	if !it.cart.Dead {
		it.cart.Dead = true
		it.cart.NoLoot = true
		it.scoresSettled = true
		s.entityDestroyed(it.uuid)
	}
	s.minecartsMu.Unlock()
}

// teleportMinecart is Entity.teleportTo for a cart: the place and yaw set
// at rest and the rider carried to the new seat once the cart lock is down;
// the tick tells the watchers.
// vanilla: Entity.teleportTo
func (s *Server) teleportMinecart(it *minecartEntity, to player.Position, yaw float32, by string) {
	var riders []*client
	minecartTracker.teleport(s, it, func() {
		it.cart.X, it.cart.Y, it.cart.Z = to.X, to.Y, to.Z
		it.cart.Yaw = yaw
		it.cart.VX, it.cart.VY, it.cart.VZ = 0, 0, 0
		riders = slices.Clone(it.passengers)
		// The hopper index is keyed by the old column; lookups scan until
		// the pass ends.
		s.cartCols = nil
	})
	for _, r := range riders {
		pos := r.player.Position()
		pos.X, pos.Y, pos.Z = to.X, to.Y+entity.MinecartSeatY, to.Z
		r.move(pos, false)
	}
	logTeleported("minecart", to, by)
}

// minecartTwinAt raises a twin of a cart for a crossing: a fresh body of the
// same kind and identity, at rest at the position given, the tags, the
// contents, the display block, the flipped direction and the ticked flag
// carried across. The husk's contents are cleared by the caller.
// vanilla: Entity.restoreFrom
func (s *Server) minecartTwinAt(src *minecartEntity, to *dimension, pos player.Position) *minecartEntity {
	twin := s.newMinecart(to.w, src.kind.Name(), src.uuid, pos.X, pos.Y, pos.Z)
	twin.tags = nbt.NewStringSet(src.tags.Values())
	twin.cargo = src.cargo
	twin.custom = src.custom
	twin.enabled = src.enabled
	twin.tnt = src.tnt
	if src.command != nil {
		cc := *src.command
		twin.command = &cc
	}
	if src.spawner != nil {
		twin.spawner.LoadSpawnerTag(src.spawner.SpawnerTag())
	}
	if src.cart.Furnace != nil {
		furnace := *src.cart.Furnace
		twin.cart.Furnace = &furnace
	}
	twin.displayState, twin.displayOffset = src.displayState, src.displayOffset
	twin.cart.NoGravity = src.cart.NoGravity
	twin.cart.Flipped = src.cart.Flipped
	twin.cart.FirstTick = src.cart.FirstTick
	return twin
}

// minecartByEntityID is the cart of a world an entity ID names, under the
// cart lock.
func (s *Server) minecartByEntityID(w *world.World, id int32) *minecartEntity {
	s.minecartsMu.Lock()
	defer s.minecartsMu.Unlock()
	for _, it := range s.minecarts {
		if it.w == w && it.cart.ID == id && !it.cart.Dead {
			return it
		}
	}
	return nil
}

// minecartSnapshots reads every standing cart's box under the cart lock,
// for a projectile tick to merge with the boats' hulls: the world's
// targets, the IDs the blows are looked up by, and the cart each rider
// belongs to.
func (s *Server) minecartSnapshots() (byWorld map[*world.World][]entity.ArrowTarget, ids map[int32]bool, roots map[int32]int32) {
	s.minecartsMu.Lock()
	defer s.minecartsMu.Unlock()
	for _, it := range s.minecarts {
		if it.cart.Dead {
			continue
		}
		if byWorld == nil {
			byWorld = make(map[*world.World][]entity.ArrowTarget)
			ids = make(map[int32]bool)
			roots = make(map[int32]int32)
		}
		byWorld[it.w] = append(byWorld[it.w], entity.MinecartTarget(it.cart.ID, it.entityTypeID(), it.cart))
		ids[it.cart.ID] = true
		for _, p := range it.passengers {
			roots[p.player.EntityID] = it.cart.ID
		}
	}
	return byWorld, ids, roots
}

// minecartProjectileHit applies a projectile's blow to the cart the entity
// ID names: the rocking wound and the damage added, told to every watcher
// of a cart left standing. A broken cart is taken out, with its item, by
// the tick's next pass. It reports whether the blow landed.
// vanilla: VehicleEntity.hurtServer
func (s *Server) minecartProjectileHit(blow boatProjectileBlow) bool {
	it := s.minecartByEntityID(blow.w, blow.boatID)
	if it == nil {
		return false
	}
	// vanilla: MinecartTNT.damageSourceIgnitesTnt, hurtServer
	return s.minecartHurtBy(it, blow.w, blow.damage, blow.dropless, entity.TNTCartHit{
		OwnerID: blow.ownerID, Ignites: blow.fire,
		FireArrow: blow.fire && blow.arrow != nil, FireArrowSpeedSq: blow.speedSq,
	})
}

// minecartHurtBy lays damage on a cart and, for one left standing, records
// the rocking in every watcher's tracked state before telling the world, so
// the tick's own metadata pass does not repeat it; hit is what a TNT cart
// reads of the source. It reports whether the blow landed.
// vanilla: VehicleEntity.hurtServer
func (s *Server) minecartHurtBy(it *minecartEntity, w *world.World, damage float64, dropless bool, hit entity.TNTCartHit) bool {
	var metadata []byte
	var fx tntCartEffects
	s.minecartsMu.Lock()
	var landed bool
	if it.kind == registry.EntityTypeTntMinecart {
		landed = s.tntCartHurt(it, damage, dropless, hit, &fx)
	} else {
		landed = it.cart.Hurt(damage, hit.DamageType, dropless)
	}
	if landed && !it.cart.Dead {
		now := minecartMetaOf(it)
		for _, was := range it.tracked.all() {
			was.own.hurtTime, was.own.hurtDir, was.own.hurtDamage = now.hurtTime, now.hurtDir, now.hurtDamage
		}
		metadata = minecartShakeMetadata(it.cart)
	}
	x, y, z := it.cart.X, it.cart.Y, it.cart.Z
	s.minecartsMu.Unlock()
	if landed {
		s.gameEvent(w, rules.GameEventEntityDamage, x, y, z, s.attackerEventContext(hit.OwnerID))
	}
	if metadata != nil {
		s.broadcastWorld(w, protocol.IDPlayEntityMetadata, metadata)
	}
	s.runTNTCartEffects(fx)
	return landed
}

// useOnMinecart answers the Interact a player made with a cart: a container
// cart opens its contents, whatever the hand does and a spectator's too, if
// no loot table is pending for the spectator; a plain cart is vanilla's
// Minecart.interact, which a sneaking hand does not mount and a passenger
// holding the seat keeps out.
// vanilla: Minecart.interact, AbstractMinecartContainer.createMenu
func (c *client) useOnMinecart(it *minecartEntity, hand int32, sneaking bool) error {
	pos := c.player.Position()
	// vanilla's handleInteract verifies the body's box against the entity
	// interaction range before the use is read at all: a use past the
	// reach is no use. The box is read under the cart lock, which the
	// tick's step holds while it moves the cart.
	c.srv.minecartsMu.Lock()
	minX, minY, minZ, maxX, maxY, maxZ := it.cart.Box()
	c.srv.minecartsMu.Unlock()
	if !rules.WithinEntityInteractionRange(c.entityRange(),
		pos.X, pos.Y+c.dims(c.player.Appearance().Pose).EyeHeight, pos.Z,
		minX, minY, minZ, maxX, maxY, maxZ) {
		return nil
	}
	spectator := c.player.GameMode() == protocol.GameModeSpectator
	if it.kindInfo().Slots > 0 {
		c.srv.minecartsMu.Lock()
		pending := it.cargo.lootTable != ""
		c.srv.minecartsMu.Unlock()
		if spectator && pending {
			return nil
		}
		if !spectator {
			c.useSwing = swingPredicted
		}
		return c.openCartCargo(it)
	}
	if it.cart.Furnace != nil {
		if spectator {
			return nil
		}
		c.useSwing = swingPredicted
		return c.fuelFurnaceCart(it, hand)
	}
	if it.command != nil {
		if c.canUseGameMasterBlocks() {
			c.useSwing = swingPredicted
		}
		return nil // the edit screen is the client's; no hand mounts it
	}
	if sneaking || spectator {
		return nil // a sneaking hand mounts nothing, vanilla's !isSecondaryUseActive
	}
	if c.srv.mountMinecart(c, it, false) {
		c.useSwing = swingServer
	}
	return nil
}

// fuelFurnaceCart feeds the held coal or charcoal to a furnace cart, which
// takes no rider; creative keeps the item.
// vanilla: MinecartFurnace.interact
func (c *client) fuelFurnaceCart(it *minecartEntity, hand int32) error {
	held := c.heldItem(hand)
	if held.Empty() || !registry.Item(held.ItemID).InTag(registry.ItemTagIDFurnaceMinecartFuel) {
		return nil
	}
	pos := c.player.Position()
	c.srv.minecartsMu.Lock()
	taken := it.cart.Furnace.AddFuel(it.cart.X, it.cart.Z, pos.X, pos.Z)
	c.srv.minecartsMu.Unlock()
	if !taken || c.player.GameMode() == protocol.GameModeCreative {
		return nil
	}
	return c.consumeHeld(hand)
}

// mountMinecart seats a player on a cart, vanilla's startRiding: the seat
// free and the player in no other, the passengers list grows by them,
// every viewer is told the cart's passengers, and the body moves to the
// seat the cart holds. The boarding player's own client takes it from
// there, switching to the cart's ride off the passengers packet alone.
// force skips the seat count, as /ride mount does; it reports whether the
// rider was seated.
// vanilla: Entity.startRiding
func (s *Server) mountMinecart(rider *client, it *minecartEntity, force bool) bool {
	if !force && !rider.mountAllowed(it.uuid, true) {
		return false
	}
	// A rider leaves the old vehicle, a boat or another cart, only once
	// this cart would take them: vanilla's startRiding asks canAddPassenger
	// before it stops riding.
	oldBoat, oldCart := rider.vehicle.Load(), rider.minecart.Load()
	if oldBoat != nil || oldCart != nil || rider.ridingMob() || rider.ridingCushion() {
		s.minecartsMu.Lock()
		ok := oldCart != it && !rider.player.Vitals().Dead && !it.cart.Dead &&
			(force || len(it.passengers) == 0 && len(it.mobs) == 0)
		s.minecartsMu.Unlock()
		if !ok {
			return false
		}
		rider.dismountBoat(false)
		rider.dismountMinecart(false)
		rider.dismountCushion(false)
	}
	s.minecartsMu.Lock()
	if rider.vehicle.Load() != nil || rider.minecart.Load() != nil || rider.ridingCushion() ||
		rider.player.Vitals().Dead || it.cart.Dead || (!force && (len(it.passengers) > 0 || len(it.mobs) > 0)) {
		s.minecartsMu.Unlock()
		return false
	}
	it.passengers = append(it.passengers, rider)
	rider.minecart.Store(it)
	vehicleID := it.cart.ID
	w := it.w
	bx, by, bz := it.cart.X, it.cart.Y, it.cart.Z
	x, y, z := bx, by+entity.MinecartSeatY, bz
	ids := it.passengerIDs()
	s.minecartsMu.Unlock()

	s.gameEvent(w, rules.GameEventEntityMount, bx, by, bz, rider.gameEventContext(0))
	s.broadcastWorld(w, protocol.IDPlaySetPassengers, setPassengersPayload(vehicleID, ids))
	// The seat is where the server keeps them from now on: recorded like
	// any step's, told of like any step's, and carrying no survival of
	// its own.
	pos := rider.player.Position()
	pos.X, pos.Y, pos.Z = x, y, z
	rider.move(pos, false)
	startedRidingAdvancements([]*client{rider})
	return true
}

// moveMinecartWithRider carries a cart and its rider into another
// dimension: the old world is told the cart is gone and the cart is
// re-homed at the arrival, still holding the rider the tick will seat; the
// mobs aboard follow as twins.
// vanilla: Entity.handlePortal moving the vehicle's own passengers
func (s *Server) moveMinecartWithRider(it *minecartEntity, to *dimension, pos player.Position) {
	s.minecartsMu.Lock()
	old := it.w
	it.w = to.w
	it.cart.X, it.cart.Y, it.cart.Z = pos.X, pos.Y, pos.Z
	minecartTracker.invalidatePistonGrid(s, it)
	it.tracked = nil
	s.minecartsMu.Unlock()
	s.broadcastWorld(old, protocol.IDPlayRemoveEntities, removeEntitiesPayload([]int32{it.cart.ID}))
	s.carryMinecartMobs(it, it, 0)
}

// ridingMinecart reports whether this player rides a cart, the guard the
// position packets' handlers ask: a body in a seat is where its vehicle
// keeps it.
func (c *client) ridingMinecart() bool {
	return c.minecart.Load() != nil
}

// moveIntent is the horizontal heading of the movement keys last held: a unit
// step (diagonals normalized) turned by the player's yaw; zero when opposite
// keys cancel or none is held.
// vanilla: ServerPlayer.getLastClientMoveIntent, Entity.getInputVector
func (c *client) moveIntent() (x, z float64) {
	keys := c.moveKeys.Load()
	axis := func(pos, neg uint32) float64 {
		switch keys & (pos | neg) {
		case pos:
			return 1
		case neg:
			return -1
		}
		return 0
	}
	left := axis(protocol.InputLeft, protocol.InputRight)
	forward := axis(protocol.InputForward, protocol.InputBackward)
	if left == 0 && forward == 0 {
		return 0, 0
	}
	if length := left*left + forward*forward; length > 1 {
		left, forward = left/math.Sqrt(length), forward/math.Sqrt(length)
	}
	rad := float64(c.player.Position().Yaw * (math.Pi / 180))
	sin, cos := float64(world.MthSin(rad)), float64(world.MthCos(rad))
	return left*cos - forward*sin, forward*cos + left*sin
}

// dismountMinecart takes a player off the cart they ride, the sneak of
// vanilla's Player Input: the seat given back, the body drawn out beside
// the cart, then every viewer told the passenger's off. The sneak and the
// death step out; a teleport and a leave remove the seat without the step,
// vanilla's own teleport and departure telling only the vehicle. A portal
// crossing does not dismount at all: the cart travels with its rider.
func (c *client) dismountMinecart(step bool) {
	it := c.minecart.Load()
	if it == nil {
		return
	}
	s := c.srv
	var (
		vehicleID int32
		ids       []int32
		broadcast bool
	)
	w := it.w
	// The direction and the standing spot are read under the cart lock,
	// which the tick's step holds while it moves the cart.
	var (
		sdx, sdz   int32
		cx, cy, cz float64
	)
	s.minecartsMu.Lock()
	if i := slices.Index(it.passengers, c); i >= 0 {
		it.passengers = slices.Delete(it.passengers, i, i+1)
		c.minecart.Store(nil)
		vehicleID = it.cart.ID
		ids = it.passengerIDs()
		broadcast = true
	}
	sdx, sdz = it.cart.MotionDir()
	cx, cy, cz = it.cart.X, it.cart.Y, it.cart.Z
	s.minecartsMu.Unlock()
	// The step off comes first, the passengers packet after it: vanilla's
	// stopRiding teleports the body where it dismounts and the Set
	// Passengers waits for the next sendChanges.
	if step {
		c.stepOffMinecart(cx, cy, cz, sdx, sdz)
	}
	if broadcast {
		s.gameEvent(w, rules.GameEventEntityDismount, cx, cy, cz, c.gameEventContext(0))
		s.broadcastWorld(w, protocol.IDPlaySetPassengers, setPassengersPayload(vehicleID, ids))
	}
}

// dismountMinecartTo draws a rider out of a cart whose ride has cast them
// off — the break's, the activator's — at the spot the rules name beside
// where the cart stood.
func (c *client) dismountMinecartTo(x, y, z float64, dx, dz int32) {
	sx, sy, sz := rules.MinecartDismountSpot(c.theWorld(), x, y, z, dx, dz, rules.PlayerDismountPoses)
	if c.player.Vitals().Dead {
		return
	}
	seat := player.Position{X: sx, Y: sy, Z: sz}
	seat.Yaw, seat.Pitch = c.player.Position().Yaw, c.player.Position().Pitch
	c.move(seat, false)
	c.dismountTo(seat)
}

// stepOffMinecart draws a rider out of the cart they have just left by
// their own sneak, to the spot the rules name around the cart.
// vanilla: AbstractMinecart.getDismountLocationForPassenger
func (c *client) stepOffMinecart(x, y, z float64, dx, dz int32) {
	c.dismountMinecartTo(x, y, z, dx, dz)
	c.updateAppearance(func(*player.Appearance) {})
}

// attackMinecart is the Attack packet against a cart: the reach and the
// swing spent before anything lands, the blow the held item carries, and
// the rocking the cart's metadata carries out to every viewer. A survival
// blow's accumulated damage past forty destroys the cart, the tick's next
// pass taking it out of the world with the cart's own item; a creative
// blow discards it with nothing left behind. The rider's own cart is no
// target of theirs.
// vanilla: VehicleEntity.hurtServer
func (c *client) attackMinecart(target int32) {
	it := c.srv.minecartByEntityID(c.theWorld(), target)
	if it == nil || c.minecart.Load() == it {
		return
	}
	// The reach the attack verifies against, as against a mob. The box is
	// read under the cart lock, which the tick's step holds while it moves
	// the cart.
	pos := c.player.Position()
	c.srv.minecartsMu.Lock()
	minX, minY, minZ, maxX, maxY, maxZ := it.cart.Box()
	c.srv.minecartsMu.Unlock()
	if !c.withinAttackRange(minX, minY, minZ, maxX, maxY, maxZ) {
		return
	}
	held := c.heldItem(protocol.HandMain)
	blow, ok := c.prepareAttack(held, it.entityTypeID(), false)
	if !ok {
		return
	}

	s := c.srv
	creative := c.player.GameMode() == protocol.GameModeCreative
	if !s.minecartHurtBy(it, c.theWorld(), blow.damage, creative, entity.TNTCartHit{OwnerID: c.player.EntityID}) {
		return
	}
	if it.cargo.slots != nil && !creative {
		s.minecartsMu.Lock()
		dead := it.cart.Dead
		s.minecartsMu.Unlock()
		if dead {
			c.angerPiglinsOnChestVehicleBreak(c.theWorld())
		}
	}
	extra := rules.AttackKnockback(rules.EnchantedKnockback(held, &blow.context, c.attackKnockbackBase()), blow.feedback.Knockback)
	if extra > 0 {
		vx, vy, vz := rules.ExtraKnockbackPush(float64(pos.Yaw), extra)
		s.pushMinecart(it, vx, vy, vz)
	}
	c.endSprintAfterKnockback(extra)
	if blow.feedback.Sweep {
		c.sweepHurt(c.theWorld(), held, blow.baseDamage, it.cart.ID, minX, minY, minZ, maxX, maxY, maxZ, blow.strength)
	}
	c.landedBlowFeedback(blow.feedback, it.cart.ID)
	c.finishMeleeItemEffects(held, blow.context, c.player.Vitals().FallDistance, false)
	// A non-living target wears nothing. vanilla: Player.itemAttackInteraction
	c.exhaust(rules.AttackExhaustion)
}

// pushMinecart adds to a standing cart's motion; its tick carries the push
// out. vanilla: Entity.push
func (s *Server) pushMinecart(it *minecartEntity, vx, vy, vz float64) {
	s.minecartsMu.Lock()
	if !it.cart.Dead {
		it.cart.VX, it.cart.VY, it.cart.VZ = it.cart.VX+vx, it.cart.VY+vy, it.cart.VZ+vz
	}
	s.minecartsMu.Unlock()
}

// placeMinecart is the minecart item's use on a block, vanilla's
// MinecartItem.useOn: the block has to be a rail, and the cart stands on
// the cell's centre a sixteenth over its floor — half a block further up
// a slope — once the item is spent, outside creative whose hand keeps it.
// vanilla: MinecartItem.useOn
func (c *client) placeMinecart(hand, x, y, z int32) (bool, error) {
	held := c.heldItem(hand)
	kind, cart := entity.MinecartOfItem(registry.Item(held.ItemID))
	if !cart {
		return false, nil
	}
	w := c.theWorld()
	state := w.BlockAt(x, y, z)
	if !registry.BlockOf(state).InTag(registry.TagIDRails) {
		return false, nil
	}
	if mode := c.player.GameMode(); mode != protocol.GameModeSurvival && mode != protocol.GameModeCreative {
		return false, nil
	}
	// The item first, the cart after: a hand that cannot spend its item
	// stands nothing.
	c.awardUsedHeld(hand)
	if mode := c.player.GameMode(); mode != protocol.GameModeCreative {
		if err := c.consumeHeld(hand); err != nil {
			return false, err
		}
	}
	shape := registry.StateValue(state, registry.PropShape)
	sx, sy, sz := rules.MinecartRailSpot(x, y, z, shape)
	// vanilla: EntityType.updateCustomEntityTag's onlyOpCanSetNbt gate
	if (kind == entity.MinecartCommand || kind == entity.MinecartSpawner) && !c.srv.ops.listed(c.player.UUID) {
		held.Raw = nil
	}
	c.srv.spawnMinecartFromItem(w, kind, sx, sy, sz, held)
	c.gameEventAt(rules.GameEventEntityPlace, float64(x)+0.5, float64(y)+0.5, float64(z)+0.5, w.BlockAt(x, y-1, z))
	slog.Info("minecart stood", "kind", kind, "x", sx, "y", sy, "z", sz, "by", c.player.Name)
	return true, nil
}

// cartCrossing answers the engine's detector-rail question: whether any
// live cart of this dimension crosses the box given. Called under the
// engine's lock, so the cart lock is taken and dropped by each read, the
// same one-at-a-time order the toucher keeps.
type railCarts struct {
	s   *Server
	dim *dimension
}

// CartCrossing implements world.RailCartSource.
func (t railCarts) CartCrossing(box world.Box) bool {
	t.s.minecartsMu.Lock()
	defer t.s.minecartsMu.Unlock()
	b := entity.NewAABB(box.MinX, box.MinY, box.MinZ, box.MaxX, box.MaxY, box.MaxZ)
	for _, it := range t.s.minecarts {
		if it.w != t.dim.w || it.cart.Dead {
			continue
		}
		if it.cart.Crosses(b) {
			return true
		}
	}
	return false
}

// CartContainerSignal implements world.RailCartSource.
func (t railCarts) CartContainerSignal(box world.Box) (int, bool) {
	t.s.minecartsMu.Lock()
	defer t.s.minecartsMu.Unlock()
	b := entity.NewAABB(box.MinX, box.MinY, box.MinZ, box.MaxX, box.MaxY, box.MaxZ)
	// A command cart wins over a container, as vanilla asks for it first, so
	// the first container is only remembered and read once no command cart
	// crosses.
	var container *minecartEntity
	for _, it := range t.s.minecarts {
		if it.w != t.dim.w || it.cart.Dead || (it.command == nil && it.cargo.slots == nil) || !it.cart.Crosses(b) {
			continue
		}
		if it.command != nil {
			return it.command.SuccessCount, true
		}
		if container == nil && it.cargo.slots != nil {
			container = it
		}
	}
	if container == nil {
		return 0, false
	}
	t.s.unpackVehicleLoot(container.w, container.cart.X, container.cart.Y, container.cart.Z, &container.cargo, lootOpener{})
	return entity.ContainerSignal(container.cargo.slots), true
}

// cartFall is a cart landing, with who rode it and the landing block's word
// on the fall.
type cartFall struct {
	w      *world.World
	landed float64
	effect rules.FallEffect
	riders []*client
	mobs   []*mobEntity
}

// fallOntoCartRiders hurts everyone aboard by the fall the cart landed, each
// against their own safe distance. Run with no cart lock held.
// vanilla: Entity.propagateFallToPassengers
func (s *Server) fallOntoCartRiders(f cartFall) {
	for _, c := range f.riders {
		c.fallOnto(f.landed, f.effect)
	}
	for _, ent := range f.mobs {
		s.mobsMu.Lock()
		m := ent.mob
		if s.herdOf(f.w).book[m] != ent || m.Dead {
			s.mobsMu.Unlock()
			continue
		}
		damage, hurt, soft := m.LandOnCart(f.landed, f.effect)
		var health []byte
		var fx mobEffects
		if hurt {
			health = ent.syncHealth()
		}
		if hurt || (entity.IsEquine(m.Type) && f.landed > 1) {
			t := mobDimTick{s: s, w: f.w}
			t.landed(&fx, entity.MobEvent{
				Mob: m, Kind: entity.MobLanded, Softened: soft,
				Damage: damage, DamageType: f.effect.DamageType, Fall: f.landed,
			})
		}
		s.mobsMu.Unlock()
		if hurt {
			s.broadcastWorld(f.w, protocol.IDPlayEntityMetadata, health)
		}
		s.sendMobSends(fx.sends)
	}
}
