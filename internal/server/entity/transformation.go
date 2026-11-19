package entity

import (
	"math"

	"github.com/golem-mc/golem/nbt"
)

// Transformation is a display's transform split into the four parts the
// client is sent. Rotations are (x, y, z, w).
type Transformation struct {
	Translation  [3]float32
	LeftRotation [4]float32
	Scale        [3]float32
	// RightRotation is applied after the scale.
	RightRotation [4]float32
}

var identityTransformation = Transformation{
	Scale: [3]float32{1, 1, 1}, LeftRotation: [4]float32{0, 0, 0, 1}, RightRotation: [4]float32{0, 0, 0, 1},
}

// TransformationOf reads a saved transformation: the four-part compound (all
// four parts required, a rotation as four components or an axis and angle) or
// a 16-number row-major matrix, which is decomposed. Anything else is the
// identity.
// vanilla: Transformation.EXTENDED_CODEC
func TransformationOf(tag nbt.Tag) Transformation {
	switch v := tag.(type) {
	case nbt.CompoundTag:
		c := v.Entries()
		t, l, s, r := floatsOf(c["translation"], 3), rotationOf(c["left_rotation"]),
			floatsOf(c["scale"], 3), rotationOf(c["right_rotation"])
		if t == nil || l == nil || s == nil || r == nil {
			return identityTransformation
		}
		return Transformation{Translation: [3]float32(t), LeftRotation: *l, Scale: [3]float32(s), RightRotation: *r}
	case nbt.List:
		if m := floatsOf(v, 16); m != nil {
			return decomposeMatrix(m)
		}
	}
	return identityTransformation
}

// rotationOf reads a rotation as vanilla's QUATERNIONF codec does: four
// components, normalized, or an {axis, angle} compound.
func rotationOf(tag nbt.Tag) *[4]float32 {
	if compound, ok := tag.(nbt.CompoundTag); ok {
		c := compound.Entries()
		axis := floatsOf(c["axis"], 3)
		angle, ok := floatOf(c["angle"])
		if axis == nil || !ok {
			return nil
		}
		n := float32(1 / math.Sqrt(float64(axis[0]*axis[0]+axis[1]*axis[1]+axis[2]*axis[2])))
		half := float64(angle) * 0.5
		s := float32(math.Sin(half)) * n
		return &[4]float32{axis[0] * s, axis[1] * s, axis[2] * s, float32(math.Cos(half))}
	}
	q := floatsOf(tag, 4)
	if q == nil {
		return nil
	}
	n := normalizedQuaternion([4]float32(q))
	return &n
}

// m3 is a 3x3 matrix indexed [column][row], so m[1][0] is JOML's m10.
type m3 [3][3]float32

func (a *m3) mul(b *m3) {
	var r m3
	for c := range 3 {
		for row := range 3 {
			r[c][row] = a[0][row]*b[c][0] + a[1][row]*b[c][1] + a[2][row]*b[c][2]
		}
	}
	*a = r
}

func (a *m3) transpose() {
	a[0][1], a[1][0] = a[1][0], a[0][1]
	a[0][2], a[2][0] = a[2][0], a[0][2]
	a[1][2], a[2][1] = a[2][1], a[1][2]
}

// quat is (x, y, z, w).
type quat [4]float32

func (a *quat) mul(q quat) {
	x, y, z, w := a[0], a[1], a[2], a[3]
	a[0] = w*q[0] + x*q[3] + y*q[2] - z*q[1]
	a[1] = w*q[1] - x*q[2] + y*q[3] + z*q[0]
	a[2] = w*q[2] + x*q[1] - y*q[0] + z*q[3]
	a[3] = w*q[3] - x*q[0] - y*q[1] - z*q[2]
}

func (a *quat) normalize() {
	n := float32(1 / math.Sqrt(float64(a[0]*a[0]+a[1]*a[1]+a[2]*a[2]+a[3]*a[3])))
	for i := range a {
		a[i] *= n
	}
}

// matrix is the rotation the quaternion stands for.
func (a quat) matrix() m3 {
	x, y, z, w := a[0], a[1], a[2], a[3]
	w2, x2, y2, z2 := w*w, x*x, y*y, z*z
	zw, xy, xz, yw, yz, xw := z*w, x*y, x*z, y*w, y*z, x*w
	return m3{
		{w2 + x2 - z2 - y2, xy + zw + zw + xy, xz - yw + xz - yw},
		{-zw + xy - zw + xy, y2 - z2 + w2 - x2, yz + yz + xw + xw},
		{yw + xz + xz + yw, yz + yz - xw - xw, z2 - y2 - x2 + w2},
	}
}

// givens is a rotation by an angle given as the sine and cosine of its half.
// vanilla: GivensParameters
type givens struct{ sinHalf, cosHalf float32 }

func givensOf(sinHalf, cosHalf float32) givens {
	w := float32(1 / math.Sqrt(float64(sinHalf*sinHalf+cosHalf*cosHalf)))
	return givens{w * sinHalf, w * cosHalf}
}

func (g givens) inverse() givens { return givens{-g.sinHalf, g.cosHalf} }
func (g givens) cos() float32    { return g.cosHalf*g.cosHalf - g.sinHalf*g.sinHalf }
func (g givens) sin() float32    { return 2 * g.sinHalf * g.cosHalf }

