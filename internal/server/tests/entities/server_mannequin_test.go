package entities

import (
	"slices"
	"strings"
	"testing"

	"github.com/golem-mc/golem/internal/config"
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
)

// mannequinConfig is the spawn chunk alone, which is all a mannequin test
// reads.
func mannequinConfig() *config.Config {
	cfg := servertest.MobTestConfig()
	cfg.ViewDistance = config.MinDistance
	cfg.SimulationDistance = config.MinDistance
	return cfg
}

// joinMannequinOp joins an operator on a held loop.
func joinMannequinOp(t *testing.T, name string) (*server.Server, *servertest.Client) {
	t.Helper()
	srv, c := servertest.Join(t, mannequinConfig(), name, servertest.OpsListing(t, name))
	servertest.HoldTicks(t, srv)
	return srv, c
}

// metaOfMannequin returns the field a mannequin's metadata carries at the
// index, failing the test when it is absent.
func metaOfMannequin(t *testing.T, meta []servertest.MetaEntry, index uint8) *servertest.MetaEntry {
	t.Helper()
	entry := servertest.MetaOf(meta, index)
	if entry == nil {
		t.Fatalf("the mannequin's metadata carries no field %d", index)
	}
	return entry
}

// mannequinArrival is a mannequin as the watcher is first shown it.
type mannequinArrival struct {
	id      int32
	x, y, z float64
	meta    []servertest.MetaEntry
}

// awaitMannequin runs ticks until the client has been sent a mannequin's
// spawn and its metadata.
func awaitMannequin(t *testing.T, c *servertest.Client, srv *server.Server) mannequinArrival {
	t.Helper()
	var got mannequinArrival
	var spawned, described bool
	c.ReadTicks(srv, 5, func(id int32, data []byte) bool {
		r := protocol.NewReader(data)
		switch id {
		case protocol.IDPlaySpawnEntity:
			entityID := r.ReadVarInt()
			r.ReadUUID()
			if r.ReadVarInt() == protocol.EntityTypeMannequin {
				got.id, got.x, got.y, got.z = entityID, r.ReadF64(), r.ReadF64(), r.ReadF64()
				spawned = true
			}
		case protocol.IDPlayEntityMetadata:
			if spawned && r.ReadVarInt() == got.id {
				meta, ok := servertest.ReadMetadata(r)
				if !ok {
					t.Fatal("the mannequin's metadata carries a field this reader does not know")
				}
				got.meta, described = meta, true
			}
		}
		return spawned && described
	})
	if !spawned || !described {
		t.Fatalf("spawned=%v described=%v within 5 ticks, want both", spawned, described)
	}
	return got
}

// dataOfMannequin runs /data get on the mannequin and returns the answer.
func dataOfMannequin(t *testing.T, c *servertest.Client, srv *server.Server, path string) string {
	t.Helper()
	c.ChatCommand("data get entity @e[type=mannequin,limit=1] " + path)
	var answer string
	c.ReadTicks(srv, 3, func(id int32, data []byte) bool {
		if id != protocol.IDPlaySystemChat {
			return false
		}
		text, ok := protocol.ReadText(protocol.NewReader(data))
		if !ok || text.Translate != "commands.data.entity.query" || len(text.With) != 2 {
			return false
		}
		answer = text.With[1].Plain()
		return true
	})
	if answer == "" {
		t.Fatalf("no /data get answer for %q", path)
	}
	return answer
}

// namesTranslation reports whether the text or anything nested in it carries
// the translation key.
func namesTranslation(text protocol.Text, key string) bool {
	if text.Translate == key {
		return true
	}
	for _, part := range append(slices.Clone(text.With), text.Extra...) {
		if namesTranslation(part, key) {
			return true
		}
	}
	return false
}

