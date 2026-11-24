package server

import (
	"fmt"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/effect"
	"github.com/golem-mc/golem/internal/server/player"
	"github.com/golem-mc/golem/internal/server/rules"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/registry"
)

// The beacon: the payment slot window, Confirm's Set Beacon Effect, and the
// 80-tick update counting the pyramid and applying the choices to players
// in reach. Pyramid, beam and choice rules are package rules'; the window's
// slots are package inventory's. The window's numbers live in the beacon's
// block entity, which also keeps the choices between windows.

// beaconPropsAt is what the window draws: the last counted level and the
// choices, in wire property form, read under the block entity's lock; a
// beacon without one opens on zeros.
func beaconPropsAt(view windowView) [protocol.BeaconPropertyCount]int32 {
	var props [protocol.BeaconPropertyCount]int32
	if view.w == nil {
		return props
	}
	if be := view.w.w.BlockEntity(view.w.x, view.w.y, view.w.z); be != nil {
		be.Use(func(b *world.BlockEntity) {
			props = rules.BeaconProps(b.BeaconLevels, b.BeaconPrimary, b.BeaconSecondary)
		})
	}
	return props
}

// sendBeaconProperties sends the window's numbers: all three with force,
// else each that changed since last told. The caller holds the inventory
// lock, which guards the open window.
// vanilla: AbstractContainerMenu.sendAllDataToRemote
func (c *client) sendBeaconProperties(view windowView, props [protocol.BeaconPropertyCount]int32, force bool) error {
	if view.kind != windowBeacon || view.w == nil {
		return nil
	}
	for property, value := range props {
		if !force && view.w.SentBeacon[property] == value {
			continue
		}
		view.w.SentBeacon[property] = value
		if err := c.sendContainerProperty(view, int32(property), value); err != nil {
			return err
		}
	}
	return nil
}

// queueBeaconProperty tells one viewer a window number from the tick, off
// the connection's goroutine: via the mailbox, diffed against what they
// were told under their inventory lock like every window packet.
func (s *Server) queueBeaconProperty(c *client, window *openWindow, property, value int32) {
	c.player.Inventory(func(*player.Inventory) {
		if c.openContainerWindow() != window {
			return
		}
		if window.SentBeacon[property] == value {
			return
		}
		window.SentBeacon[property] = value
		c.send(protocol.IDPlayContainerProperty, containerPropertyPayload(window.id, property, value))
	})
}

// readBeaconChoice reads one side of Set Beacon Effect: the effect ID, and
// whether one was named. An ID the registry cannot name fails the packet,
// as vanilla's effect lookup throws on it.
// vanilla: ServerboundSetBeaconPacket, MobEffect.STREAM_CODEC
func readBeaconChoice(r *protocol.Reader) (effect int32, ok bool) {
	if !r.ReadBool() {
		return rules.NoBeaconEffect, r.Err() == nil
	}
	effect = r.ReadVarInt()
	if err := r.Err(); err != nil {
		return rules.NoBeaconEffect, false
	}
	if registry.MobEffectName(effect) == "" {
		return rules.NoBeaconEffect, false
	}
	return effect, true
}

