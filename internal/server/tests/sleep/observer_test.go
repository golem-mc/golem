package sleep

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
)

type bedLayout struct {
	facing     string
	dx, dz     int32
	hx, hz     int32 // head cell
	fromX      float64
	fromZ      float64
	yaw        float32
	lateWatch  bool
	tickHealth bool
}

// bodyView is the position a client decodes for one entity: the last absolute
// position it was told plus every relative step since. Setting the pose into a
// bed moves the drawn body there but not this base.
type bodyView struct {
	id      int32
	x, y, z int64 // 1/4096 blocks
	told    bool
}

const stepsPerBlock = 4096

func (v *bodyView) absolute(x, y, z float64) {
	v.x, v.y, v.z = int64(math.Round(x*stepsPerBlock)), int64(math.Round(y*stepsPerBlock)), int64(math.Round(z*stepsPerBlock))
	v.told = true
}

// see applies a packet about the body and reports the position it leaves.
func (v *bodyView) see(pid int32, data []byte) (moved bool) {
	r := protocol.NewReader(data)
	switch pid {
	case protocol.IDPlaySpawnEntity:
		if r.ReadVarInt() != v.id {
			return false
		}
		r.ReadUUID()
		r.ReadVarInt()
		v.absolute(r.ReadF64(), r.ReadF64(), r.ReadF64())
		return true
	case protocol.IDPlayTeleportEntity:
		if r.ReadVarInt() != v.id {
			return false
		}
		r.ReadVarInt() // path type: absolute
		v.absolute(r.ReadF64(), r.ReadF64(), r.ReadF64())
		return true
	case protocol.IDPlayEntityPosition, protocol.IDPlayEntityPositionRot:
		if r.ReadVarInt() != v.id {
			return false
		}
		r.ReadVarInt() // properties: on ground, no step ticks for a player
		v.x += int64(r.ReadI16())
		v.y += int64(r.ReadI16())
		v.z += int64(r.ReadI16())
		return true
	}
	return false
}

// watchSleeper records what b is told about the body of a for ticks ticks
// of a's sleep, failing on any position but the bed's and on metadata that
// does not name the bed head.
func watchSleeper(t *testing.T, srv *server.Server, a, b *servertest.Client, view *bodyView, l bedLayout, ticks int) {
	t.Helper()
	wantX, wantY, wantZ := float64(l.hx)+0.5, 64+0.6875, float64(l.hz)+0.5
	check := func(pid int32, data []byte) bool {
		if pid == protocol.IDPlayEntityMetadata {
			m := servertest.ReadSleepMetadata(t, data)
			if m.EntityID() != view.id {
				return false
			}
			x, y, z := m.Bed()
			if m.Pose != protocol.PoseSleeping || !m.Sleeping || x != l.hx || y != 64 || z != l.hz {
				t.Errorf("%s: metadata pose=%d sleeping=%v bed=(%d,%d,%d)", l.facing, m.Pose, m.Sleeping, x, y, z)
			}
			return false
		}
		if view.see(pid, data) && (view.x != int64(wantX*stepsPerBlock) || view.y != int64(math.Round(wantY*stepsPerBlock)) || view.z != int64(wantZ*stepsPerBlock)) {
			t.Errorf("%s: packet 0x%02X leaves the sleeper at (%v,%v,%v)/4096, want (%v,%v,%v)",
				l.facing, pid, view.x, view.y, view.z, wantX, wantY, wantZ)
		}
		return false
	}
	for i := 0; i < ticks; i += 5 {
		if l.tickHealth && i%20 == 0 {
			srv.SetHealth("Actor", float64(20-i/20))
		}
		a.MoveTo(wantX, wantY, wantZ, l.yaw+float32(i), float32(i%40))
		b.ReadTicks(srv, 5, check)
	}
}

// TestObserverSeesTheSleeperInTheBed has a second client watch a sleeper lie
// in a bed of each facing, tracking before or only after the sleep, with the
// sleeper's client reporting its position and turning every few ticks.
// vanilla: LivingEntity.setPosToBed
func TestObserverSeesTheSleeperInTheBed(t *testing.T) {
	t.Parallel()
	layouts := []bedLayout{
		{facing: "south", dx: 0, dz: 1, hx: 20, hz: 11, fromX: 18.5, fromZ: 10.5, yaw: 90},
		{facing: "north", dx: 0, dz: -1, hx: 24, hz: 9, fromX: 26.5, fromZ: 10.5, yaw: 270, lateWatch: true},
		{facing: "east", dx: 1, dz: 0, hx: 31, hz: 10, fromX: 28.5, fromZ: 12.5, yaw: 0, tickHealth: true},
		{facing: "west", dx: -1, dz: 0, hx: 36, hz: 10, fromX: 39.5, fromZ: 8.5, yaw: 180, lateWatch: true, tickHealth: true},
	}
	for _, l := range layouts {
		t.Run(l.facing, func(t *testing.T) {
			t.Parallel()
			srv, connect := servertest.Start(t, servertest.BlockTestConfig(), servertest.NoTimeSync)
			a := connect().Join("Actor")
			var b *servertest.Client
			if !l.lateWatch {
				b = connect().Join("Watcher")
			}
			servertest.SetNight(t, srv)
			if !srv.SetBlock(l.hx, 64, l.hz, servertest.BedStateName("red", "head", l.facing)) ||
				!srv.SetBlock(l.hx-l.dx, 64, l.hz-l.dz, servertest.BedStateName("red", "foot", l.facing)) {
				t.Fatal("could not stand the bed")
			}
			a.MoveTo(l.fromX, 64, l.fromZ, l.yaw, 0)
			a.Settle()
			view := &bodyView{id: a.PlayerEntityID}
			if b != nil {
				b.SettleAndCollect(func(pid int32, data []byte) { view.see(pid, data) })
			}
			a.UseBlock(l.hx, 64, l.hz)
			a.Settle()
			if !srv.Sleeping("Actor") {
				t.Fatal("the player did not lie down")
			}
			if b == nil {
				b = connect().JoinKeeping("Watcher")
			}
			servertest.HoldTicks(t, srv)
			// A late watcher's spawn rides in the join's kept packets.
			watchSleeper(t, srv, a, b, view, l, 200)
		})
	}
}