func (g givens) aroundX() (quat, m3) {
	c, s := g.cos(), g.sin()
	return quat{g.sinHalf, 0, 0, g.cosHalf}, m3{{1, 0, 0}, {0, c, s}, {0, -s, c}}
}

func (g givens) aroundY() (quat, m3) {
	c, s := g.cos(), g.sin()
	return quat{0, g.sinHalf, 0, g.cosHalf}, m3{{c, 0, -s}, {0, 1, 0}, {s, 0, c}}
}

func (g givens) aroundZ() (quat, m3) {
	c, s := g.cos(), g.sin()
	return quat{0, 0, g.sinHalf, g.cosHalf}, m3{{c, s, 0}, {-s, c, 0}, {0, 0, 1}}
}

// pi4 is the quarter-turn rotation. vanilla: MatrixUtil.PI_4
var pi4 = func() givens {
	half := float32(0.7853982) / 2
	sin := float32(math.Sin(float64(half)))
	return givens{sin, float32(math.Sqrt(float64(1 - sin*sin)))}
}()

// vanilla: MatrixUtil.G
var givensG = float32(3 + 2*math.Sqrt(2))

func approxGivens(a11, a12, a22 float32) givens {
	sh, ch := a12, 2*(a11-a22)
	if givensG*sh*sh < ch*ch {
		return givensOf(sh, ch)
	}
	return pi4
}

func qrGivens(a1, a2 float32) givens {
	p := float32(math.Hypot(float64(a1), float64(a2)))
	var sh float32
	if p > 1e-6 {
		sh = a2
	}
	ch := float32(math.Abs(float64(a1))) + max(p, 1e-6)
	if a1 < 0 {
		sh, ch = ch, sh
	}
	return givensOf(sh, ch)
}

// similarity sets a to qT*a*q.
func similarity(a, q *m3) {
	a.mul(q)
	q.transpose()
	q.mul(a)
	*a = *q
}

// stepJacobi is one sweep of the Jacobi eigenvalue iteration on m.
// vanilla: MatrixUtil.stepJacobi
func stepJacobi(m *m3, out *quat) {
	if m[0][1]*m[0][1]+m[1][0]*m[1][0] > 1e-6 {
		p := approxGivens(m[0][0], 0.5*(m[0][1]+m[1][0]), m[1][1])
		q, mat := p.aroundZ()
		out.mul(q)
		similarity(m, &mat)
	}
	if m[0][2]*m[0][2]+m[2][0]*m[2][0] > 1e-6 {
		p := approxGivens(m[0][0], 0.5*(m[0][2]+m[2][0]), m[2][2]).inverse()
		q, mat := p.aroundY()
		out.mul(q)
		similarity(m, &mat)
	}
	if m[1][2]*m[1][2]+m[2][1]*m[2][1] > 1e-6 {
		p := approxGivens(m[1][1], 0.5*(m[1][2]+m[2][1]), m[2][2])
		q, mat := p.aroundX()
		out.mul(q)
		similarity(m, &mat)
	}
}

// decomposeMatrix splits a row-major 4x4 matrix into translation, rotations
// and scale.
// vanilla: MatrixUtil.svdDecompose
func decomposeMatrix(list []float32) Transformation {
	var in m3
	for c := range 3 {
		for r := range 3 {
			in[c][r] = list[r*4+c]
		}
	}
	factor := 1 / list[15]
	for c := range 3 {
		for r := range 3 {
			in[c][r] *= factor
		}
	}
	t := [3]float32{list[3] * factor, list[7] * factor, list[11] * factor}

	scratch := in
	scratch.transpose()
	scratch.mul(&in)
	v := quat{0, 0, 0, 1}
	for range 5 {
		stepJacobi(&scratch, &v)
	}
	v.normalize()
	zero0 := float64(scratch[0][0]) < 1e-6
	zero1 := float64(scratch[1][1]) < 1e-6

	rot := v.matrix()
	in.mul(&rot) // u012s
	u := quat{0, 0, 0, 1}

	var p givens
	if zero0 {
		p = qrGivens(in[1][1], -in[1][0])
	} else {
		p = qrGivens(in[0][0], in[0][1])
	}
	q, u12s := p.aroundZ()
	u.mul(q)
	u12s.transpose()
	u12s.mul(&in)

	if zero0 {
		p = qrGivens(u12s[2][2], -u12s[2][0])
	} else {
		p = qrGivens(u12s[0][0], u12s[0][2])
	}
	q, u2s := p.inverse().aroundY()
	u.mul(q)
	u2s.transpose()
	u2s.mul(&u12s)

	if zero1 {
		p = qrGivens(u2s[2][2], -u2s[2][1])
	} else {
		p = qrGivens(u2s[1][1], u2s[1][2])
	}
	q, sm := p.aroundX()
	u.mul(q)
	sm.transpose()
	sm.mul(&u2s)

	return Transformation{
		Translation:   t,
		LeftRotation:  normalizedQuaternion(u),
		Scale:         [3]float32{sm[0][0], sm[1][1], sm[2][2]},
		RightRotation: normalizedQuaternion(quat{-v[0], -v[1], -v[2], v[3]}),
	}
}
