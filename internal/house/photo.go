package house

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	_ "image/png"
	"math"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

const MaxUpload = 12 << 20
const MaxPixels = 50_000_000

var ErrFormat = errors.New("choose a JPEG, PNG or WebP photo; for HEIC/HEIF, export as JPEG in Photos or Preview, or select it through the supported-format photo picker")

type PhotoImage struct {
	JPEG, Thumbnail []byte
	Width, Height   int
}

func Sanitize(raw []byte) (PhotoImage, error) {
	if len(raw) > MaxUpload {
		return PhotoImage{}, errors.New("photo exceeds 12 MiB; export a smaller JPEG")
	}
	cfg, kind, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil || !(kind == "jpeg" || kind == "png" || kind == "webp") {
		return PhotoImage{}, ErrFormat
	}
	if cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 16384 || cfg.Height > 16384 || int64(cfg.Width)*int64(cfg.Height) > MaxPixels {
		return PhotoImage{}, errors.New("photo exceeds 50 million pixels or 16384 pixels per edge; resize before uploading")
	}
	if decodeBytes(raw, kind, cfg) > MaxDecodeBytes {
		return PhotoImage{}, errors.New("photo needs too much memory to decode (for example a 16-bit PNG or a large progressive JPEG); export it as a standard JPEG")
	}
	src, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return PhotoImage{}, errors.New("photo is damaged or incomplete")
	}
	if src.Bounds().Dx() != cfg.Width || src.Bounds().Dy() != cfg.Height {
		return PhotoImage{}, errors.New("photo dimensions changed while decoding")
	}
	// Scale the concrete decoded type (fast paths, no per-sample allocation),
	// then apply the orientation to the bounded working image.
	full := orient(resize(src, 4096), orientation(raw, kind))
	thumb := resize(full, 512)
	var a, b bytes.Buffer
	if err = jpeg.Encode(&a, full, &jpeg.Options{Quality: 88}); err != nil {
		return PhotoImage{}, err
	}
	if err = jpeg.Encode(&b, thumb, &jpeg.Options{Quality: 80}); err != nil {
		return PhotoImage{}, err
	}
	return PhotoImage{a.Bytes(), b.Bytes(), full.Bounds().Dx(), full.Bounds().Dy()}, nil
}
func resize(src image.Image, edge int) *image.RGBA {
	b := src.Bounds()
	ratio := math.Min(1, float64(edge)/float64(max(b.Dx(), b.Dy())))
	out := image.NewRGBA(image.Rect(0, 0, max(1, int(math.Round(float64(b.Dx())*ratio))), max(1, int(math.Round(float64(b.Dy())*ratio)))))
	draw.Draw(out, out.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	draw.ApproxBiLinear.Scale(out, out.Bounds(), src, b, draw.Over, nil)
	return out
}

// orient returns m with EXIF orientation o applied.
func orient(m *image.RGBA, o int) *image.RGBA {
	if o < 2 || o > 8 {
		return m
	}
	w, h := m.Rect.Dx(), m.Rect.Dy()
	dw, dh := w, h
	if o >= 5 {
		dw, dh = h, w
	}
	out := image.NewRGBA(image.Rect(0, 0, dw, dh))
	for y := range dh {
		for x := range dw {
			sx, sy := orientedSource(o, x, y, w, h)
			s := m.PixOffset(m.Rect.Min.X+sx, m.Rect.Min.Y+sy)
			copy(out.Pix[y*out.Stride+4*x:y*out.Stride+4*x+4], m.Pix[s:s+4])
		}
	}
	return out
}

// orientedSource maps a pixel of the oriented image to its source pixel in a
// w by h image.
func orientedSource(o, x, y, w, h int) (int, int) {
	switch o {
	case 2:
		return w - 1 - x, y
	case 3:
		return w - 1 - x, h - 1 - y
	case 4:
		return x, h - 1 - y
	case 5:
		return y, x
	case 6:
		return y, h - 1 - x
	case 7:
		return w - 1 - y, h - 1 - x
	case 8:
		return w - 1 - y, x
	}
	return x, y
}

// Read only the bounded TIFF IFD0 orientation SHORT; never follow pointers to
// GPS, thumbnails or other directories. Re-encoding discards all input metadata.
func tiffOrientation(b []byte) int {
	b = bytes.TrimPrefix(b, []byte("Exif\x00\x00"))
	if len(b) < 8 {
		return 1
	}
	var order binary.ByteOrder
	switch string(b[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return 1
	}
	if order.Uint16(b[2:4]) != 42 {
		return 1
	}
	offset := uint64(order.Uint32(b[4:8]))
	if offset+2 > uint64(len(b)) {
		return 1
	}
	n := uint64(order.Uint16(b[offset : offset+2]))
	if n > 4096 || offset+2+n*12 > uint64(len(b)) {
		return 1
	}
	for i := uint64(0); i < n; i++ {
		entry := b[offset+2+i*12 : offset+2+(i+1)*12]
		if order.Uint16(entry[:2]) == 0x112 && order.Uint16(entry[2:4]) == 3 && order.Uint32(entry[4:8]) == 1 {
			v := int(order.Uint16(entry[8:10]))
			if v >= 1 && v <= 8 {
				return v
			}
		}
	}
	return 1
}
func orientation(b []byte, kind string) int {
	switch kind {
	case "jpeg":
		for i := 2; i+4 <= len(b); {
			if b[i] != 0xff {
				return 1
			}
			marker := b[i+1]
			if marker == 0xff {
				i++
				continue
			}
			if marker == 0xda || marker == 0xd9 {
				return 1
			}
			if marker == 0x01 || marker >= 0xd0 && marker <= 0xd7 {
				i += 2
				continue
			}
			n := int(binary.BigEndian.Uint16(b[i+2 : i+4]))
			if n < 2 || i+2+n > len(b) {
				return 1
			}
			part := b[i+4 : i+2+n]
			if marker == 0xe1 && bytes.HasPrefix(part, []byte("Exif\x00\x00")) {
				return tiffOrientation(part)
			}
			i += 2 + n
		}
	case "png":
		for i := 8; i+12 <= len(b); {
			n := uint64(binary.BigEndian.Uint32(b[i : i+4]))
			if n > uint64(len(b)-i-12) {
				return 1
			}
			end := i + 8 + int(n)
			if string(b[i+4:i+8]) == "eXIf" {
				return tiffOrientation(b[i+8 : end])
			}
			i = end + 4
		}
	case "webp":
		for i := 12; i+8 <= len(b); {
			n := uint64(binary.LittleEndian.Uint32(b[i+4 : i+8]))
			if n > uint64(len(b)-i-8) {
				return 1
			}
			end := i + 8 + int(n)
			if string(b[i:i+4]) == "EXIF" {
				return tiffOrientation(b[i+8 : end])
			}
			i = end + int(n%2)
		}
	}
	return 1
}
