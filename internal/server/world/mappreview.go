package world

import (
	"github.com/golem-mc/golem/internal/server/world/levelgen"
	"github.com/golem-mc/golem/registry"
)

// RenderBiomePreview paints an explorer map's outlines using the caller's biome reader.
// The reader samples blocks at Y=0; it need not generate terrain chunks.
// vanilla: MapItem.renderBiomePreviewMap
func (m *Maps) RenderBiomePreview(id int32, dimension string, biomeAt func(x, y, z int32) string) {
	data, ok := m.Get(id)
	if !ok || data.Dimension != dimension || biomeAt == nil {
		return
	}
	m.mu.Lock()
	snapshot := *data
	m.mu.Unlock()
	renderBiomePreview(&snapshot, biomeAt)
	m.mu.Lock()
	data.Colors = snapshot.Colors
	m.dirty[id] = true
	m.mu.Unlock()
}

// renderBiomePreview leaves borders and unpainted pixels as they were.
// vanilla: MapItem.renderBiomePreviewMap
func renderBiomePreview(data *MapData, biomeAt func(x, y, z int32) string) {
	scale := int32(1) << uint(data.Scale)
	startX, startZ := data.CenterX/scale-64, data.CenterZ/scale-64
	waterOnMapOutlines := registry.BiomeTagOf("minecraft:water_on_map_outlines")
	biome := func(name string) registry.Biome { b, _ := registry.BiomeByName(name); return b }
	var watery [MapSide * MapSide]bool
	for z := range MapSide {
		for x := range MapSide {
			watery[z*MapSide+x] = biome(biomeAt((startX+int32(x))*scale, 0, (startZ+int32(z))*scale)).InTag(waterOnMapOutlines)
		}
	}
	for x := 1; x < MapSide-1; x++ {
		for z := 1; z < MapSide-1; z++ {
			count := 0
			for dx := -1; dx <= 1; dx++ {
				for dz := -1; dz <= 1; dz++ {
					if (dx != 0 || dz != 0) && watery[(z+dz)*MapSide+x+dx] {
						count++
					}
				}
			}
			color, brightness := mapColorNone, byte(3) // vanilla: MapColor.Brightness.LOWEST
			if watery[z*MapSide+x] {
				color = 15 // vanilla: MapColor.COLOR_ORANGE
				switch {
				case count > 7 && z%2 == 0:
					wave := (x + int(levelgen.MthSin(float64(z))*7)) / 8 % 5
					switch wave {
					case 0, 4:
						brightness = mapBrightnessLow
					case 1, 3:
						brightness = mapBrightnessNormal
					case 2:
						brightness = mapBrightnessHigh
					}
				case count > 7:
					color = mapColorNone
				case count > 5:
					brightness = mapBrightnessNormal
				case count > 1:
					brightness = mapBrightnessLow
				}
			} else if count > 0 {
				color = 26 // vanilla: MapColor.COLOR_BROWN
				if count > 3 {
					brightness = mapBrightnessNormal
				}
			}
			if color != mapColorNone {
				data.Colors[z*MapSide+x] = color*4 + brightness
			}
		}
	}
}

// MapBiomeReader reads zoomed block biomes, probing unloaded terrain without generating it.
// Each reader belongs to one preview, since biome probes retain sampling state.
// vanilla: BiomeManager.getBiome
func (w *World) MapBiomeReader() func(x, y, z int32) string {
	sampler, ok := w.gen.(BiomeSampler)
	if !ok {
		return w.BiomeAt
	}
	probe := sampler.NewBiomeProbe()
	if isFlatGenerator(w.gen) {
		return func(x, y, z int32) string { return probe(x>>2, y>>2, z>>2) }
	}
	zoomSeed := w.timeWorld().zoomSeed
	return func(x, y, z int32) string {
		qx, qy, qz := levelgen.ZoomedQuart(zoomSeed, x, y, z)
		if loaded := w.NoiseBiome(qx*4, qy*4, qz*4).Name(); loaded != "" {
			return loaded
		}
		return probe(qx, qy, qz)
	}
}
