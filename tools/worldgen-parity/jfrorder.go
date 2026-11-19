package main

import (
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"
)

type recordedStep struct {
	Pos   [2]int32
	Load  bool
	Light string
}

// featuresOnly excludes reloads from decoration indices.
func featuresOnly(steps []recordedStep) [][2]int32 {
	var out [][2]int32
	for _, step := range steps {
		if !step.Load && step.Light == "" {
			out = append(out, step.Pos)
		}
	}
	return out
}

// featuresOrder reads decoration starts and light publication across worker threads.
func featuresOrder(jsonBytes []byte, level string) ([]recordedStep, error) {
	var data struct {
		Recording struct {
			Events []struct {
				Type   string `json:"type"`
				Values struct {
					Dimension string `json:"dimension"`
					Type      string `json:"type"`
					StartTime string `json:"startTime"`
					Duration  string `json:"duration"`
					X         int32  `json:"chunkPosX"`
					Z         int32  `json:"chunkPosZ"`
					Status    string `json:"status"`
					Level     string `json:"level"`
				} `json:"values"`
			} `json:"events"`
		} `json:"recording"`
	}
	if err := json.Unmarshal(jsonBytes, &data); err != nil {
		return nil, err
	}
	type feature struct {
		step  recordedStep
		start time.Time
	}
	var events []feature
	terrain := map[[2]int32]time.Time{}
	for _, e := range data.Recording.Events {
		v := e.Values
		generation := e.Type == "minecraft.ChunkGeneration"
		load := e.Type == "minecraft.ChunkRegionRead" && v.Type == "chunk" && v.Dimension == level
		lightStatus := level == "minecraft:overworld" && (v.Status == "minecraft:initialize_light" || v.Status == "minecraft:light")
		terrainStatus := v.Status == "minecraft:terrain" || v.Status == "minecraft:carvers"
		generation = generation && v.Level == level && (v.Status == "minecraft:features" || terrainStatus || lightStatus)
		if !load && !generation {
			continue
		}
		start, err := time.Parse(time.RFC3339Nano, v.StartTime)
		if err != nil {
			return nil, fmt.Errorf("event at %d,%d: %w", v.X, v.Z, err)
		}
		pos := [2]int32{v.X, v.Z}
		if generation && terrainStatus {
			if prior, ok := terrain[pos]; !ok || start.Before(prior) {
				terrain[pos] = start
			}
		} else {
			step := recordedStep{Pos: pos, Load: load}
			if generation && lightStatus {
				if v.Status == "minecraft:initialize_light" {
					events = append(events, feature{recordedStep{Pos: pos, Light: "light_sources"}, start})
				}
				duration, err := time.ParseDuration(strings.ToLower(strings.TrimPrefix(v.Duration, "PT")))
				if err != nil || duration < 0 || !strings.HasPrefix(v.Duration, "PT") {
					return nil, fmt.Errorf("invalid light duration %q", v.Duration)
				}
				start = start.Add(duration)
				step.Light = strings.TrimPrefix(v.Status, "minecraft:")
			}
			events = append(events, feature{step, start})
		}
	}
	// JFR file order groups events from different workers, not their start times.
	sort.SliceStable(events, func(i, j int) bool { return events[i].start.Before(events[j].start) })
	out := make([]recordedStep, 0, len(events))
	for _, e := range events {
		if e.step.Load {
			prior, ok := terrain[e.step.Pos]
			if !ok || !prior.Before(e.start) {
				continue
			}
		}
		out = append(out, e.step)
	}
	return out, nil
}

// publishedDuringFeatures moves the light completions recorded while a
// FEATURES step ran, up to the worker's next event, ahead of that step. The
// recording runs one worker thread, and the light thread publishes a batch's
// section map before its completions end, so a step's late reads (small
// mushrooms in VEGETAL_DECORATION) already see it.
// vanilla: ThreadedLevelLightEngine.runUpdate, LayerLightSectionStorage.swapSectionMap
func publishedDuringFeatures(steps []recordedStep) []recordedStep {
	out := slices.Clone(steps)
	for i := 0; i < len(out); i++ {
		if out[i].Load || out[i].Light != "" {
			continue
		}
		j := i + 1
		for j < len(out) && (out[j].Light == "initialize_light" || out[j].Light == "light") {
			j++
		}
		feature := out[i]
		copy(out[i:j-1], out[i+1:j])
		out[j-1] = feature
		i = j - 1
	}
	return out
}
