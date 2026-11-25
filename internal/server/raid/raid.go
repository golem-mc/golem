// Package raid is the village raid: its waves, boss bar and saved state.
// vanilla: Raid, Raids
package raid

import (
	"math"
	"math/rand"
	"slices"

	"github.com/google/uuid"
)

// Difficulty ids, as the difficulty rule numbers them.
const (
	difficultyPeaceful = 0
	difficultyEasy     = 1
	difficultyNormal   = 2
	difficultyHard     = 3
)

const (
	// preRaidTicks is the countdown before each wave. vanilla: Raid.DEFAULT_PRE_RAID_TICKS
	preRaidTicks = 300
	// timeoutTicks ends a raid that has run this long. vanilla: Raid.RAID_TIMEOUT_TICKS
	timeoutTicks = 48000
	// postRaidTickLimit is the pause after the last raider before victory.
	postRaidTickLimit = 40
	// celebrationTicks is how long the result bar stays up. vanilla: Raid.MAX_CELEBRATION_TICKS
	celebrationTicks = 600
	// maxNoActionTicks and outsideTimeout drop a raider that idles outside
	// the village. vanilla: Raid.MAX_NO_ACTION_TIME, OUTSIDE_RAID_BOUNDS_TIMEOUT
	maxNoActionTicks = 2400
	outsideTimeout   = 30
	// MaxOmenLevel is the raid omen level a raid stops absorbing at.
	MaxOmenLevel = 5
	// HeroDuration is the Hero of the Village effect, in ticks.
	HeroDuration = 48000
	// validRadiusSqr is how near a block must be to a raid centre to be in
	// it; removalSqr is how far a raider may stray. vanilla: Raid.VALID_RAID_RADIUS_SQR, RAID_REMOVAL_THRESHOLD_SQR
	validRadiusSqr = 9216
	removalSqr     = 12544
	// spawnAttempts bounds the failed spawn searches before the raid stops.
	spawnAttempts = 5
)

// Entity types of the raiders, in the order a wave spawns them.
const (
	Vindicator = "minecraft:vindicator"
	Evoker     = "minecraft:evoker"
	Pillager   = "minecraft:pillager"
	Witch      = "minecraft:witch"
	Ravager    = "minecraft:ravager"
)

// spawnsPerWave is each type's count per wave before the difficulty bonus,
// indexed by wave (0 unused, 7 the bonus wave).
// vanilla: Raid.RaiderType
var spawnsPerWave = []struct {
	kind  string
	count [8]int
}{
	{Vindicator, [8]int{0, 0, 2, 0, 1, 4, 2, 5}},
	{Evoker, [8]int{0, 0, 0, 0, 0, 1, 1, 2}},
	{Pillager, [8]int{0, 4, 3, 3, 4, 4, 4, 2}},
	{Witch, [8]int{0, 0, 0, 0, 3, 0, 0, 1}},
	{Ravager, [8]int{0, 0, 0, 1, 0, 1, 0, 2}},
}

// Pos is a block position.
type Pos struct{ X, Y, Z int }

func (p Pos) distSqr(o Pos) float64 {
	dx, dy, dz := float64(p.X-o.X), float64(p.Y-o.Y), float64(p.Z-o.Z)
	return dx*dx + dy*dy + dz*dz
}

// Player is a player of the raid's dimension.
type Player struct {
	UUID      uuid.UUID
	EntityID  int32
	Alive     bool
	Spectator bool
	X, Y, Z   float64
}

func (p Player) viewer() Viewer { return Viewer{UUID: p.UUID, EntityID: p.EntityID} }

// Raider is a mob in a raid, identified across reloads by its UUID.
type Raider interface {
	UUID() uuid.UUID
	// Alive is false once the mob is dying or gone.
	Alive() bool
	Health() float64
	Position() (x, y, z float64)
	// Age is the mob's tick count; NoActionTicks its idleness counter.
	Age() int
	NoActionTicks() int
	TicksOutside() int
	SetTicksOutside(n int)
	// Enlist is told the raid and wave now counting the mob; Leave that the
	// raid no longer does.
	Enlist(r *Raid, wave int)
	Leave()
}

