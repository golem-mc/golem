package world

import (
	"math"

	"github.com/golem-mc/golem/registry"
)

// The MapColor palette ids the drawing code names itself. The rest come from
// registry.BlockMapColor. vanilla: MapColor.MATERIAL_COLORS
const (
	mapColorNone  byte = 0
	mapColorDirt  byte = 10
	mapColorStone byte = 11
	mapColorWater byte = 12
)

// The MapColor.Brightness ids a packed map colour carries in its low two
// bits. vanilla: MapColor.Brightness
const (
	mapBrightnessLow    = 0
	mapBrightnessNormal = 1
	mapBrightnessHigh   = 2
)

// Draw paints the map from the world around the holder, what an empty map shows
// on first use. The whole face is sampled at once: vanilla's one-column-in-16
// sweep while held only spreads the work over ticks, and the first draw has no
// earlier packet to spread against. Unloaded chunks are left blank rather than
// forced in.
// vanilla: MapItem.update
func (m *Maps) Draw(id int32, w *World, holderX, holderZ float64) {
	data, ok := m.Get(id)
	if !ok || data.Dimension != w.Name() {
		return
	}
	var colors [MapSide * MapSide]byte
	renderMapFace(w, data, holderX, holderZ,
		func(int) bool { return true },
		func(imgX, imgY int, color byte) bool { colors[imgX+imgY*MapSide] = color; return true })
	m.mu.Lock()
	data.Colors = colors
	m.dirty[id] = true
	m.mu.Unlock()
}

// MapPatch is the rectangle of pixels a redraw changed: top-left pixel, width
// and height.
type MapPatch struct{ X, Z, Width, Height uint8 }

// Update redraws the one-column-in-16 stripe step selects, a held map's
// per-tick sweep, and reports the changed rectangle. An absent, locked or
// other-dimension map redraws nothing (inventoryTick's guard). step counts the
// map's own ticks, so the stripe walks the face while carried.
// vanilla: MapItem.update, MapItemSavedData.HoldingPlayer
func (m *Maps) Update(id int32, w *World, holderX, holderZ float64, step uint32) (MapPatch, bool) {
	data, ok := m.Get(id)
	if !ok || data.Locked || data.Dimension != w.Name() {
		return MapPatch{}, false
	}
	minX, minY, maxX, maxY := MapSide, MapSide, -1, -1
	column := int(step) & 0xF
	m.mu.Lock()
	defer m.mu.Unlock()
	renderMapFace(w, data, holderX, holderZ,
		func(imgX int) bool { return imgX&0xF == column },
		func(imgX, imgY int, color byte) bool {
			off := imgX + imgY*MapSide
			if data.Colors[off] == color {
				return false
			}
			data.Colors[off] = color
			minX, minY = min(minX, imgX), min(minY, imgY)
			maxX, maxY = max(maxX, imgX), max(maxY, imgY)
			return true
		})
	if maxX < 0 {
		return MapPatch{}, false
	}
	m.dirty[id] = true
	return MapPatch{X: uint8(minX), Z: uint8(minY), Width: uint8(maxX - minX + 1), Height: uint8(maxY - minY + 1)}, true
}

