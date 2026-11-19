package command

import (
	"math"

	"github.com/golem-mc/golem/internal/protocol"
)

// responseTracker sums what a multi-target command did to each target and
// remembers the one target that did anything, if only one did.
// vanilla: CommandResponseTracker (its NON_ZERO element type)
type responseTracker[E any] struct {
	total, nonZero int
	only           E
}

// track adds one target's value; a target whose value is zero is not counted.
func (t *responseTracker[E]) track(e E, value int) {
	t.total += value
	if value != 0 {
		if t.nonZero++; t.nonZero == 1 {
			t.only = e
		}
	}
}

// trackBool tracks a target as 1 when it changed and 0 when it did not.
func (t *responseTracker[E]) trackBool(e E, changed bool) {
	value := 0
	if changed {
		value = 1
	}
	t.track(e, value)
}

// feedback refuses with failedKey when no target counted (an empty failedKey
// answers anyway, with the multiple form counting nobody), else broadcasts
// prefix+"single" when one target counted or prefix+"multiple" when several
// did. args builds the arguments around who: the one target's name, or the
// count of the targets that counted. The result is the tracked total.
// vanilla: CommandResponseTracker.sendFeedback
func (t *responseTracker[E]) feedback(src Source, failedKey, prefix string, name func(E) protocol.Text, args func(who protocol.Text, total int) []protocol.Text) (int32, error) {
	if t.nonZero == 0 && failedKey != "" {
		return 0, refuse(src, failedKey)
	}
	form, who := t.target(name)
	return resultOf(int32(min(t.total, math.MaxInt32)), feedbackToAdmins(src, prefix+form, args(who, t.total)...))
}

// target is the form a success line takes and who it names: "single" and
// the one target that counted, or "multiple" and how many did.
// vanilla: CommandResponseTracker.dispatch
func (t *responseTracker[E]) target(name func(E) protocol.Text) (form string, who protocol.Text) {
	if t.nonZero == 1 {
		return "single", name(t.only)
	}
	return "multiple", entityCount(t.nonZero)
}
