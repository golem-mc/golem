package entity

import "math"

const (
	// FurnaceFuelPerItem and FurnaceMaxFuel are the burn ticks one coal or
	// charcoal adds and the most a cart takes.
	// vanilla: MinecartFurnace.FUEL_TICKS_PER_ITEM, MAX_FUEL_TICKS
	FurnaceFuelPerItem = 3600
	FurnaceMaxFuel     = 32000

	// furnacePushKeep is the share of its speed a pushed cart keeps before the
	// push is added back, and furnacePushWater the scale in water.
	// vanilla: MinecartFurnace.applyNaturalSlowdown
	furnacePushKeep  = 0.8
	furnacePushWater = 0.1
	// furnaceIdleKeep is the share an unpushed cart keeps, ahead of the
	// shared slowdown.
	furnaceIdleKeep = 0.98
	// furnaceLandSpeed and furnaceWaterSpeed scale the shared top speed.
	// vanilla: MinecartFurnace.getMaxSpeed
	furnaceLandSpeed  = 0.5
	furnaceWaterSpeed = 0.75
)

// FurnaceCart is the state a furnace minecart adds to the cart: its fuel in
// burn ticks and the push the fuel drives it with. PushY is never saved.
type FurnaceCart struct {
	Fuel                int32
	PushX, PushY, PushZ float64
	// Lit is the synced flag the client draws a lit furnace by; it follows
	// Fuel at the end of each tick, not at once. vanilla: MinecartFurnace.hasFuel
	Lit bool
}

// Tick burns a tick of fuel and sets the lit flag; an empty cart loses its
// push.
// vanilla: MinecartFurnace.tick
func (f *FurnaceCart) Tick() {
	if f.Fuel > 0 {
		f.Fuel--
	}
	if f.Fuel <= 0 {
		f.PushX, f.PushY, f.PushZ = 0, 0, 0
	}
	f.Lit = f.Fuel > 0
}

// AddFuel takes one coal or charcoal, pushing the cart away from the player
// at (px, pz); it reports whether the item was taken.
// vanilla: MinecartFurnace.addFuel
func (f *FurnaceCart) AddFuel(cartX, cartZ, px, pz float64) bool {
	if f.Fuel+FurnaceFuelPerItem > FurnaceMaxFuel {
		return false
	}
	f.Fuel += FurnaceFuelPerItem
	f.PushX, f.PushY, f.PushZ = cartX-px, 0, cartZ-pz
	return true
}

// Slowdown applies the furnace's own slowdown before the shared one: the
// push is turned along the motion, then added back after the cart keeps 0.8
// of its speed.
// vanilla: MinecartFurnace.applyNaturalSlowdown, calculateNewPushAlong
func (f *FurnaceCart) Slowdown(vx, vy, vz float64, inWater bool) (float64, float64) {
	if f.PushX*f.PushX+f.PushY*f.PushY+f.PushZ*f.PushZ <= 1.0e-7 {
		return vx * furnaceIdleKeep, vz * furnaceIdleKeep
	}
	f.turnPush(vx, vy, vz)
	nx, nz := vx*furnacePushKeep+f.PushX, vz*furnacePushKeep+f.PushZ
	if inWater {
		nx, nz = nx*furnacePushWater, nz*furnacePushWater
	}
	return nx, nz
}

// turnPush points the push along the motion, keeping its length, once both
// are above the epsilons.
func (f *FurnaceCart) turnPush(vx, vy, vz float64) {
	if f.PushX*f.PushX+f.PushZ*f.PushZ <= 1.0e-4 || vx*vx+vz*vz <= 0.001 {
		return
	}
	pushLen := math.Sqrt(f.PushX*f.PushX + f.PushY*f.PushY + f.PushZ*f.PushZ)
	// vanilla: Vec3.projectedOn, Vec3.normalize
	lenSq := vx*vx + vy*vy + vz*vz
	dot := f.PushX*vx + f.PushY*vy + f.PushZ*vz
	px, py, pz := vx*dot/lenSq, vy*dot/lenSq, vz*dot/lenSq
	if dist := math.Sqrt(px*px + py*py + pz*pz); dist < 1.0e-5 {
		f.PushX, f.PushY, f.PushZ = 0, 0, 0
	} else {
		f.PushX, f.PushY, f.PushZ = px/dist*pushLen, py/dist*pushLen, pz/dist*pushLen
	}
}