// handleSetBeaconEffect handles the beacon window's Confirm: one payment
// item spent on choices the pyramid's level allows. A missing payment or
// choices beyond the level disconnect the lying client. Two lock passes:
// payment and choices written under the inventory lock with the block
// entity's beside it, then sound, corrections and properties after.
// vanilla: ServerGamePacketListenerImpl.handleSetBeaconPacket,
// BeaconMenu.updateEffects
func (c *client) handleSetBeaconEffect(r *protocol.Reader) error {
	primary, primaryRead := readBeaconChoice(r)
	secondary, secondaryRead := readBeaconChoice(r)
	if !primaryRead || !secondaryRead {
		err := r.Err()
		if err == nil {
			err = fmt.Errorf("unknown mob effect")
		}
		return c.decodeFailure("set_beacon", err)
	}

	view := c.currentView()
	if view.kind != windowBeacon || view.w == nil || view.be == nil {
		// A confirm outside a beacon window is ignored; vanilla's listener checks
		// its menu first.
		c.log.Debug("ignoring a set beacon effect outside a beacon window")
		return nil
	}
	if !c.containerStillOpen(view.w) {
		c.log.Debug("ignoring a set beacon effect on a window out of reach")
		return nil
	}

	// Payment and level are read under the inventory lock, the entity's numbers
	// under the entity's, the clicks' lock order; a client that cannot pay or
	// asks past the level is lying about its menu.
	var refused bool
	c.player.Inventory(func(*player.Inventory) {
		var levels int
		view.be.Use(func(b *world.BlockEntity) { levels = b.BeaconLevels })
		if view.w.Menu[0].Empty() || !rules.BeaconValidateEffects(primary, secondary, levels) {
			refused = true
			return
		}
		view.be.Use(func(b *world.BlockEntity) {
			b.BeaconPrimary, b.BeaconSecondary = primary, secondary
			view.w.w.Touch(b.X, b.Y, b.Z)
		})
		view.w.Menu[0].Count--
		if view.w.Menu[0].Count <= 0 {
			view.w.Menu[0] = protocol.ItemStack{}
		}
	})
	if refused {
		return c.disconnect(fmt.Errorf("invalid beacon effects from %s", c.player.Name),
			protocol.Text{Translate: "multiplayer.disconnect.generic"})
	}

	// The choice chime goes before the corrections, only while the beam can
	// leave the block.
	if rules.BeaconBeamClear(view.w.w, view.w.x, view.w.y, view.w.z,
		view.w.w.SurfaceHeight(view.w.x, view.w.z)) {
		payload := soundEffectIDAt(registry.SoundBlockBeaconPowerSelect,
			protocol.SoundCategoryBlock,
			float64(view.w.x)+0.5, float64(view.w.y)+0.5, float64(view.w.z)+0.5, 1.0, 1.0)
		c.srv.broadcastNear(view.w.w,
			float64(view.w.x)+0.5, float64(view.w.y)+0.5, float64(view.w.z)+0.5,
			soundRange(1), protocol.IDPlaySoundEffect, payload)
	}

	// Both changes wait for the menu's next broadcast, slots before data slots.
	// vanilla: AbstractContainerMenu.broadcastChanges
	var sendErr error
	c.player.Inventory(func(inv *player.Inventory) {
		if err := c.sendContainerSlot(inv, view, 0, view.w.Menu[0]); err != nil {
			sendErr = err
			return
		}
		sendErr = c.sendBeaconProperties(view, beaconPropsAt(view), false)
	})
	return sendErr
}