// selects reports whether a selector finds anything, by giving what it finds
// a tag not yet in use.
func selects(c *servertest.Client, srv *server.Server, selector, tag string) bool {
	c.ChatCommand("tag " + selector + " add " + tag)
	found := false
	c.ReadTicks(srv, 3, func(id int32, data []byte) bool {
		if id != protocol.IDPlaySystemChat {
			return false
		}
		text, ok := protocol.ReadText(protocol.NewReader(data))
		if !ok {
			return false
		}
		found = namesTranslation(text, "commands.tag.add.success.single")
		return found || namesTranslation(text, "argument.entity.notfound.entity")
	})
	return found
}

// TestASummonedMannequinCarriesItsTag summons a crouching, immovable,
// left-handed mannequin and reads its pose, hand and immovable flag off the
// spawn and its pose back through /data get.
func TestASummonedMannequinCarriesItsTag(t *testing.T) {
	t.Parallel()
	srv, c := joinMannequinOp(t, "Summoner")

	c.ChatCommand(`summon minecraft:mannequin 1.5 64 1.5 {pose:"crouching",immovable:1b,main_hand:"left"}`)
	spawn := awaitMannequin(t, c, srv)
	if spawn.x != 1.5 || spawn.y != 64 || spawn.z != 1.5 {
		t.Errorf("the mannequin stands at (%v, %v, %v), want (1.5, 64, 1.5)", spawn.x, spawn.y, spawn.z)
	}
	meta := spawn.meta

	if got := metaOfMannequin(t, meta, protocol.MetadataIndexPose).VarintVal; got != protocol.PoseSneaking {
		t.Errorf("pose = %d, want crouching %d", got, protocol.PoseSneaking)
	}
	if got := metaOfMannequin(t, meta, protocol.MetadataIndexAvatarMainHand); got.Typ != protocol.MetadataHumanoidArm || got.VarintVal != 0 {
		t.Errorf("main hand = %+v, want the left arm 0", got)
	}
	if !metaOfMannequin(t, meta, protocol.MetadataIndexMannequinImmovable).FlagVal {
		t.Error("immovable = false, want true")
	}
	if got := metaOfMannequin(t, meta, protocol.MetadataIndexAvatarLayers).ByteVal; got != 0x7F {
		t.Errorf("layers = %#x, want every layer shown", got)
	}
	if servertest.MetaOf(meta, protocol.MetadataIndexMannequinLabel) != nil {
		t.Error("the default label was sent")
	}

	if got := dataOfMannequin(t, c, srv, "pose"); !strings.Contains(got, "crouching") {
		t.Errorf("/data get entity ... pose answered %s, want crouching", got)
	}
}

// TestAMannequinRefusesAnInvalidPose checks that a pose a mannequin cannot
// take fails the summon.
func TestAMannequinRefusesAnInvalidPose(t *testing.T) {
	t.Parallel()
	_, c := servertest.Join(t, mannequinConfig(), "Summoner", servertest.OpsListing(t, "Summoner"))

	c.ChatCommand(`summon minecraft:mannequin 1.5 64 1.5 {pose:"sitting"}`)
	c.ExpectCommandFailure(servertest.CommandErrorMessageForTest("commands.summon.failed"))
}

