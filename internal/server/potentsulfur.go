package server

import (
	"math"
	"sync"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/effect"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/registry"
)

const (
	// sulfurNauseaEvery and sulfurNauseaTicks are the stride and length of the
	// Nausea a gas cloud gives. vanilla: PotentSulfurBlockEntity.EFFECT_APPLICATION_FREQUENCY_TICKS
	sulfurNauseaEvery = 10
	sulfurNauseaTicks = 80
	// sulfurCountdownEvery is the stride of a geyser's countdown, in game ticks.
	sulfurCountdownEvery = 20
	// sulfurLaunchSpeed and sulfurLaunchForce are the speed under which a body
	// is pushed (plus 0.1 per water block) and the push.
	// vanilla: PotentSulfurBlockEntity.GEYSER_BASE_LAUNCH_SPEED, GEYSER_LAUNCH_FORCE
	sulfurLaunchSpeed = float64(float32(0.3))
	sulfurLaunchForce = float64(float32(0.2))
	// geyserSalt xors the world seed for the countdown rolls.
	// vanilla: PotentSulfurBlockEntity.geyserPositional
	geyserSalt = -904011478
)

// tickPotentSulfur runs one potent sulfur block's ticker for its state.
// vanilla: PotentSulfurBlock.getTicker
func (s *Server) tickPotentSulfur(dim *dimension, be *world.BlockEntity, age int64) {
	w := dim.w
	state := w.BlockAt(be.X, be.Y, be.Z)
	if registry.BlockOf(state) != registry.BlockPotentSulfur {
		return
	}
	switch registry.StateValue(state, world.PotentSulfurStateProp) {
	case "wet":
		s.sulfurNausea(dim, be, age)
	case "dormant":
		s.sulfurCountdown(dim, be, state, age)
		s.sulfurNausea(dim, be, age)
	case "erupting":
		s.sulfurLaunch(dim, be)
		s.sulfurCountdown(dim, be, state, age)
	case "continuous":
		s.sulfurLaunch(dim, be)
	}
}

// sulfurBodies is the mobs, items and players of one dimension, gathered once
// per tick on a geyser's first use and shared by every geyser after it, so a
// geyser costs a pass over the gathered boxes rather than over the world.
// Positions are those at the start of the block entity pass.
type sulfurBodies struct {
	gathered bool
	mobs     []sulfurBody[*entity.Mob]
	items    []sulfurBody[*itemEntity]
	players  []sulfurBody[*client]
	pushed   []sulfurPushed
	// launches are the geysers that fired this tick, applied to pushed once
	// the entities have stepped.
	launches []sulfurLaunchBox
}

// sulfurLaunchBox is a firing geyser's column and its speed limit.
type sulfurLaunchBox struct {
	box   [6]float64
	limit float64
}

// sulfurPushed is a body of another kind: its hull box, the lock guarding
// its fields, a pointer to its vertical velocity, and the fall-distance cap
// (nil for a body that banks none). controlled is a body a player's client
// moves.
type sulfurPushed struct {
	mu         *sync.Mutex
	box        [6]float64
	vy         *float64
	capFall    sulfurCapper
	controlled bool
}

// sulfurCapper is a body that banks fall distance.
type sulfurCapper interface{ CapFall() }

// sulfurRead is one body as gatherSulfurKind reads it; ok is false for one the
// geyser skips outright.
type sulfurRead struct {
	box        [6]float64
	vy         *float64
	capFall    sulfurCapper
	controlled bool
	ok         bool
}

// gatherSulfurKind appends every live body of one tracked kind in the world.
func gatherSulfurKind[B comparable, S any](s *Server, k *trackedKind[B, S], w *world.World, out *[]sulfurPushed, read func(B) sulfurRead) {
	mu := k.lock(s)
	mu.Lock()
	defer mu.Unlock()
	for _, b := range *k.list(s) {
		if k.dead(b) || k.world(b) != w {
			continue
		}
		if r := read(b); r.ok {
			*out = append(*out, sulfurPushed{mu, r.box, r.vy, r.capFall, r.controlled})
		}
	}
}

func sulfurBoxOf(x0, y0, z0, x1, y1, z1 float64) [6]float64 {
	return [6]float64{x0, y0, z0, x1, y1, z1}
}

// sulfurBody is a body with its hull box and, for a player, the eye position.
type sulfurBody[T any] struct {
	b   T
	box [6]float64
	eye [3]float64
}

