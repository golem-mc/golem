package main

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/golem-mc/golem/tools/internal/gogen"
)

// envAttr is one ENVATTR row: an environment attribute's type data.
type envAttr struct {
	toFloat, toInt, positional, discrete bool
	deflt, lo, hi                        string
}

// envFloatOps are the float modifiers by their codec name, as the registry
// constants name them; override is the entry a bare value decodes to.
// vanilla: AttributeModifier.FLOAT_LIBRARY
var envFloatOps = map[string]string{
	"override": "EnvOverride", "alpha_blend": "EnvAlphaBlend", "add": "EnvAdd",
	"subtract": "EnvSubtract", "multiply": "EnvMultiply", "minimum": "EnvMinimum", "maximum": "EnvMaximum",
	"and": "EnvAnd", "nand": "EnvNand", "or": "EnvOr", "nor": "EnvNor", "xor": "EnvXor", "xnor": "EnvXnor",
}

// envFloat32 writes a Java float's text as a Go float32 expression.
func envFloat32(s string) string {
	switch s {
	case "Infinity":
		return "posInf"
	case "-Infinity":
		return "negInf"
	}
	// Java's Float.toString is the shortest text that reads back as the
	// same float, which a float32 constant does too.
	if _, err := strconv.ParseFloat(s, 32); err != nil {
		fail("environment attribute value %q: %v", s, err)
	}
	return s
}

// envArgument encodes a supported modifier argument in the shared numeric form.
// vanilla: FloatWithAlpha.CODEC
func envArgument(op string, v any, where string) string {
	if b, ok := v.(bool); ok {
		if b {
			return "Value: 1, Alpha: 1"
		}
		return "Value: 0, Alpha: 1"
	}
	if v == "default" {
		return "Value: -1, Alpha: 1"
	}
	if n, ok := asNum(v); ok {
		return "Value: " + gogen.Float(n) + ", Alpha: 1"
	}
	o := asObj(v, where)
	if op != "alpha_blend" || !keysAre(o, "value", "alpha") && !keysAre(o, "value") {
		fail("%s: unsupported float argument %v", where, v)
	}
	return "Value: " + gogen.Float(o.num("value")) + ", Alpha: " + gogen.Float(o.numOr("alpha", 1))
}

// envEntry reads an attribute map entry: a bare value overrides, a
// compound names its modifier and argument.
// vanilla: EnvironmentAttributeMap.Entry.createCodec
func envEntry(v any, where string) string {
	op := "override"
	if m, ok := v.(map[string]any); ok && obj(m).has("modifier") {
		o := obj(m)
		op = o.str("modifier")
		v = o.field("argument")
	}
	ident, ok := envFloatOps[op]
	if !ok {
		fail("%s: unsupported float modifier %q", where, op)
	}
	return "{Op: " + ident + ", " + envArgument(op, v, where) + "}"
}

// envFloatEntries are the numeric and discrete entries of an attributes map,
// as Go source of a map[string]EnvFloatEntry; "" when there are none.
func envFloatEntries(attrs map[string]envAttr, m obj, where string) string {
	var lines []string
	for _, name := range sortedKeys(m) {
		a, ok := attrs[name]
		if !ok {
			fail("%s: unknown environment attribute %s", where, name)
		}
		if a.toFloat || a.discrete {
			lines = append(lines, gogen.Quote(name)+": "+envEntry(m[name], where+" "+name))
		}
	}
	if len(lines) == 0 {
		return ""
	}
	return "{" + strings.Join(lines, ", ") + "}"
}

// envEase reads a keyframe track's easing.
// vanilla: EasingType.CODEC
func envEase(v any, where string) string {
	switch v {
	case nil, "linear":
		return "EnvEase{}"
	case "constant":
		return "EnvEase{Kind: EnvEaseConstant}"
	}
	o := asObj(v, where)
	controls := o.list("cubic_bezier")
	if len(o) != 1 || len(controls) != 4 {
		fail("%s: unsupported easing %v", where, v)
	}
	parts := make([]string, 4)
	for i, c := range controls {
		n, ok := asNum(c)
		if !ok {
			fail("%s: cubic_bezier control %v is not a number", where, c)
		}
		parts[i] = gogen.Float(n)
	}
	return "EnvEase{Kind: EnvEaseCubicBezier, Controls: [4]float32{" + strings.Join(parts, ", ") + "}}"
}

// envTimeline writes a timeline's supported tracks as an EnvTimeline literal,
// "" for a timeline without any.
// vanilla: Timeline.DIRECT_CODEC, AttributeTrack.createCodec, KeyframeTrack.mapCodec
func envTimeline(attrs map[string]envAttr, t obj, where string) string {
	tracks, _ := t.obj("tracks")
	var lines []string
	for _, name := range sortedKeys(tracks) {
		a, ok := attrs[name]
		if !ok {
			fail("%s: unknown environment attribute %s", where, name)
		}
		if !a.toFloat && !a.discrete {
			continue
		}
		track := asObj(tracks[name], where+" "+name)
		op := track.strOr("modifier", "override")
		ident, ok := envFloatOps[op]
		if !ok {
			fail("%s %s: unsupported float modifier %q", where, name, op)
		}
		var keyframes []string
		for _, k := range track.list("keyframes") {
			kf := asObj(k, where+" keyframe")
			keyframes = append(keyframes, fmt.Sprintf("{Ticks: %d, %s}", int(kf.num("ticks")), envArgument(op, kf.field("value"), where+" "+name)))
		}
		if len(keyframes) == 0 {
			fail("%s %s: no keyframes", where, name)
		}
		ease := envEase(track["ease"], where+" "+name)
		if a.discrete {
			ease = "EnvEase{Kind: EnvEaseConstant}"
		}
		lines = append(lines, fmt.Sprintf("%s: {Op: %s, Ease: %s, Keyframes: []EnvKeyframe{%s}}",
			gogen.Quote(name), ident, ease, strings.Join(keyframes, ", ")))
	}
	if len(lines) == 0 {
		return ""
	}
	return fmt.Sprintf("{Clock: %s, Period: %d, Tracks: map[string]EnvFloatTrack{%s}}",
		gogen.Quote(t.str("clock")), t.intOr("period_ticks", 0), strings.Join(lines, ", "))
}