// Level is what a raid asks of its dimension.
type Level interface {
	Difficulty() uint8
	// ChunkLoaded reports whether the chunk holding the block is in memory.
	ChunkLoaded(x, z int) bool
	// EntityTicking reports whether entities at the block are stepped.
	EntityTicking(x, z int) bool
	IsVillage(x, y, z int) bool
	// SurfaceY is the world-surface height of the column.
	SurfaceY(x, z int) int
	// CanSpawnAt is the placement test of a raider at the block, with the
	// surrounding chunks loaded and ticking.
	CanSpawnAt(x, y, z int) bool
	Players() []Player
	// Spawn stands a raider of the kind up on the block, nil for a kind
	// that is not simulated. leader gives it the ominous banner.
	Spawn(kind string, x, y, z, wave int, leader bool, enchantOdds float32) Raider
	// Ride seats the rider on the vehicle.
	Ride(vehicle, rider Raider)
	// Hero gives the player Hero of the Village at the amplifier and counts
	// the win; Omen counts the raid trigger.
	Hero(id uuid.UUID, amplifier int)
	Omen(id uuid.UUID)
	// Horn plays the raid horn from the block to the player, 13 blocks
	// toward the wave.
	Horn(id uuid.UUID, x, z float64, seed int64)
	Bar
}

// Stage is a step of a raid's life an Observer is told of.
type Stage int

const (
	Trigger Stage = iota
	Wave
	Won
	Lost
	// Ended is a raid stopped without a result.
	Ended
)

// Observer is an optional part of a Level: it hears each Stage of a raid, as
// it happens, with the raid's own state in the server's raid lock. The
// Wave is the number just spawned; Player the trigger's.
type Observer interface {
	Observe(r *Raid, stage Stage, wave int, player uuid.UUID)
}

func (r *Raid) note(stage Stage, wave int, player uuid.UUID) {
	if o, ok := r.level.(Observer); ok {
		o.Observe(r, stage, wave, player)
	}
}

// Heroes are the players who killed a raider, who share a won raid.
func (r *Raid) Heroes() []uuid.UUID { return slices.Clone(r.heroes) }

// Raid is one village raid.
type Raid struct {
	level   Level
	owner   *Raids
	rng     *rand.Rand
	groups  map[int][]Raider
	leaders map[int]Raider
	heroes  []uuid.UUID

	// id is the key in owner's raids, 0 until the raid is registered.
	id            int
	center        Pos
	started       bool
	active        bool
	ticksActive   int64
	omenLevel     int
	groupsSpawned int
	cooldown      int
	postRaid      int
	celebration   int
	totalHealth   float32
	numGroups     int
	status        Status
	waveSpawn     *Pos
	bar           *bossBar
}

// Status is where a raid stands.
type Status int

// The statuses a raid moves through; the saved form names them.
const (
	Ongoing Status = iota
	Victory
	Loss
	Stopped
)

var statusNames = []string{"ongoing", "victory", "loss", "stopped"}

func (s Status) String() string { return statusNames[s] }

// NumGroups is the number of waves of the difficulty.
// vanilla: Raid.getNumGroups
func NumGroups(difficulty uint8) int {
	switch difficulty {
	case difficultyEasy:
		return 3
	case difficultyNormal:
		return 5
	case difficultyHard:
		return 7
	}
	return 0
}

func newRaid(level Level, owner *Raids, center Pos, seed int64) *Raid {
	r := &Raid{
		level: level, owner: owner, rng: rand.New(rand.NewSource(seed)),
		groups: map[int][]Raider{}, leaders: map[int]Raider{},
		active: true, cooldown: preRaidTicks, center: center,
		numGroups: NumGroups(level.Difficulty()),
	}
	r.bar = newBossBar(level, uuid.New())
	return r
}

// Center is the block the raid is centred on.
func (r *Raid) Center() Pos { return r.center }

// OmenLevel is the accumulated raid omen level.
func (r *Raid) OmenLevel() int { return r.omenLevel }

// Started reports whether a wave has spawned.
func (r *Raid) Started() bool { return r.started }

// Status is where the raid stands.
func (r *Raid) Status() Status { return r.status }

// GroupsSpawned is the number of waves spawned so far.
func (r *Raid) GroupsSpawned() int { return r.groupsSpawned }

// Preparing reports whether the raid is before its first wave or between waves.
// vanilla: Raid.hasFirstWaveSpawned, Raid.isBetweenWaves
func (r *Raid) Preparing() bool {
	return r.groupsSpawned == 0 || r.Alive() == 0 && r.cooldown > 0
}

