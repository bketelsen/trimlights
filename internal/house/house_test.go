package house

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"hash/crc32"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math"
	"os"
	"runtime"
	"strings"
	"testing"

	"golang.org/x/image/draw"
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
	rgba := image.NewRGBA(src.Bounds())
	draw.Draw(rgba, rgba.Bounds(), src, image.Point{}, draw.Src)
	for o := 1; o <= 8; o++ {
		view := orient(rgba, o)
		var got []byte
		for y := 0; y < view.Bounds().Dy(); y++ {
			for x := 0; x < view.Bounds().Dx(); x++ {
				got = append(got, view.RGBAAt(x, y).R)
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

// withEXIF inserts an APP1 EXIF orientation after the JPEG SOI marker.
func withEXIF(encoded []byte, o int) []byte {
	metadata := exif(o)
	segment := []byte{0xff, 0xe1, 0, 0}
	binary.BigEndian.PutUint16(segment[2:], uint16(len(metadata)+2))
	raw := append([]byte{}, encoded[:2]...)
	raw = append(raw, segment...)
	raw = append(raw, metadata...)
	return append(raw, encoded[2:]...)
}

func TestSanitizeOrientsPixels(t *testing.T) {
	// A 3x2 grid of 16-pixel blocks; each block value names its source position.
	expected := [][]byte{{1, 2, 3, 4, 5, 6}, {3, 2, 1, 6, 5, 4}, {6, 5, 4, 3, 2, 1}, {4, 5, 6, 1, 2, 3}, {1, 4, 2, 5, 3, 6}, {4, 1, 5, 2, 6, 3}, {6, 3, 5, 2, 4, 1}, {3, 6, 2, 5, 1, 4}}
	src := image.NewGray(image.Rect(0, 0, 48, 32))
	for y := range 32 {
		for x := range 48 {
			src.Pix[y*src.Stride+x] = byte(40 * (1 + x/16 + 3*(y/16)))
		}
	}
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, src, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}
	for o := 1; o <= 8; o++ {
		clean, err := Sanitize(withEXIF(encoded.Bytes(), o))
		if err != nil {
			t.Fatal(o, err)
		}
		img, err := jpeg.Decode(bytes.NewReader(clean.JPEG))
		if err != nil {
			t.Fatal(o, err)
		}
		cols := img.Bounds().Dx() / 16
		var got []byte
		for i := range 6 {
			r, _, _, _ := img.At(16*(i%cols)+8, 16*(i/cols)+8).RGBA()
			got = append(got, byte(math.Round(float64(r>>8)/40)))
		}
		if !bytes.Equal(got, expected[o-1]) {
			t.Fatal(o, got)
		}
	}
}

func pngHeader(w, h uint32, depth, kind, interlace byte) []byte {
	ihdr := binary.BigEndian.AppendUint32(nil, w)
	ihdr = binary.BigEndian.AppendUint32(ihdr, h)
	ihdr = append(ihdr, depth, kind, 0, 0, interlace)
	b := []byte("\x89PNG\r\n\x1a\n")
	for _, c := range []struct {
		name string
		data []byte
	}{{"IHDR", ihdr}, {"IEND", nil}} {
		b = binary.BigEndian.AppendUint32(b, uint32(len(c.data)))
		chunk := append([]byte(c.name), c.data...)
		b = append(b, chunk...)
		b = binary.BigEndian.AppendUint32(b, crc32.ChecksumIEEE(chunk))
	}
	return b
}

// withChunk inserts a PNG chunk after IHDR.
func withChunk(raw []byte, name string, data []byte) []byte {
	const ihdrEnd = 8 + 12 + 13
	chunk := binary.BigEndian.AppendUint32(nil, uint32(len(data)))
	chunk = append(chunk, name...)
	chunk = append(chunk, data...)
	chunk = binary.BigEndian.AppendUint32(chunk, crc32.ChecksumIEEE(chunk[4:]))
	out := append([]byte{}, raw[:ihdrEnd]...)
	out = append(out, chunk...)
	return append(out, raw[ihdrEnd:]...)
}

// riff wraps chunks in a RIFF WEBP container.
func riff(chunks ...[]byte) []byte {
	var body []byte
	for _, c := range chunks {
		body = append(body, c...)
		if len(c)%2 == 1 {
			body = append(body, 0)
		}
	}
	b := append([]byte("RIFF"), binary.LittleEndian.AppendUint32(nil, uint32(4+len(body)))...)
	return append(append(b, "WEBP"...), body...)
}

func webpChunk(id string, data []byte) []byte {
	return append(append([]byte(id), binary.LittleEndian.AppendUint32(nil, uint32(len(data)))...), data...)
}

// vp8x is an extended header; flag 0x10 announces alpha.
func vp8x(w, h uint32, flags byte) []byte {
	d := []byte{flags, 0, 0, 0}
	d = append(d, byte(w-1), byte((w-1)>>8), byte((w-1)>>16), byte(h-1), byte((h-1)>>8), byte((h-1)>>16))
	return webpChunk("VP8X", d)
}

// vp8lHeader is the 5-byte VP8L signature and size header.
func vp8lHeader(w, h uint32, alpha bool) []byte {
	bits := (w - 1) | (h-1)<<14
	if alpha {
		bits |= 1 << 28
	}
	return append([]byte{0x2f}, binary.LittleEndian.AppendUint32(nil, bits)...)
}

// vp8Header is a lossy key-frame header.
func vp8Header(w, h uint16) []byte {
	d := []byte{0x10, 0, 0, 0x9d, 0x01, 0x2a}
	d = binary.LittleEndian.AppendUint16(d, w)
	return binary.LittleEndian.AppendUint16(d, h)
}

func jpegHeader(w, h uint16, sof byte, factors ...byte) []byte {
	b := []byte{0xff, 0xd8, 0xff, sof}
	b = binary.BigEndian.AppendUint16(b, uint16(8+3*len(factors)))
	b = append(b, 8)
	b = binary.BigEndian.AppendUint16(b, h)
	b = binary.BigEndian.AppendUint16(b, w)
	b = append(b, byte(len(factors)))
	for i, f := range factors {
		b = append(b, byte(i+1), f, 0)
	}
	// image/jpeg returns the configuration at SOS when no JFIF marker exists.
	return append(b, 0xff, 0xda, 0, 2, 0xff, 0xd9)
}

func TestDecodeMemoryBudget(t *testing.T) {
	for _, c := range []struct {
		name    string
		raw     []byte
		refused bool
	}{
		{"16-bit RGBA PNG at the pixel limit", pngHeader(7071, 7071, 16, 6, 0), true},
		{"8-bit gray PNG at the budget", pngHeader(5792, 5792, 8, 0, 0), false},
		{"8-bit gray PNG over the budget (a later tRNS makes it NRGBA)", pngHeader(5793, 5793, 8, 0, 0), true},
		{"16-bit gray PNG at the budget", pngHeader(4096, 4096, 16, 0, 0), false},
		{"16-bit gray PNG over the budget", pngHeader(4097, 4097, 16, 0, 0), true},
		{"8-bit RGBA PNG at the budget", pngHeader(5792, 5792, 8, 6, 0), false},
		{"8-bit RGBA PNG over the budget", pngHeader(5793, 5793, 8, 6, 0), true},
		{"interlaced 8-bit RGBA PNG", pngHeader(5000, 4000, 8, 6, 1), true},
		{"lossy WebP, 24 MP", riff(webpChunk("VP8 ", vp8Header(6000, 4000))), false},
		{"lossless WebP over the budget", riff(webpChunk("VP8L", vp8lHeader(4100, 4100, false))), true},
		{"extended lossless WebP without alpha", riff(vp8x(4100, 4100, 0), webpChunk("VP8L", vp8lHeader(4100, 4100, false))), true},
		{"extended lossless WebP with alpha", riff(vp8x(4100, 4100, 0x10), webpChunk("VP8L", vp8lHeader(4100, 4100, true))), true},
		{"baseline 4:2:0 JPEG, 48 MP", jpegHeader(8000, 6000, 0xc0, 0x22, 0x11, 0x11), false},
		{"baseline 4:4:4 JPEG, 24 MP", jpegHeader(6000, 4000, 0xc0, 0x11, 0x11, 0x11), false},
		{"progressive 4:2:0 JPEG, 12 MP", jpegHeader(4000, 3000, 0xc2, 0x22, 0x11, 0x11), false},
		{"progressive 4:2:0 JPEG, 24 MP", jpegHeader(6000, 4000, 0xc2, 0x22, 0x11, 0x11), true},
		{"4-component JPEG, 24 MP", jpegHeader(6000, 4000, 0xc0, 0x11, 0x11, 0x11, 0x11), true},
	} {
		_, err := Sanitize(c.raw)
		if err == nil {
			t.Fatal(c.name, "header-only image accepted")
		}
		memory := strings.Contains(err.Error(), "memory")
		// An accepted header must pass DecodeConfig and fail only in Decode.
		if memory != c.refused || !c.refused && !strings.Contains(err.Error(), "damaged") {
			t.Fatal(c.name, err)
		}
	}
}

// TestDecodeEstimateCoversAllocation checks the estimates against what the
// decoders actually allocate.
func TestDecodeEstimateCoversAllocation(t *testing.T) {
	r := image.Rect(0, 0, 1024, 768)
	palette := make(color.Palette, 256)
	for i := range palette {
		palette[i] = color.Gray{uint8(i)}
	}
	encodings := map[string]func() ([]byte, error){}
	for name, img := range map[string]image.Image{
		"gray": image.NewGray(r), "gray16": image.NewGray16(r), "paletted": image.NewPaletted(r, palette),
		"nrgba": image.NewNRGBA(r), "nrgba64": image.NewNRGBA64(r), "rgba": image.NewRGBA(r), "rgba64": image.NewRGBA64(r),
	} {
		encodings["png "+name] = func() ([]byte, error) {
			var b bytes.Buffer
			err := png.Encode(&b, img)
			return b.Bytes(), err
		}
	}
	for name, img := range map[string]image.Image{"gray": image.NewGray(r), "gray16": image.NewGray16(r)} {
		// DecodeConfig reports Gray or Gray16, but tRNS decodes to NRGBA or NRGBA64.
		encodings["png tRNS "+name] = func() ([]byte, error) {
			var b bytes.Buffer
			err := png.Encode(&b, img)
			return withChunk(b.Bytes(), "tRNS", []byte{0, 0}), err
		}
	}
	for name, img := range map[string]image.Image{"gray": image.NewGray(r), "ycbcr 4:2:0": image.NewRGBA(r)} {
		encodings["jpeg "+name] = func() ([]byte, error) {
			var b bytes.Buffer
			err := jpeg.Encode(&b, img, nil)
			return b.Bytes(), err
		}
	}
	for name, encode := range encodings {
		raw, err := encode()
		if err != nil {
			t.Fatal(name, err)
		}
		cfg, kind, err := image.DecodeConfig(bytes.NewReader(raw))
		if err != nil {
			t.Fatal(name, err)
		}
		estimate := decodeBytes(raw, kind, cfg)
		var before, after runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&before)
		decoded, _, err := image.Decode(bytes.NewReader(raw))
		if err != nil {
			t.Fatal(name, err)
		}
		if strings.Contains(name, "tRNS") {
			switch decoded.(type) {
			case *image.NRGBA, *image.NRGBA64:
			default:
				t.Fatalf("%s decoded as %T", name, decoded)
			}
		}
		runtime.ReadMemStats(&after)
		allocated := int64(after.TotalAlloc - before.TotalAlloc)
		if allocated > estimate+256<<10 || estimate > 4*allocated {
			t.Fatal(name, "allocated", allocated, "estimated", estimate)
		}
	}
}

// TestSanitizeAllocationBudget bounds all allocation of one upload: the decode
// estimate, the scaled and oriented working images and the encoders.
func TestSanitizeAllocationBudget(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 6000, 1000))
	for i := range src.Pix {
		src.Pix[i] = byte(i)
	}
	var encoded bytes.Buffer
	if err := jpeg.Encode(&encoded, src, nil); err != nil {
		t.Fatal(err)
	}
	raw := withEXIF(encoded.Bytes(), 6)
	cfg, kind, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	clean, err := Sanitize(raw)
	if err != nil {
		t.Fatal(err)
	}
	runtime.ReadMemStats(&after)
	if clean.Width != 683 || clean.Height != 4096 {
		t.Fatal(clean.Width, clean.Height)
	}
	working := int64(2*4*4096*683 + 4*512*85)
	budget := decodeBytes(raw, kind, cfg) + working + int64(4*len(clean.JPEG)+4*len(clean.Thumbnail)) + 4<<20
	if allocated := int64(after.TotalAlloc - before.TotalAlloc); allocated > budget {
		t.Fatal("allocated", allocated, "budget", budget)
	}
}
