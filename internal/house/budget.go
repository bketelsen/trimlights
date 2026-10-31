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
		// Use the IHDR bit depth and color type, not cfg.ColorModel: DecodeConfig
		// stops after IHDR, and a later tRNS chunk turns gray or truecolor into
		// NRGBA or NRGBA64. Budget that worst case for every non-paletted PNG.
		if len(raw) < 29 {
			return pixels * 16
		}
		depth, colorType, interlace := raw[24], raw[25], raw[28]
		per := int64(4)
		if colorType == 3 {
			per = 1
		} else if depth == 16 {
			per = 8
		}
		// Adam7 holds pass images beside the result.
		if interlace != 0 {
			per *= 2
		}
		return pixels * per
	case "webp":
		return webpDecodeBytes(raw, pixels)
	}
	return pixels * 24
}

// webpDecodeBytes walks the RIFF chunks to the image chunk, because a VP8X
// header reports YCbCr or NYCbCrA even when the image chunk is VP8L.
func webpDecodeBytes(b []byte, pixels int64) int64 {
	alpha := int64(0)
	for i, n := 12, 0; i+8 <= len(b) && n < 64; n++ {
		size := uint64(binary.LittleEndian.Uint32(b[i+4 : i+8]))
		if size > uint64(len(b)-i-8) {
			break
		}
		data := b[i+8 : i+8+int(size)]
		switch string(b[i : i+4]) {
		case "ALPH":
			// One byte per pixel; compression method 1 decodes through a
			// lossless image first.
			alpha = pixels
			if len(data) > 0 && data[0]&0x03 == 1 {
				alpha += vp8lDecodeBytes(pixels)
			}
		case "VP8 ":
			// 4:2:0 planes padded to 16-pixel macroblocks, plus the partitions
			// that the decoder reads into memory.
			return pixels*2 + int64(len(b)) + alpha
		case "VP8L":
			return vp8lDecodeBytes(pixels)
		}
		i += 8 + int(size) + int(size%2)
	}
	// No image chunk: the decode fails, but budget the worst case.
	return pixels*3 + int64(len(b)) + 2*vp8lDecodeBytes(pixels)
}

// vp8lDecodeBytes covers a lossless image: 4-byte pixels and an expanded
// copy from the color-indexing transform.
func vp8lDecodeBytes(pixels int64) int64 {
	return pixels * 8
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