// Active reports whether the raid's chunk is loaded and it still runs.
func (r *Raid) Active() bool { return r.active }

// Progress is the bar's fill.
func (r *Raid) Progress() float32 { return r.bar.progress }

// BarTitle is what the bar says.
func (r *Raid) BarTitle() Title { return r.bar.title }

// BarPlayers are the players the bar is shown to.
func (r *Raid) BarPlayers() []Viewer { return slices.Clone(r.bar.players) }

// BarWire is the identity the Boss Event packets name the bar by.
func (r *Raid) BarWire() uuid.UUID { return r.bar.wire }

// Alive is the number of raiders the raid counts.
func (r *Raid) Alive() int {
	n := 0
	for _, g := range r.groups {
		n += len(g)
	}
	return n
}

func (r *Raid) isOver() bool    { return r.status == Victory || r.status == Loss }
func (r *Raid) isStopped() bool { return r.status == Stopped }

// hasBonusWave: a raid omen level above 1 adds a wave.
func (r *Raid) hasBonusWave() bool     { return r.omenLevel > 1 }
func (r *Raid) hasSpawnedBonus() bool  { return r.groupsSpawned > r.numGroups }
func (r *Raid) isFinalWave() bool      { return r.groupsSpawned == r.numGroups }
func (r *Raid) shouldSpawnBonus() bool { return r.isFinalWave() && r.Alive() == 0 && r.hasBonusWave() }

func (r *Raid) hasMoreWaves() bool {
	if r.hasBonusWave() {
		return !r.hasSpawnedBonus()
	}
	return !r.isFinalWave()
}

func (r *Raid) shouldSpawnGroup() bool {
	return r.cooldown == 0 && (r.groupsSpawned < r.numGroups || r.shouldSpawnBonus()) && r.Alive() == 0
}

// AbsorbOmen raises the raid's level by the omen amplifier plus one, capped;
// it reports whether the player had the effect.
// vanilla: Raid.absorbRaidOmen
func (r *Raid) AbsorbOmen(id uuid.UUID, amplifier int, has bool) bool {
	if !has {
		return false
	}
	r.omenLevel = min(max(r.omenLevel+amplifier+1, 0), MaxOmenLevel)
	if r.groupsSpawned == 0 {
		r.level.Omen(id)
	}
	return true
}

// Stop ends the raid and clears its bar.
func (r *Raid) Stop() {
	r.active = false
	r.bar.removeAll()
	r.status = Stopped
	r.note(Ended, 0, uuid.Nil)
}

// AddHero records a player who killed a raider.
func (r *Raid) AddHero(id uuid.UUID) {
	if !slices.Contains(r.heroes, id) {
		r.heroes = append(r.heroes, id)
	}
}

// RemoveLeader forgets a wave's captain.
func (r *Raid) RemoveLeader(wave int) { delete(r.leaders, wave) }

// SetLeader makes the raider the captain of the wave.
// vanilla: Raid.setLeader
func (r *Raid) SetLeader(wave int, raider Raider) { r.leaders[wave] = raider }

// Over reports whether the raid ended in a victory or a loss.
// vanilla: Raid.isOver
func (r *Raid) Over() bool { return r.status == Victory || r.status == Loss }

// Leader is the captain of the wave, nil for none.
func (r *Raid) Leader(wave int) Raider { return r.leaders[wave] }

// Join counts a raider in a wave and adds its health to the wave's total.
// vanilla: Raid.addWaveMob, Raid.joinRaid
func (r *Raid) Join(wave int, raider Raider) {
	r.addWaveMob(wave, raider, true)
}

// Restore replaces a saved raider's wave entry, preserving the raid's total health.
// vanilla: Raider.readAdditionalSaveData, Raid.addWaveMob
func (r *Raid) Restore(wave int, raider Raider) {
	r.addWaveMob(wave, raider, false)
}

func (r *Raid) addWaveMob(wave int, raider Raider, updateHealth bool) {
	group := r.groups[wave]
	id := raider.UUID()
	if i := slices.IndexFunc(group, func(existing Raider) bool { return existing.UUID() == id }); i >= 0 {
		group[i] = raider
	} else {
		group = append(group, raider)
	}
	r.groups[wave] = group
	raider.Enlist(r, wave)
	if updateHealth {
		r.totalHealth += float32(raider.Health())
	}
	r.UpdateBar()
}

