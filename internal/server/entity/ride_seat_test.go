package entity

import "testing"

// A mob riding a chicken stands in the chicken's column at any yaw, as a
// vanilla server reports it.
func TestAChickensMobPassengerSitsInItsColumn(t *testing.T) {
	t.Parallel()
	for _, yaw := range []float32{0, 90, 180, -45} {
		chicken := NewMob(TypeChicken, 1, 148.66, 126, 153.5, 1)
		chicken.Yaw = yaw
		pig := NewMob(TypePig, 2, 0, 0, 0, 2)
		chicken.seatPassenger(pig)
		if pig.X != 148.66 || pig.Z != 153.5 || pig.Y != 126.7 {
			t.Errorf("yaw %v: pig at (%v, %v, %v), want (148.66, 126.7, 153.5)", yaw, pig.X, pig.Y, pig.Z)
		}
	}
}
