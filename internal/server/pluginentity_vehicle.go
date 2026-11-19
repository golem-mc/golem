package server

import (
	"github.com/golem-mc/golem/internal/server/command"
	"github.com/golem-mc/golem/plugin"
)

type (
	vehicleOps struct{ e pluginEntity }

	pluginVehicleEntity struct {
		pluginEntity
		vehicleOps
	}
)

// pluginVehicleOf wraps a minecart or boat body, nil for any other.
func pluginVehicleOf(base pluginEntity, b command.Entity) plugin.Entity {
	snap, ok := b.(*entitySnapshot)
	if !ok {
		return nil
	}
	switch snap.ref.(type) {
	case *boatEntity, *minecartEntity:
		return pluginVehicleEntity{pluginEntity: base, vehicleOps: vehicleOps{base}}
	}
	return nil
}

// DamageTaken is the hurt damage the tracker tells the client as the wobble.
// vanilla: VehicleEntity.getDamage
func (o vehicleOps) DamageTaken() float32 {
	b, ok := o.e.live()
	if !ok {
		return 0
	}
	snap, ok := b.(*entitySnapshot)
	if !ok {
		return 0
	}
	switch v := snap.ref.(type) {
	case *boatEntity:
		o.e.host.srv.boatsMu.Lock()
		defer o.e.host.srv.boatsMu.Unlock()
		return float32(v.boat.HurtDamage)
	case *minecartEntity:
		o.e.host.srv.minecartsMu.Lock()
		defer o.e.host.srv.minecartsMu.Unlock()
		return float32(v.cart.HurtDamage)
	}
	return 0
}

// SetDamageTaken writes the damage under the kind's lock; the kind's tracker
// tells the client on its next tick.
// vanilla: VehicleEntity.setDamage
func (o vehicleOps) SetDamageTaken(n float32) {
	b, ok := o.e.live()
	if !ok {
		return
	}
	snap, ok := b.(*entitySnapshot)
	if !ok {
		return
	}
	n = max(n, 0)
	switch v := snap.ref.(type) {
	case *boatEntity:
		o.e.host.srv.boatsMu.Lock()
		v.boat.HurtDamage = float64(n)
		o.e.host.srv.boatsMu.Unlock()
	case *minecartEntity:
		o.e.host.srv.minecartsMu.Lock()
		v.cart.HurtDamage = float64(n)
		o.e.host.srv.minecartsMu.Unlock()
	}
}

// Each handle type has exactly the interfaces of its kind, with no method
// left to a nil embedded interface.
var (
	_ plugin.Mob           = pluginMobEntity{}
	_ plugin.Ageable       = pluginAgeableEntity{}
	_ plugin.Breedable     = pluginBreedableEntity{}
	_ plugin.Tameable      = pluginTameableEntity{}
	_ plugin.Villager      = pluginVillagerEntity{}
	_ plugin.ExperienceOrb = pluginOrbEntity{}
	_ plugin.Vehicle       = pluginVehicleEntity{}
	_ plugin.LivingEntity  = pluginLivingEntity{}
	_ plugin.ArmorStand    = pluginStandEntity{}
)