// TestAMannequinSurvivesARestart checks that a mannequin's own fields are
// saved with its chunk and come back with it.
func TestAMannequinSurvivesARestart(t *testing.T) {
	t.Parallel()
	cfg := mannequinConfig()
	cfg.LevelName = t.TempDir()
	ops := servertest.OpsListing(t, "Keeper")

	srv, c := servertest.Join(t, cfg, "Keeper", ops)
	servertest.HoldTicks(t, srv)
	c.ChatCommand(`summon minecraft:mannequin 1.5 64 1.5 {pose:"swimming",immovable:1b,` +
		`main_hand:"left",hidden_layers:["hat"],hide_description:1b,profile:"Notch",` +
		`CustomName:"Guide",CustomNameVisible:1b,Glowing:1b,HasVisualFire:1b,` +
		`equipment:{mainhand:{id:"diamond_sword"},head:{id:"diamond_helmet"}}}`)
	awaitMannequin(t, c, srv)
	c.Settle()
	srv.FlushMobs()
	srv.Shutdown()

	srv2, again := servertest.Start(t, cfg, ops)
	client := again()
	client.Login("Keeper")
	client.Configure()
	shown := client.JoinCapturing("Keeper", map[int32]string{protocol.EntityTypeMannequin: "mannequin"})
	back := servertest.CapturedOr(t, client, shown, "mannequin", protocol.EntityTypeMannequin)
	if back.EntityID == 0 {
		t.Fatal("the mannequin did not come back")
	}
	servertest.HoldTicks(t, srv2)
	if back.MetaBad {
		t.Fatal("the restored mannequin's metadata could not be read")
	}
	if got := metaOfMannequin(t, back.Meta, protocol.MetadataIndexPose).VarintVal; got != protocol.PoseSwimming {
		t.Errorf("restored pose = %d, want swimming", got)
	}
	if got := metaOfMannequin(t, back.Meta, protocol.MetadataIndexAvatarMainHand).VarintVal; got != 0 {
		t.Errorf("restored main hand = %d, want left", got)
	}
	if !metaOfMannequin(t, back.Meta, protocol.MetadataIndexMannequinImmovable).FlagVal {
		t.Error("restored immovable = false")
	}
	if got := metaOfMannequin(t, back.Meta, protocol.MetadataIndexAvatarLayers).ByteVal; got != 0x3F {
		t.Errorf("restored layers = %#x, want the hat hidden (0x3f)", got)
	}
	label := metaOfMannequin(t, back.Meta, protocol.MetadataIndexMannequinLabel)
	if label.TextVal != nil {
		t.Error("restored label shown, want it hidden")
	}

	if got := metaOfMannequin(t, back.Meta, protocol.MetadataIndexEntityFlags).ByteVal; got != 0x41 {
		t.Errorf("restored shared flags = %#x, want 0x41", got)
	}
	if !metaOfMannequin(t, back.Meta, protocol.MetadataIndexCustomNameVisible).FlagVal {
		t.Error("restored name is not visible")
	}
	for path, want := range map[string]string{"equipment.mainhand.id": "minecraft:diamond_sword", "equipment.head.id": "minecraft:diamond_helmet", "CustomName": "Guide", "CustomNameVisible": "1b", "Glowing": "1b", "HasVisualFire": "1b"} {
		if got := dataOfMannequin(t, client, srv2, path); !strings.Contains(got, want) {
			t.Errorf("restored %s = %s, want %s", path, got, want)
		}
	}
	if got := dataOfMannequin(t, client, srv2, "profile"); !strings.Contains(got, "Notch") {
		t.Errorf("restored profile = %s, want the one named Notch", got)
	}
}

// TestAMergeMovesAMannequinForItsWatcher checks that a /data merge of the
// position and rotation reaches a watcher that already sees the mannequin.
// vanilla: Entity.load
func TestAMergeMovesAMannequinForItsWatcher(t *testing.T) {
	t.Parallel()
	srv, c := joinMannequinOp(t, "Mover")
	c.ChatCommand(`summon minecraft:mannequin 1.5 64 1.5`)
	spawn := awaitMannequin(t, c, srv)

	c.ChatCommand(`data merge entity @e[type=mannequin,limit=1] {Pos:[3.5d,64d,1.5d],Rotation:[90f,30f]}`)
	var moved, turned []byte
	record := func(id int32, data []byte) bool {
		if protocol.NewReader(data).ReadVarInt() == spawn.id {
			switch id {
			case protocol.IDPlayTeleportEntity:
				moved = data
			case protocol.IDPlayHeadRotation:
				turned = data
			}
		}
		return moved != nil && turned != nil
	}
	c.SettleAndCollect(func(id int32, data []byte) { record(id, data) })
	if moved == nil || turned == nil {
		c.ReadTicks(srv, 5, record)
	}
	if moved == nil {
		t.Fatal("the watcher was sent no movement for the merged position")
	}
	if turned == nil {
		t.Fatal("the watcher was sent no head rotation for the merged yaw")
	}
	hr := protocol.NewReader(turned)
	hr.ReadVarInt()
	if head := hr.ReadU8(); head != 64 {
		t.Errorf("head turned to %d, want 64 (90°)", head)
	}
	r := protocol.NewReader(moved)
	r.ReadVarInt()
	r.ReadVarInt() // position path
	x, y, z := r.ReadF64(), r.ReadF64(), r.ReadF64()
	yaw, pitch := r.ReadF32(), r.ReadF32()
	if x != 3.5 || y != 64 || z != 1.5 || yaw != 90 || pitch != 30 {
		t.Errorf("moved to (%v, %v, %v) facing (%v, %v), want (3.5, 64, 1.5) facing (90, 30)", x, y, z, yaw, pitch)
	}
}

