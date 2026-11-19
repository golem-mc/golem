package main

import (
	"strings"
	"testing"
)

func TestRuinedPortalDefinitionRejectsUnsupportedSetup(t *testing.T) {
	const valid = `{"type":"minecraft:ruined_portal","step":"surface_structures","setups":[{"placement":"underground","weight":1}]}`
	for _, data := range []string{valid, strings.ReplaceAll(valid, "underground", "floating"), strings.ReplaceAll(valid, `"weight":1`, `"weight":0`), strings.ReplaceAll(valid, `"weight":1`, `"weight":1,"unmodeled":true`)} {
		if err := checkRuinedPortalDefinition([]byte(data)); (err == nil) != (data == valid) {
			t.Errorf("%s: validation error %v", data, err)
		}
	}
}
