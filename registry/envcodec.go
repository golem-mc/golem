package registry

import (
	"errors"
	"fmt"
	"strings"

	"github.com/golem-mc/golem/internal/protocol"
	"github.com/golem-mc/golem/nbt"
)

// DecodeEnvTimeline resolves a holder and reads its numeric and discrete tracks.
// vanilla: Timeline.CODEC, Timeline.DIRECT_CODEC
func DecodeEnvTimeline(tag nbt.Tag) (EnvTimeline, error) {
	if id, ok := tag.(nbt.String); ok {
		if !protocol.ValidIdentifier(string(id)) {
			return EnvTimeline{}, errors.New("timeline is no identifier")
		}
		name := Qualify(string(id))
		r, _ := Get("minecraft:timeline")
		if data, ok := r.Data[name]; ok {
			tag = data
		} else if tl, ok := EnvTimelineByName(name); ok {
			return tl, nil
		} else {
			return EnvTimeline{}, errors.New("unknown timeline " + name)
		}
	}
	var tl EnvTimeline
	c, ok := nbt.CompoundView(tag)
	if !ok {
		return tl, errors.New("timeline is not a map")
	}
	clock, ok := c["clock"].(nbt.String)
	if !ok || !protocol.ValidIdentifier(string(clock)) {
		return tl, errors.New("timeline has no clock")
	}
	tl.Clock = Qualify(string(clock))
	if r, _ := Get("minecraft:world_clock"); r.ID(tl.Clock) < 0 {
		return tl, errors.New("unknown world clock " + tl.Clock)
	}
	if v, has := c["period_ticks"]; has {
		if tl.Period, ok = nbt.IntValue(v); !ok || tl.Period <= 0 {
			return tl, errors.New("timeline period_ticks must be positive")
		}
	}
	if _, has := c["tracks"]; !has {
		return tl, nil
	}
	tracks, ok := nbt.CompoundView(c["tracks"])
	if !ok {
		return tl, errors.New("timeline tracks is not a map")
	}
	tl.Tracks = make(map[string]EnvFloatTrack)
	for name, tag := range tracks {
		name = Qualify(name)
		attr, ok := EnvAttributeByName(name)
		if !ok {
			return tl, errors.New("unknown environment attribute " + name)
		}
		if !attr.Float && !attr.Discrete {
			continue
		}
		track, err := decodeEnvTrack(tag, tl.Period, attr)
		if err != nil {
			return tl, fmt.Errorf("%s: %w", name, err)
		}
		tl.Tracks[name] = track
	}
	return tl, nil
}

var envModifiers = map[string]EnvFloatOp{
	"override": EnvOverride, "alpha_blend": EnvAlphaBlend,
	"add": EnvAdd, "subtract": EnvSubtract, "multiply": EnvMultiply,
	"minimum": EnvMinimum, "maximum": EnvMaximum,
	"and": EnvAnd, "nand": EnvNand, "or": EnvOr, "nor": EnvNor, "xor": EnvXor, "xnor": EnvXnor,
}

// decodeEnvTrack reads the modifier, easing and ordered keyframes.
// vanilla: AttributeTrack.createCodec, KeyframeTrack.mapCodec, validatePeriod
func decodeEnvTrack(tag nbt.Tag, period int32, attr EnvAttribute) (EnvFloatTrack, error) {
	var track EnvFloatTrack
	c, ok := nbt.CompoundView(tag)
	if !ok {
		return track, errors.New("track is not a map")
	}
	var err error
	if track.Ease, err = decodeEnvEase(c["ease"]); err != nil {
		return track, err
	}
	if attr.Discrete {
		track.Ease = EnvEase{Kind: EnvEaseConstant}
	}
	frames, ok := c["keyframes"].(nbt.List)
	if !ok || len(frames.Items) == 0 {
		return track, errors.New("track has no keyframes")
	}
	repeats := 0
	for i, tag := range frames.Items {
		frame, ok := nbt.CompoundView(nbt.ListElement(tag))
		if !ok {
			return track, errors.New("keyframe is not a map")
		}
		ticks, ok := nbt.IntValue(frame["ticks"])
		if !ok || period > 0 && (ticks < 0 || ticks > period) {
			return track, errors.New("keyframe ticks is out of range")
		}
		if i > 0 && ticks < track.Keyframes[i-1].Ticks {
			return track, errors.New("keyframes must be ordered by ticks")
		}
		if i > 0 && ticks == track.Keyframes[i-1].Ticks {
			repeats++
		} else {
			repeats = 1
		}
		if repeats > 2 {
			return track, errors.New("more than two keyframes on the same tick")
		}
		entry, err := decodeEnvEntry(attr, c["modifier"], frame["value"])
		if err != nil {
			return track, err
		}
		track.Op = entry.Op
		track.Keyframes = append(track.Keyframes, EnvKeyframe{Ticks: ticks, Value: entry.Value, Alpha: entry.Alpha})
	}
	return track, nil
}

// decodeEnvEase reads the easing forms supported by EnvFloatTrack.
func decodeEnvEase(tag nbt.Tag) (EnvEase, error) {
	var ease EnvEase
	if tag == nil || tag == nbt.String("linear") {
		return ease, nil
	}
	if tag == nbt.String("constant") {
		ease.Kind = EnvEaseConstant
		return ease, nil
	}
	if c, ok := nbt.CompoundView(tag); ok {
		var controls []nbt.Tag
		switch v := c["cubic_bezier"].(type) {
		case nbt.List:
			controls = v.Items
		case nbt.IntArray:
			for _, n := range v {
				controls = append(controls, nbt.Int(n))
			}
		}
		if len(controls) == 4 {
			ease.Kind = EnvEaseCubicBezier
			for i, v := range controls {
				f, ok := nbt.FloatField(nbt.Compound{"v": nbt.ListElement(v)}, "v")
				if !ok || i%2 == 0 && (f < 0 || f > 1) {
					return ease, errors.New("invalid cubic bezier control")
				}
				ease.Controls[i] = f
			}
			return ease, nil
		}
	}
	return ease, errors.New("unsupported track easing")
}

