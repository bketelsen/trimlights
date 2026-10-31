package pattern

import (
	"encoding/json"
	"errors"
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
		if err := d.Validate(); !errors.Is(err, ErrInvalid) {
			t.Fatal(d, err)
		}
	}
	for _, v := range [][2]int{{0, 0}, {2049, 0}, {10, -1}, {10, 86400001}} {
		if _, err := Render(valid, v[0], v[1]); err == nil {
			t.Fatal(v)
		}
	}
}

// TestSharedNameVectors keeps Go name validation equal to web/src/pattern.ts.
func TestSharedNameVectors(t *testing.T) {
	raw, err := os.ReadFile("../../web/src/name-vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var vectors []struct {
		Name  string
		Value string
		Valid bool
	}
	if err = json.Unmarshal(raw, &vectors); err != nil {
		t.Fatal(err)
	}
	for _, v := range vectors {
		t.Run(v.Name, func(t *testing.T) {
			d := Design{Schema: 1, Name: v.Value, Effect: "static", Brightness: 50, Palette: []Run{{"#ff0000", 1}}}
			if err := d.Validate(); (err == nil) != v.Valid {
				t.Fatalf("%q valid=%v: %v", v.Value, v.Valid, err)
			}
		})
	}
}

// Invalid UTF-8 cannot travel in the shared JSON vectors (encoding/json stores
// U+FFFD), so the Go side checks it here; pattern.test.mjs checks lone surrogates.
func TestNamesRejectInvalidUTF8(t *testing.T) {
	for _, name := range []string{"\xff", "Porch \xed\xa0\x80", "\xf0\x9f\x8e"} {
		d := Design{Schema: 1, Name: name, Effect: "static", Brightness: 50, Palette: []Run{{"#ff0000", 1}}}
		if ValidName(name) || !errors.Is(d.Validate(), ErrInvalid) {
			t.Fatalf("%q accepted", name)
		}
	}
	if !ValidName("Porch \U0001F383") {
		t.Fatal("valid supplementary character rejected")
	}
}
