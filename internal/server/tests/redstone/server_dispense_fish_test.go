package redstone

import (
	"testing"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server"
	"github.com/golem-mc/golem/internal/server/servertest"
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
