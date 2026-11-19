package entities

// The server-only entity tests: the kinds no /summon can name — an area
// effect cloud, a firework rocket, a fishing bobber, a leash knot, a painting
// and an item frame — answering an entity selector so that /kill and /tp
// reach them, not just the mobs and projectiles the world spawns on its own.

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/nbt"
)

// serverOnlyTarget is one of the kinds that answers selectors without being
// spawnable by /summon: how a test stands one up and the translation its
// display name carries.
type serverOnlyTarget struct {
	typ         string
	translation string
	spawn       func(*server.Server) server.TestTrackedBody
	// savedKeys are the tag keys vanilla saves for this kind, which
	// /data get entity must carry beside the shared Pos and UUID.
	savedKeys []string
	// noWire is set for a kind that cannot be told: an unowned hook is gone
	// on its first tick (see TestATeleportedHookIsToldOncePerMovementTick).
	noWire bool
	// flies is set for a kind that moves on after the teleport, so the wire
	// and the selector agree only on x and z.
	flies bool
}

func serverOnlyTargets() []serverOnlyTarget {
	return []serverOnlyTarget{
		{
			typ:         "minecraft:area_effect_cloud",
			translation: "entity.minecraft.area_effect_cloud",
			spawn:       func(s *server.Server) server.TestTrackedBody { return s.SpawnCloudEntity(0.5, 64, 3.5) },
			savedKeys: []string{
				"Age", "Duration", "WaitTime", "ReapplicationDelay",
				"DurationOnUse", "RadiusOnUse", "RadiusPerTick", "Radius",
				"potion_contents", "potion_duration_scale",
			},
		},
		{
			typ:         "minecraft:firework_rocket",
			translation: "entity.minecraft.firework_rocket",
			spawn:       func(s *server.Server) server.TestTrackedBody { return s.SpawnFireworkEntity(0.5, 64, 3.5) },
			flies:       true,
			savedKeys:   []string{"Life", "LifeTime", "FireworksItem", "HasBeenShot", "ShotAtAngle"},
		},
		{
			typ:         "minecraft:fishing_bobber",
			translation: "entity.minecraft.fishing_bobber",
			spawn:       func(s *server.Server) server.TestTrackedBody { return s.SpawnBobberEntity(0.5, 64, 3.5) },
			noWire:      true,
		},
		{
			typ:         "minecraft:leash_knot",
			translation: "entity.minecraft.leash_knot",
			spawn:       func(s *server.Server) server.TestTrackedBody { return s.SpawnKnotEntity(0, 64, 3) },
		},
		{
			typ:         "minecraft:painting",
			translation: "entity.minecraft.painting",
			spawn:       func(s *server.Server) server.TestTrackedBody { return s.SpawnPaintingEntity(0, 64, 3, 0) },
		},
		{
			typ:         "minecraft:item_frame",
			translation: "entity.minecraft.item_frame",
			spawn:       func(s *server.Server) server.TestTrackedBody { return s.SpawnFrameEntity(0, 64, 3) },
		},
		{
			typ:         "minecraft:glow_item_frame",
			translation: "entity.minecraft.glow_item_frame",
			spawn:       func(s *server.Server) server.TestTrackedBody { return s.SpawnGlowFrameEntity(0, 64, 3) },
		},
	}
}

