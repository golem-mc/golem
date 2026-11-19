package server

import (
	"math"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
)

// sendInitializeBorder sends the player's dimension's border; an untold
// client draws none. vanilla: PlayerList.sendLevelInfo
func (c *client) sendInitializeBorder() error {
	return c.writePacket(protocol.IDPlayInitializeBorder, "initialize world border",
		initializeBorderPayload(c.dimension().border.State()))
}

// initializeBorderPayload builds Initialize World Border; lerp and warning
// times are in ticks. vanilla: ClientboundInitializeBorderPacket
func initializeBorderPayload(st world.BorderState) []byte {
	var b protocol.Buffer
	b.PutF64(st.CenterX)
	b.PutF64(st.CenterZ)
	b.PutF64(st.Size)
	b.PutF64(st.Target)
	b.PutVarLong(st.LerpTicks)
	b.PutVarInt(st.AbsoluteMax)
	b.PutVarInt(st.WarningBlocks)
	b.PutVarInt(st.WarningTime)
	return b.Bytes()
}

// tickBorders moves every dimension's border one tick along its lerp.
// vanilla: ServerLevel.tick
func (s *Server) tickBorders() {
	for _, d := range s.allDims() {
		d.border.Tick()
	}
}

// borderView is the command package's handle on one dimension's border,
// telling each change to that dimension's players.
// vanilla: PlayerList.addWorldborderListener
type borderView struct {
	srv *Server
	dim *dimension
}

var _ command.WorldBorder = borderView{}

func (v borderView) tell(id int32, data []byte) {
	v.srv.broadcastWorld(v.dim.w, id, data)
}

func (v borderView) State() world.BorderState { return v.dim.border.State() }

func (v borderView) DamageSettings() (perBlock, buffer float64) {
	return v.dim.border.DamageSettings()
}

func (v borderView) SetCenter(x, z float64) {
	v.dim.border.SetCenter(x, z)
	var b protocol.Buffer
	b.PutF64(x)
	b.PutF64(z)
	v.tell(protocol.IDPlaySetBorderCenter, b.Bytes())
}

func (v borderView) SetSize(d float64) {
	v.dim.border.SetSize(d)
	var b protocol.Buffer
	b.PutF64(d)
	v.tell(protocol.IDPlaySetBorderSize, b.Bytes())
}

func (v borderView) LerpSize(from, to float64, ticks int64) {
	v.dim.border.LerpSize(from, to, ticks)
	st := v.dim.border.State()
	var b protocol.Buffer
	b.PutF64(st.Size)
	b.PutF64(st.Target)
	b.PutVarLong(st.LerpTicks)
	v.tell(protocol.IDPlaySetBorderLerpSize, b.Bytes())
}

func (v borderView) SetDamagePerBlock(d float64) { v.dim.border.SetDamagePerBlock(d) }

func (v borderView) SetSafeZone(d float64) { v.dim.border.SetSafeZone(d) }

func (v borderView) SetWarningBlocks(n int32) {
	v.dim.border.SetWarningBlocks(n)
	var b protocol.Buffer
	b.PutVarInt(n)
	v.tell(protocol.IDPlaySetBorderWarningDistance, b.Bytes())
}

func (v borderView) SetWarningTime(n int32) {
	v.dim.border.SetWarningTime(n)
	var b protocol.Buffer
	b.PutVarInt(n)
	v.tell(protocol.IDPlaySetBorderWarningDelay, b.Bytes())
}

// WorldBorder is the border of the runner's dimension.
// vanilla: CommandSourceStack.getLevel().getWorldBorder()
func (ws worldScope) WorldBorder() command.WorldBorder {
	return borderView{srv: ws.srv, dim: ws.in()}
}

// borderHurt is the hurt the player's border deals this tick at their
// position, if any. vanilla: LivingEntity.baseTick
func (c *client) borderHurt(x, z float64) (rules.Hurt, bool) {
	hw := c.dims(c.player.Appearance().Pose).Width / 2
	amount := c.dimension().border.OutsideDamage(x-hw, z-hw, x+hw, z+hw, x, z)
	if amount <= 0 {
		return rules.Hurt{}, false
	}
	return rules.Hurt{DamageType: rules.DamageTypeOutsideBorder, Amount: amount}, true
}

// mayInteractAt reports whether the column is inside the player's border
// and outside spawn protection, where they may change blocks.
// vanilla: ServerLevel.mayInteract
func (c *client) mayInteractAt(x, z int32) bool {
	if c.underSpawnProtection(x, z) {
		return false
	}
	if c.dimension().border.ContainsBlock(x, z) {
		return true
	}
	c.log.Debug("block edit outside the world border", "x", x, "z", z)
	return false
}

// targetOutsideBorder reports whether the entity with this ID stands in a
// block column outside the player's border; an unknown ID is not outside.
// vanilla: ServerGamePacketListenerImpl.handleAttack, handleInteract
func (c *client) targetOutsideBorder(id int32) bool {
	border := c.dimension().border
	// The default border skips the snapshot of every entity; it misses only a
	// body in the last 16 blocks before the world's edge.
	if st := border.State(); st.Size >= world.BorderMaxSize && st.CenterX == 0 && st.CenterZ == 0 && st.AbsoluteMax == world.BorderAbsoluteMax {
		return false
	}
	target := c.cameraCommandByID(id)
	if target == nil {
		return false
	}
	p := target.Position()
	return !border.ContainsBlock(int32(math.Floor(p.X)), int32(math.Floor(p.Z)))
}