// gather fills the lists on the first call after reset.
func (g *sulfurBodies) gather(s *Server, dim *dimension) {
	if g.gathered {
		return
	}
	g.gathered = true
	s.mobsMu.Lock()
	for _, m := range dim.mobs.herd {
		if !m.Alive() {
			continue
		}
		hw := m.BbWidth() / 2
		ex, ey, ez := m.EyePosition()
		g.mobs = append(g.mobs, sulfurBody[*entity.Mob]{m,
			[6]float64{m.X - hw, m.Y, m.Z - hw, m.X + hw, m.Y + m.BbHeight(), m.Z + hw}, [3]float64{ex, ey, ez}})
	}
	s.mobsMu.Unlock()
	s.itemsMu.Lock()
	for _, it := range s.items {
		if it.w != dim.w || it.item.Dead {
			continue
		}
		x0, y0, z0, x1, y1, z1 := it.item.BlastBox()
		g.items = append(g.items, sulfurBody[*itemEntity]{b: it, box: [6]float64{x0, y0, z0, x1, y1, z1}})
	}
	s.itemsMu.Unlock()
	s.mu.RLock()
	for _, c := range s.clients {
		if c.theWorld() != dim.w {
			continue
		}
		pos, h := c.player.Position(), c.dims(c.player.Appearance().Pose).EyeHeight
		if c.player.GameMode() == protocol.GameModeSpectator || c.player.Vitals().Dead {
			continue
		}
		g.players = append(g.players, sulfurBody[*client]{c,
			[6]float64{pos.X - 0.3, pos.Y, pos.Z - 0.3, pos.X + 0.3, pos.Y + h + 0.18, pos.Z + 0.3}, [3]float64{pos.X, pos.Y + h, pos.Z}})
	}
	s.mu.RUnlock()
}

// gatherOthers fills pushed with the kinds that have a server-side velocity
// besides mobs and items. Crystals retain the impulse without integrating it.
func (g *sulfurBodies) gatherOthers(s *Server, w *world.World) {
	gatherSulfurKind(s, &endCrystalTracker, w, &g.pushed, func(it *endCrystalEntity) sulfurRead {
		return sulfurRead{box: sulfurBoxOf(it.crystal.BlastBox()), vy: &it.crystal.VY, capFall: it.crystal, ok: true}
	})
	gatherSulfurKind(s, &orbTracker, w, &g.pushed, func(it *orbEntity) sulfurRead {
		return sulfurRead{box: sulfurBoxOf(it.orb.BlastBox()), vy: &it.orb.VY, capFall: it.orb, ok: true}
	})
	gatherSulfurKind(s, &tntTracker, w, &g.pushed, func(it *tntEntity) sulfurRead {
		return sulfurRead{box: sulfurBoxOf(it.tnt.BlastBox()), vy: &it.tnt.VY, capFall: it.tnt, ok: true}
	})
	gatherSulfurKind(s, &fallingTracker, w, &g.pushed, func(it *fallingEntity) sulfurRead {
		return sulfurRead{box: sulfurBoxOf(it.fb.BlastBox()), vy: &it.fb.VY, capFall: it.fb, ok: true}
	})
	gatherSulfurKind(s, &arrowTracker, w, &g.pushed, func(it *arrowEntity) sulfurRead {
		return sulfurRead{box: sulfurBoxOf(it.arrow.BlastBox()), vy: &it.arrow.VY, ok: true}
	})
	gatherSulfurKind(s, &boatTracker, w, &g.pushed, func(it *boatEntity) sulfurRead {
		return sulfurRead{box: sulfurBoxOf(it.boat.BlastBox()), vy: &it.boat.VY, capFall: it.boat,
			controlled: it.driver() != nil, ok: true}
	})
	gatherSulfurKind(s, &minecartTracker, w, &g.pushed, func(it *minecartEntity) sulfurRead {
		return sulfurRead{box: sulfurBoxOf(it.cart.BlastBox()), vy: &it.cart.VY, capFall: it.cart, ok: true}
	})
	gatherSulfurKind(s, &thrownTracker, w, &g.pushed, func(it *thrownEntity) sulfurRead {
		return sulfurRead{box: sulfurBoxOf(it.throwable.BlastBox()), vy: &it.throwable.VY, ok: true}
	})
	gatherSulfurKind(s, &standTracker, w, &g.pushed, func(it *armorStandEntity) sulfurRead {
		st := it.stand
		return sulfurRead{box: sulfurBoxOf(st.BlastBox()), vy: &st.VY, capFall: st, ok: !st.Marker}
	})
	gatherSulfurKind(s, &bobberTracker, w, &g.pushed, func(it *bobberEntity) sulfurRead {
		return sulfurRead{box: sulfurBoxOf(it.hook.BlastBox()), vy: &it.hook.VY, capFall: it.hook, ok: true}
	})
	gatherSulfurKind(s, &fireworkTracker, w, &g.pushed, func(it *fireworkEntity) sulfurRead {
		return sulfurRead{box: sulfurBoxOf(it.rocket.Box().Corners()), vy: &it.rocket.VY, ok: true}
	})
	gatherSulfurKind(s, &eyeTracker, w, &g.pushed, func(it *eyeEntity) sulfurRead {
		return sulfurRead{box: sulfurBoxOf(it.eye.Box().Corners()), vy: &it.eye.VY, ok: true}
	})
	gatherSulfurKind(s, &mannequinTracker, w, &g.pushed, func(it *mannequinEntity) sulfurRead {
		return sulfurRead{box: sulfurBoxOf(it.mannequin.BlastBox()), vy: &it.mannequin.VY, capFall: it.mannequin, ok: !it.mannequin.Dying()}
	})
}

