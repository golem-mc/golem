package registry

import (
	"github.com/golem-mc/golem/internal/protocol"
)

func stackWithMaxDamage(item Item, damage, maxDamage int32) protocol.ItemStack {
	var b protocol.Buffer
	b.PutVarInt(maxDamage)
	s := protocol.ItemStack{ItemID: int32(item), Count: 1, Damage: damage}
	s.SetRaw(protocol.RawComponent{Type: protocol.DataComponentMaxDamage, Data: b.Bytes()})
	return s
}