// TestATeleportedMannequinMovesForItsWatcher checks that /tp and /rotate move
// and turn a mannequin for a player watching it.
// vanilla: TeleportCommand.performTeleport, RotateCommand.rotate
func TestATeleportedMannequinMovesForItsWatcher(t *testing.T) {
	t.Parallel()
	srv, c := joinMannequinOp(t, "Porter")
	t.Cleanup(srv.HoldChunkInDimension(world.NetherName, 8, 8))
	c.ChatCommand(`summon minecraft:mannequin 1.5 64 1.5`)
	spawn := awaitMannequin(t, c, srv)

	// readMove runs a command and reads the Teleport Entity and the Rotate
	// Head that turns the player model's head with the body; head is the
	// packed angle. The head is sent with the command's answer, the move by
	// the next tracking pass.
	// vanilla: Entity.teleportSetPosition, Entity.forceSetRotation
	readMove := func(what, command string) (x, y, z float64, yaw, pitch float32, head uint8) {
		t.Helper()
		var moved, turned []byte
		record := func(id int32, data []byte) bool {
			if protocol.NewReader(data).ReadVarInt() == spawn.id {
				switch id {
				case protocol.IDPlayTeleportEntity:
					moved = data
				case protocol.IDPlayHeadRotation:
					turned = data
				}
			}
			return moved != nil && turned != nil
		}
		c.ChatCommand(command)
		c.SettleAndCollect(func(id int32, data []byte) { record(id, data) })
		if moved == nil || turned == nil {
			c.ReadTicks(srv, 5, record)
		}
		if moved == nil {
			t.Fatalf("the watcher was sent no movement for the %s", what)
		}
		if turned == nil {
			t.Fatalf("the watcher was sent no head rotation for the %s", what)
		}
		hr := protocol.NewReader(turned)
		hr.ReadVarInt()
		r := protocol.NewReader(moved)
		r.ReadVarInt()
		r.ReadVarInt() // position path
		return r.ReadF64(), r.ReadF64(), r.ReadF64(), r.ReadF32(), r.ReadF32(), hr.ReadU8()
	}

	if x, y, z, yaw, pitch, head := readMove("teleport", `tp @e[type=mannequin,limit=1] 3.5 64 1.5 90 30`); x != 3.5 || y != 64 || z != 1.5 || yaw != 90 || pitch != 30 || head != 64 {
		t.Errorf("teleported to (%v, %v, %v) facing (%v, %v) head %d, want (3.5, 64, 1.5) facing (90, 30) head 64 (90°)", x, y, z, yaw, pitch, head)
	}

	if x, _, _, yaw, pitch, head := readMove("rotation", `rotate @e[type=mannequin,limit=1] 45 0`); x != 3.5 || yaw != 45 || pitch != 0 || head != 32 {
		t.Errorf("rotated to x %v facing (%v, %v) head %d, want x 3.5 facing (45, 0) head 32 (45°)", x, yaw, pitch, head)
	}

	// vanilla: Entity.teleportCrossDimension
	c.ChatCommand(`execute in minecraft:the_nether run tp @e[type=mannequin,limit=1] 8.5 70 8.5`)
	c.Settle()
	if selects(c, srv, "@e[type=mannequin,distance=..100]", "left") {
		t.Error("a mannequin teleported into the nether is still in the overworld")
	}
	if !selects(c, srv, "@e[type=mannequin]", "crossed") {
		t.Error("a mannequin teleported into the nether is nowhere")
	}
}

