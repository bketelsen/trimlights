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

// app0 is an APP0 segment; "JFIF" marks JFIF.
func app0(id string) []byte {
	data := append([]byte(id+"\x00"), 1, 2, 0, 0, 1, 0, 1, 0, 0)
	return append(binary.BigEndian.AppendUint16([]byte{0xff, 0xe0}, uint16(2+len(data))), data...)
}

// adobe is an APP14 Adobe segment; transform 0 means RGB.
func adobe(transform byte) []byte {
	data := []byte{'A', 'd', 'o', 'b', 'e', 0, 100, 0, 0, 0, 0, transform}
	return append(binary.BigEndian.AppendUint16([]byte{0xff, 0xee}, uint16(2+len(data))), data...)
}

// afterSOF inserts segments right after the first SOF0/1/2 segment.
func afterSOF(raw []byte, segments ...[]byte) []byte {
	for i := 2; i+4 <= len(raw); {
		n := int(binary.BigEndian.Uint16(raw[i+2 : i+4]))
		end := i + 2 + n
		if raw[i+1] >= 0xc0 && raw[i+1] <= 0xc2 {
			out := append([]byte{}, raw[:end]...)
			for _, s := range segments {
				out = append(out, s...)
			}
			return append(out, raw[end:]...)
		}
		i = end
	}
	panic("no SOF")
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
		{"lossless WebP, 1 MP", riff(webpChunk("VP8L", vp8lHeader(1000, 1000, false))), true},
		{"lossless WebP, 128x128", riff(webpChunk("VP8L", vp8lHeader(128, 128, false))), false},
		{"lossy WebP with lossless alpha, 1 MP", riff(vp8x(1000, 1000, 0x10), webpChunk("ALPH", []byte{1}), webpChunk("VP8 ", vp8Header(1000, 1000))), true},
		{"baseline 4:4:4 JPEG, 24 MP, later Adobe RGB", afterSOF(jpegHeader(6000, 4000, 0xc0, 0x11, 0x11, 0x11), app0("XXXX"), adobe(0)), true},
		{"baseline 4:4:4 JPEG, 24 MP, later Adobe YCbCr", afterSOF(jpegHeader(6000, 4000, 0xc0, 0x11, 0x11, 0x11), adobe(1)), false},
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

func TestPNGChunkBeforeIHDRRefused(t *testing.T) {
	// An unknown chunk whose bytes sit where IHDR fields would be: color type
	// 3 (paletted) and no interlace. The real IHDR is 16-bit RGBA at the pixel limit.
	ihdr := pngHeader(7071, 7071, 16, 6, 0)
	data := []byte{0, 0, 0, 0, 0, 0, 0, 0, 8, 3, 0, 0, 0}
	chunk := binary.BigEndian.AppendUint32(nil, uint32(len(data)))
	chunk = append(chunk, "xxXx"...)
	chunk = append(chunk, data...)
	chunk = binary.BigEndian.AppendUint32(chunk, crc32.ChecksumIEEE(chunk[4:]))
	raw := append(append(append([]byte{}, ihdr[:8]...), chunk...), ihdr[8:]...)
	if raw[25] != 3 || raw[28] != 0 {
		t.Fatal("fixture does not spoof the IHDR offsets")
	}
	cfg, kind, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil || kind != "png" || cfg.Width != 7071 || cfg.ColorModel != color.NRGBA64Model {
		t.Fatal("image/png no longer accepts a chunk before IHDR", cfg, err)
	}
	if n := decodeBytes(raw, kind, cfg); n != -1 {
		t.Fatal(n)
	}
	if _, err := Sanitize(raw); err != ErrFormat {
		t.Fatal(err)
	}
	// A first IHDR whose size disagrees with DecodeConfig is refused too.
	if n := decodeBytes(pngHeader(10, 10, 8, 6, 0), kind, image.Config{Width: 7071, Height: 7071}); n != -1 {
		t.Fatal(n)
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
	encodings["jpeg JFIF, later Adobe RGB"] = func() ([]byte, error) {
		// DecodeConfig stops at SOF after JFIF and reports YCbCr; the later
		// non-JFIF APP0 and Adobe transform 0 make the decoder convert to RGBA.
		var b bytes.Buffer
		err := jpeg.Encode(&b, image.NewRGBA(r), nil)
		raw := append([]byte{0xff, 0xd8}, app0("JFIF")...)
		return afterSOF(append(raw, b.Bytes()[2:]...), app0("XXXX"), adobe(0)), err
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
		var decoded image.Image
		allocated, err := measureAlloc(func() (err error) {
			decoded, _, err = image.Decode(bytes.NewReader(raw))
			return err
		})
		if err != nil {
			t.Fatal(name, err)
		}
		if _, ok := decoded.(*image.RGBA); strings.Contains(name, "Adobe RGB") && (!ok || cfg.ColorModel != color.YCbCrModel) {
			t.Fatalf("%s: config %v, decoded as %T", name, cfg.ColorModel, decoded)
		}
		if strings.Contains(name, "tRNS") {
			switch decoded.(type) {
			case *image.NRGBA, *image.NRGBA64:
			default:
				t.Fatalf("%s decoded as %T", name, decoded)
			}
		}
		// The 1 MiB margin absorbs process-wide noise; tests here never run in parallel.
		if allocated > estimate+1<<20 || estimate > 4*allocated {
			t.Fatal(name, "allocated", allocated, "estimated", estimate)
		}
	}
}

// TestSanitizeAllocationBudget bounds all allocation of one upload: the decode
// estimate, the scaled and oriented working images and the encoders, with an
// 8 MiB margin. Reading the source through a per-sample interface, as before,
// allocates about 72 MB here against a bound of about 45 MB.
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
	var clean PhotoImage
	allocated, err := measureAlloc(func() (err error) {
		clean, err = Sanitize(raw)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	if clean.Width != 683 || clean.Height != 4096 {
		t.Fatal(clean.Width, clean.Height)
	}
	working := int64(2*4*4096*683 + 4*512*85)
	budget := decodeBytes(raw, kind, cfg) + working + int64(4*len(clean.JPEG)+4*len(clean.Thumbnail)) + 8<<20
	if allocated > budget {
		t.Fatal("allocated", allocated, "budget", budget)
	}
}

func TestVP8LHuffmanBound(t *testing.T) {
	// Per group: 5 trees of 536 bytes, 2*3136-5 nodes of 8 bytes and 3136
	// 4-byte codes; per decoded group: 3136 code lengths and 5 small trees.
	if vp8lGroupBytes != 65360 || vp8lScratchBytes != 17664 {
		t.Fatal(vp8lGroupBytes, vp8lScratchBytes)
	}
	tiles := int64(1024 * 1024)
	want := int64(4096*4096*8) + 2600*65360 + 2600*17666 + 4*tiles + 1<<20
	if got := vp8lDecodeBytes(4096, 4096); got != want {
		t.Fatal(got, want)
	}
	// Small images keep only the groups that their tiles can reference.
	if got := vp8lDecodeBytes(8, 8); got != 8*8*8+4*65360+2600*17666+16+1<<20 {
		t.Fatal(got)
	}
	// The largest square lossless WebP that fits the budget is tiny.
	n := int64(1)
	for vp8lDecodeBytes(n+1, n+1) <= MaxDecodeBytes {
		n++
	}
	if n < 100 || n > 200 {
		t.Fatal(n)
	}
}

// bitWriter writes the least significant bit first, as VP8L reads.
type bitWriter struct {
	b []byte
	n uint
}

func (w *bitWriter) put(v uint32, bits uint) {
	for i := range bits {
		if w.n%8 == 0 {
			w.b = append(w.b, 0)
		}
		w.b[len(w.b)-1] |= byte(v>>i&1) << (w.n % 8)
		w.n++
	}
}

// simpleTree writes a one-symbol Huffman tree; reading its symbol takes no bits.
func (w *bitWriter) simpleTree(symbol uint32) {
	w.put(1, 1) // simple code
	w.put(0, 1) // one symbol
	w.put(1, 1) // 8-bit symbol
	w.put(symbol, 8)
}

// TestVP8LManyGroups decodes a lossless stream whose meta image references
// Huffman group 999, so the decoder builds 1000 groups for 128x128 pixels.
func TestVP8LManyGroups(t *testing.T) {
	var w bitWriter
	w.put(0x2f, 8)
	w.put(127, 14)
	w.put(127, 14)
	w.put(0, 4)       // no alpha, version 0
	w.put(0, 1)       // no transform
	w.put(0, 1)       // no color cache
	w.put(1, 1)       // meta image
	w.put(0, 3)       // 4x4 tiles: 32x32 meta pixels
	w.put(0, 1)       // meta image: no color cache
	w.simpleTree(231) // green: low byte of group 999
	w.simpleTree(3)   // red: high byte of group 999
	w.simpleTree(0)
	w.simpleTree(0)
	w.simpleTree(0)
	for range 1000 * 5 {
		w.simpleTree(0)
	}
	raw := riff(webpChunk("VP8L", w.b))
	cfg, kind, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil || kind != "webp" {
		t.Fatal(kind, err)
	}
	estimate := decodeBytes(raw, kind, cfg)
	allocated, err := measureAlloc(func() error {
		_, _, err := image.Decode(bytes.NewReader(raw))
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	// 1000 stored groups of 2680 bytes need more than 2.6 MB.
	if allocated < 1000*2680 || allocated > estimate {
		t.Fatal("allocated", allocated, "estimated", estimate)
	}
}

// measureAlloc runs f alone after a collection and returns the bytes that the
// whole process allocated meanwhile. Callers must not run in parallel.
func measureAlloc(f func() error) (int64, error) {
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	err := f()
	runtime.ReadMemStats(&after)
	return int64(after.TotalAlloc - before.TotalAlloc), err
}

// orientedView is the earlier per-sample reference: it reads the source
// through orientedSource for every sample.
type orientedView struct {
	src image.Image
	o   int
}

func (v orientedView) ColorModel() color.Model { return v.src.ColorModel() }
func (v orientedView) Bounds() image.Rectangle {
	b := v.src.Bounds()
	if v.o >= 5 {
		return image.Rect(0, 0, b.Dy(), b.Dx())
	}
	return image.Rect(0, 0, b.Dx(), b.Dy())
}
func (v orientedView) At(x, y int) color.Color {
	b := v.src.Bounds()
	sx, sy := orientedSource(v.o, x, y, b.Dx(), b.Dy())
	return v.src.At(b.Min.X+sx, b.Min.Y+sy)
}

// TestOrientAfterScalingMatchesReference downscales an odd, non-square image
// and checks that orienting the scaled image equals scaling the oriented one.
func TestOrientAfterScalingMatchesReference(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 1001, 403))
	seed := uint32(1)
	for i := range src.Pix {
		seed = seed*1664525 + 1013904223
		src.Pix[i] = byte(seed >> 24)
	}
	for o := 1; o <= 8; o++ {
		got := orient(resize(src, 301), o)
		want := resize(orientedView{src, o}, 301)
		if got.Bounds() != want.Bounds() {
			t.Fatal(o, got.Bounds(), want.Bounds())
		}
		worst := 0
		for i := range got.Pix {
			worst = max(worst, int(got.Pix[i])-int(want.Pix[i]), int(want.Pix[i])-int(got.Pix[i]))
		}
		// The sample positions mirror exactly, so amd64 gives identical
		// pixels. The bilinear weights come from mirrored float coordinates,
		// and another architecture (for example with fused multiply-add) can
		// round one of them differently; allow 1 level for that.
		if worst > 1 {
			t.Fatal(o, "max channel difference", worst)
		}
	}
}