// reset empties the lists for the next tick, keeping their capacity.
func (g *sulfurBodies) reset() {
	g.gathered = false
	clear(g.mobs)
	clear(g.items)
	clear(g.players)
	clear(g.pushed)
	g.launches = g.launches[:0]
	g.mobs, g.items, g.players, g.pushed = g.mobs[:0], g.items[:0], g.players[:0], g.pushed[:0]
}

// sulfurNausea gives Nausea to the living bodies the gas reaches.
// vanilla: PotentSulfurBlockEntity.SERVER_NAUSEA_EFFECT_TICKER
func (s *Server) sulfurNausea(dim *dimension, be *world.BlockEntity, age int64) {
	if age%sulfurNauseaEvery != 0 {
		return
	}
	w := dim.w
	sy, ok := w.PotentSulfurGasSource(be.X, be.Y, be.Z)
	if !ok {
		return
	}
	sx, sz := be.X, be.Z
	const nausea = registry.MobEffectNausea
	box := [6]float64{float64(sx) - 2.5, float64(sy), float64(sz) - 2.5, float64(sx) + 3.5, float64(sy) + 1, float64(sz) + 3.5}
	g := &dim.sulfur
	g.gather(s, dim)
	var tells []splashMobTell
	s.mobsMu.Lock()
	for _, m := range g.mobs {
		if !boxesTouch(box, m.box) || !m.b.Alive() || !w.PotentSulfurGasReaches(sx, sy, sz, m.eye[0], m.eye[1], m.eye[2]) {
			continue
		}
		ch, added := m.b.AddEffect(effect.New(nausea, sulfurNauseaTicks, 0, true, true, true))
		if !added {
			continue
		}
		if tell, ok := mobEffectTell(dim, m.b, []effect.Change{ch}); ok {
			tells = append(tells, tell)
		}
	}
	s.mobsMu.Unlock()
	for _, t := range tells {
		s.tellSplashMob(w, t)
	}
	for _, p := range g.players {
		if boxesTouch(box, p.box) && w.PotentSulfurGasReaches(sx, sy, sz, p.eye[0], p.eye[1], p.eye[2]) {
			p.b.AddEffect(effect.New(nausea, sulfurNauseaTicks, 0, true, true, true))
		}
	}
}

// sulfurCountdown steps a geyser's countdown every 20 ticks and swaps its
// state at zero.
// vanilla: PotentSulfurBlockEntity.SERVER_WAITING_COUNTDOWN_TICKER
func (s *Server) sulfurCountdown(dim *dimension, be *world.BlockEntity, state uint16, age int64) {
	if age%sulfurCountdownEvery != 0 {
		return
	}
	w := dim.w
	sy, ok := w.PotentSulfurGasSource(be.X, be.Y, be.Z)
	if !ok {
		return
	}
	dormant := registry.StateValue(state, world.PotentSulfurStateProp) == "dormant"
	var zero, live bool
	be.UseLive(func(b *world.BlockEntity) {
		live = true
		if b.Countdown <= 0 {
			water := int(sy - be.Y - 1)
			r := levelgen.NewXoroshiroRandom(w.SeedValue()^geyserSalt).ForkPositional().At(be.X, be.Y, be.Z)
			if dormant {
				b.Countdown = 10*(water-1) + int(r.NextIntN(16)) + 15
			} else {
				r.NextInt()
				b.Countdown = water - 1 + int(r.NextIntN(2)) + 1
			}
		}
		if b.Countdown > 0 {
			b.Countdown--
		}
		zero = b.Countdown == 0
	})
	if !live || !zero {
		return
	}
	next, _ := registry.WithProp(state, world.PotentSulfurStateProp, map[bool]string{true: "erupting", false: "dormant"}[dormant])
	s.setBlockAndTell(w, be.X, be.Y, be.Z, next)
	if dormant {
		s.tellEffect(w, world.PotentSulfurGeyserStart(next, be.X, be.Y, be.Z))
	} else {
		s.tellEffect(w, world.Effect{GameEvent: registry.GameEventBlockDeactivate, X: be.X, Y: be.Y, Z: be.Z})
	}
}

