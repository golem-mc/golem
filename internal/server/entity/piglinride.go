package entity

const (
	// piglinRideStartMin and piglinRideStartMax bound the ticker between a
	// baby's looks for a baby hoglin to ride: 10 to 40 seconds.
	// vanilla: PiglinAi.RIDE_START_INTERVAL
	piglinRideStartMin = 200
	piglinRideStartMax = 800
	// piglinRideMin and piglinRideMax bound RIDE_TARGET's expiry in ticks:
	// 10 to 30 seconds.
	// vanilla: PiglinAi.RIDE_DURATION
	piglinRideMin = 200
	piglinRideMax = 600
	// piglinRideReach is how near the baby hoglin must be to keep the ride
	// target.
	// vanilla: PiglinAi.MAX_WALK_DISTANCE_TO_START_RIDING
	piglinRideReach = 8.0
	// piglinMountReach is Mount's closerThan distance.
	piglinMountReach = 1.0
	// piglinMountSpeed is the walk speed towards the mount.
	// vanilla: PiglinAi.SPEED_MULTIPLIER_WHEN_MOUNTING
	piglinMountSpeed = 0.8
)

// rideState is a baby piglin's RIDE_TARGET memory and the ticker that fills it.
type rideState struct {
	// rideIn is SetEntityLookTargetSometimes.Ticker's countdown.
	rideIn int
	// ride is the baby hoglin's ID and rideLeft the ticks the target lasts; 0
	// for none.
	ride     int32
	rideLeft int
}

// rideStep is the baby's idle roll for a baby hoglin to ride and the
// countdown of the target it set; a baby on a baby mount with no target left
// gets off.
// vanilla: PiglinAi.babySometimesRideBabyHoglin, updateActivity
func (m *Mob) rideStep(t *tickView) {
	r := &m.barter.rideState
	if r.rideLeft > 0 {
		r.rideLeft--
		if r.rideLeft == 0 {
			r.ride = 0
		}
	}
	if !m.IsBaby() {
		return
	}
	if r.ride == 0 && m.ridesBaby() {
		m.getOffMount(t)
	}
	idle := m.TargetID == 0 && m.barter.retreat == 0 && m.barter.admiring == 0 && !m.celebrating()
	if !idle || r.ride != 0 {
		return
	}
	if r.rideIn == 0 {
		r.rideIn = piglinRideStartMin + m.Rand.Intn(piglinRideStartMax-piglinRideStartMin+1) - 1
		return
	}
	if r.rideIn--; r.rideIn != 0 {
		return
	}
	var best *Mob
	bd := 0.0
	for _, o := range t.others {
		if o.Type != TypeHoglin || !o.IsBaby() || !o.Alive() || !m.sees(t, o) {
			continue
		}
		if d := Dist2(m.X, m.Y, m.Z, o.X, o.Y, o.Z); best == nil || d < bd {
			best, bd = o, d
		}
	}
	if best != nil {
		r.ride = best.ID
		r.rideLeft = piglinRideMin + m.Rand.Intn(piglinRideMax-piglinRideMin+1)
	}
}

// ridesBaby is PiglinAi.isBabyRidingBaby.
func (m *Mob) ridesBaby() bool {
	v := m.Vehicle
	return m.IsBaby() && v != nil && (v.Type == TypeHoglin || v.Type == TypePiglin) && v.IsBaby()
}

// getOffMount is Entity.stopRiding, told to the dimension.
func (m *Mob) getOffMount(t *tickView) {
	v := m.Vehicle
	if v == nil || !Dismount(v, m) {
		return
	}
	t.events = append(t.events, mountEvent(m, v, MobDismounted), MobEvent{Mob: v, Kind: MobSeated})
}

// rideGoal is Activity.RIDE: walk to the baby hoglin, get on within a block,
// and get off when it is dead, grown, hurt, or further than eight blocks, or
// the baby was hurt.
// vanilla: Mount, DismountOrSkipMounting, PiglinAi.wantsToStopRiding
type rideGoal struct{ m *Mob }

func (g *rideGoal) Priority() int           { return priorityCelebrate }
func (g *rideGoal) Flags() GoalFlags        { return GoalMove }
func (g *rideGoal) Start(*tickView)         {}
func (g *rideGoal) RequiresEveryTick() bool { return true }
func (g *rideGoal) Stop(*tickView)          { g.m.stopSeeking() }

func (g *rideGoal) CanStart(*tickView) bool {
	m := g.m
	return m.barter.ride != 0 && m.TargetID == 0 && m.barter.retreat == 0 && m.barter.admiring == 0 && !m.celebrating()
}

func (g *rideGoal) ShouldContinue(t *tickView) bool { return g.CanStart(t) }

func (g *rideGoal) Tick(t *tickView) {
	m := g.m
	r := &m.barter.rideState
	veh := m.Vehicle
	if veh == nil {
		veh = t.mobByID(r.ride)
	}
	if veh == nil || !veh.Alive() || !veh.IsBaby() || Dist2(m.X, m.Y, m.Z, veh.X, veh.Y, veh.Z) >= piglinRideReach*piglinRideReach ||
		m.lastDamageSource() != 0 || veh.lastDamageSource() != 0 {
		m.getOffMount(t)
		r.ride, r.rideLeft = 0, 0
		return
	}
	if m.Vehicle != nil {
		return
	}
	if Dist2(m.X, m.Y, m.Z, veh.X, veh.Y, veh.Z) < piglinMountReach*piglinMountReach {
		// vanilla: Piglin.getTopPassenger(vehicle, MAX_PASSENGERS_ON_ONE_HOGLIN)
		for range 2 {
			if len(veh.Passengers) == 0 {
				break
			}
			veh = veh.Passengers[0]
		}
		if len(veh.Passengers) != 0 {
			return
		}
		Ride(veh, m)
		m.stopSeeking()
		t.events = append(t.events, mountEvent(m, veh, MobMounted), MobEvent{Mob: veh, Kind: MobSeated})
		return
	}
	m.seekTo(t.w, veh.X, veh.Y, veh.Z, piglinMountSpeed, 0)
}

// SetRideTarget gives a baby piglin the baby hoglin id as its RIDE_TARGET for
// the longest a roll lasts, for a test that does not wait out the ticker.
func (m *Mob) SetRideTarget(id int32) {
	m.barter.ride, m.barter.rideLeft = id, piglinRideMax
}
