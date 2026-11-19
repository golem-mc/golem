package rules

import "github.com/golem-mc/golem/internal/server/player"

// movePacketBurst is the most move packets a tick may bring before the
// budget falls back to a lone packet's.
// vanilla: ServerGamePacketListenerImpl.handleMovePlayer
const movePacketBurst = 5

// MovedTooQuickly reports whether a step of movedSq (squared blocks) exceeds
// 100 per move packet this tick (300 gliding) on top of expectedSq, the
// squared velocity carried. Packets past the burst count as one. speed is
// the body's walk or fly speed over vanilla's default; above 1 it widens
// the budget by its square, so a plugin's faster player is not sent back.
// vanilla: ServerGamePacketListenerImpl.handleMovePlayer
// vanilla-bukkit: the budget's speed scaling is Spigot's (movedTooQuicklyMultiplier)
func MovedTooQuickly(movedSq, expectedSq float64, packets int, fallFlying bool, speed float64) bool {
	if packets > movePacketBurst {
		packets = 1
	}
	metersPerTick := float32(100)
	if fallFlying {
		metersPerTick = 300
	}
	budget := float64(metersPerTick * float32(packets))
	if speed > 1 {
		budget *= speed * speed
	}
	return movedSq-expectedSq > budget
}

// MoveCheck is the state of the moved-too-quickly check across a tick:
// where the body stood when the tick's first move packet came, and how many
// move packets the tick has brought.
// vanilla: firstGoodX/Y/Z, receivedMovePacketCount - knownMovePacketCount
type MoveCheck struct {
	Tick    uint64
	Anchor  [3]float64
	Packets int
}

// Reset puts the anchor at a point a teleport placed the body at.
// vanilla: ServerGamePacketListenerImpl.resetPosition
func (m *MoveCheck) Reset(x, y, z float64) { m.Anchor = [3]float64{x, y, z} }

// MoveStep is one reported step handed to MoveCheck.Judge.
type MoveStep struct {
	Tick                       uint64 // the server tick the packet came in
	Was, To                    player.Position
	TickRunning                bool // the tick rate is not frozen
	MovementCheck, ElytraCheck bool // the player_movement_check and elytra_movement_check rules
	FallFlying                 bool
	Spectator                  bool
	// Speed is the walk or fly speed over vanilla's default (1 or less: none).
	Speed float64
	// Collides says whether the step enters a collision shape the body was
	// not already in; asked only when the step reaches that check.
	Collides func() bool
}

// MoveVerdict is what MoveCheck.Judge decides about a step.
type MoveVerdict struct {
	Refused bool
	// TooQuick tells which check refused: moved too quickly, or moved wrongly.
	TooQuick bool
	// Back is where a refused step is sent to: where the body stood, with
	// its own look after a step too quick and the packet's after a step into
	// a block.
	Back player.Position
	// Delta is the step measured from the anchor, for the log.
	Delta [3]float64
}

// Judge counts the step's packet and decides whether it is refused: first
// the moved-too-quickly check against the tick's anchor, then, but for a
// spectator, the moved-wrongly check.
// vanilla: ServerGamePacketListenerImpl.handleMovePlayer
func (m *MoveCheck) Judge(s MoveStep) MoveVerdict {
	if m.Tick != s.Tick {
		m.Tick, m.Packets = s.Tick, 0
		m.Anchor = [3]float64{s.Was.X, s.Was.Y, s.Was.Z}
	}
	if s.TickRunning {
		m.Packets++
		if s.MovementCheck && (!s.FallFlying || s.ElytraCheck) {
			dx, dy, dz := s.To.X-m.Anchor[0], s.To.Y-m.Anchor[1], s.To.Z-m.Anchor[2]
			if MovedTooQuickly(dx*dx+dy*dy+dz*dz, 0, m.Packets, s.FallFlying, s.Speed) {
				return MoveVerdict{Refused: true, TooQuick: true, Back: s.Was, Delta: [3]float64{dx, dy, dz}}
			}
		}
	}
	// A spectator passes through everything. vanilla: Entity.noPhysics
	if s.Spectator || s.Collides == nil || !s.Collides() {
		return MoveVerdict{}
	}
	back := s.Was
	back.Yaw, back.Pitch = s.To.Yaw, s.To.Pitch
	return MoveVerdict{Refused: true, Back: back}
}
