package entity

import (
	"math"
	"testing"

	"github.com/golem-mc/golem/nbt"
)

func floats(v ...float32) nbt.Tag {
	items := make([]nbt.Tag, len(v))
	for i, f := range v {
		items[i] = nbt.Float(f)
	}
	return nbt.List{Elem: nbt.TypeFloat, Items: items}
}

// compose is translation * leftRotation * scale * rightRotation as a row-major matrix.
func compose(t Transformation) [16]float64 {
	l, r := quat(t.LeftRotation).matrix(), quat(t.RightRotation).matrix()
	var out [16]float64
	for row := range 3 {
		for col := range 3 {
			var sum float64
			for k := range 3 {
				sum += float64(l[k][row]) * float64(t.Scale[k]) * float64(r[col][k])
			}
			out[row*4+col] = sum
		}
		out[row*4+3] = float64(t.Translation[row])
	}
	out[15] = 1
	return out
}

// TestAMatrixTransformationDecomposesToTheSameMatrix checks that the parts a
// matrix splits into compose back to it.
// vanilla: MatrixUtil.svdDecompose
func TestAMatrixTransformationDecomposesToTheSameMatrix(t *testing.T) {
	source := Transformation{
		Translation: [3]float32{1, 2, 3}, Scale: [3]float32{2, 0.5, 3},
		LeftRotation:  normalizedQuaternion([4]float32{0.3, -0.5, 0.2, 0.8}),
		RightRotation: normalizedQuaternion([4]float32{-0.1, 0.4, 0.6, 0.7}),
	}
	m := compose(source)
	list := make([]float32, 16)
	for i, v := range m {
		list[i] = float32(v)
	}
	got := compose(TransformationOf(floats(list...)))
	for i := range m {
		if math.Abs(got[i]-m[i]) > 1e-3 {
			t.Fatalf("recomposed %v, want %v", got, m)
		}
	}
}
