package server

import (
	"github.com/golem-mc/golem/internal/server/pluginruntime"
	"github.com/golem-mc/golem/plugin"
	"math"
	"strconv"
	"sync"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/effect"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/raid"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

// Raid omen and Hero of the Village timings.
// vanilla: BadOmenMobEffect.applyEffectTick
const raidOmenTicks = 600

// Raiders that can join a raid look for one every second.
// vanilla: Raider.aiStep
const raiderJoinInterval = 20

// barScale is the fixed-point maximum of a raid bar's fill, a power of two
// so a float32 fill round-trips exactly.
const barScale = 1 << 24

// raiderDeath is a raider's death and the player who dealt it, if any.
type raiderDeath struct {
	ref    *raiderRef
	killer uuid.UUID
}

// raiderRef is a mob's place in a raid, the raid.Raider the raid counts.
// mobsMu guards its fields, raidsMu the raid's state; s is set by the
// enrolling sweep.
type raiderRef struct {
	s       *Server
	ent     *mobEntity
	raid    *raid.Raid
	wave    int
	canJoin bool
	outside int
	// savedRaid is the RaidId read from NBT, restored before loot pickup.
	savedRaid int
}

// enrollable is the part of Raids.canJoinRaid that reads no lock; the caller
// holds mobsMu.
func (r *raiderRef) enrollable(noAction int) bool {
	return r.canJoin && noAction <= 2400
}

func (r *raiderRef) UUID() uuid.UUID { return r.ent.uuid }

func (r *raiderRef) Alive() bool {
	r.s.mobsMu.Lock()
	defer r.s.mobsMu.Unlock()
	return !r.ent.dying && r.ent.mob.Health > 0 && r.s.herdOf(r.ent.w).book[r.ent.mob] == r.ent
}

func (r *raiderRef) Health() float64 {
	r.s.mobsMu.Lock()
	defer r.s.mobsMu.Unlock()
	return r.ent.mob.Health
}

func (r *raiderRef) Position() (x, y, z float64) {
	r.s.mobsMu.Lock()
	defer r.s.mobsMu.Unlock()
	return r.ent.mob.X, r.ent.mob.Y, r.ent.mob.Z
}

func (r *raiderRef) Age() int {
	r.s.mobsMu.Lock()
	defer r.s.mobsMu.Unlock()
	return r.ent.tickCount
}

func (r *raiderRef) NoActionTicks() int {
	r.s.mobsMu.Lock()
	defer r.s.mobsMu.Unlock()
	return r.ent.mob.NoActionTicks
}

func (r *raiderRef) TicksOutside() int {
	r.s.mobsMu.Lock()
	defer r.s.mobsMu.Unlock()
	return r.outside
}

func (r *raiderRef) SetTicksOutside(n int) {
	r.s.mobsMu.Lock()
	r.outside = n
	r.s.mobsMu.Unlock()
}

func (r *raiderRef) Enlist(rd *raid.Raid, wave int) {
	r.s.mobsMu.Lock()
	r.raid, r.wave, r.outside, r.savedRaid = rd, wave, 0, 0
	r.ent.mob.InRaid = true
	r.s.mobsMu.Unlock()
}

func (r *raiderRef) Leave() {
	r.s.mobsMu.Lock()
	r.raid = nil
	r.ent.mob.InRaid = false
	r.s.mobsMu.Unlock()
}

// raidState is what the server keeps beside the raids themselves.
type raidState struct {
	mu sync.Mutex
	// omenAt is where each player stood when bad omen became raid omen.
	omenAt map[uuid.UUID]raid.Pos
	tick   int64
	// viewsLive is whether the last tickRaiderViews pass ran, so one more
	// pass clears the views once the last raid is gone.
	viewsLive bool
}

// raidLevel is a dimension as its raids see it.
type raidLevel struct {
	s   *Server
	dim *dimension
}

func (l raidLevel) Difficulty() uint8 { return l.dim.w.Difficulty() }

func (l raidLevel) ChunkLoaded(x, z int) bool { return l.dim.w.ChunkLoadedAt(int32(x), int32(z)) }

// EntityTicking keeps a wave inside the simulation distance: the send
// view's neighbor ring loads chunks where raiders would stand still.
// vanilla: ServerLevel.isPositionEntityTicking
func (l raidLevel) EntityTicking(x, z int) bool {
	w := l.dim.w
	pos := world.ChunkPos{X: int32(x >> 4), Z: int32(z >> 4)}
	return w.ChunkLoaded(pos) && l.s.newEntityGate().ticks(w, float64(x), float64(z))
}

func (l raidLevel) IsVillage(x, y, z int) bool { return l.dim.isVillage(x, y, z) }

// isVillage is true within one section of an occupied village POI.
// vanilla: ServerLevel.isVillage
func (d *dimension) isVillage(x, y, z int) bool {
	return d.villages.SectionsToVillage(x, y, z, 1) <= 1
}

// SurfaceY is the height a mob stands at above the column's highest block.
// vanilla: Heightmap.Types.WORLD_SURFACE
func (l raidLevel) SurfaceY(x, z int) int { return int(l.dim.w.SurfaceHeight(int32(x), int32(z))) + 1 }

// CanSpawnAt checks wave placement, every chunk within ten blocks and that
// the block entity-ticks.
// vanilla: Raid.findRandomSpawnPos, LevelReader.hasChunksAt
func (l raidLevel) CanSpawnAt(x, y, z int) bool {
	w := l.dim.w
	if !l.EntityTicking(x, z) {
		return false
	}
	for cx := (x - 10) >> 4; cx <= (x+10)>>4; cx++ {
		for cz := (z - 10) >> 4; cz <= (z+10)>>4; cz++ {
			if !w.ChunkLoaded(world.ChunkPos{X: int32(cx), Z: int32(cz)}) {
				return false
			}
		}
	}
	return entity.RaidSpawnSpotOk(
		w.BlockAt(int32(x), int32(y-1), int32(z)),
		w.BlockAt(int32(x), int32(y), int32(z)),
		w.BlockAt(int32(x), int32(y+1), int32(z)),
	)
}

func (l raidLevel) Players() []raid.Player {
	l.s.mu.RLock()
	defer l.s.mu.RUnlock()
	var out []raid.Player
	for _, c := range l.s.clients {
		if c.theWorld() != l.dim.w {
			continue
		}
		pos := c.player.Position()
		out = append(out, raid.Player{
			UUID: c.player.UUID, EntityID: c.player.EntityID, Alive: c.Alive(), X: pos.X, Y: pos.Y, Z: pos.Z,
			Spectator: c.player.GameMode() == protocol.GameModeSpectator,
		})
	}
	return out
}

// Spawn stands a raider up and counts it as a raider of the wave.
// vanilla: Raid.joinRaid
func (l raidLevel) Spawn(kind string, x, y, z, wave int, leader bool, enchantOdds float32) raid.Raider {
	if !entity.KnownMobKind(entity.MobTypeOf(kind)) {
		return nil
	}
	var ref *raiderRef
	l.s.standMobArriving(l.dim, entity.MobTypeOf(kind), float64(x)+0.5, float64(y), float64(z)+0.5, 0, nil, false, "", true, func(e *mobEntity) {
		e.mob.DropSpawnLead()
		ref = &raiderRef{s: l.s, ent: e, wave: wave, canJoin: true}
		e.raider = ref
		e.mob.InRaid = true
		e.mob.ApplyRaidBuffs(wave, raid.NumGroups(1), raid.NumGroups(2), enchantOdds)
		if leader {
			e.mob.WearOminousBanner(ominousBanner())
		}
	})
	if ref == nil {
		return nil
	}
	return ref
}

// Ride seats the rider on the ravager.
func (l raidLevel) Ride(vehicle, rider raid.Raider) {
	v, ok1 := vehicle.(*raiderRef)
	r, ok2 := rider.(*raiderRef)
	if ok1 && ok2 {
		l.s.linkMobPassengers(v.ent, r.ent)
	}
}

func (l raidLevel) Hero(id uuid.UUID, amplifier int) {
	c := l.s.clientByUUID(id)
	if c == nil || c.player.GameMode() == protocol.GameModeSpectator {
		return
	}
	c.AddEffect(effect.New(registry.MobEffectHeroOfTheVillage, raid.HeroDuration, int32(amplifier), false, false, true))
	c.awardStat(player.Custom(registry.CustomStatRaidWin), 1)
	c.heroOfTheVillageAdvancements()
}

func (l raidLevel) Omen(id uuid.UUID) {
	if c := l.s.clientByUUID(id); c != nil {
		c.awardStat(player.Custom(registry.CustomStatRaidTrigger), 1)
		c.raidOmenAdvancements()
	}
}

// Horn plays the raid horn to one player from 13 blocks toward the wave.
// vanilla: Raid.playSound
func (l raidLevel) Horn(id uuid.UUID, x, z float64, _ int64) {
	if c := l.s.clientByUUID(id); c != nil {
		c.send(protocol.IDPlaySoundEffect, soundEffectIDAt(registry.SoundEventRaidHorn, protocol.SoundCategoryNeutral, x, c.player.Position().Y, z, 64, 1))
	}
}

// SendBar sends the players a Boss Event of the raid's bar.
func (l raidLevel) SendBar(players []raid.Viewer, action raid.BarAction, bar raid.BarView) {
	act := [...]int32{raid.BarAdd: protocol.BossEventAdd, raid.BarRemove: protocol.BossEventRemove,
		raid.BarProgress: protocol.BossEventProgress, raid.BarName: protocol.BossEventName}[action]
	view := world.BossBar{
		Wire: bar.Wire, Name: raidBarName(bar.Title), Value: int32(bar.Progress * barScale), Max: barScale,
		// Red, notched_10.
		Color: 2, Overlay: 2,
	}
	data := bossEventPayload(view, act)
	for _, p := range players {
		// A stale viewer must never address its replacement connection.
		// vanilla: ServerBossEvent.broadcast, addPlayer, removePlayer
		if c := l.s.clientByUUID(p.UUID); c != nil && c.player.EntityID == p.EntityID {
			c.send(protocol.IDPlayBossEvent, data)
		}
	}
}

// raidBarName is the bar title: the raid, its result, or the raid with the
// raiders left once two or fewer remain.
// vanilla: Raid.RAID_NAME_COMPONENT, RAID_BAR_VICTORY_COMPONENT, RAID_BAR_DEFEAT_COMPONENT
func raidBarName(t raid.Title) nbt.Tag {
	switch t.Kind {
	case raid.TitleVictory:
		return protocol.TextTag(protocol.Text{Translate: "event.minecraft.raid.victory.full"})
	case raid.TitleDefeat:
		return protocol.TextTag(protocol.Text{Translate: "event.minecraft.raid.defeat.full"})
	}
	text := protocol.Text{Translate: "event.minecraft.raid"}
	if t.Remaining > 0 {
		text.Extra = []protocol.Text{{Text: " - "}, {
			Translate: "event.minecraft.raid.raiders_remaining",
			With:      []protocol.Text{{Text: strconv.Itoa(t.Remaining)}},
		}}
	}
	return protocol.TextTag(text)
}

// rarityUncommon is Rarity.UNCOMMON's ordinal.
const rarityUncommon = 1

var (
	banner     protocol.ItemStack
	bannerOnce sync.Once
)

// ominousBanner is the white banner with the raid captain's eight layers, its
// item name, uncommon rarity and hidden pattern tooltip.
// vanilla: Raid.getOminousBannerTemplate
func ominousBanner() protocol.ItemStack {
	bannerOnce.Do(func() {
		layers := [][2]string{
			{"rhombus", "cyan"}, {"stripe_bottom", "light_gray"}, {"stripe_center", "gray"}, {"border", "light_gray"},
			{"stripe_middle", "black"}, {"half_horizontal", "light_gray"}, {"circle", "light_gray"}, {"border", "black"},
		}
		list := nbt.List{Elem: nbt.TypeCompound}
		for _, l := range layers {
			list.Items = append(list.Items, nbt.Compound{"pattern": nbt.String("minecraft:" + l[0]), "color": nbt.String(l[1])})
		}
		banner, _ = registry.StoredStack(nbt.Compound{
			"id": nbt.String("minecraft:white_banner"), "count": nbt.Int(1),
			"components": nbt.Compound{"minecraft:banner_patterns": list},
		})
		banner.SetItemName(protocol.Text{Translate: "block.minecraft.ominous_banner"})
		banner.SetRarity(rarityUncommon)
		banner.SetHiddenTooltipComponents(protocol.DataComponentBannerPatterns)
	})
	return banner
}

// raidTick is the tick count the village index ages its scans by.
func (s *Server) raidTick() int64 {
	s.raidState.mu.Lock()
	defer s.raidState.mu.Unlock()
	return s.raidState.tick
}

// villagePOIsAround lists claimed village POIs within radius blocks of at.
func (s *Server) villagePOIsAround(dim *dimension, at raid.Pos, radius int) []raid.Pos {
	var out []raid.Pos
	for _, p := range dim.villages.OccupiedAround(at.X, at.Y, at.Z, radius) {
		out = append(out, raid.Pos{X: p.X, Y: p.Y, Z: p.Z})
	}
	return out
}

// loadDimensionRaids reads the dimension's raids.dat.
func (s *Server) loadDimensionRaids(dim *dimension) {
	dim.villages = rules.NewVillageIndex(dim.w)
	dim.raids = raid.New(raidLevel{s: s, dim: dim})
	if data, ok := dim.w.ReadRaids(); ok {
		dim.raids.Decode(data)
	}
}

// saveRaids writes the raids of each dimension that changed.
func (s *Server) saveRaids() {
	s.raidsMu.Lock()
	defer s.raidsMu.Unlock()
	for _, dim := range s.allDims() {
		if dim.raids != nil && dim.raids.Dirty() {
			dim.w.WriteRaids(dim.raids.Encode())
			dim.raids.MarkSaved()
		}
	}
}

// tickRaids advances the omens, the raids and the raiders looking for one.
// vanilla: ServerLevel.tick (raids.tick), BadOmenMobEffect, RaidOmenMobEffect
func (s *Server) tickRaids() {
	s.raidsMu.Lock()
	s.tickRaidsLocked()
	notes := s.raidNotes
	s.raidNotes = nil
	s.raidsMu.Unlock()
	s.fireRaidNotes(notes)
}

func (s *Server) tickRaidsLocked() {
	s.raidState.mu.Lock()
	s.raidState.tick++
	tick := s.raidState.tick
	s.raidState.mu.Unlock()

	s.mu.RLock()
	clients := s.clientsLocked()
	s.mu.RUnlock()
	for _, c := range clients {
		s.tickOmens(c)
	}
	for _, dim := range s.allDims() {
		if dim.raids != nil {
			dim.raids.Tick(dim.w.BoolRule(world.RuleRaids))
		}
	}
	s.tickRaiderViews(tick)
	if tick%raiderJoinInterval == 0 {
		s.joinRaiders()
	}
}

// setRaidOmenPosition records where a player's bad omen became raid omen.
func (s *Server) setRaidOmenPosition(id uuid.UUID, at raid.Pos) {
	s.raidState.mu.Lock()
	defer s.raidState.mu.Unlock()
	if s.raidState.omenAt == nil {
		s.raidState.omenAt = map[uuid.UUID]raid.Pos{}
	}
	s.raidState.omenAt[id] = at
}

// raidOmenPosition returns the position saved for a player's raid omen.
func (s *Server) raidOmenPosition(id uuid.UUID) (raid.Pos, bool) {
	s.raidState.mu.Lock()
	defer s.raidState.mu.Unlock()
	at, ok := s.raidState.omenAt[id]
	return at, ok
}

// tickOmens turns bad omen in a village into raid omen, and expiring raid
// omen into a raid.
func (s *Server) tickOmens(c *client) {
	dim := s.dimensionOfWorld(c.theWorld())
	if dim.raids == nil || c.player.GameMode() == protocol.GameModeSpectator {
		return
	}
	const badOmen, raidOmen = registry.MobEffectBadOmen, registry.MobEffectRaidOmen
	pos := c.player.Position()
	block := raid.Pos{X: int(rules.BlockOf(pos.X)), Y: int(rules.BlockOf(pos.Y)), Z: int(rules.BlockOf(pos.Z))}
	level := raidLevel{s: s, dim: dim}
	if in, ok := c.player.Effect(badOmen); ok {
		if raid.BadOmenTurns(dim.w.Difficulty(), level.IsVillage(block.X, block.Y, block.Z), dim.raids.RaidAt(block)) {
			c.AddEffect(effect.New(raidOmen, raidOmenTicks, in.Amplifier, false, true, true))
			c.RemoveEffect(badOmen)
			s.setRaidOmenPosition(c.player.UUID, block)
			return
		}
	}
	in, ok := c.player.Effect(raidOmen)
	if !ok || !raid.RaidOmenDue(in.Duration) {
		return
	}
	s.raidState.mu.Lock()
	at, has := s.raidState.omenAt[c.player.UUID]
	delete(s.raidState.omenAt, c.player.UUID)
	s.raidState.mu.Unlock()
	if !has {
		return
	}
	// vanilla: Raids.createOrExtendRaid (CAN_START_RAID)
	if dim.w.BoolRule(world.RuleRaids) && dim.w.CanStartRaid() {
		p := raid.Player{UUID: c.player.UUID, Alive: c.Alive(), X: pos.X, Y: pos.Y, Z: pos.Z}
		dim.raids.CreateOrExtend(p, int(in.Amplifier), at, s.villagePOIsAround(dim, at, 64))
	}
	c.RemoveEffect(raidOmen)
}

// joinRaiders enrols raiders in range of a raid and gives freshly spawned
// raiders their raid state. vanilla: Raider.aiStep
func (s *Server) joinRaiders() {
	type candidate struct {
		dim *dimension
		ref *raiderRef
	}
	var found, restored []candidate
	s.mobsMu.Lock()
	for _, dim := range s.allDims() {
		for _, m := range dim.mobs.herd {
			ent := dim.mobs.book[m]
			if ent == nil || ent.dying || !entity.IsRaiderType(m.Type) {
				continue
			}
			if ent.raider == nil {
				ent.raider = &raiderRef{ent: ent}
			}
			ent.raider.s = s
			if ent.raider.savedRaid > 0 && ent.raider.raid == nil {
				// vanilla: Raider.readAdditionalSaveData
				restored = append(restored, candidate{dim, ent.raider})
			} else if ent.raider.raid == nil && ent.raider.enrollable(m.NoActionTicks) {
				found = append(found, candidate{dim, ent.raider})
			}
		}
	}
	s.mobsMu.Unlock()
	for _, f := range restored {
		s.restoreRaider(f.dim, f.ref)
	}
	for _, f := range found {
		x, y, z := f.ref.Position()
		r := f.dim.raids.RaidAt(raid.Pos{X: int(rules.BlockOf(x)), Y: int(rules.BlockOf(y)), Z: int(rules.BlockOf(z))})
		if r == nil {
			continue
		}
		r.Join(r.GroupsSpawned(), f.ref)
	}
}

// restoreRaider resolves a loaded raider's membership with raidsMu held and
// mobsMu released, since Restore calls back into the raider.
// vanilla: Raider.readAdditionalSaveData
func (s *Server) restoreRaider(dim *dimension, ref *raiderRef) *raid.Raid {
	s.mobsMu.Lock()
	id, wave := ref.savedRaid, ref.wave
	ref.savedRaid, ref.s = 0, s
	s.mobsMu.Unlock()
	r := dim.raids.Get(id)
	if r != nil {
		r.Restore(wave, ref)
	}
	return r
}

// raiderDied removes a dead raider from its raid and credits its killer as
// a hero of the village. vanilla: Raider.die
func (s *Server) raiderDied(ref *raiderRef, killer uuid.UUID) {
	s.raidsMu.Lock()
	defer s.raidsMu.Unlock()
	s.mobsMu.Lock()
	r, wave := ref.raid, ref.wave
	s.mobsMu.Unlock()
	if r == nil {
		return
	}
	if r.Leader(wave) == raid.Raider(ref) {
		r.RemoveLeader(wave)
	}
	if killer != uuid.Nil {
		r.AddHero(killer)
	}
	r.RemoveFromRaid(wave, ref, false)
}

// finishRaiderDeath takes a raider that died this tick out of its raid.
func (s *Server) finishRaiderDeath(d raiderDeath) {
	s.raiderDied(d.ref, d.killer)
}

// raiderHomeInterval is the ticks between a raider's looks for beds.
const raiderHomeInterval = 20

// tickRaiderViews snapshots raid state for raiders and villagers, and enrols
// raiders around one whose walk to the raid asked for recruits.
// vanilla: Raider.hasActiveRaid, PathfindToRaidGoal.recruitNearby
func (s *Server) tickRaiderViews(tick int64) {
	anyRaid := false
	for _, dim := range s.allDims() {
		if dim.raids != nil && len(dim.raids.All()) > 0 {
			anyRaid = true
		}
	}
	if !anyRaid && !s.raidState.viewsLive {
		return
	}
	s.raidState.viewsLive = anyRaid
	type seen struct {
		dim     *dimension
		ent     *mobEntity
		rd      *raid.Raid
		x, y, z float64
		id      int32
	}
	var raiders []seen
	s.lockTick(&s.mobsMu)
	for _, dim := range s.allDims() {
		for _, m := range dim.mobs.herd {
			if ent := dim.mobs.book[m]; ent != nil && (ent.raider != nil || m.Type == entity.TypeVillager) && !ent.dying {
				var rd *raid.Raid
				if ent.raider != nil {
					rd = ent.raider.raid
				}
				raiders = append(raiders, seen{dim: dim, ent: ent, rd: rd, x: m.X, y: m.Y, z: m.Z, id: m.ID})
			}
		}
	}
	s.unlockTick(&s.mobsMu)
	views := make([]entity.RaidView, len(raiders))
	for i, r := range raiders {
		if r.ent.mob.Type == entity.TypeVillager && r.dim.raids != nil {
			r.rd = r.dim.raids.RaidAt(raid.Pos{X: int(rules.BlockOf(r.x)), Y: int(rules.BlockOf(r.y)), Z: int(rules.BlockOf(r.z))})
		}
		if r.rd == nil {
			continue
		}
		v := &views[i]
		v.Has, v.Active, v.Over, v.Loss = true, r.rd.Active(), r.rd.Over(), r.rd.Status() == raid.Loss
		v.Preparing = r.rd.Preparing()
		if r.ent.mob.Type == entity.TypeVillager {
			continue
		}
		c := r.rd.Center()
		v.Center = entity.RaidPos{X: c.X, Y: c.Y, Z: c.Z}
		bx, by, bz := int(rules.BlockOf(r.x)), int(rules.BlockOf(r.y)), int(rules.BlockOf(r.z))
		v.InVillage = v.Active && r.dim.isVillage(bx, by, bz)
		if v.Active && !v.Over && (tick+int64(r.id))%raiderHomeInterval == 0 {
			v.Homes = []entity.RaidPos{}
			for _, p := range r.dim.villages.Around(bx, by, bz, entity.RaidHomeRadius, tick, rules.VillageScanTicks) {
				if p.Kind != rules.POIHome {
					continue
				}
				v.Homes = append(v.Homes, entity.RaidPos{X: p.X, Y: p.Y, Z: p.Z})
			}
		}
	}
	type recruiter struct {
		dim     *dimension
		rd      *raid.Raid
		x, y, z float64
	}
	var recruiters []recruiter
	s.lockTick(&s.mobsMu)
	for i, r := range raiders {
		r.ent.mob.SetRaidView(views[i])
		if r.rd != nil && r.ent.mob.TakeRaidRecruit() {
			recruiters = append(recruiters, recruiter{r.dim, r.rd, r.x, r.y, r.z})
		}
	}
	s.unlockTick(&s.mobsMu)
	for _, rc := range recruiters {
		if !rc.rd.Active() {
			continue
		}
		var near []*raiderRef
		s.lockTick(&s.mobsMu)
		for _, m := range rc.dim.mobs.herd {
			ent := rc.dim.mobs.book[m]
			if ent == nil || ent.dying || ent.raider == nil || !ent.raider.enrollable(m.NoActionTicks) || (ent.raider.raid != nil && ent.raider.raid.Active()) {
				continue
			}
			if math.Abs(m.X-rc.x) <= raidRecruitRange && math.Abs(m.Y-rc.y) <= raidRecruitRange && math.Abs(m.Z-rc.z) <= raidRecruitRange {
				near = append(near, ent.raider)
			}
		}
		s.unlockTick(&s.mobsMu)
		for _, ref := range near {
			if raid.CanJoin(ref, ref.canJoin) {
				rc.rd.Join(rc.rd.GroupsSpawned(), ref)
			}
		}
	}
}

// raidRecruitRange is how far around itself a raider walking to the raid
// recruits, in blocks. vanilla: PathfindToRaidGoal.recruitNearby
const raidRecruitRange = 16

// lootRaidCenters snapshots active raid areas without nesting the raid lock under the mob lock.
func (s *Server) lootRaidCenters() map[*world.World][]raid.Pos {
	s.raidsMu.Lock()
	defer s.raidsMu.Unlock()
	var centers map[*world.World][]raid.Pos
	for _, dim := range s.allDims() {
		if dim.raids == nil {
			continue
		}
		for _, r := range dim.raids.All() {
			if r.Active() {
				if centers == nil {
					centers = make(map[*world.World][]raid.Pos)
				}
				centers[dim.w] = append(centers[dim.w], r.Center())
			}
		}
	}
	return centers
}

// mobHasLootRaid reads membership under mobsMu and proximity from a raid snapshot.
// vanilla: Raider.hasRaid, ServerLevel.getRaidAt
func mobHasLootRaid(ent *mobEntity, centers []raid.Pos) bool {
	if !entity.IsRaiderType(ent.mob.Type) {
		return false
	}
	if ent.raider != nil && ent.raider.raid != nil {
		return true
	}
	m := ent.mob
	x, y, z := int(rules.BlockOf(m.X)), int(rules.BlockOf(m.Y)), int(rules.BlockOf(m.Z))
	for _, c := range centers {
		dx, dy, dz := x-c.X, y-c.Y, z-c.Z
		if dx*dx+dy*dy+dz*dz < 9216 { // vanilla: ServerLevel.getRaidAt, 96 blocks squared
			return true
		}
	}
	return false
}

// raidNote is one raid stage waiting for its listeners.
type raidNote struct {
	dim    *dimension
	raid   *raid.Raid
	stage  raid.Stage
	wave   int
	player uuid.UUID
	center raid.Pos
	heroes []uuid.UUID
}

// Observe queues a raid's stage for the listeners; the raid lock is held.
func (l raidLevel) Observe(r *raid.Raid, stage raid.Stage, wave int, player uuid.UUID) {
	if l.s.plugins == nil || !l.s.plugins.Has(pluginruntime.Raid) {
		return
	}
	l.s.raidNotes = append(l.s.raidNotes, raidNote{dim: l.dim, raid: r, stage: stage, wave: wave, player: player, center: r.Center(), heroes: r.Heroes()})
}

// fireRaidNotes fires RaidEvent for each queued stage with no lock held. A
// refused trigger discards the raid it created.
// vanilla: Raids.createOrExtendRaid, Raid.tick, Raid.spawnGroup
func (s *Server) fireRaidNotes(notes []raidNote) {
	stages := [...]plugin.RaidStage{raid.Trigger: plugin.RaidTrigger, raid.Wave: plugin.RaidWave, raid.Won: plugin.RaidVictory, raid.Lost: plugin.RaidLoss, raid.Ended: plugin.RaidStopped}
	for _, n := range notes {
		e := &plugin.RaidEvent{
			World:  pluginWorld{srv: s, dim: n.dim, host: s.pluginAdapter},
			Center: plugin.BlockPos{X: n.center.X, Y: n.center.Y, Z: n.center.Z},
			Stage:  stages[n.stage], Wave: n.wave,
		}
		if c := s.clientByUUID(n.player); n.player != uuid.Nil && c != nil {
			e.Player = pluginPlayer{c: c}
		}
		if n.stage == raid.Won {
			for _, id := range n.heroes {
				if c := s.clientByUUID(id); c != nil {
					e.Winners = append(e.Winners, pluginPlayer{c: c})
				}
			}
		}
		s.plugins.Fire(e)
		if n.stage == raid.Trigger && e.Cancelled() {
			s.raidsMu.Lock()
			n.dim.raids.Discard(n.raid)
			s.raidsMu.Unlock()
		}
	}
}
