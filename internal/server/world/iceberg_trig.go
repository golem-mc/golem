package world

import "math"

// icebergSinCos rounds the rotation with double-double arithmetic. Go's trig
// can put a point on the other side of an ellipse's zero-distance boundary,
// changing whether IcebergFeature.generateIcebergBlock draws another random.
// Angles are in [0, 5*pi/2]; each rotation is computed once per shape or cutout.
func icebergSinCos(angle float64) (sin, cos float64) {
	if angle < 0 || angle > 5*math.Pi/2 || math.IsNaN(angle) {
		panic("iceberg angle outside modeled range")
	}
	n := int(math.Round(angle / (math.Pi / 2)))
	// pi/2 split into a rounded double and its remainder.
	pi2 := icebergDouble{0x1.921fb54442d18p0, 0x1.1a62633145c07p-54}
	x := icebergDouble{hi: angle}.add(pi2.mul(icebergDouble{hi: -float64(n)}))
	xx := x.mul(x)
	xx.hi, xx.lo = -xx.hi, -xx.lo
	st, ct := x, icebergDouble{hi: 1}
	ss, cs := st, ct
	// At |x| <= pi/4, the omitted Taylor terms are below 2^-150.
	for i := 1; i <= 20; i++ {
		st = st.mul(xx).div(float64(2 * i * (2*i + 1)))
		ct = ct.mul(xx).div(float64((2*i - 1) * 2 * i))
		ss, cs = ss.add(st), cs.add(ct)
	}
	sin, cos = ss.hi+ss.lo, cs.hi+cs.lo
	switch n & 3 {
	case 1:
		return cos, -sin
	case 2:
		return -sin, -cos
	case 3:
		return -cos, sin
	default:
		return sin, cos
	}
}

// icebergDouble retains the rounding residual alongside the leading double.
// FMA keeps multiplication's residual independent of compiler contraction.
type icebergDouble struct{ hi, lo float64 }

func (x icebergDouble) add(y icebergDouble) icebergDouble {
	s := x.hi + y.hi
	v := s - x.hi
	e := (x.hi - (s - v)) + (y.hi - v)
	e += x.lo + y.lo
	hi := s + e
	return icebergDouble{hi, e - (hi - s)}
}

func (x icebergDouble) mul(y icebergDouble) icebergDouble {
	p := x.hi * y.hi
	e := math.FMA(x.hi, y.hi, -p) + x.hi*y.lo + x.lo*y.hi
	hi := p + e
	return icebergDouble{hi, e - (hi - p)}
}

func (x icebergDouble) div(y float64) icebergDouble {
	q := x.hi / y
	r := x.add(icebergDouble{hi: -q}.mul(icebergDouble{hi: y}))
	return icebergDouble{hi: q}.add(icebergDouble{hi: (r.hi + r.lo) / y})
}
