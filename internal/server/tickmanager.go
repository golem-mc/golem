package server

import (
	"log/slog"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/tickrate"
	"github.com/google/uuid"
)

// tickSprintReportKey is the line a finished sprint answers with.
const tickSprintReportKey = "commands.tick.sprint.report"

// tickRateView is the /tick command's side of the loop's ticking state:
// each change is told to everyone at once, a freeze ends a running sprint
// and the remaining steps, and a finished sprint reports and restores what
// it found. Deliberately unlike vanilla, whose onTickRateChanged re-paces
// the auto-save, a rate change or sprint finish leaves it on its tick
// count. vanilla: ServerTickRateManager
type tickRateView struct {
	s *Server
}

// The reads and setters the state itself carries.
func (t tickRateView) Rate() float32             { return t.s.tickRate.Rate() }
func (t tickRateView) NanosecondsPerTick() int64 { return t.s.tickRate.NanosPerTick() }
func (t tickRateView) SetRate(rate float32) {
	t.s.tickRate.SetRate(rate)
	t.s.broadcastTickingState()
}
func (t tickRateView) Frozen() bool { return t.s.tickRate.Frozen() }

// SetFrozen freezes or unfreezes the game; freezing first ends a running
// sprint (with its report), then the remaining steps.
// vanilla: TickCommand.setFreeze
func (t tickRateView) SetFrozen(frozen bool) {
	if frozen {
		if report, stopped := t.s.tickRate.StopSprint(); stopped {
			t.s.sprintReported(report)
		}
		if t.s.tickRate.StopStep() {
			t.s.broadcastTickingStep()
		}
	}
	t.s.tickRate.SetFrozen(frozen)
	t.s.broadcastTickingState()
}

func (t tickRateView) Step(ticks int) bool {
	ok := t.s.tickRate.Step(ticks)
	if ok {
		t.s.broadcastTickingStep()
	}
	return ok
}

func (t tickRateView) StopStep() bool {
	ok := t.s.tickRate.StopStep()
	if ok {
		t.s.broadcastTickingStep()
	}
	return ok
}

func (t tickRateView) Sprinting() bool { return t.s.tickRate.Sprinting() }

// Sprint hands the sprint to the loop; the request already tells every
// player the unfrozen state it leaves.
// vanilla: ServerTickRateManager.requestGameToSprint
func (t tickRateView) Sprint(ticks int) bool {
	interrupted := t.s.tickRate.Sprint(ticks)
	t.s.broadcastTickingState()
	return interrupted
}

func (t tickRateView) StopSprint() bool {
	report, stopped := t.s.tickRate.StopSprint()
	if stopped {
		t.s.sprintReported(report)
	}
	return stopped
}

// sprintReported delivers a finished sprint's report (a loop log line and
// the [Server: ...] echo to operators) and broadcasts the restored freeze
// state. vanilla: ServerTickRateManager.finishTickSprint
func (s *Server) sprintReported(report *tickrate.SprintReport) {
	tps := report.TicksPerSecond
	line := protocol.Text{
		Translate: tickSprintReportKey,
		With:      []protocol.Text{{Number: &tps}, {Text: report.MSPerTick}},
	}
	log := s.tickLog
	if log == nil {
		log = slog.Default()
	}
	log.Info(line.Plain(), protocol.LogComponentKey, line)
	s.informAdmins(uuid.Nil, false, protocol.Text{Text: consoleSourceName}, line)
	s.broadcastTickingState()
}
