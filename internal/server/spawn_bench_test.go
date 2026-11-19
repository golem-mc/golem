package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
	"github.com/golem-mc/golem/registry"
	"github.com/google/uuid"
	"testing"
)

type spawnPacketCase struct {
	name   string
	encode func() []byte
}

func spawnPacketCases(vx, vy, vz float64) []spawnPacketCase {
	id := uuid.MustParse("00112233-4455-6677-8899-aabbccddeeff")
	return []spawnPacketCase{
		{name: "orb", encode: func() []byte {
			return orbSpawnPayload(&orbEntity{uuid: id, orb: &entity.Orb{ID: 300, X: -12.25, Y: 64.5, Z: 300000.125, VX: vx, VY: vy, VZ: vz}})
		}},
		{name: "tnt", encode: func() []byte {
			return tntSpawnPayload(&tntEntity{uuid: id, tnt: &entity.TNT{ID: 300, X: -12.25, Y: 64.5, Z: 300000.125, VX: vx, VY: vy, VZ: vz}})
		}},
		{name: "falling", encode: func() []byte {
			return fallingSpawnPayload(&fallingEntity{uuid: id, fb: &entity.FallingBlock{ID: 300, X: -12.25, Y: 64.5, Z: 300000.125, VX: vx, VY: vy, VZ: vz, BlockState: 1234}})
		}},
		{name: "arrow", encode: func() []byte {
			return arrowSpawnPayload(&arrowEntity{uuid: id, entityType: protocol.EntityTypeArrow, arrow: &entity.Arrow{ID: 300, X: -12.25, Y: 64.5, Z: 300000.125, VX: vx, VY: vy, VZ: vz, Yaw: -47.25, Pitch: 33.75, OwnerID: 513}})
		}},
		{name: "firework", encode: func() []byte {
			return fireworkSpawnPayload(&fireworkEntity{uuid: id, rocket: &entity.Firework{ID: 300, X: -12.25, Y: 64.5, Z: 300000.125, VX: vx, VY: vy, VZ: vz, Yaw: -47.25, Pitch: 33.75, OwnerID: 513}})
		}},
		{name: "bobber", encode: func() []byte {
			return bobberSpawnPayload(&bobberEntity{uuid: id, hook: &entity.Bobber{ID: 300, X: -12.25, Y: 64.5, Z: 300000.125, VX: vx, VY: vy, VZ: vz, Yaw: -47.25, Pitch: 33.75, OwnerID: 513}})
		}},
		{name: "boat", encode: func() []byte {
			return boatSpawnPayload(&boatEntity{uuid: id, kind: registry.EntityTypeOakBoat, boat: &entity.Boat{ID: 300, X: -12.25, Y: 64.5, Z: 300000.125, VX: vx, VY: vy, VZ: vz, Yaw: -47.25}})
		}},
		{name: "minecart", encode: func() []byte {
			return minecartSpawnPayload(&minecartEntity{uuid: id, kind: registry.EntityTypeChestMinecart, cart: &entity.Minecart{ID: 300, X: -12.25, Y: 64.5, Z: 300000.125, VX: vx, VY: vy, VZ: vz, Yaw: -47.25}})
		}},
		{name: "item", encode: func() []byte {
			return spawnItemPayload(&itemEntity{uuid: id, item: &entity.Item{ID: 300, X: -12.25, Y: 64.5, Z: 300000.125, VX: vx, VY: vy, VZ: vz}})
		}},
		{name: "armorstand", encode: func() []byte {
			return spawnArmorStandPayload(&armorStandEntity{uuid: id, stand: &entity.ArmorStand{ID: 300, X: -12.25, Y: 64.5, Z: 300000.125, VX: vx, VY: vy, VZ: vz, Yaw: -47.25}})
		}},
		{name: "endcrystal", encode: func() []byte {
			return spawnEndCrystalPayload(&endCrystalEntity{uuid: id, crystal: &entity.EndCrystal{ID: 300, X: -12.25, Y: 64.5, Z: 300000.125}})
		}},
		{name: "cloud", encode: func() []byte {
			return cloudSpawnPayload(&cloudEntity{uuid: id, cloud: &entity.Cloud{ID: 300, X: -12.25, Y: 64.5, Z: 300000.125}})
		}},
		{name: "bolt", encode: func() []byte {
			return boltSpawnPayload(&boltEntity{uuid: id, bolt: &entity.Bolt{ID: 300, X: -12.25, Y: 64.5, Z: 300000.125}})
		}},
		{name: "fangs", encode: func() []byte {
			return fangsSpawnPayload(&fangsEntity{uuid: id, fangs: &entity.EvokerFangs{ID: 300, X: -12.25, Y: 64.5, Z: 300000.125, Yaw: -47.25}})
		}},
		{name: "mob", encode: func() []byte {
			return spawnMobPayload(&mobEntity{uuid: id, mob: &entity.Mob{ID: 300, X: -12.25, Y: 64.5, Z: 300000.125, VX: vx, VY: vy, VZ: vz, Type: entity.TypePig, Yaw: -47.25, Pitch: 33.75, HeadYaw: 91.5}})
		}},
		{name: "thrown", encode: func() []byte {
			return thrownSpawnPayload(&thrownEntity{uuid: id, throwable: &entity.Thrown{ID: 300, X: -12.25, Y: 64.5, Z: 300000.125, VX: vx, VY: vy, VZ: vz, Kind: entity.ThrownSnowball, Yaw: -47.25, Pitch: 33.75, OwnerID: 513}})
		}},
	}
}

// spawnPacketSink keeps the encoded payload alive across benchmark iterations.
var spawnPacketSink []byte

func BenchmarkSpawnPackets(b *testing.B) {
	for _, motion := range []struct {
		name       string
		vx, vy, vz float64
	}{
		{"stationary", 0, 0, 0}, {"moving", -8.25, 0.125, 1.5},
	} {
		for _, tc := range spawnPacketCases(motion.vx, motion.vy, motion.vz) {
			b.Run(motion.name+"/"+tc.name, func(b *testing.B) {
				b.ReportAllocs()
				for b.Loop() {
					spawnPacketSink = tc.encode()
				}
			})
		}
	}
}