// RemoveFromRaid takes a raider out of its wave.
// vanilla: Raid.removeFromRaid
func (r *Raid) RemoveFromRaid(wave int, raider Raider, removeHealth bool) {
	group := r.groups[wave]
	i := slices.Index(group, raider)
	if i < 0 {
		return
	}
	r.groups[wave] = slices.Delete(group, i, i+1)
	if removeHealth {
		r.totalHealth -= float32(raider.Health())
	}
	raider.Leave()
	r.UpdateBar()
}

// UpdateBar sets the bar to the share of the wave's health that is left.
// vanilla: Raid.updateBossbar
func (r *Raid) UpdateBar() {
	r.bar.setProgress(clamp01(float32(r.healthOfLiving()) / r.totalHealth))
}

func (r *Raid) healthOfLiving() float64 {
	var h float64
	for _, g := range r.groups {
		for _, raider := range g {
			h += raider.Health()
		}
	}
	return h
}

func clamp01(f float32) float32 {
	if f != f { // 0/0 before the first wave
		return 0
	}
	return min(max(f, 0), 1)
}

// EnchantOdds is the share of raid weapons that are enchanted.
// vanilla: Raid.getEnchantOdds
func (r *Raid) EnchantOdds() float32 {
	return [...]float32{0, 0, 0.1, 0.25, 0.5, 0.75}[min(r.omenLevel, MaxOmenLevel)]
}

func (r *Raid) validPlayer(p Player) bool {
	return p.Alive && r.owner.RaidAt(blockOf(p.X, p.Y, p.Z)) == r
}

func blockOf(x, y, z float64) Pos {
	return Pos{int(math.Floor(x)), int(math.Floor(y)), int(math.Floor(z))}
}

func (r *Raid) updatePlayers() {
	var valid []Viewer
	for _, p := range r.level.Players() {
		if r.validPlayer(p) {
			valid = append(valid, p.viewer())
		}
	}
	for _, id := range valid {
		r.bar.addPlayer(id)
	}
	for _, id := range slices.Clone(r.bar.players) {
		if !slices.Contains(valid, id) {
			r.bar.removePlayer(id)
		}
	}
}

// Tick runs the raid one tick.
// vanilla: Raid.tick
func (r *Raid) Tick() {
	if r.isStopped() {
		return
	}
	if r.status == Ongoing {
		r.tickOngoing()
	} else if r.isOver() {
		r.celebration++
		if r.celebration >= celebrationTicks {
			r.Stop()
			return
		}
		if r.celebration%20 == 0 {
			r.updatePlayers()
			r.bar.setVisible(true)
			if r.status == Victory {
				r.bar.setProgress(0)
				r.bar.setTitle(Title{Kind: TitleVictory})
			} else {
				r.bar.setTitle(Title{Kind: TitleDefeat})
			}
		}
	}
}

func (r *Raid) tickOngoing() {
	was := r.active
	r.active = r.level.ChunkLoaded(r.center.X, r.center.Z)
	if r.level.Difficulty() == difficultyPeaceful {
		r.Stop()
		return
	}
	if was != r.active {
		r.bar.setVisible(r.active)
	}
	if !r.active {
		return
	}
	if !r.level.IsVillage(r.center.X, r.center.Y, r.center.Z) {
		r.moveCenterToVillage()
	}
	if !r.level.IsVillage(r.center.X, r.center.Y, r.center.Z) {
		if r.groupsSpawned > 0 {
			r.status = Loss
			r.note(Lost, r.groupsSpawned, uuid.Nil)
		} else {
			r.Stop()
		}
	}
	r.ticksActive++
	if r.ticksActive >= timeoutTicks {
		r.Stop()
		return
	}
	alive := r.Alive()
	if alive > 0 {
		// vanilla updates the bar from Raider.hurtServer; a raider's health
		// only moves through damage, so following it here is the same.
		r.UpdateBar()
	}
	if alive == 0 && r.hasMoreWaves() {
		if r.cooldown > 0 {
			find := r.waveSpawn == nil && r.cooldown%5 == 0
			if r.waveSpawn != nil && !r.level.EntityTicking(r.waveSpawn.X, r.waveSpawn.Z) {
				find = true
			}
			if find {
				r.waveSpawn = r.findSpawnPos(8)
			}
			if r.cooldown == preRaidTicks || r.cooldown%20 == 0 {
				r.updatePlayers()
			}
			r.cooldown--
			r.bar.setProgress(clamp01(float32(preRaidTicks-r.cooldown) / preRaidTicks))
		} else if r.cooldown == 0 && r.groupsSpawned > 0 {
			r.cooldown = preRaidTicks
			r.bar.setTitle(Title{Kind: TitleRaid})
			return
		}
	}
	if r.ticksActive%20 == 0 {
		r.updatePlayers()
		r.updateRaiders()
		if alive > 0 && alive <= 2 {
			r.bar.setTitle(Title{Kind: TitleRaid, Remaining: alive})
		} else {
			r.bar.setTitle(Title{Kind: TitleRaid})
		}
	}
	r.spawnDueGroups()
	if r.started && !r.hasMoreWaves() && alive == 0 {
		if r.postRaid < postRaidTickLimit {
			r.postRaid++
		} else {
			r.status = Victory
			r.note(Won, r.groupsSpawned, uuid.Nil)
			for _, id := range r.heroes {
				r.level.Hero(id, r.omenLevel-1)
			}
		}
	}
}