// TestAMannequinsBoxFollowsItsPose checks that a selector volume meets the
// body of the pose, a swimming one lying under a volume a standing one reaches.
// vanilla: Avatar.POSES
func TestAMannequinsBoxFollowsItsPose(t *testing.T) {
	t.Parallel()
	srv, c := joinMannequinOp(t, "Measurer")
	const above = "@e[type=mannequin,x=1,y=65,z=1,dx=0,dy=0,dz=0]"

	c.ChatCommand(`summon minecraft:mannequin 1.5 64 1.5`)
	awaitMannequin(t, c, srv)
	if !selects(c, srv, above, "high") {
		t.Fatal("a standing mannequin, 1.8 tall, is not found by a volume at its head")
	}

	c.ChatCommand(`data merge entity @e[type=mannequin,limit=1] {pose:"swimming"}`)
	c.Settle()
	if selects(c, srv, above, "high2") {
		t.Error("a swimming mannequin, 0.6 tall, is found by a volume above it")
	}
	if !selects(c, srv, "@e[type=mannequin,x=1,y=64,z=1,dx=0,dy=0,dz=0]", "low") {
		t.Error("a swimming mannequin is not found by a volume at its body")
	}
}

// summonPunchable summons a mannequin a block in front of the player and
// waits until the player's swing is whole again; it returns the mannequin's
// ID.
func summonPunchable(t *testing.T, c *servertest.Client, srv *server.Server, name string) int32 {
	t.Helper()
	c.ChatCommand("summon minecraft:mannequin ~ ~ ~1")
	id := awaitMannequin(t, c, srv).id
	servertest.AwaitFullSwing(t, srv, name)
	return id
}

// mannequinHurtSeen is what a watcher was told of a hurt mannequin.
type mannequinHurtSeen struct {
	damageEvent bool
	causeID     int32 // the damage event's cause: entity ID + 1, 0 for none
	sounds      []string
	pose        int32
	poseSent    bool
	health      float32
	healthSent  bool
	statuses    []uint8
	removed     bool
}

// readMannequinHurt reads n ticks of what the client is told of the
// mannequin, stopping once stop holds.
func readMannequinHurt(c *servertest.Client, srv *server.Server, id int32, n int, stop func(*mannequinHurtSeen) bool) *mannequinHurtSeen {
	c.T.Helper()
	seen := &mannequinHurtSeen{}
	c.ReadTicks(srv, n, func(packet int32, data []byte) bool {
		r := protocol.NewReader(data)
		switch packet {
		case protocol.IDPlayDamageEvent:
			if r.ReadVarInt() == id {
				r.ReadVarInt() // damage type
				seen.damageEvent, seen.causeID = true, r.ReadVarInt()
			}
		case protocol.IDPlaySoundEffect:
			sound, _, _, _, _, _, _ := servertest.ReadAmbient(c.T, data)
			seen.sounds = append(seen.sounds, sound)
		case protocol.IDPlayEntityMetadata:
			if r.ReadVarInt() != id {
				break
			}
			meta, ok := servertest.ReadMetadata(r)
			if !ok {
				c.T.Fatal("the mannequin's metadata carries a field this reader does not know")
			}
			if e := servertest.MetaOf(meta, protocol.MetadataIndexLivingHealth); e != nil {
				seen.health, seen.healthSent = e.FloatVal, true
			}
			if e := servertest.MetaOf(meta, protocol.MetadataIndexPose); e != nil {
				seen.pose, seen.poseSent = e.VarintVal, true
			}
		case protocol.IDPlayEntityEvent:
			if r.ReadI32() == id {
				seen.statuses = append(seen.statuses, r.ReadU8())
			}
		case protocol.IDPlayRemoveEntities:
			for range r.ReadVarInt() {
				seen.removed = seen.removed || r.ReadVarInt() == id
			}
		}
		return stop(seen)
	})
	return seen
}

