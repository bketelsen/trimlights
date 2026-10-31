// Package pattern defines versioned household designs and logical-pixel previews.
// Preview agreement does not imply verified controller playback.
package pattern

import (
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

type Run struct {
	Color string `json:"color"`
	Count int    `json:"count"`
}
type Native struct {
	ID            byte      `json:"id"`
	Name          string    `json:"name"`
	ParametersHex string    `json:"parameters_hex"`
	SnapshotAt    time.Time `json:"snapshot_at"`
}
type Design struct {
	Schema     int     `json:"schema"`
	Name       string  `json:"name"`
	Favorite   bool    `json:"favorite"`
	Effect     string  `json:"effect"`
	Brightness int     `json:"brightness"`
	Speed      int     `json:"speed"`
	Reverse    bool    `json:"reverse"`
	Palette    []Run   `json:"palette"`
	Native     *Native `json:"native,omitempty"`
}

var colorPattern = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// ValidName reports whether s is a household display name: 1–80 characters of
// valid UTF-8, no outer white space, no control (Cc) and no format (Cf)
// characters. Zero-width joiner and non-joiner stay allowed for emoji sequences
// and joined scripts. web/src/pattern.ts applies the same rule; both must pass
// web/src/name-vectors.json.
func ValidName(s string) bool {
	if s == "" || !utf8.ValidString(s) || utf8.RuneCountInString(s) > 80 || strings.TrimSpace(s) != s {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) || (unicode.Is(unicode.Cf, r) && r != '\u200c' && r != '\u200d') {
			return false
		}
	}
	return true
}

func (d Design) Validate() error {
	if d.Schema != 1 {
		return errors.New("unsupported pattern schema")
	}
	if !ValidName(d.Name) {
		return errors.New("name must contain 1–80 characters without leading or trailing spaces, control or invisible format characters")
	}
	if d.Effect == "native" {
		if d.Native == nil || len(d.Palette) != 0 || d.Brightness != 100 || d.Speed != 0 || d.Reverse {
			return errors.New("native patterns retain opaque parameters; palette and effect settings cannot be edited")
		}
		raw, err := hex.DecodeString(d.Native.ParametersHex)
		if err != nil || len(raw) != 156 || d.Native.SnapshotAt.IsZero() || !utf8.ValidString(d.Native.Name) || utf8.RuneCountInString(d.Native.Name) > 80 {
			return errors.New("invalid captured native pattern")
		}
		return nil
	}
	if d.Native != nil {
		return errors.New("custom design cannot contain native parameters")
	}
	if d.Effect != "static" && d.Effect != "chase" {
		return errors.New("unsupported effect; choose static or chase")
	}
	if d.Brightness < 0 || d.Brightness > 100 || d.Speed < 0 || d.Speed > 100 {
		return errors.New("brightness and speed must be between 0 and 100")
	}
	if len(d.Palette) < 1 || len(d.Palette) > 30 {
		return errors.New("palette must contain 1–30 color runs")
	}
	for _, run := range d.Palette {
		if !colorPattern.MatchString(run.Color) || run.Count < 1 || run.Count > 60 {
			return errors.New("each color run needs a #RRGGBB color and 1–60 bulbs")
		}
	}
	return nil
}

// Render produces RGB colors for logical addresses starting at zero. One chase
// step shifts the repeated palette by one pixel. Time is an integer millisecond
// tick, bounded to one day so JS and Go integer arithmetic agree exactly.
func Render(d Design, count int, tickMS int) ([]string, error) {
	if err := d.Validate(); err != nil {
		return nil, err
	}
	if d.Effect == "native" {
		return nil, errors.New("native effect preview has not been decoded")
	}
	if count < 1 || count > 2048 || tickMS < 0 || tickMS > 86400000 {
		return nil, errors.New("preview count or time outside supported range")
	}
	base := []string{}
	for _, run := range d.Palette {
		raw, _ := hex.DecodeString(run.Color[1:])
		scaled := [3]int{}
		for i, c := range raw {
			scaled[i] = (int(c)*d.Brightness + 50) / 100
		}
		color := fmt.Sprintf("#%02x%02x%02x", scaled[0], scaled[1], scaled[2])
		for i := 0; i < run.Count; i++ {
			base = append(base, color)
		}
	}
	shift := 0
	if d.Effect == "chase" && d.Speed > 0 {
		shift = tickMS * d.Speed / 10000
	}
	out := make([]string, count)
	for i := range out {
		index := i
		if d.Reverse {
			index = len(base) - 1 - index
		}
		index = (index - shift) % len(base)
		if index < 0 {
			index += len(base)
		}
		out[i] = base[index]
	}
	return out, nil
}
