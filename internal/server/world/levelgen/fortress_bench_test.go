package levelgen

import "testing"

func BenchmarkFortressCorridorTurns(b *testing.B) {
	st := &FortressStates{Air: 0, NetherBricks: 1}
	for i := range st.Fence {
		st.Fence[i] = uint16(10 + i)
	}
	clip := BoundingBox{0, 1, 0, 15, 255, 15}
	w := &recordingWorld{writes: map[[3]int]uint16{}, chunk: clip}
	c := &fortressCtx{w: w, st: st, rng: NewLegacyRandom(42), chunk: clip}
	pieces := []fortressPiece{{kind: pRightTurn, dir: dS, bb: BoundingBox{0, 64, 0, 4, 70, 4}, needsChest: true}, {kind: pLeftTurn, dir: dS, bb: BoundingBox{8, 64, 0, 12, 70, 4}, needsChest: true}}
	for i := range pieces {
		pieces[i].postProcess(c)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := range b.N {
		pieces[i&1].postProcess(c)
	}
}