// TestAPunchedMannequinIsHurt checks that a full-strength bare-handed punch
// lands on a mannequin: the damage event caused by the player, the generic
// hurt sound and its health down to 19.
// vanilla: Player.attack, LivingEntity.hurtServer
func TestAPunchedMannequinIsHurt(t *testing.T) {
	t.Parallel()
	srv, c := joinMannequinOp(t, "Puncher")
	id := summonPunchable(t, c, srv, "Puncher")

	c.Attack(id)
	seen := readMannequinHurt(c, srv, id, 5, func(s *mannequinHurtSeen) bool {
		return s.damageEvent && s.healthSent && slices.Contains(s.sounds, "minecraft:entity.generic.hurt")
	})
	if !seen.damageEvent {
		t.Error("no damage event for the punched mannequin")
	} else if seen.causeID != c.PlayerEntityID+1 {
		t.Errorf("damage event cause = %d, want the player's ID + 1 = %d", seen.causeID, c.PlayerEntityID+1)
	}
	if !slices.Contains(seen.sounds, "minecraft:entity.generic.hurt") {
		t.Errorf("sounds = %v, want entity.generic.hurt among them", seen.sounds)
	}
	if !seen.healthSent || seen.health != 19 {
		t.Errorf("health sent=%v value=%v, want 19", seen.healthSent, seen.health)
	}
}

// TestAMannequinDamagedToDeathIsRemoved checks that /damage for its whole
// health kills a mannequin: the death sound, the dying pose and entity
// event 3, then, after its 20 ticks of dying, the poof and its removal.
// vanilla: LivingEntity.hurtServer, die, tickDeath
func TestAMannequinDamagedToDeathIsRemoved(t *testing.T) {
	t.Parallel()
	srv, c := joinMannequinOp(t, "Killer")
	c.ChatCommand("summon minecraft:mannequin ~ ~ ~1")
	id := awaitMannequin(t, c, srv).id

	c.ChatCommand("damage @e[type=mannequin,limit=1] 20")
	seen := readMannequinHurt(c, srv, id, 3, func(s *mannequinHurtSeen) bool {
		return slices.Contains(s.statuses, protocol.EntityStatusDeath)
	})
	if !slices.Contains(seen.statuses, protocol.EntityStatusDeath) {
		t.Fatalf("entity events = %v, want the death %d", seen.statuses, protocol.EntityStatusDeath)
	}
	if !seen.damageEvent || !slices.Contains(seen.sounds, "minecraft:entity.generic.death") {
		t.Errorf("damage event=%v sounds=%v, want the event and entity.generic.death", seen.damageEvent, seen.sounds)
	}
	if !seen.healthSent || seen.health != 0 {
		t.Errorf("health sent=%v value=%v, want 0", seen.healthSent, seen.health)
	}
	if !seen.poseSent || seen.pose != protocol.PoseDying {
		t.Errorf("pose sent=%v value=%v, want dying %d", seen.poseSent, seen.pose, protocol.PoseDying)
	}

	gone := readMannequinHurt(c, srv, id, 21, func(s *mannequinHurtSeen) bool { return s.removed })
	if !gone.removed {
		t.Fatal("the dead mannequin was not removed within 21 ticks")
	}
	if !slices.Contains(gone.statuses, protocol.EntityStatusDeathPoof) {
		t.Errorf("entity events = %v, want the poof %d before the removal", gone.statuses, protocol.EntityStatusDeathPoof)
	}
	if selects(c, srv, "@e[type=mannequin]", "gone") {
		t.Error("a selector still finds the removed mannequin")
	}
}

