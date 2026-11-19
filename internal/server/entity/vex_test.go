package entity

// vexView builds the view a vex's goals run against.
func vexView(w World, players []Player, others []*Mob) *tickView {
	return &tickView{w: w, players: players, others: others}
}

// vexCharges counts the CanStart offers of a fresh charge goal out of n.
func vexCharges(m *Mob, v *tickView, n int) int {
	g := &vexChargeGoal{m: m}
	started := 0
	for range n {
		if g.CanStart(v) {
			started++
		}
	}
	return started
}