// renderMapFace samples the face for both the first draw and the stripe sweep.
// due picks the columns this pass reads; the column after a changed one is
// always read, so a moved surface leaves no seam. put stores a pixel and
// reports whether it changed. Each pixel is the dominant MapColor over its
// scale x scale cell, shaded by its height against the cell above, water by
// depth.
// vanilla: MapItem.update
func renderMapFace(w *World, data *MapData, holderX, holderZ float64, due func(imgX int) bool, put func(imgX, imgY int, color byte) bool) {
	scale := 1 << uint(data.Scale)
	centerX, centerZ := int(data.CenterX), int(data.CenterZ)
	playerImgX := int(math.Floor(holderX-float64(centerX)))/scale + 64
	playerImgY := int(math.Floor(holderZ-float64(centerZ)))/scale + 64
	radius := MapSide / scale
	ceiling := w.HasCeiling()
	if ceiling {
		radius /= 2
	}
	minY := w.MinY()

	foundConsecutiveChanges := false
	for imgX := playerImgX - radius + 1; imgX < playerImgX+radius; imgX++ {
		if !due(imgX) && !foundConsecutiveChanges {
			continue
		}
		foundConsecutiveChanges = false
		previousAverageAreaHeight := 0.0
		for imgY := playerImgY - radius - 1; imgY < playerImgY+radius; imgY++ {
			if imgX < 0 || imgY < -1 || imgX >= MapSide || imgY >= MapSide {
				continue
			}
			distanceToPlayerSqr := (imgX-playerImgX)*(imgX-playerImgX) + (imgY-playerImgY)*(imgY-playerImgY)
			ditherBlack := distanceToPlayerSqr > (radius-2)*(radius-2)
			areaX := (centerX/scale + imgX - 64) * scale
			areaZ := (centerZ/scale + imgY - 64) * scale
			if !w.ChunkLoadedAt(int32(areaX), int32(areaZ)) {
				continue
			}

			var (
				counts            [64]int // vanilla: MapColor.MATERIAL_COLORS
				order             [64]byte
				orderLen          int
				averageAreaHeight float64
				waterDepth        int
			)
			add := func(color byte, count int) {
				if counts[color] == 0 {
					order[orderLen] = color
					orderLen++
				}
				counts[color] += count
			}

			if ceiling {
				// vanilla: MapItem.update's hasCeiling branch
				noise := int32(areaX) + int32(areaZ)*231871
				noise = noise*noise*31287121 + noise*11
				if (noise>>20)&1 == 0 {
					add(mapColorDirt, 10)
				} else {
					add(mapColorStone, 100)
				}
				averageAreaHeight = 100
			} else {
				for dx := range scale {
					for dz := range scale {
						x, z := int32(areaX+dx), int32(areaZ+dz)
						columnY := w.SurfaceHeight(x, z) + 1
						var state uint16
						if columnY > minY {
							for {
								columnY--
								state = w.BlockAt(x, columnY, z)
								if registry.BlockMapColor(state) != mapColorNone || columnY <= minY {
									break
								}
							}
							if columnY > minY && fluidHolding(state).kind != fluidNone {
								solidY := columnY - 1
								for {
									waterDepth++
									below := w.BlockAt(x, solidY, z)
									solidY--
									if solidY <= minY || fluidHolding(below).kind == fluidNone {
										break
									}
								}
								state = correctFluidState(state)
							}
						} else {
							state = registry.BlockBedrock.DefaultState()
						}
						averageAreaHeight += float64(columnY) / float64(scale*scale)
						add(registry.BlockMapColor(state), 1)
					}
				}
			}

			color, best := mapColorNone, 0
			for _, candidate := range order[:orderLen] {
				if best == 0 || counts[candidate] > best {
					color, best = candidate, counts[candidate]
				}
			}
			brightness := mapBrightnessNormal
			if color == mapColorWater {
				waterDepth /= scale * scale
				diff := float64(waterDepth)*0.1 + float64((imgX+imgY)&1)*0.2
				if diff < 0.5 {
					brightness = mapBrightnessHigh
				} else if diff > 0.9 {
					brightness = mapBrightnessLow
				}
			} else {
				diff := (averageAreaHeight-previousAverageAreaHeight)*4/float64(scale+4) + (float64((imgX+imgY)&1)-0.5)*0.4
				if diff > 0.6 {
					brightness = mapBrightnessHigh
				} else if diff < -0.6 {
					brightness = mapBrightnessLow
				}
			}
			previousAverageAreaHeight = averageAreaHeight
			if imgY < 0 || distanceToPlayerSqr >= radius*radius || ditherBlack && (imgX+imgY)&1 == 0 {
				continue
			}
			if put(imgX, imgY, color<<2|byte(brightness)) {
				foundConsecutiveChanges = true
			}
		}
	}
}

// correctFluidState is the state whose colour a fluid block draws: the fluid's
// own block unless the state's top face is sturdy, so a waterlogged full block
// keeps its colour.
// vanilla: MapItem.getCorrectStateForFluidBlock
func correctFluidState(state uint16) uint16 {
	if fluidHolding(state).kind == fluidNone || registry.FaceSturdy(state, dirUp, registry.SupportFull) {
		return state
	}
	if registry.BlockOf(state) == registry.BlockLava {
		return registry.BlockLava.DefaultState()
	}
	return registry.BlockWater.DefaultState()
}
