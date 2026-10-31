package house

import (
	"encoding/binary"
	"image"
	"image/color"
)

// MaxDecodeBytes bounds the estimated memory of one full decode. With the
// 12 MiB upload and the 4096-pixel working image (at most 64 MiB), one upload
// stays near 210 MiB at its peak.
const MaxDecodeBytes = 128 << 20

// decodeBytes returns a conservative upper estimate of the bytes that
// image.Decode allocates for raw, from the headers alone.
func decodeBytes(raw []byte, kind string, cfg image.Config) int64 {
	pixels := int64(cfg.Width) * int64(cfg.Height)
	switch kind {
	case "jpeg":
		if n, ok := jpegDecodeBytes(raw, cfg); ok {
			return n
		}
		// Worst case: 4 progressive components plus a converted copy.
		return pixels * 24
	case "png":
		per := int64(8)
		if _, ok := cfg.ColorModel.(color.Palette); ok {
			per = 1
		} else {
			switch cfg.ColorModel {
			case color.GrayModel:
				per = 1
			case color.Gray16Model:
				per = 2
			case color.RGBAModel, color.NRGBAModel:
				per = 4
			}
		}
		// IHDR interlace method: Adam7 holds pass images beside the result.
		if len(raw) > 28 && raw[28] != 0 {
			per *= 2
		}
		return pixels * per
	case "webp":
		switch cfg.ColorModel {
		case color.YCbCrModel:
			// 4:2:0 planes, padded to 16-pixel macroblocks.
			return pixels * 2
		case color.NYCbCrAModel:
			// Alpha can decode through a 4-byte lossless image first.
			return pixels * 7
		}
		// Lossless: 4-byte pixels plus transform work.
		return pixels * 8
	}
	return pixels * 24
}

// jpegDecodeBytes reads the SOF0/1/2 frame header and sums what image/jpeg
// allocates: component planes at their sampling factors and MCU padding, a
// 4-byte converted image for RGB or 4-component input, and 4 bytes of
// coefficients per sample for progressive input.
func jpegDecodeBytes(b []byte, cfg image.Config) (int64, bool) {
	for i := 2; i+4 <= len(b); {
		if b[i] != 0xff {
			return 0, false
		}
		marker := b[i+1]
		if marker == 0xff {
			i++
			continue
		}
		if marker == 0xda || marker == 0xd9 {
			return 0, false
		}
		if marker == 0x01 || marker >= 0xd0 && marker <= 0xd7 {
			i += 2
			continue
		}
		n := int(binary.BigEndian.Uint16(b[i+2 : i+4]))
		if n < 2 || i+2+n > len(b) {
			return 0, false
		}
		part := b[i+4 : i+2+n]
		if marker != 0xc0 && marker != 0xc1 && marker != 0xc2 {
			i += 2 + n
			continue
		}
		if len(part) < 6 {
			return 0, false
		}
		comps := int(part[5])
		if comps < 1 || comps > 4 || len(part) < 6+3*comps {
			return 0, false
		}
		h, v := make([]int64, comps), make([]int64, comps)
		maxH, maxV := int64(1), int64(1)
		for c := range comps {
			hv := part[6+3*c+1]
			h[c], v[c] = int64(hv>>4), int64(hv&0x0f)
			if comps == 1 {
				// image/jpeg ignores the factors of a single component.
				h[c], v[c] = 1, 1
			}
			if h[c] < 1 || h[c] > 4 || v[c] < 1 || v[c] > 4 {
				return 0, false
			}
			maxH, maxV = max(maxH, h[c]), max(maxV, v[c])
		}
		mxx := (int64(cfg.Width) + 8*maxH - 1) / (8 * maxH)
		myy := (int64(cfg.Height) + 8*maxV - 1) / (8 * maxV)
		padded := 64 * maxH * maxV * mxx * myy
		var planes int64
		for c := range comps {
			planes += 64 * h[c] * v[c] * mxx * myy
		}
		if comps >= 3 && (h[1] != h[2] || v[1] != v[2] || h[0] != maxH || v[0] != maxV || !standardRatio(maxH/h[1], maxV/v[1])) {
			// Unusual factors decode into full-size 4:4:4 planes.
			planes = 3*padded + 64*h[comps-1]*v[comps-1]*mxx*myy*int64(comps-3)
		}
		total := planes
		if comps == 4 || comps == 3 && cfg.ColorModel == color.RGBAModel {
			total += 4 * padded
		}
		if marker == 0xc2 {
			total += 4 * planes
		}
		return total, true
	}
	return 0, false
}

// standardRatio reports whether image/jpeg has a YCbCr subsample ratio for the
// chroma factors; other ratios use flex mode.
func standardRatio(h, v int64) bool {
	switch h<<4 | v {
	case 0x11, 0x12, 0x21, 0x22, 0x41, 0x42:
		return true
	}
	return false
}
