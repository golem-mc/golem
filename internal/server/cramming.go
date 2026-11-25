package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/google/uuid"
)

// crammer is a pushable body a player's box may share, as it stood when the
// survival tick first needed the crowd.
type crammer struct {
	box   entity.AABB
	owner *client
	team  string
	rule  entity.CollisionRule
	// riding is whether the body is a passenger; passengers are not counted.
	riding bool
}

// crammingCrowd is one tick's pushable bodies per world, filed in a grid.
// It is built once, by the first player whose world has a cramming limit,
// from one pass over each body list instead of a lock per player.
type crammingCrowd struct {
	built   bool
	clients []*client
	worlds  map[*world.World]*crammingWorld
	// self is where each filed player's own body lies.
	self map[*client]crammerAt
	// filed are the mobs already in a world's crowd, so one near two players
	// is filed once.
	filed map[*entity.Mob]struct{}
}

type crammerAt struct {
	cw *crammingWorld
	i  int32
}

type crammingWorld struct {
	// bodies[:players] are the players; the rest cross one of their boxes.
	bodies  []crammer
	players int
	grid    entity.BoxGrid
	// all bounds the players' boxes.
	all entity.AABB
}

func (cc *crammingCrowd) world(w *world.World) *crammingWorld {
	cw := cc.worlds[w]
	if cw == nil {
		cw = &crammingWorld{grid: entity.BoxGrid{Cell: cartPushCell}}
		cc.worlds[w] = cw
	}
	return cw
}

func (cw *crammingWorld) add(b crammer) int32 {
	i := int32(len(cw.bodies))
	cw.grid.Add(i, b.box)
	cw.bodies = append(cw.bodies, b)
	return i
}

// nearPlayer reports whether box crosses a filed player's box; a body that
// crosses none can be counted by no player.
func (cw *crammingWorld) nearPlayer(box entity.AABB) bool {
	if !box.Crosses(cw.all) {
		return false
	}
	found := false
	cw.grid.Query(box, func(i int32) {
		if !found && int(i) < cw.players && box.Crosses(cw.bodies[i].box) {
			found = true
		}
	})
	return found
}

// lookup is the world's crowd if it holds a player, else nil.
func (cc *crammingCrowd) lookup(w *world.World) *crammingWorld {
	if cw := cc.worlds[w]; cw != nil && cw.players > 0 {
		return cw
	}
	return nil
}

// build files the players that are alive and not spectating, then each mob,
// boat and minecart whose box crosses one of theirs.
// vanilla: Level.getPushableEntities, EntitySelector.pushableBy
func (s *Server) buildCrammingCrowd(cc *crammingCrowd) {
	cc.built = true
	if cc.worlds == nil {
		cc.worlds = make(map[*world.World]*crammingWorld)
		cc.self = make(map[*client]crammerAt)
		cc.filed = make(map[*entity.Mob]struct{})
	}
	for _, cw := range cc.worlds {
		clear(cw.bodies)
		cw.bodies = cw.bodies[:0]
		cw.players = 0
		cw.grid.Reset()
	}
	clear(cc.self)
	clear(cc.filed)
	teams := s.scoreboard.MemberTeams()
	var rulesOf map[string]entity.CollisionRule
	if teams != nil {
		rulesOf = s.teamCollisionRules()
	}
	for _, c := range cc.clients {
		if c.player == nil || c.player.GameMode() == protocol.GameModeSpectator || c.player.Vitals().Dead {
			continue
		}
		pos := c.player.Position()
		dims := c.dims(c.player.Appearance().Pose)
		half := dims.Width / 2
		team := teams[c.player.Name]
		cw := cc.world(c.theWorld())
		box := entity.NewAABB(pos.X-half, pos.Y, pos.Z-half, pos.X+half, pos.Y+dims.Height, pos.Z+half)
		if cw.players == 0 {
			cw.all = box
		} else {
			cw.all = cw.all.Union(box)
		}
		i := cw.add(crammer{box: box, owner: c, team: team, rule: rulesOf[team], riding: c.isPassenger()})
		cw.players++
		cc.self[c] = crammerAt{cw, i}
	}
	if len(cc.self) == 0 {
		return
	}
	s.lockTick(&s.mobsMu)
	for _, dim := range s.allDims() {
		cw := cc.lookup(dim.w)
		if cw == nil {
			continue
		}
		herd := s.herdOf(dim.w)
		for p := 0; p < cw.players; p++ {
			bounds := cw.bodies[p].box.Bounds()
			area, indexed := smallChunkArea(&bounds)
			herd.mobsInArea(area, indexed, func(m *entity.Mob) bool {
				if _, filed := cc.filed[m]; filed {
					return true
				}
				// The box first: it is fewer of the mob's fields to read than
				// Pushable's.
				box := entity.NewAABB(m.BlastBox())
				if !cw.nearPlayer(box) || !m.Pushable() {
					return true
				}
				cc.filed[m] = struct{}{}
				cw.add(crammer{box: box, team: m.Team, rule: m.TeamCollision, riding: m.RidesVehicle})
				return true
			})
		}
	}
	s.unlockTick(&s.mobsMu)
	s.lockTick(&s.boatsMu)
	for _, it := range s.boats {
		cw := cc.lookup(it.w)
		if cw == nil || it.boat.Dead {
			continue
		}
		box := entity.NewAABB(it.boat.Box())
		if !cw.nearPlayer(box) {
			continue
		}
		team := teams[it.uuid.String()]
		cw.add(crammer{box: box, team: team, rule: rulesOf[team]})
	}
	s.unlockTick(&s.boatsMu)
	s.lockTick(&s.minecartsMu)
	for _, it := range s.minecarts {
		cw := cc.lookup(it.w)
		if cw == nil || it.cart.Dead {
			continue
		}
		box := entity.NewAABB(it.cart.Box())
		if !cw.nearPlayer(box) {
			continue
		}
		team := teams[it.uuid.String()]
		cw.add(crammer{box: box, team: team, rule: rulesOf[team]})
	}
	s.unlockTick(&s.minecartsMu)
}

