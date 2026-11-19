package server

import (
	"testing"

	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
)

func TestWardenVibrationSourceOfAThrow(t *testing.T) {
	id := uuid.New()
	ctx := rules.GameEventContext{Player: id}
	if s, o := wardenVibrationSource(registry.GameEventProjectileShoot, ctx); s != id || o != uuid.Nil {
		t.Fatalf("projectile_shoot = (%v, %v), want (%v, nil)", s, o, id)
	}
	if s, o := wardenVibrationSource(registry.GameEventProjectileLand, ctx); s != uuid.Nil || o != id {
		t.Fatalf("projectile_land = (%v, %v), want (nil, %v)", s, o, id)
	}
}
