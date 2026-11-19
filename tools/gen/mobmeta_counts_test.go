package main

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Compile counters into an overlay so the production hot path needs no test hooks.
// A fresh server test binary can take over 10s to build on a busy host.
func TestMobMetadataProjectionCounters(t *testing.T) {
	root, tmp := repoRoot(t), t.TempDir()
	overlay := map[string]string{}
	for _, file := range []string{"mobmeta_gen.go", "effects.go", "mobs.go", "entity/mob_attributes.go", "mobmeta_tracking_test.go"} {
		path := filepath.Join(root, "internal", "server", file)
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		text := string(src)
		if file == "mobmeta_tracking_test.go" {
			text += mobMetaCounterTest
		} else {
			fs := token.NewFileSet()
			tree, err := parser.ParseFile(fs, path, src, 0)
			if err != nil {
				t.Fatal(err)
			}
			inserts := map[int]string{}
			counters := map[string]int{"mobMetadataStateOf": 0, "captureMobMetadata": 1, "mobEffectShown": 2, "mobEquipmentSame": 3}
			for _, decl := range tree.Decls {
				fn, ok := decl.(*ast.FuncDecl)
				if !ok {
					continue
				}
				if fn.Name.Name == "syncEquipmentModifiers" {
					inserts[fs.Position(fn.Body.Lbrace).Offset+1] = "\nZZMobEquipmentComparisons++\n"
				}
				if n, ok := counters[fn.Name.Name]; ok {
					inserts[fs.Position(fn.Body.Lbrace).Offset+1] = "\nzzMobMetaCounts[" + string(rune('0'+n)) + "]++\n"
				}
			}
			var instrumented strings.Builder
			for i := range len(src) {
				instrumented.WriteByte(src[i])
				instrumented.WriteString(inserts[i+1])
			}
			text = instrumented.String()
			if file == "entity/mob_attributes.go" {
				text += "\nvar ZZMobEquipmentComparisons int\n"
			}
			if file == "mobmeta_gen.go" {
				text = strings.ReplaceAll(text, "s.health = zero.health", "zzMobMetaCounts[4]++; s.health = zero.health")
				text = strings.ReplaceAll(text, "s.mobFlags = zero.mobFlags", "zzMobMetaCounts[5]++; s.mobFlags = zero.mobFlags")
			}
		}
		replacement := filepath.Join(tmp, filepath.Base(file))
		if err := os.WriteFile(replacement, []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
		overlay[path] = replacement
	}
	data, err := json.Marshal(struct{ Replace map[string]string }{overlay})
	if err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(tmp, "overlay.json")
	if err := os.WriteFile(config, data, 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("mise", "exec", "--", "go", "test", "-overlay", config, "./internal/server", "-run", "^TestZZMobMetadataProjectionCounts$", "-count=1")
	cmd.Dir = root
	if output, err := cmd.CombinedOutput(); err != nil {
		lines := strings.Split(string(output), "\n")
		if len(lines) > 60 {
			lines = lines[len(lines)-60:]
		}
		t.Fatalf("counter assertion: %v\n%s", err, strings.Join(lines, "\n"))
	}
}

const mobMetaCounterTest = `
var zzMobMetaCounts [6]int

func TestZZMobMetadataProjectionCounts(t *testing.T) {
 ent, tick, in := mobMetadataTracking(entity.TypeCow)
 ent.mob.SetAttributes(ent.mob.BuildAttributes(savedAttributes(ent.saved)))
 metadataTrackingSends(tick,in) // consume the initial equipment revision
 zzMobMetaCounts = [6]int{}
 entity.ZZMobEquipmentComparisons = 0
 for range 100 { metadataTrackingSends(tick,in) }
 if entity.ZZMobEquipmentComparisons != 0 { t.Fatal("clean tracking compared attribute equipment") }
 if zzMobMetaCounts != [6]int{} { t.Fatalf("clean tracking read sources: %v",zzMobMetaCounts) }
 ent.mob.SetMetadataHealth(7)
 metadataTrackingSends(tick,in)
 if want := [6]int{0,1,0,0,1,0}; zzMobMetaCounts != want { t.Fatalf("health capture = %v, want %v",zzMobMetaCounts,want) }
 zzMobMetaCounts = [6]int{}
 ent.mob.SetMetadataAggressive(true)
 metadataTrackingSends(tick,in)
 if want := [6]int{0,1,0,0,0,1}; zzMobMetaCounts != want { t.Fatalf("flag capture = %v, want %v",zzMobMetaCounts,want) }
 if entity.ZZMobEquipmentComparisons != 0 { t.Fatal("metadata write compared attribute equipment") }
 zzMobMetaCounts = [6]int{}
 ent.mob.SetEquipment(protocol.EquipMainHand,protocol.ItemStack{ItemID:int32(registry.ItemStone),Count:1})
 metadataTrackingSends(tick,in)
 if entity.ZZMobEquipmentComparisons != 1 || zzMobMetaCounts != [6]int{0,0,0,1,0,0} { t.Fatalf("equipment write compared/captured unexpected state: %d/%v",entity.ZZMobEquipmentComparisons,zzMobMetaCounts) }
}
`
