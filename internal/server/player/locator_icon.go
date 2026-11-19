package player

import (
	"math"

	"github.com/golem-mc/golem/nbt"
)

// DefaultWaypointStyle is the style of an icon nobody has changed.
// vanilla: WaypointStyleAssets.DEFAULT
const DefaultWaypointStyle = "minecraft:default"

// LocatorIcon is a player's waypoint icon on others' locator bars. Color
// tints it when HasColor; only its low 24 bits are sent, but it is kept as
// the codec's int so a saved file round-trips.
// vanilla: Waypoint.Icon
type LocatorIcon struct {
	Style    string
	Color    int32
	HasColor bool
}

// HasData reports whether the icon differs from the default, the only case
// saved.
// vanilla: Waypoint.Icon.hasData
func (i LocatorIcon) HasData() bool {
	return i.Style != DefaultWaypointStyle && i.Style != "" || i.HasColor
}

// StyleName is the style, the default where none is set.
func (i LocatorIcon) StyleName() string {
	if i.Style == "" {
		return DefaultWaypointStyle
	}
	return i.Style
}

// Tag is the icon as the locator_bar_icon compound.
// vanilla: Waypoint.Icon.CODEC
func (i LocatorIcon) Tag() nbt.Compound {
	tag := nbt.Compound{"style": nbt.String(i.StyleName())}
	if i.HasColor {
		tag["color"] = nbt.Int(i.Color)
	}
	return tag
}

// LocatorIconFromTag reads locator_bar_icon; an unreadable style stays
// default. The colour is an int or three floats in 0..1.
// vanilla: Waypoint.Icon.CODEC, ExtraCodecs.RGB_COLOR_CODEC
func LocatorIconFromTag(tag nbt.Compound) LocatorIcon {
	icon := LocatorIcon{Style: DefaultWaypointStyle}
	if style, ok := nbt.Get[nbt.String](tag, "style"); ok && style != "" {
		icon.Style = string(style)
	}
	switch color := tag["color"].(type) {
	case nbt.Int:
		icon.Color, icon.HasColor = int32(color), true
	case nbt.List:
		if len(color.Items) != 3 {
			break
		}
		rgb := int32(-1 << 24) // opaque alpha
		for n, item := range color.Items {
			f, ok := item.(nbt.Float)
			if !ok {
				return icon
			}
			// vanilla: ARGB.as8BitChannel
			rgb |= (int32(math.Floor(float64(f*255))) & 0xFF) << (16 - 8*n)
		}
		icon.Color, icon.HasColor = rgb, true
	}
	return icon
}

// LocatorIcon is the icon the player's waypoint is drawn with.
func (p *Player) LocatorIcon() LocatorIcon {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return LocatorIcon{Style: p.locatorIcon.StyleName(), Color: p.locatorIcon.Color, HasColor: p.locatorIcon.HasColor}
}

// SetLocatorIcon replaces the icon.
func (p *Player) SetLocatorIcon(icon LocatorIcon) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.locatorIcon = icon
}