// tickBeacon runs a beacon's 80-tick update: the pyramid counted while the
// beam can leave the block, and the choices applied to every player in
// reach. Player effects are written after the block entity's lock.
// vanilla: BeaconBlockEntity.tick
func (s *Server) tickBeacon(w *world.World, be *world.BlockEntity, age int64) {
	if age%rules.BeaconUpdateTicks != 0 {
		return
	}
	x, y, z := be.X, be.Y, be.Z
	clearBeam := rules.BeaconBeamClear(w, x, y, z, w.SurfaceHeight(x, z))
	var prevLevels, levels int
	var primary, secondary int32
	be.Use(func(b *world.BlockEntity) {
		prevLevels = b.BeaconLevels
		// Count the level only while the beam escapes; a blocked beam keeps the
		// last count, which vanilla shows in the window but never applies.
		if clearBeam {
			if counted := rules.BeaconPyramidLevel(w, x, y, z, w.MinY()); counted != b.BeaconLevels {
				b.BeaconLevels = counted
				w.Touch(b.X, b.Y, b.Z)
			}
		}
		levels = b.BeaconLevels
		primary, secondary = b.BeaconPrimary, b.BeaconSecondary
	})

	// Apply the choices to every player in reach: the primary at the tier a
	// level-4 beacon with a matching secondary reaches, the secondary only when
	// level 4 bought another or both match. A choiceless beacon applies nothing
	// but still hums, the hum before the pyramid's activate or deactivate.
	if clearBeam && levels > 0 {
		if primary >= 0 {
			duration := rules.BeaconEffectDuration(levels)
			amplifier := rules.BeaconEffectAmplifier(levels, primary, secondary)
			lo, hi := rules.BeaconReachBounds(rules.BeaconRange(levels), w.Height(), x, y, z)
			s.mu.RLock()
			clients := s.clientsLocked()
			s.mu.RUnlock()
			for _, c := range clients {
				if c.theWorld() != w {
					continue
				}
				// vanilla: BeaconBlockEntity.applyEffects, EntitySelector.NO_SPECTATORS
				if c.player.GameMode() == protocol.GameModeSpectator {
					continue
				}
				// Reject by position alone before the pose and scale reads.
				pos := c.player.Position()
				if pos.X < lo[0]-rules.MaxPlayerHalfWidth || pos.X > hi[0]+rules.MaxPlayerHalfWidth ||
					pos.Z < lo[2]-rules.MaxPlayerHalfWidth || pos.Z > hi[2]+rules.MaxPlayerHalfWidth {
					continue
				}
				if b := c.bodyBoxAt(pos); !rules.BoxesIntersect(lo, hi, [3]float64{b[0], b[1], b[2]}, [3]float64{b[3], b[4], b[5]}) {
					continue
				}
				c.effectCause = effect.CauseBeacon
				c.AddEffect(effect.New(primary, duration, amplifier, true, true, true))
				if rules.BeaconAppliesSecondary(levels, primary, secondary) {
					c.effectCause = effect.CauseBeacon
					c.AddEffect(effect.New(secondary, duration, 0, true, true, true))
				}
			}
		}
		s.broadcastNear(w, float64(x)+0.5, float64(y)+0.5, float64(z)+0.5, soundRange(1),
			protocol.IDPlaySoundEffect, soundEffectIDAt(registry.SoundBlockBeaconAmbient,
				protocol.SoundCategoryBlock,
				float64(x)+0.5, float64(y)+0.5, float64(z)+0.5, 1.0, 1.0))
	}
	if clearBeam && levels != prevLevels {
		if (prevLevels <= 0) != (levels <= 0) {
			// A pyramid come or gone: the beacon announces which.
			sound := registry.SoundBlockBeaconActivate
			if levels <= 0 {
				sound = registry.SoundBlockBeaconDeactivate
			}
			s.broadcastNear(w, float64(x)+0.5, float64(y)+0.5, float64(z)+0.5, soundRange(1),
				protocol.IDPlaySoundEffect, soundEffectIDAt(sound,
					protocol.SoundCategoryBlock,
					float64(x)+0.5, float64(y)+0.5, float64(z)+0.5, 1.0, 1.0))
			if levels > 0 {
				s.constructedBeacon(w, x, y, z, int32(levels))
			}
		}
		s.tellBeaconLevels(w, x, y, z, levels)
	}
}

// tellBeaconLevels tells viewers of the beacon its new level, which the
// window's choice buttons are drawn by.
func (s *Server) tellBeaconLevels(w *world.World, x, y, z int32, levels int) {
	s.containerViewers(w, x, y, z, nil, func(c *client) {
		window := c.openContainerWindow()
		if window == nil || window.kind != windowBeacon {
			return
		}
		s.queueBeaconProperty(c, window, protocol.BeaconPropertyLevels, int32(levels))
	})
}

// constructedBeacon fires construct_beacon for players in the box around a
// newly active beacon: 10 blocks sideways, 5 above, 9 below.
// vanilla: BeaconBlockEntity.tick
func (s *Server) constructedBeacon(w *world.World, x, y, z, levels int32) {
	s.mu.RLock()
	clients := s.clientsLocked()
	s.mu.RUnlock()
	for _, c := range clients {
		if c.theWorld() != w {
			continue
		}
		b := c.bodyBox()
		if b[3] >= float64(x)-10 && b[0] <= float64(x)+10 &&
			b[4] >= float64(y)-9 && b[1] <= float64(y)+5 &&
			b[5] >= float64(z)-10 && b[2] <= float64(z)+10 {
			c.constructBeaconAdvancements(levels)
		}
	}
}