// TestSelectorsKillAndTeleportServerOnlyEntities checks that an entity
// selector reaches the kinds no /summon can name, and that /kill takes one
// out of the world and /tp moves it: each answers with its own entity ID on
// the wire rather than being reported while staying put.
func TestSelectorsKillAndTeleportServerOnlyEntities(t *testing.T) {
	t.Parallel()
	for _, target := range serverOnlyTargets() {
		t.Run(target.typ, func(t *testing.T) {
			t.Parallel()
			srv, c := servertest.Join(t, servertest.MobTestConfig(), "Tester", servertest.OpsListing(t, "Tester"))

			// The loop is stopped so a cloud or a rocket is still there to
			// be moved and killed rather than spent first.
			srv.StopTick()

			body := target.spawn(srv)
			if body.ID == 0 {
				t.Fatalf("no %s was stood up", target.typ)
			}

			selected := srv.SelectedEntities(target.typ)
			if len(selected) != 1 {
				t.Fatalf("selector found %d %s, want 1", len(selected), target.typ)
			}
			if got := selected[0].EntityID(); got != body.ID {
				t.Fatalf("selected entity = %d, want the %s's %d", got, target.typ, body.ID)
			}

			if !target.noWire {
				for range 2 { // a fresh body is paired, not told, on its first pass
					srv.RunTick()
				}
			}
			c.ChatCommand("tp @e[type=" + target.typ + "] 6.5 64 6.5")
			servertest.PollUntil(t, 10*time.Second, func() bool {
				got := srv.SelectedEntities(target.typ)
				return len(got) == 1 && got[0].Position().X == 6.5
			})
			if !target.noWire {
				for range 10 { // beyond a rocket's movement period
					srv.RunTick()
				}
				data, ok := servertest.ReadUntil(t, c, servertest.ExpectTimeout, protocol.IDPlayTeleportEntity)
				if !ok {
					t.Fatalf("no teleport entity arrived for the %s", target.typ)
				}
				r := protocol.NewReader(data)
				if id := r.ReadVarInt(); id != body.ID {
					t.Fatalf("teleported entity = %d, want the %s's %d", id, target.typ, body.ID)
				}
				r.ReadVarInt() // position path
				x, y, z := r.ReadF64(), r.ReadF64(), r.ReadF64()
				pos := srv.SelectedEntities(target.typ)[0].Position()
				if target.typ == "minecraft:painting" {
					// vanilla: Painting.trackingPosition, BlockAttachedEntity.setPos,
					// HangingEntity.recalculateBoundingBox
					// The wire carries the anchor; selectors use the hanging box's center.
					if x != 6 || y != 64 || z != 6 {
						t.Errorf("/tp painting wire anchor = (%v, %v, %v), want (6, 64, 6)", x, y, z)
					}
					if pos.X != 6.5 || pos.Y != 64.5 || pos.Z != 6.03125 {
						t.Errorf("/tp painting selector center = (%v, %v, %v), want (6.5, 64.5, 6.03125)", pos.X, pos.Y, pos.Z)
					}
				} else if x != pos.X || z != pos.Z || !target.flies && y != pos.Y {
					t.Errorf("/tp put the %s at (%v, %v, %v) on the wire, but the selector reads (%v, %v, %v)",
						target.typ, x, y, z, pos.X, pos.Y, pos.Z)
				}
			}

			// The tag option reaches it too, and /tag writes to it as it
			// does to every loaded body.
			c.ChatCommand("tag @e[type=" + target.typ + "] add noted")
			servertest.AwaitCommandFeedbackText(t, c, "commands.tag.add.success.single",
				protocol.Text{Text: "noted"},
				servertest.EntityDisplayComponent(target.typ, target.translation, body.UUID))
			c.ChatCommand("tag @e[type=" + target.typ + ",tag=noted] list")
			servertest.AwaitCommandFeedbackText(t, c, "commands.tag.list.single.success",
				servertest.EntityDisplayComponent(target.typ, target.translation, body.UUID),
				servertest.TagCountText(1), servertest.GreenTagText("noted"))

			c.ChatCommand("kill @e[type=" + target.typ + "]")
			servertest.AwaitCommandFeedbackText(t, c, "commands.kill.success.single",
				servertest.EntityDisplayComponent(target.typ, target.translation, body.UUID))

			if got := srv.SelectedEntities(target.typ); len(got) != 0 {
				t.Errorf("%s still answers the selector after /kill: %d left", target.typ, len(got))
			}
			c.Disconnect()
		})
	}
}