// envAttributeTables writes the environment attributes and supported layers
// the data pack gives them: each dimension type's constants, each biome's
// modifiers and the tracks of each dimension type's timelines. The
// weather layer is code in vanilla (WeatherAttributes) and in internal/server/world.
func (g *tablesGen) envAttributeTables() error {
	attrs := map[string]envAttr{}
	values := map[string]string{}
	for _, r := range g.rows["ENVATTR"] {
		if len(r) != 8 {
			return fmt.Errorf("ENVATTR row %v", r)
		}
		a := envAttr{toFloat: r[2] == "true", toInt: r[3] == "true", positional: r[4] == "true", discrete: r[2] == "false" && r[5] != "-", deflt: r[5], lo: r[6], hi: r[7]}
		if a.toInt {
			// No 26.3 attribute converts to an integer; the int provider
			// refuses every attribute until one does.
			return fmt.Errorf("%s converts to an integer, which the /compute int provider does not model", r[0])
		}
		attrs[r[0]] = a
		v := fmt.Sprintf("{Float: %t, Positional: %t, Discrete: %t", a.toFloat, a.positional, a.discrete)
		if a.toFloat || a.discrete {
			v += ", Default: " + envFloat32(a.deflt) + ", Min: " + envFloat32(a.lo) + ", Max: " + envFloat32(a.hi)
		}
		values[r[0]] = v + "}"
	}
	g.w.emit("envAttributes", "map[string]EnvAttribute", values)

	dimTypes, err := g.objects("dimension_type")
	if err != nil {
		return err
	}
	biomes, err := g.objects("worldgen/biome")
	if err != nil {
		return err
	}
	timelines, err := g.objects("timeline")
	if err != nil {
		return err
	}
	tags, err := g.objects("tags/timeline")
	if err != nil {
		return err
	}
	// resolve flattens a timeline holder set: a tag, an id or a list of them.
	var resolve func(v any, where string) []string
	resolve = func(v any, where string) []string {
		switch x := v.(type) {
		case string:
			if tag, ok := strings.CutPrefix(x, "#"); ok {
				t, ok := tags[tag]
				if !ok {
					fail("%s: unknown timeline tag %s", where, x)
				}
				return resolve(t.list("values"), where)
			}
			return []string{x}
		case []any:
			var out []string
			for _, e := range x {
				for _, n := range resolve(e, where) {
					if !slices.Contains(out, n) {
						out = append(out, n)
					}
				}
			}
			return out
		}
		fail("%s: unsupported timeline set %v", where, v)
		return nil
	}

	namedTimelines := map[string]string{}
	for name, t := range timelines {
		s := envTimeline(attrs, t, name)
		if s == "" {
			s = fmt.Sprintf("{Clock: %s, Period: %d}", gogen.Quote(t.str("clock")), t.intOr("period_ticks", 0))
		}
		namedTimelines[name] = s
	}
	sets := map[string]string{}
	dimFloats, biomeFloats, dimTimelines := map[string]string{}, map[string]string{}, map[string]string{}
	for name, d := range dimTypes {
		var ids []string
		switch v := d["timelines"].(type) {
		case string:
			ids = append(ids, gogen.Quote(v))
		case []any:
			for _, id := range v {
				ids = append(ids, gogen.Quote(id.(string)))
			}
		default:
			fail("%s: unsupported timeline holder set %v", name, v)
		}
		sets[name] = "{" + strings.Join(ids, ", ") + "}"
		m, _ := d.obj("attributes")
		if s := envFloatEntries(attrs, m, name); s != "" {
			dimFloats[name] = s
		}
		var lines []string
		for _, tl := range resolve(d["timelines"], name) {
			t, ok := timelines[tl]
			if !ok {
				fail("%s: unknown timeline %s", name, tl)
			}
			if envTimeline(attrs, t, tl) != "" {
				lines = append(lines, "envNamedTimelines["+gogen.Quote(tl)+"]")
			}
		}
		if len(lines) == 0 {
			continue
		}
		dimTimelines[name] = "{" + strings.Join(lines, ", ") + "}"
	}
	for name, b := range biomes {
		m, _ := b.obj("attributes")
		if s := envFloatEntries(attrs, m, name); s != "" {
			biomeFloats[name] = s
		}
	}
	g.w.emit("envDimensionFloats", "map[string]map[string]EnvFloatEntry", dimFloats)
	g.w.emit("envBiomeFloats", "map[string]map[string]EnvFloatEntry", biomeFloats)
	g.w.emit("envNamedTimelines", "map[string]EnvTimeline", namedTimelines)
	g.w.emit("envDimensionTimelineSets", "map[string][]string", sets)
	g.w.emit("envTimelines", "map[string][]EnvTimeline", dimTimelines)
	return nil
}