// TestASecondPunchInsideTheWindowLandsNothing checks that a weaker blow
// inside the first's ten ticks of invulnerability lands nothing.
// vanilla: LivingEntity.hurtServer
func TestASecondPunchInsideTheWindowLandsNothing(t *testing.T) {
	t.Parallel()
	srv, c := joinMannequinOp(t, "Twice")
	id := summonPunchable(t, c, srv, "Twice")

	c.Attack(id)
	first := readMannequinHurt(c, srv, id, 3, func(s *mannequinHurtSeen) bool {
		return s.healthSent && s.damageEvent && slices.Contains(s.sounds, "minecraft:entity.generic.hurt")
	})
	if !first.healthSent || first.health != 19 {
		t.Fatalf("first punch: health sent=%v value=%v, want 19", first.healthSent, first.health)
	}

	c.Attack(id)
	second := readMannequinHurt(c, srv, id, 2, func(*mannequinHurtSeen) bool { return false })
	if second.damageEvent || second.healthSent {
		t.Errorf("second punch: damage event=%v health sent=%v (%v), want neither", second.damageEvent, second.healthSent, second.health)
	}
	if got := dataOfMannequin(t, c, srv, "Health"); got != "19.0f" {
		t.Errorf("Health = %s, want 19.0f", got)
	}
}

// TestAnInvulnerableMannequinRefusesAllButBypassingDamage checks that the
// Invulnerable tag is kept and turns away a survival punch and plain
// /damage, while out_of_world, which bypasses invulnerability, still lands.
// vanilla: Entity.isInvulnerableToBase
func TestAnInvulnerableMannequinRefusesAllButBypassingDamage(t *testing.T) {
	t.Parallel()
	srv, c := joinMannequinOp(t, "Statue")
	c.ChatCommand("summon minecraft:mannequin ~ ~ ~1 {Invulnerable:1b}")
	id := awaitMannequin(t, c, srv).id
	servertest.AwaitFullSwing(t, srv, "Statue")
	if got := dataOfMannequin(t, c, srv, "Invulnerable"); got != "1b" {
		t.Errorf("Invulnerable = %s, want 1b", got)
	}

	c.Attack(id)
	punched := readMannequinHurt(c, srv, id, 3, func(*mannequinHurtSeen) bool { return false })
	if punched.damageEvent || punched.healthSent {
		t.Errorf("punch: damage event=%v health sent=%v, want neither", punched.damageEvent, punched.healthSent)
	}
	c.ChatCommand("damage @e[type=mannequin,limit=1] 5")
	if got := dataOfMannequin(t, c, srv, "Health"); got != "20.0f" {
		t.Errorf("Health after a punch and /damage = %s, want 20.0f", got)
	}

	c.ChatCommand("damage @e[type=mannequin,limit=1] 5 minecraft:out_of_world")
	if got := dataOfMannequin(t, c, srv, "Health"); got != "15.0f" {
		t.Errorf("Health after out_of_world /damage = %s, want 15.0f", got)
	}
}

// TestAMannequinSummonedAtZeroHealthIsDying checks that a stored Health of 0
// loads as a dying body that lands no hurt, while a missing Health is 20.
// vanilla: LivingEntity.readAdditionalSaveData, setHealth
func TestAMannequinSummonedAtZeroHealthIsDying(t *testing.T) {
	t.Parallel()
	srv, c := joinMannequinOp(t, "Zero")
	c.ChatCommand("summon minecraft:mannequin ~ ~ ~1 {Health:0f}")
	id := awaitMannequin(t, c, srv).id

	if selects(c, srv, "@e[type=mannequin]", "alive") {
		t.Error("a selector finds the mannequin summoned at zero health")
	}
	gone := readMannequinHurt(c, srv, id, 25, func(s *mannequinHurtSeen) bool { return s.removed })
	if !gone.removed {
		t.Fatal("the dying mannequin was not removed")
	}

	c.ChatCommand("summon minecraft:mannequin ~ ~ ~1")
	awaitMannequin(t, c, srv)
	if !selects(c, srv, "@e[type=mannequin]", "full") {
		t.Error("a mannequin without Health is not alive")
	}
}
