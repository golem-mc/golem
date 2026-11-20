package server

import (
	"maps"
	"math"
	"slices"
	"sync"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// The wither: the summoning a placed skull completes, the skulls its heads
// shoot, the summoning's blast and its boss bar. The body is
// internal/server/entity/wither.go's.

// witherSummonReach is the box growth around the new wither inside which
// players get summoned_entity.
const witherSummonReach = 50.0

// barPrecision scales a fill to the bar's integer value and maximum.
const barPrecision = 1_000_000

// trySummonWither builds the wither, if any, a skull just placed at (x, y, z)
// completes: the pattern is cleared, the wither stands in the foot cell's
// half-block lifted 0.55, facing along the pattern's axis, invulnerable.
// vanilla: WitherSkullBlock.checkSpawn
func (s *Server) trySummonWither(w *world.World, x, y, z int32) {
	block := registry.BlockOf(w.BlockAt(x, y, z))
	if block != registry.BlockWitherSkeletonSkull && block != registry.BlockWitherSkeletonWallSkull ||
		y < w.MinY() || w.Difficulty() == rules.DifficultyPeaceful {
		return
	}
	spawn, ok := rules.WitherSpawnOf(w, x, y, z)
	if !ok {
		return
	}
	for _, cell := range spawn.Clear {
		s.clearGolemCell(w, cell[0], cell[1], cell[2])
	}
	yaw := float32(90)
	if spawn.ForwardsX {
		yaw = 0
	}
	wx, wy, wz := float64(spawn.Feet[0])+0.5, float64(spawn.Feet[1])+0.55, float64(spawn.Feet[2])+0.5
	wither := s.standMobArriving(s.dimensionOfWorld(w), entity.TypeWither, wx, wy, wz, 0, nil, false, "", false, func(ent *mobEntity) {
		ent.mob.Yaw, ent.mob.HeadYaw = yaw, yaw
		ent.mob.MakeInvulnerable()
	})
	s.summonedBuildAdvancements(w, wither, wx, wy, wz, witherSummonReach)
}

// witherSkullPlaces reports whether dispenseWitherSkull would place the skull.
func (s *Server) witherSkullPlaces(w *world.World, d world.Dispense) bool {
	tx, ty, tz := faceOffset(d.X, d.Y, d.Z, int32(d.Dir))
	return world.IsKnownAirState(w.BlockAt(tx, ty, tz)) && ty >= w.MinY()+2 && w.Difficulty() != rules.DifficultyPeaceful &&
		rules.WitherBaseAt(w, tx, ty, tz)
}

// dispenseWitherSkull places the skull on the face's cell when the T stands
// there, which then summons like a player's. It reports whether it did.
// vanilla: DispenseItemBehavior (wither skeleton skull), WitherSkullBlock.canSpawnMob
func (s *Server) dispenseWitherSkull(w *world.World, d world.Dispense) bool {
	if !s.witherSkullPlaces(w, d) {
		return false
	}
	tx, ty, tz := faceOffset(d.X, d.Y, d.Z, int32(d.Dir))
	// A skull faces opposite the dispenser, a vertical one rotation 0.
	// vanilla: SkullBlock.ROTATION, Direction.get2DDataValue
	rotation := 0
	switch d.Dir {
	case 3: // south
		rotation = 8
	case 4: // west
		rotation = 12
	case 5: // east
		rotation = 4
	}
	state, ok := registry.WithInt(registry.BlockWitherSkeletonSkull.DefaultState(), registry.PropRotation, rotation)
	if !ok {
		return false
	}
	old := w.BlockAt(tx, ty, tz)
	if !s.writeBlock(w, tx, ty, tz, state, nil, writeLight|writeLightAfter) {
		return false
	}
	s.updateNeighborShapes(w, tx, ty, tz)
	s.blockChanged(w, tx, ty, tz, old)
	s.gameEvent(w, rules.GameEventBlockPlace, float64(tx)+0.5, float64(ty)+0.5, float64(tz)+0.5, rules.GameEventContext{Affected: state})
	s.trySummonWither(w, tx, ty, tz)
	return true
}

// witherShot is a wither head's skull, spawned outside the mob lock.
type witherShot struct {
	w          *world.World
	shooter    int32
	uuid       uuid.UUID
	silent     bool
	bx, by, bz int32
	x, y, z    float64
	dx, dy, dz float64
	dangerous  bool
	seed       int64
}

func witherShotOf(w *world.World, ev entity.MobEvent, ent *mobEntity) witherShot {
	shot := witherShot{w: w, shooter: ev.Mob.ID, silent: ev.Mob.Silent,
		bx: int32(math.Floor(ev.Mob.X)), by: int32(math.Floor(ev.Mob.Y)), bz: int32(math.Floor(ev.Mob.Z)),
		x: ev.X, y: ev.Y, z: ev.Z, dx: ev.VX, dy: ev.VY, dz: ev.VZ, dangerous: ev.Amount != 0, seed: ev.Mob.Rand.Int63()}
	if ent != nil {
		shot.uuid = ent.uuid
	}
	return shot
}

// witherShoot rings the launch and spawns the skull.
// vanilla: WitherBoss.performRangedAttack
func (s *Server) witherShoot(shot witherShot) {
	if !shot.silent {
		s.broadcastLevelEvent(shot.w, protocol.WorldEventWitherShoot, shot.bx, shot.by, shot.bz, 0)
	}
	s.spawnFireball(entity.ThrownWitherSkull, shot.w, shot.x, shot.y, shot.z, shot.dx, shot.dy, shot.dz,
		shot.shooter, shot.uuid, 0, shot.seed, func(t *entity.Thrown) { t.Dangerous = shot.dangerous })
}

// witherBlast is the summoning's end: a blast of the wither's own, which a
// mob_griefing rule vetoes, and the global level event 1023.
// vanilla: WitherBoss.customServerAiStep
func (t *mobDimTick) witherBlast(fx *mobEffects, ev entity.MobEvent) {
	s, h, w := t.s, t.h, t.w
	ent := h.book[ev.Mob]
	if ent == nil {
		return
	}
	if !ev.Mob.Silent {
		fx.witherSpawns = append(fx.witherSpawns, witherSpawnSound{w: w,
			x: int32(math.Floor(ev.Mob.X)), y: int32(math.Floor(ev.Mob.Y)), z: int32(math.Floor(ev.Mob.Z))})
	}
	fx.blasts = append(fx.blasts, blast{
		w: w, x: ev.X, y: ev.Y, z: ev.Z, radius: ev.Radius,
		causeID: ev.Mob.ID, causeName: mobName(ent), causeKiller: t.s.mobKiller(ent),
		directID: ev.Mob.ID, directUUID: ent.uuid, directLiving: true, directType: ev.Mob.Type.Name(), directLoot: frozenMobLoot(ev.Mob), silent: ev.Mob.Silent, directInWater: ev.Mob.InWater(),
		causeUUID: ent.uuid, damageType: rules.DamageTypePlayerExplosion, rng: ev.Mob.Rand,
		interaction: rules.MobExplosionInteraction(s.world),
		sourceTeam:  ev.Mob.Team,
	})
}

// witherSpawnSound is the global level event 1023 a summoning owes.
type witherSpawnSound struct {
	w       *world.World
	x, y, z int32
}

func (s *Server) tellWitherSpawn(e witherSpawnSound) {
	s.tellGlobalLevelEvent(e.w, protocol.WorldEventWitherSpawn, e.x, e.y, e.z, 0)
}

// witherBar is one wither's boss bar as its watchers were told it.
type witherBar struct {
	wire    uuid.UUID
	viewers map[uuid.UUID]struct{}
	fill    float32
}

// witherBarView is a wither's bar as a tick found it.
type witherBarView struct {
	w        *world.World
	id       int32
	wire     uuid.UUID
	name     nbt.Tag
	fill     float32
	watchers []uuid.UUID
}

// witherBars are the live bars by wither entity ID.
type witherBars struct {
	mu   sync.Mutex
	bars map[int32]*witherBar
}

func witherBarOf(w *world.World, ent *mobEntity) witherBarView {
	name := ent.name
	if !protocol.IsTextComponent(name) {
		name = nbt.String(mobDisplayName(ent.mob.Type))
	}
	return witherBarView{w: w, id: ent.mob.ID, wire: ent.uuid, name: name, fill: ent.mob.WitherBarFill(),
		watchers: trackedWatchers(ent.tracked)}
}

// bar is a purple, darkening progress bar named for the wither.
func (v witherBarView) bar() world.BossBar {
	return world.BossBar{Wire: v.wire, Name: v.name, Visible: true,
		Value: int32(math.Round(float64(v.fill) * barPrecision)), Max: barPrecision,
		Color: protocol.BossBarColorPurple, Overlay: protocol.BossBarOverlayProgress, DarkenScreen: true}
}

// showWitherBars gives each watcher of a wither its bar, takes it from one
// that stopped watching and tells the viewers a new fill; a bar whose wither
// did not tick this time is taken from all its viewers.
// vanilla: WitherBoss.startSeenByPlayer, stopSeenByPlayer, ServerBossEvent.setProgress
func (s *Server) showWitherBars(views []witherBarView) {
	s.witherBars.mu.Lock()
	defer s.witherBars.mu.Unlock()
	if s.witherBars.bars == nil {
		s.witherBars.bars = map[int32]*witherBar{}
	}
	seen := make(map[int32]bool, len(views))
	for _, v := range views {
		seen[v.id] = true
		shown, ok := s.witherBars.bars[v.id]
		if !ok {
			shown = &witherBar{wire: v.wire, viewers: map[uuid.UUID]struct{}{}, fill: v.fill}
			s.witherBars.bars[v.id] = shown
		}
		bar := v.bar()
		var joined, left []uuid.UUID
		for _, p := range v.watchers {
			if _, in := shown.viewers[p]; !in {
				joined = append(joined, p)
				shown.viewers[p] = struct{}{}
			}
		}
		for p := range shown.viewers {
			if !slices.Contains(v.watchers, p) {
				left = append(left, p)
				delete(shown.viewers, p)
			}
		}
		s.sendBossEvent(joined, bar, protocol.BossEventAdd)
		s.sendBossEvent(left, bar, protocol.BossEventRemove)
		if v.fill != shown.fill {
			shown.fill = v.fill
			var rest []uuid.UUID
			for p := range shown.viewers {
				if !slices.Contains(joined, p) {
					rest = append(rest, p)
				}
			}
			s.sendBossEvent(rest, bar, protocol.BossEventProgress)
		}
	}
	for id, shown := range s.witherBars.bars {
		if !seen[id] {
			s.sendBossEvent(slices.Collect(maps.Keys(shown.viewers)), world.BossBar{Wire: shown.wire}, protocol.BossEventRemove)
			delete(s.witherBars.bars, id)
		}
	}
}

// witherSmash carries the bounds of a wither's block-breaking pass.
type witherSmash struct {
	w              *world.World
	x, y, z        int32
	radius, height int32
	mob            *entity.Mob
}

func witherSmashOf(w *world.World, ev entity.MobEvent) witherSmash {
	return witherSmash{w: w, x: int32(math.Floor(ev.X)), y: int32(math.Floor(ev.Y)), z: int32(math.Floor(ev.Z)),
		radius: int32(ev.Radius), height: int32(ev.Amount), mob: ev.Mob}
}

// witherSmashed breaks the cells with drops and rings level event 1022 when
// any broke.
// vanilla: WitherBoss.customServerAiStep
func (s *Server) witherSmashed(sm witherSmash) {
	broke := false
	for dx := -sm.radius; dx <= sm.radius; dx++ {
		for dz := -sm.radius; dz <= sm.radius; dz++ {
			for dy := int32(0); dy <= sm.height; dy++ {
				if s.mobBrokeBlock(mobBlockBreak{w: sm.w, mob: sm.mob, x: sm.x + dx, y: sm.y + dy, z: sm.z + dz, by: entity.TypeWither}) {
					broke = true
				}
			}
		}
	}
	if broke {
		s.broadcastLevelEvent(sm.w, protocol.WorldEventWitherBreak, sm.x, sm.y, sm.z, 0)
	}
}

// witherRose is where a body a wither killed leaves a rose.
type witherRose struct {
	w       *world.World
	x, y, z float64
}

// witherRoseLeft plants a wither rose in the cell the body died in when
// mob_griefing allows and the rose survives there, else drops the item.
// vanilla: LivingEntity.createWitherRose
func (s *Server) witherRoseLeft(r witherRose) {
	bx, by, bz := int32(math.Floor(r.x)), int32(math.Floor(r.y)), int32(math.Floor(r.z))
	rose := registry.BlockWitherRose.DefaultState()
	if r.w.BoolRule(world.RuleMobGriefing) && r.w.IsAirAt(bx, by, bz) && r.w.Survives(bx, by, bz, rose) {
		s.setBlockAndTell(r.w, bx, by, bz, rose)
		return
	}
	s.spawnItemAtFeet(r.w, protocol.ItemStack{ItemID: int32(registry.ItemWitherRose), Count: 1}, r.x, r.y, r.z)
}