func (r *Raid) spawnDueGroups() {
	horn, attempt := false, 0
	for r.shouldSpawnGroup() {
		pos := r.waveSpawn
		if pos == nil {
			pos = r.findSpawnPos(20)
		}
		if pos != nil {
			r.started = true
			r.spawnGroup(*pos)
			if !horn {
				r.playHorn(*pos)
				horn = true
			}
		} else {
			attempt++
		}
		if attempt > spawnAttempts {
			r.Stop()
			return
		}
	}
}

// moveCenterToVillage moves the centre to the nearest village section
// within two sections.
// vanilla: Raid.moveRaidCenterToNearbyVillageSection
func (r *Raid) moveCenterToVillage() {
	cx, cy, cz := r.center.X>>4, r.center.Y>>4, r.center.Z>>4
	best, bestDist := Pos{}, math.MaxFloat64
	for dx := -2; dx <= 2; dx++ {
		for dy := -2; dy <= 2; dy++ {
			for dz := -2; dz <= 2; dz++ {
				c := Pos{(cx+dx)<<4 + 8, (cy+dy)<<4 + 8, (cz+dz)<<4 + 8}
				if r.level.IsVillage(c.X, c.Y, c.Z) {
					if d := c.distSqr(r.center); d < bestDist {
						best, bestDist = c, d
					}
				}
			}
		}
	}
	if bestDist != math.MaxFloat64 {
		r.center = best
	}
}

// updateRaiders drops the raiders that died unseen, strayed too far or idled
// outside the village.
// vanilla: Raid.updateRaiders
func (r *Raid) updateRaiders() {
	type gone struct {
		wave   int
		raider Raider
	}
	var out []gone
	for wave, group := range r.groups {
		for _, raider := range group {
			x, y, z := raider.Position()
			pos := blockOf(x, y, z)
			if !raider.Alive() || r.center.distSqr(pos) >= removalSqr {
				out = append(out, gone{wave, raider})
				continue
			}
			if raider.Age() <= 600 {
				continue
			}
			if !r.level.IsVillage(pos.X, pos.Y, pos.Z) && raider.NoActionTicks() > maxNoActionTicks {
				raider.SetTicksOutside(raider.TicksOutside() + 1)
			}
			if raider.TicksOutside() >= outsideTimeout {
				out = append(out, gone{wave, raider})
			}
		}
	}
	for _, g := range out {
		r.RemoveFromRaid(g.wave, g.raider, true)
		if r.leaders[g.wave] == g.raider {
			r.RemoveLeader(g.wave)
		}
	}
}

func (r *Raid) playHorn(at Pos) {
	seed := r.rng.Int63()
	for _, p := range r.level.Players() {
		dx, dz := float64(at.X)+0.5-p.X, float64(at.Z)+0.5-p.Z
		dist := math.Sqrt(dx*dx + dz*dz)
		if dist > 64 && !slices.Contains(r.bar.players, p.viewer()) {
			continue
		}
		r.level.Horn(p.UUID, p.X+13/dist*dx, p.Z+13/dist*dz, seed)
	}
}

