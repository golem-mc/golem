package registry

import "github.com/golem-mc/golem/internal/protocol"

// Aliases let importers outside the module's internal tree name the protocol
// types registry's API exposes.
type (
	ItemStack          = protocol.ItemStack
	SoundHolder        = protocol.SoundHolder
	BannerPatternLayer = protocol.BannerPatternLayer
	Text               = protocol.Text
	FilterablePage     = protocol.FilterablePage
	WrittenPage        = protocol.WrittenPage
	SignText           = protocol.SignText
	RawComponent       = protocol.RawComponent
	PotionContents     = protocol.PotionContents
	FireworkExplosion  = protocol.FireworkExplosion
	Bee                = protocol.Bee
)