// envArgumentValue encodes false/true as 0/1 and TriState.DEFAULT as -1;
// discrete tracks use constant interpolation in the shared keyframe sampler.
func envArgumentValue(tag nbt.Tag, attr EnvAttribute) (float32, bool) {
	if attr.Discrete {
		if b, ok := tag.(nbt.Byte); ok && (b == 0 || b == 1) {
			return float32(b), true
		}
		if attr.Min < 0 && tag == nbt.String("default") {
			return -1, true
		}
		return 0, false
	}
	return nbt.FloatField(nbt.Compound{"v": tag}, "v")
}

// EnvDimension holds a dimension type's resolved constant and timeline layers.
type EnvDimension struct {
	Attributes map[string]EnvFloatEntry
	Timelines  []EnvTimeline
}

// EnvDimensionDefaults returns generated layers before packs are loaded.
func EnvDimensionDefaults(name string) EnvDimension {
	return EnvDimension{Attributes: envDimensionFloats[name], Timelines: envTimelines[name]}
}

// DecodeEnvDimension reads supported attributes and a timeline holder set,
// using the generated defaults when the type has no pack entry.
// vanilla: DimensionType.DIRECT_CODEC, EnvironmentAttributeMap.CODEC
func DecodeEnvDimension(name string) (EnvDimension, error) {
	d := EnvDimension{Attributes: envDimensionFloats[name]}
	var holders []nbt.Tag
	for _, id := range envDimensionTimelineSets[name] {
		holders = append(holders, nbt.String(id))
	}
	r, _ := Get("minecraft:dimension_type")
	if data, ok := r.Data[name]; ok {
		c, ok := nbt.CompoundView(data)
		if !ok {
			return d, errors.New("dimension type is not a map")
		}
		var err error
		if d.Attributes, err = decodeEnvEntries(c["attributes"]); err != nil {
			return d, err
		}
		holders = nil
		if tag, exists := c["timelines"]; exists {
			if list, ok := tag.(nbt.List); ok {
				holders = list.Items
			} else {
				holders = []nbt.Tag{tag}
			}
		}
	}
	for _, tag := range holders {
		id, ok := tag.(nbt.String)
		if !ok {
			return d, errors.New("timeline holder is not an identifier")
		}
		ids := []string{Qualify(string(id))}
		if strings.HasPrefix(string(id), "#") {
			ids = nil
			found := false
			tags, _ := CurrentTags()
			for _, reg := range tags {
				if reg.Registry != "minecraft:timeline" {
					continue
				}
				for _, tag := range reg.Tags {
					if tag.Name == Qualify(string(id)[1:]) {
						ids = tag.Entries
						found = true
						break
					}
				}
			}
			if !found {
				return d, errors.New("unknown timeline tag " + string(id))
			}
		}
		for _, id := range ids {
			tl, err := DecodeEnvTimeline(nbt.String(id))
			if err != nil {
				return d, err
			}
			d.Timelines = append(d.Timelines, tl)
		}
	}
	return d, nil
}

func decodeEnvEntries(tag nbt.Tag) (map[string]EnvFloatEntry, error) {
	if tag == nil {
		return nil, nil
	}
	c, ok := nbt.CompoundView(tag)
	if !ok {
		return nil, errors.New("attributes is not a map")
	}
	entries := make(map[string]EnvFloatEntry)
	for name, tag := range c {
		name = Qualify(name)
		attr, ok := EnvAttributeByName(name)
		if !ok {
			return nil, errors.New("unknown attribute " + name)
		}
		if !attr.Float && !attr.Discrete {
			continue
		}
		op, value := nbt.String("override"), tag
		if c, ok := nbt.CompoundView(tag); ok && c["modifier"] != nil {
			op, _ = c["modifier"].(nbt.String)
			value = c["argument"]
		}
		entry, err := decodeEnvEntry(attr, op, value)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", name, err)
		}
		entries[name] = entry
	}
	return entries, nil
}

// decodeEnvEntry reads the modifier argument shared by maps and tracks.
func decodeEnvEntry(attr EnvAttribute, modifier, argument nbt.Tag) (EnvFloatEntry, error) {
	e := EnvFloatEntry{Alpha: 1}
	if modifier != nil {
		name, _ := modifier.(nbt.String)
		var ok bool
		if e.Op, ok = envModifiers[string(name)]; !ok {
			return e, errors.New("unknown attribute modifier " + string(name))
		}
	}
	if attr.Discrete && e.Op != EnvOverride && (attr.Min < 0 || e.Op < EnvAnd) || attr.Float && e.Op >= EnvAnd {
		return e, errors.New("modifier does not match attribute type")
	}
	if c, ok := nbt.CompoundView(argument); ok && e.Op == EnvAlphaBlend {
		argument = c["value"]
		if v, has := c["alpha"]; has {
			alpha, ok := nbt.FloatField(nbt.Compound{"v": v}, "v")
			if !ok || alpha < 0 || alpha > 1 {
				return e, errors.New("alpha is out of range")
			}
			e.Alpha = alpha
		}
	}
	var ok bool
	if e.Value, ok = envArgumentValue(argument, attr); !ok {
		return e, errors.New("argument does not match attribute type")
	}
	return e, nil
}