// awaitEntityDataCompound returns the SNBT printed by /data get entity, with
// an optional NBT path. Packets read past are kept for the next reader.
func awaitEntityDataCompound(t *testing.T, srv *server.Server, c *servertest.Client, selector string, path ...string) string {
	t.Helper()
	c.ChatCommand("data get entity " + strings.Join(append([]string{selector}, path...), " "))

	var skipped []servertest.PushedPacket
	defer func() { c.Unread(skipped...) }()

	var compound string
	if c.ReadTicks(srv, 200, func(packetID int32, data []byte) bool {
		t.Helper()
		text, ok := protocol.ReadText(protocol.NewReader(data))
		if packetID != protocol.IDPlaySystemChat || !ok || text.Translate != "commands.data.entity.query" {
			skipped = append(skipped, servertest.PushedPacket{Id: packetID, Data: data})
			return false
		}
		if len(text.With) != 2 {
			t.Fatalf("the /data query for %s carries %d arguments, want 2", selector, len(text.With))
		}
		compound = text.With[1].Plain()
		return true
	}) {
		return compound
	}
	t.Fatalf("no /data get entity feedback for %s arrived", selector)
	return ""
}

// TestTeleportSendsOneTeleportEntityPerWatcher checks that a teleport reaches
// each client shown the entity once. A rocket and a hook each once built one
// Teleport Entity per watcher and then sent all of them to every watcher, so
// two watchers read two apiece; the kind's own teleport must send one.
func TestTeleportSendsOneTeleportEntityPerWatcher(t *testing.T) {
	t.Parallel()
	targets := []struct {
		typ   string
		spawn func(*server.Server) server.TestTrackedBody
	}{
		{"minecraft:firework_rocket", func(s *server.Server) server.TestTrackedBody {
			return s.SpawnFireworkEntity(0.5, 64, 3.5)
		}},
	}
	for _, target := range targets {
		t.Run(target.typ, func(t *testing.T) {
			t.Parallel()
			srv, connect := servertest.Start(t, servertest.MobTestConfig(), servertest.OpsListing(t, "Alice", "Bob"))
			alice := servertest.JoinFeedbackWorld(t, connect, "Alice", nil)
			bob := servertest.JoinFeedbackWorld(t, connect, "Bob", []*servertest.Client{alice})

			// The loop is stopped so the rocket is not spent before the move.
			srv.StopTick()
			if body := target.spawn(srv); body.ID == 0 {
				t.Fatalf("no %s was stood up", target.typ)
			}

			for range 2 { // a fresh body is paired, not told, on its first pass
				srv.RunTick()
			}
			alice.ChatCommand("tp @e[type=" + target.typ + "] 6.5 64 6.5")
			servertest.PollUntil(t, 10*time.Second, func() bool {
				got := srv.SelectedEntities(target.typ)
				return len(got) == 1 && got[0].Position().X == 6.5
			})
			for range 10 { // beyond the rocket's movement period
				srv.RunTick()
			}

			for i, c := range []*servertest.Client{alice, bob} {
				if _, ok := servertest.ReadUntil(t, c, servertest.ExpectTimeout, protocol.IDPlayTeleportEntity); !ok {
					t.Fatalf("watcher %d read no teleport for the %s", i, target.typ)
				}
				// A second Teleport Entity within the window (the loop is stopped, so no tick runs) is the copy
				// neither watcher may receive.
				c.ExpectNoneWithinTicks(srv, 0, servertest.PacketIs(protocol.IDPlayTeleportEntity))
			}
			alice.Disconnect()
			bob.Disconnect()
		})
	}
}

// expectSavedTags checks a saved compound's Tags against the words given,
// whatever order the list comes out in.
func expectSavedTags(t *testing.T, compound nbt.Compound, want ...string) {
	t.Helper()
	list, ok := nbt.Get[nbt.List](compound, "Tags")
	if !ok {
		t.Fatalf("the saved compound carries no Tags, want %v", want)
	}
	var tags []string
	for _, item := range list.Items {
		tags = append(tags, string(item.(nbt.String)))
	}
	slices.Sort(tags)
	slices.Sort(want)
	if !slices.Equal(tags, want) {
		t.Errorf("the saved Tags = %v, want %v", tags, want)
	}
}