// sulfurLaunch pushes the bodies in the column above a geyser upward, and caps
// the fall distance of every body in it, players included; players and
// steered mounts are left to their clients.
// vanilla: PotentSulfurBlockEntity.LAUNCH_ENTITY_TICKER, Entity.canSimulateMovement
func (s *Server) sulfurLaunch(dim *dimension, be *world.BlockEntity) {
	w := dim.w
	sy, ok := w.PotentSulfurGasSource(be.X, be.Y, be.Z)
	if !ok {
		return
	}
	water := int(sy - be.Y - 1)
	height := w.PotentSulfurLaunchHeight(be.X, be.Y, be.Z, water)
	// vanilla: AABB(pos.above()).expandTowards(0, height - 1, 0)
	grow := float64(height - 1)
	box := [6]float64{float64(be.X), float64(be.Y+1) + math.Min(0, grow), float64(be.Z),
		float64(be.X + 1), float64(be.Y+2) + math.Max(0, grow), float64(be.Z + 1)}
	limit := sulfurLaunchSpeed + float64(water)*0.1

	g := &dim.sulfur
	g.gather(s, dim)
	s.mobsMu.Lock()
	for _, mb := range g.mobs {
		m := mb.b
		// vanilla: Entity.canSimulateMovement; the rider's client moves a steered mount
		if !boxesTouch(box, mb.box) || !m.Alive() {
			continue
		}
		entity.CapFallAccumulation(m.VY, &m.FallDistance)
		if m.Vehicle == nil && !m.RidesVehicle && !m.Steered() && m.VY < limit &&
			!registry.EntityType(mobEntityType(m.Type)).InTag(registry.EntityTypeTagIDNotAffectedByGeysers) {
			m.VY += sulfurLaunchForce
		}
	}
	s.mobsMu.Unlock()

	s.itemsMu.Lock()
	for _, it := range g.items {
		if it.b.item.Dead || !boxesTouch(box, it.box) {
			continue
		}
		it.b.item.CapFall()
		if it.b.item.VY < limit {
			it.b.item.Push(0, sulfurLaunchForce, 0)
		}
	}
	s.itemsMu.Unlock()

	g.launches = append(g.launches, sulfurLaunchBox{box, limit})
	for _, p := range g.players {
		if boxesTouch(box, p.box) && p.b.player.Velocity().Y > -0.5 {
			p.b.player.UpdateVitals(func(v *player.Vitals) { v.FallDistance = min(v.FallDistance, 1) })
		}
	}
}

// finishSulfurLaunches pushes the boats, carts, projectiles, TNT, falling
// blocks, orbs and armor stands over this tick's firing geysers, after the
// entities have stepped. vanilla: ServerLevel.tick (entities, then block entities)
func (s *Server) finishSulfurLaunches() {
	for _, dim := range s.allDims() {
		g := &dim.sulfur
		if len(g.launches) == 0 {
			continue
		}
		g.gatherOthers(s, dim.w)
		for _, l := range g.launches {
			for _, p := range g.pushed {
				if !boxesTouch(l.box, p.box) {
					continue
				}
				p.mu.Lock()
				if p.capFall != nil {
					p.capFall.CapFall()
				}
				if !p.controlled && *p.vy < l.limit {
					*p.vy += sulfurLaunchForce
				}
				p.mu.Unlock()
			}
		}
		clear(g.pushed)
		g.pushed, g.launches = g.pushed[:0], g.launches[:0]
	}
}

// boxesTouch is AABB.intersects: overlap with positive volume.
func boxesTouch(a, b [6]float64) bool {
	return a[0] < b[3] && a[3] > b[0] && a[1] < b[4] && a[4] > b[1] && a[2] < b[5] && a[5] > b[2]
}
