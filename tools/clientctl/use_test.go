package main

import (
	"fmt"
	"math"
	"os"
	"testing"
	"time"

	"github.com/golem-mc/golem/tools/internal/clientctl"
)

// TestUseInClient needs a disposable world and an operator in a running real
// client. It exercises the Java agent, not an HTTP stand-in.
func TestUseInClient(t *testing.T) {
	if os.Getenv("CLIENTCTL_USE_TEST") != "1" {
		t.Skip("set CLIENTCTL_USE_TEST=1 with a real client joined as an operator in a disposable world")
	}
	c := clientctl.FromEnv()
	call := func(r clientctl.Result, err error) clientctl.Result {
		t.Helper()
		if err != nil || !r.OK() {
			t.Fatalf("clientctl: %v, %v", r, err)
		}
		return r
	}
	wait := func(t *testing.T, pred func(clientctl.Result) bool) clientctl.Result {
		t.Helper()
		r, err := c.WaitTicks(pred, 100, 30*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	call(c.WaitScreen("none", 10*time.Second))
	s := call(c.State())
	p := s["player"].(map[string]any)
	x := int(math.Floor(p["x"].(float64)/16))*16 + 8
	z := int(math.Floor(p["z"].(float64)/16))*16 + 8
	chat := func(t *testing.T, format string, args ...any) {
		t.Helper()
		call(c.Chat(fmt.Sprintf(format, args...)))
	}
	item := func(s clientctl.Result, section, slot string) string {
		inv := s["inventory"].(map[string]any)[section].(map[string]any)
		stack, _ := inv[slot].(map[string]any)
		id, _ := stack["id"].(string)
		return id
	}
	chat(t, "/gamemode creative")
	chat(t, "/fill %d 200 %d %d 201 %d minecraft:air", x-3, z-3, x+3, z+3)
	chat(t, "/fill %d 199 %d %d 199 %d minecraft:grass_block", x-3, z-3, x+3, z+3)
	chat(t, "/tp @s %.1f 200 %.1f 0 60", float64(x)+0.5, float64(z)+0.5)
	wait(t, func(s clientctl.Result) bool {
		return s["target"].(map[string]any)["block"] == "minecraft:grass_block"
	})
	chat(t, "/gamemode survival")
	call(c.Slot(0))
	for _, offhand := range []bool{false, true} {
		t.Run(fmt.Sprintf("bucket-offhand-%t", offhand), func(t *testing.T) {
			chat(t, "/clear @s")
			slot, section, key := "hotbar.0", "slots", "0"
			if offhand {
				// A nonempty main hand that passes must also allow the offhand.
				chat(t, "/item replace entity @s hotbar.0 with minecraft:stick")
				slot, section, key = "weapon.offhand", "equipment", "offhand"
			}
			chat(t, "/item replace entity @s %s with minecraft:axolotl_bucket", slot)
			wait(t, func(s clientctl.Result) bool { return item(s, section, key) == "minecraft:axolotl_bucket" })
			r := call(c.Use())
			if r["target"] != "item" || r["result"] != "Success" {
				t.Fatalf("bucket use = %v, want held-item Success after block Pass", r)
			}
			wait(t, func(s clientctl.Result) bool {
				if item(s, section, key) != "minecraft:bucket" {
					return false
				}
				for _, entity := range s["entities"].([]any) {
					if entity.(map[string]any)["type"] == "minecraft:axolotl" {
						return true
					}
				}
				return false
			})
			marker := fmt.Sprintf("water-%d", time.Now().UnixNano())
			chat(t, "/execute if block %d 200 %d minecraft:water run say %s", x, z+1, marker)
			wait(t, func(s clientctl.Result) bool {
				for _, msg := range s["chat"].([]any) {
					if msg.(map[string]any)["text"] == "[Tester] "+marker {
						return true
					}
				}
				return false
			})
		})
		chat(t, "/kill @e[type=minecraft:axolotl,distance=..16]")
		chat(t, "/fill %d 200 %d %d 201 %d minecraft:air", x-3, z-3, x+3, z+3)
	}
	t.Run("consuming-block", func(t *testing.T) {
		chat(t, "/clear @s")
		chat(t, "/item replace entity @s hotbar.0 with minecraft:axolotl_bucket")
		chat(t, "/setblock %d 200 %d minecraft:lever[face=floor,powered=false]", x, z+1)
		wait(t, func(s clientctl.Result) bool {
			return s["target"].(map[string]any)["block"] == "minecraft:lever" && item(s, "slots", "0") == "minecraft:axolotl_bucket"
		})
		r := call(c.Use())
		if r["target"] != "block" || r["result"] != "Success" {
			t.Fatalf("lever use = %v, want block Success", r)
		}
		marker := fmt.Sprintf("lever-on-%d", time.Now().UnixNano())
		chat(t, "/execute if block %d 200 %d minecraft:lever[powered=true] run say %s", x, z+1, marker)
		wait(t, func(s clientctl.Result) bool {
			for _, msg := range s["chat"].([]any) {
				if msg.(map[string]any)["text"] == "[Tester] "+marker {
					return item(s, "slots", "0") == "minecraft:axolotl_bucket"
				}
			}
			return false
		})
	})
}
