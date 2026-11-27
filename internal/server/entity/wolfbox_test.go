package entity

import "testing"

func TestBabyWolfBox(t *testing.T) {
	w := NewMob(TypeWolf, 1, 0, 64, 0, 1)
	if w.BbWidth() != 0.6 || w.BbHeight() != 0.85 || w.EyeHeight() != 0.68 {
		t.Fatalf("adult box %v %v %v", w.BbWidth(), w.BbHeight(), w.EyeHeight())
	}
	w.BabyAge = -24000
	if w.BbWidth() != 0.3 || w.BbHeight() != 0.425 || w.EyeHeight() != 0.34375 {
		t.Fatalf("baby box %v %v %v", w.BbWidth(), w.BbHeight(), w.EyeHeight())
	}
}
