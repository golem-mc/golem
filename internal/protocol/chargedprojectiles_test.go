package protocol

import "testing"

func TestChargedProjectilesSurviveTheWire(t *testing.T) {
	t.Parallel()
	crossbow := ItemStack{ItemID: 1, Count: 1}
	crossbow.SetChargedProjectiles([]ItemStack{{ItemID: 2, Count: 1}, {ItemID: 2, Count: 1}})
	if got := len(crossbow.ChargedProjectiles()); got != 2 {
		t.Fatalf("a loaded stack holds %d projectiles, want 2", got)
	}
	var b Buffer
	b.PutUntrustedSlot(crossbow)
	back, ok := NewReader(b.Bytes()).ReadUntrustedSlot()
	if !ok {
		t.Fatal("a loaded crossbow was refused from a creative slot")
	}
	if !back.Equal(crossbow) {
		t.Errorf("the stack came back as %+v, want %+v", back, crossbow)
	}
	crossbow.SetChargedProjectiles(nil)
	if len(crossbow.ChargedProjectiles()) != 0 || len(crossbow.Raw) != 0 {
		t.Error("unloading left the component on the stack")
	}
}
