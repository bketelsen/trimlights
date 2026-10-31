package house

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math"
	"os"
	"testing"
)

func TestSharedGeometryVectors(t *testing.T) {
	raw, err := os.ReadFile("../../web/src/house-vectors.json")
	if err != nil {
		t.Fatal(err)
	}
	var vectors []struct {
		Name          string
		Width, Height float64
		Count         int
		Mapping       Mapping
		Expected      []Point
	}
	if err = json.Unmarshal(raw, &vectors); err != nil {
		t.Fatal(err)
	}
	for _, v := range vectors {
		t.Run(v.Name, func(t *testing.T) {
			got := Positions(v.Mapping, v.Count, v.Width, v.Height)
			if len(got) != len(v.Expected) {
				t.Fatal(got)
			}
			for i := range got {
				if math.Abs(got[i].X-v.Expected[i].X) > 1e-9 || math.Abs(got[i].Y-v.Expected[i].Y) > 1e-9 {
					t.Fatal(got, v.Expected)
				}
			}
		})
	}
}
func exif(o int) []byte {
	b := []byte{'I', 'I', 42, 0, 8, 0, 0, 0, 1, 0, 0x12, 1, 3, 0, 1, 0, 0, 0, byte(o), 0, 0, 0, 0, 0, 0, 0}
	return append([]byte("Exif\x00\x00"), b...)
}
func TestAllEXIFOrientationsAndMetadataRemoval(t *testing.T) {
	// Independent expected destination pixel order for a 3x2 source.
	src := image.NewGray(image.Rect(0, 0, 3, 2))
	copy(src.Pix, []byte{1, 2, 3, 4, 5, 6})
	expected := [][]byte{{1, 2, 3, 4, 5, 6}, {3, 2, 1, 6, 5, 4}, {6, 5, 4, 3, 2, 1}, {4, 5, 6, 1, 2, 3}, {1, 4, 2, 5, 3, 6}, {4, 1, 5, 2, 6, 3}, {6, 3, 5, 2, 4, 1}, {3, 6, 2, 5, 1, 4}}
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, src, nil); err != nil {
		t.Fatal(err)
	}
	for o := 1; o <= 8; o++ {
		view := orientedImage{src, o}
		var got []byte
		for y := 0; y < view.Bounds().Dy(); y++ {
			for x := 0; x < view.Bounds().Dx(); x++ {
				got = append(got, color.GrayModel.Convert(view.At(x, y)).(color.Gray).Y)
			}
		}
		if !bytes.Equal(got, expected[o-1]) {
			t.Fatal(o, got)
		}
		metadata := append(exif(o), []byte("PRIVATE GPS LOCATION")...)
		segment := []byte{0xff, 0xe1, 0, 0}
		binary.BigEndian.PutUint16(segment[2:], uint16(len(metadata)+2))
		raw := append([]byte{}, encoded.Bytes()[:2]...)
		raw = append(raw, segment...)
		raw = append(raw, metadata...)
		raw = append(raw, encoded.Bytes()[2:]...)
		if orientation(raw, "jpeg") != o {
			t.Fatal(o)
		}
		clean, err := Sanitize(raw)
		if err != nil {
			t.Fatal(err)
		}
		if clean.Width != view.Bounds().Dx() || clean.Height != view.Bounds().Dy() {
			t.Fatal(o, clean.Width, clean.Height)
		}
		if bytes.Contains(clean.JPEG, []byte("Exif")) || bytes.Contains(clean.JPEG, []byte("PRIVATE")) {
			t.Fatal("metadata retained")
		}
	}
}
func TestSanitizeBoundsTransparencyAndInvalidImages(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 5000, 1))
	var b bytes.Buffer
	if err := png.Encode(&b, src); err != nil {
		t.Fatal(err)
	}
	p, err := Sanitize(b.Bytes())
	if err != nil || p.Width != 4096 || p.Height != 1 {
		t.Fatal(p.Width, p.Height, err)
	}
	cfg, err := jpeg.DecodeConfig(bytes.NewReader(p.Thumbnail))
	if err != nil || cfg.Width != 512 {
		t.Fatal(cfg, err)
	}
	img, err := jpeg.Decode(bytes.NewReader(p.JPEG))
	if err != nil {
		t.Fatal(err)
	}
	r, g, bl, _ := img.At(1, 0).RGBA()
	if r < 65000 || g < 65000 || bl < 65000 {
		t.Fatal("transparent pixels not flattened to white")
	}
	for _, raw := range [][]byte{[]byte("\x00\x00\x00\x18ftypheic"), []byte("<svg></svg>"), b.Bytes()[:30], make([]byte, MaxUpload+1)} {
		if _, err := Sanitize(raw); err == nil {
			t.Fatal("invalid image accepted")
		}
	}
	if _, err := Sanitize(pngOversized(t)); err == nil {
		t.Fatal("oversized dimensions accepted")
	}
}
func pngOversized(t *testing.T) []byte {
	t.Helper()
	img := image.NewGray(image.Rect(0, 0, 16385, 1))
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func TestLayoutRejectsAmbiguousAddressesAndInvalidGeometry(t *testing.T) {
	base := Layout{Schema: 1, Runs: []Run{{ID: "front", Name: "Front", Output: "one", Start: 1, Count: 10}}, Views: []View{{PhotoID: "photo", Mappings: []Mapping{{RunID: "front", Points: []Point{{0, 0}, {1, 1}}, Adjustments: []Adjustment{}}}}}}
	if err := base.Validate(); err != nil {
		t.Fatal(err)
	}
	cases := []func(*Layout){func(l *Layout) {
		l.Runs = append(l.Runs, Run{ID: "other", Name: "Overlap", Output: "one", Start: 10, Count: 5})
	}, func(l *Layout) { l.Runs[0].MirrorOf = "front" }, func(l *Layout) { l.Views[0].Mappings[0].Points[1].X = math.NaN() }, func(l *Layout) { l.Views[0].Mappings[0].Points[1] = Point{} }, func(l *Layout) { l.Views[0].Mappings[0].Adjustments = []Adjustment{{Index: 10, Point: Point{.5, .5}}} }}
	raw, _ := json.Marshal(base)
	for i, change := range cases {
		var l Layout
		_ = json.Unmarshal(raw, &l)
		change(&l)
		if l.Validate() == nil {
			t.Fatal("accepted case", i)
		}
	}
}
func FuzzOrientation(f *testing.F) {
	f.Add(exif(6))
	f.Add([]byte{0xff, 0xd8})
	f.Fuzz(func(t *testing.T, b []byte) {
		for _, kind := range []string{"jpeg", "png", "webp"} {
			n := orientation(b, kind)
			if n < 1 || n > 8 {
				t.Fatal(n)
			}
		}
		n := tiffOrientation(b)
		if n < 1 || n > 8 {
			t.Fatal(n)
		}
	})
}
