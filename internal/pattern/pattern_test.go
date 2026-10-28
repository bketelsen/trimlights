package pattern

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

func TestSharedPreviewVectors(t *testing.T) {
	raw, err := os.ReadFile("../../web/src/pattern-vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var vectors []struct {
		Name     string
		Design   Design
		Count    int
		TickMS   int `json:"tick_ms"`
		Expected []string
	}
	if err = json.Unmarshal(raw, &vectors); err != nil {
		t.Fatal(err)
	}
	for _, v := range vectors {
		t.Run(v.Name, func(t *testing.T) {
			got, err := Render(v.Design, v.Count, v.TickMS)
			if err != nil || !reflect.DeepEqual(got, v.Expected) {
				t.Fatal(got, v.Expected, err)
			}
		})
	}
}
func TestInvalidDesignsAndRenderBounds(t *testing.T) {
	valid := Design{Schema: 1, Name: "Test", Effect: "static", Brightness: 50, Palette: []Run{{"#ff0000", 1}}}
	for _, mutate := range []func(*Design){func(d *Design) { d.Schema = 2 }, func(d *Design) { d.Name = " Test" }, func(d *Design) { d.Name = "bad\nname" }, func(d *Design) { d.Brightness = 101 }, func(d *Design) { d.Speed = -1 }, func(d *Design) { d.Effect = "unknown" }, func(d *Design) { d.Palette = []Run{{"#ff00ff", 0}} }, func(d *Design) { d.Palette = []Run{{"red", 2}} }} {
		d := valid
		mutate(&d)
		if err := d.Validate(); err == nil {
			t.Fatal(d)
		}
	}
	for _, v := range [][2]int{{0, 0}, {2049, 0}, {10, -1}, {10, 86400001}} {
		if _, err := Render(valid, v[0], v[1]); err == nil {
			t.Fatal(v)
		}
	}
}
