package server

import (
	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/internal/server/entity"
)

// putCreakingMetadata writes the creaking's changed synched entries; the
// spawn passes the defaults (can move, asleep, no home).
// vanilla: Creaking.defineSynchedData
func putCreakingMetadata(b *protocol.Buffer, was, now entity.CreakingView) {
	if now.Frozen != was.Frozen {
		b.PutMetadata(protocol.MetadataIndexCreakingCanMove, protocol.MetadataBoolean)
		b.PutBool(!now.Frozen)
	}
	if now.Active != was.Active {
		b.PutMetadata(protocol.MetadataIndexCreakingActive, protocol.MetadataBoolean)
		b.PutBool(now.Active)
	}
	if now.TearingDown != was.TearingDown {
		b.PutMetadata(protocol.MetadataIndexCreakingTearingDown, protocol.MetadataBoolean)
		b.PutBool(now.TearingDown)
	}
	if now.Bound != was.Bound || now.Home != was.Home {
		b.PutMetadata(protocol.MetadataIndexCreakingHome, protocol.MetadataOptionalPosition)
		b.PutBool(now.Bound)
		if now.Bound {
			b.PutPosition(now.Home[0], now.Home[1], now.Home[2])
		}
	}
}

// creakingTearingDownPayload is the metadata that sets the flag the twitch
// plays on. The dying body has left the herd the tracker walks, so the flag
// is told here.
// vanilla: Creaking.setTearingDown
func creakingTearingDownPayload(id int32) []byte {
	var b protocol.Buffer
	b.PutVarInt(id)
	putCreakingMetadata(&b, entity.CreakingView{}, entity.CreakingView{TearingDown: true})
	b.EndMetadata()
	return b.Bytes()
}