// crammingHurt is the cramming hurt of the player's tick, if it lands: the
// bodies sharing its box counted against max_entity_cramming.
// vanilla: LivingEntity.pushEntities
func (c *client) crammingHurt() (rules.Hurt, bool) {
	w := c.theWorld()
	limit := int(w.MaxEntityCramming())
	if limit <= 0 || c.player.GameMode() == protocol.GameModeSpectator || !c.srv.tickRate.Running() {
		return rules.Hurt{}, false
	}
	cc := &c.srv.crammers
	if !cc.built {
		c.srv.buildCrammingCrowd(cc)
	}
	// A player missing from the crowd was dead or spectating when it was built.
	at, ok := cc.self[c]
	if !ok {
		return rules.Hurt{}, false
	}
	cw := at.cw
	me := cw.bodies[at.i]
	box, team, rule := me.box, me.team, me.rule
	total, standing := 0, 0
	cw.grid.Query(box, func(i int32) {
		b := &cw.bodies[i]
		if b.owner == c || !box.Crosses(b.box) || !entity.PushableBy(team, rule, b.team, b.rule) {
			return
		}
		total++
		if !b.riding {
			standing++
		}
	})
	if !rules.CrammingHurt(limit, total, standing, func() int { return c.srv.envRng.Intn(4) }) {
		return rules.Hurt{}, false
	}
	return rules.Hurt{DamageType: rules.DamageTypeCramming, Amount: rules.CrammingDamage}, true
}

// readMobVehicleCrowds files each dimension's live boats and minecarts for
// the mob tick's cramming, in the worlds where the rule is on.
// vanilla: LivingEntity.pushEntities, AbstractBoat.isPushable,
// AbstractMinecart.isPushable
func (s *Server) readMobVehicleCrowds(in *mobTickInputs) {
	crowd := func(w *world.World) *entity.PushCrowd {
		for _, dim := range s.allDims() {
			if dim.w == w && dim.vehiclesCrammed {
				return &dim.vehicles
			}
		}
		return nil
	}
	body := func(box entity.AABB, id uuid.UUID) entity.PushBody {
		b := entity.PushBody{Box: box, Rule: entity.CollideAlways}
		if in.teams != nil {
			b.Team = in.teams[id.String()]
			b.Rule = in.teamRules[b.Team]
		}
		return b
	}
	for _, dim := range s.allDims() {
		dim.vehicles.Reset()
		dim.vehicles.Grid.Cell = cartPushCell
		dim.vehiclesCrammed = dim.w.MaxEntityCramming() > 0
	}
	s.lockTick(&s.boatsMu)
	for _, it := range s.boats {
		if c := crowd(it.w); c != nil && !it.boat.Dead {
			c.Add(body(entity.NewAABB(it.boat.Box()), it.uuid))
		}
	}
	s.unlockTick(&s.boatsMu)
	s.lockTick(&s.minecartsMu)
	for _, it := range s.minecarts {
		if c := crowd(it.w); c != nil && !it.cart.Dead {
			c.Add(body(entity.NewAABB(it.cart.Box()), it.uuid))
		}
	}
	s.unlockTick(&s.minecartsMu)
}
