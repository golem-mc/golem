package main

import "testing"

func TestReadBiomeParameters(t *testing.T) {
	const line = "biomepoint -1 2 -3 4 -5 6 -7 8 -9 10 -11 12 13 dappled_forest"
	points, err := readBiomeParameters("# header\n" + line + "\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(points) != 1 || points[0].name != "dappled_forest" || points[0].bounds != [7][2]int64{{-1, 2}, {-3, 4}, {-5, 6}, {-7, 8}, {-9, 10}, {-11, 12}, {13, 13}} {
		t.Fatalf("bad parameters: %+v", points)
	}
	for _, text := range []string{"", "biomepoint", line + " extra", "unknown " + line, "biomepoint 1 -1 0 0 0 0 0 0 0 0 0 0 0 plains", "biomepoint x 0 0 0 0 0 0 0 0 0 0 0 0 plains", "biomepoint 0 0 0 0 0 0 0 0 0 0 0 0 0 plains/other"} {
		if _, err := readBiomeParameters(text); err == nil {
			t.Errorf("accepted %q", text)
		}
	}
}
