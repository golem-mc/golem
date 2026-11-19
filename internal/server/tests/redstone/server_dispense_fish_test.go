package redstone

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
	"github.com/golem-mc/golem/internal/server/world"
	"github.com/golem-mc/golem/nbt"
)

// fishBucket is a filled fish bucket carrying the data a scooped fish saves.
func fishBucket(t *testing.T, item string, tag nbt.Compound, name string) protocol.ItemStack {
	t.Helper()
	bucket := servertest.ItemStack(t, item, 1)
	bucket.SetBucketEntityData(tag)
	if name != "" {
		bucket.CustomName = &protocol.Text{Text: name}
	}
	return bucket
}

// TestADispenserPoursItsFishBucket: a mob bucket pours its water
// into the cell the face opens onto, lets the fish out marked as from a
// bucket with the bucket's health and name, and leaves an empty bucket.
// vanilla: DispenseItemBehavior (filledBucketBehavior), MobBucketItem.checkExtraContent
func TestADispenserPoursItsFishBucket(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ bucket, kind string }{
		{"minecraft:cod_bucket", "minecraft:cod"},
		{"minecraft:salmon_bucket", "minecraft:salmon"},
		{"minecraft:axolotl_bucket", "minecraft:axolotl"},
		{"minecraft:tadpole_bucket", "minecraft:tadpole"},
		{"minecraft:pufferfish_bucket", "minecraft:pufferfish"},
		{"minecraft:tropical_fish_bucket", "minecraft:tropical_fish"},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			t.Parallel()
			srv, _ := servertest.Dispensing(t)
			tag := nbt.Compound{"Health": nbt.Float(2)}
			bucket := fishBucket(t, tc.bucket, tag, "Finny")
			const variant = 0x04000500
			if tc.kind == "minecraft:tropical_fish" {
				bucket.SetTropicalFishVariant(variant)
			}
			if !srv.SetInventoryAt(4, 64, 0, 0, bucket) {
				t.Fatal("the dispenser has no block entity to fill")
			}
			srv.Dispense(4, 64, 0, protocol.FaceSouth)

			if name, props := servertest.StatePropsOf(t, srv, 4, 64, 1); name != "minecraft:water" || props["level"] != "0" {
				t.Errorf("the pour left %s %v in front of the dispenser, want a water source", name, props)
			}
			var fish []server.TestMob
			for _, m := range srv.Mobs() {
				if m.Kind == tc.kind {
					fish = append(fish, m)
				}
			}
			if len(fish) != 1 {
				t.Fatalf("the bucket let out %d %s, want one", len(fish), tc.kind)
			}
			f := fish[0]
			if f.X != 4.5 || f.Y != 64 || f.Z != 1.5 {
				t.Errorf("the fish stands at (%v, %v, %v), want the poured cell (4.5, 64, 1.5)", f.X, f.Y, f.Z)
			}
			if f.Name != "Finny" {
				t.Errorf("the fish is named %q, want the bucket's Finny", f.Name)
			}
			if st, _ := srv.MobFishState(f.ID); !st.FromBucket {
				t.Error("the released fish is not marked as from a bucket")
			}
			if tc.kind == "minecraft:tropical_fish" {
				if st, _ := srv.MobFishState(f.ID); st.TropicalVariant != variant {
					t.Errorf("the released fish has variant %#x, want the bucket's %#x", st.TropicalVariant, variant)
				}
			}
			if hp, _ := srv.MobHealth(f.ID); hp != 2 {
				t.Errorf("the released fish has %.1f health, want the bucket's 2", hp)
			}
			if got := srv.InventoryAt(4, 64, 0); servertest.NothingIn(got) || got[0].ItemID != servertest.ItemID(t, "minecraft:bucket") {
				t.Errorf("the dispenser's slot = %+v, want the empty bucket", got)
			}
		})
	}
}

// TestADispenserDropsAFishBucketItCannotPour: facing a block no water can
// replace, the bucket is dropped whole and no fish gets out.
// vanilla: DispenseItemBehavior (filledBucketBehavior)
func TestADispenserDropsAFishBucketItCannotPour(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Dispensing(t)
	srv.SetBlock(4, 64, 1, servertest.MustState(t, "minecraft:stone", nil))
	if !srv.SetInventoryAt(4, 64, 0, 0, fishBucket(t, "minecraft:cod_bucket", nbt.Compound{}, "")) {
		t.Fatal("the dispenser has no block entity to fill")
	}
	srv.Dispense(4, 64, 0, protocol.FaceSouth)
	for _, m := range srv.Mobs() {
		if m.Kind == "minecraft:cod" {
			t.Fatal("a cod got out of a bucket poured into stone")
		}
	}
	items := srv.ItemsInDimension(world.DimensionName)
	if len(items) != 1 || items[0].Item != "minecraft:cod_bucket" {
		t.Errorf("the dispenser dropped %+v, want the one cod bucket", items)
	}
}

func TestDispenserReleasesSulfurCubeWithoutReplacingBlock(t *testing.T) {
	t.Parallel()
	srv, _ := servertest.Dispensing(t)
	stone := servertest.MustState(t, "minecraft:stone", nil)
	srv.SetBlock(4, 64, 1, stone)
	bucket := fishBucket(t, "minecraft:sulfur_cube_bucket", nbt.Compound{"age": nbt.Int(-1200), "age_locked": nbt.Byte(1), "Health": nbt.Float(3)}, "Sulfur")
	if !srv.SetInventoryAt(4, 64, 0, 0, bucket) {
		t.Fatal("missing dispenser")
	}
	srv.Dispense(4, 64, 0, protocol.FaceSouth)
	if srv.BlockAt(4, 64, 1) != stone {
		t.Fatal("bucket replaced solid block")
	}
	var cubes []server.TestMob
	for _, m := range srv.Mobs() {
		if m.Kind == "minecraft:sulfur_cube" {
			cubes = append(cubes, m)
		}
	}
	if len(cubes) != 1 || cubes[0].Name != "Sulfur" {
		t.Fatalf("released cubes = %+v", cubes)
	}
	if hp, _ := srv.MobHealth(cubes[0].ID); hp != 3 {
		t.Fatalf("health = %g", hp)
	}
	if got := srv.InventoryAt(4, 64, 0); len(got) == 0 || got[0].ItemID != servertest.ItemID(t, "minecraft:bucket") {
		t.Fatalf("dispenser contents = %+v", got)
	}
}
