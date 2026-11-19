package mobs

import ()

// The hostile targeting through a target that is not fully visible. The
// targeting conditions cut the follow range by the target's own visibility
// percent before the hunt may name it — a sneaking body reads 0.8, an
// invisible one 0.7 by its armour cover (a quarter a worn piece, floored at
// a tenth), and the head matching the hunting kind halves it again — while
// the hunt that has opened keeps its prey by the whole range. The zombie's
// follow range is the vanilla 35, so each shape has its own reach: 28 for
// sneaking, 2.45 for bare invisibility, 6.125 invisible under one helmet,
// 17.5 under the zombie's own head.