// spawnGroup spawns the next wave at the position.
// vanilla: Raid.spawnGroup
func (r *Raid) spawnGroup(at Pos) {
	leaderSet := false
	wave := r.groupsSpawned + 1
	r.totalHealth = 0
	bonus := r.shouldSpawnBonus()
	for i, t := range spawnsPerWave {
		n := r.defaultSpawns(i, wave, bonus) + r.bonusSpawns(i, wave, bonus)
		ravagers := 0
		for range n {
			raider := r.level.Spawn(t.kind, at.X, at.Y+1, at.Z, wave, !leaderSet && canLead(t.kind), r.EnchantOdds())
			if raider == nil {
				continue
			}
			if !leaderSet && canLead(t.kind) {
				r.leaders[wave] = raider
				leaderSet = true
			}
			r.Join(wave, raider)
			if t.kind == Ravager {
				r.spawnRavagerRider(at, wave, raider, ravagers)
				ravagers++
			}
		}
	}
	r.waveSpawn = nil
	r.groupsSpawned++
	r.note(Wave, r.groupsSpawned, uuid.Nil)
	r.UpdateBar()
}

// spawnRavagerRider seats a pillager on the ravagers of the normal raid's last
// wave, and from the hard raid's last wave an evoker on the first ravager and
// vindicators on the rest. vanilla: Raid.spawnGroup
func (r *Raid) spawnRavagerRider(at Pos, wave int, ravager Raider, index int) {
	kind := ""
	switch {
	case wave == NumGroups(difficultyNormal):
		kind = Pillager
	case wave >= NumGroups(difficultyHard):
		kind = Vindicator
		if index == 0 {
			kind = Evoker
		}
	}
	if kind == "" {
		return
	}
	rider := r.level.Spawn(kind, at.X, at.Y+1, at.Z, wave, false, r.EnchantOdds())
	if rider == nil {
		return
	}
	r.Join(wave, rider)
	r.level.Ride(ravager, rider)
}

// canLead is Raider.canBeLeader: the illagers carry the banner, the witch
// and the ravager do not.
func canLead(kind string) bool { return kind != Witch && kind != Ravager }

func (r *Raid) defaultSpawns(typeIndex, wave int, bonus bool) int {
	if bonus {
		return spawnsPerWave[typeIndex].count[r.numGroups]
	}
	return spawnsPerWave[typeIndex].count[wave]
}

// bonusSpawns is the extra spawns of the difficulty.
// vanilla: Raid.getPotentialBonusSpawns
func (r *Raid) bonusSpawns(typeIndex, wave int, bonus bool) int {
	d := r.level.Difficulty()
	easy, normal := d == difficultyEasy, d == difficultyNormal
	var n int
	switch spawnsPerWave[typeIndex].kind {
	case Witch:
		if easy || wave <= 2 || wave == 4 {
			return 0
		}
		n = 1
	case Vindicator, Pillager:
		switch {
		case easy:
			n = r.rng.Intn(2)
		case normal:
			n = 1
		default:
			n = 2
		}
	case Ravager:
		if !easy && bonus {
			n = 1
		}
	default:
		return 0
	}
	if n > 0 {
		return r.rng.Intn(n + 1)
	}
	return 0
}

// findSpawnPos searches a ring around the centre that narrows as the
// countdown runs down.
// vanilla: Raid.findRandomSpawnPos
func (r *Raid) findSpawnPos(tries int) *Pos {
	seconds := r.cooldown / 20
	howFar := 0.22*float32(seconds) - 0.24
	start := r.rng.Float32() * (math.Pi * 2)
	for i := range tries {
		angle := start + math.Pi*float32(i)/8
		x := r.center.X + int(math.Floor(float64(float32(math.Cos(float64(angle)))*32*howFar))) + r.rng.Intn(3)*int(math.Floor(float64(howFar)))
		z := r.center.Z + int(math.Floor(float64(float32(math.Sin(float64(angle)))*32*howFar))) + r.rng.Intn(3)*int(math.Floor(float64(howFar)))
		y := r.level.SurfaceY(x, z)
		if abs(y-r.center.Y) > 96 {
			continue
		}
		if r.level.IsVillage(x, y, z) && seconds > 7 {
			continue
		}
		if !r.level.CanSpawnAt(x, y, z) {
			continue
		}
		return &Pos{x, y, z}
	}
	return nil
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
