// Package trimlight implements the local protocol observed on one Trimlight Edge
// controller. Unknown formats fail closed; this is not a general API yet.
package trimlight

import (
	"bytes"
	"crypto/md5" // Wire-protocol integrity field, not password/security storage.
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"time"
	"unicode/utf8"
)

var hello = []byte{0x53, 1, 0, 0, 4, 4, 'K', 'G', 'Q', 'u'}
var editReply = []byte("SPTECH\x0b\x00\x00\x01\x00")

func encodeFrame(command byte, payload []byte, key byte) ([]byte, error) {
	if len(payload) > 65535 {
		return nil, errors.New("payload exceeds wire length")
	}
	frame := make([]byte, 27+len(payload))
	copy(frame, "SPTECH")
	frame[6] = command
	binary.BigEndian.PutUint16(frame[8:10], uint16(len(payload)))
	digest := md5.Sum(payload)
	copy(frame[10:26], digest[:])
	frame[26] = key
	for i, value := range payload {
		frame[27+i] = value ^ key
	}
	return frame, nil
}

func readSnapshotFrame(r io.Reader) ([]byte, error) {
	header := make([]byte, 10)
	if _, err := io.ReadFull(r, header); err != nil {
		return nil, err
	}
	if !bytes.Equal(header[:8], []byte("SPTECH\x04\x00")) {
		return nil, errors.New("unexpected snapshot frame")
	}
	body := make([]byte, 17+int(binary.BigEndian.Uint16(header[8:10])))
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, err
	}
	payload := make([]byte, len(body)-17)
	for i := range payload {
		payload[i] = body[17+i] ^ body[16]
	}
	digest := md5.Sum(payload)
	if !bytes.Equal(digest[:], body[:16]) {
		return nil, errors.New("snapshot checksum mismatch")
	}
	return payload, nil
}

func clockQuery(now time.Time) ([]byte, error) {
	if now.Year() < 2000 || now.Year() > 2255 {
		return nil, errors.New("year outside wire range")
	}
	if now.Weekday() == time.Sunday {
		return nil, errors.New("Sunday clock encoding has not been verified")
	}
	return []byte{0x15, 0, 7, byte(now.Year() - 2000), byte(now.Month()), byte(now.Day()), byte(now.Weekday()), byte(now.Hour()), byte(now.Minute()), byte(now.Second())}, nil
}

type Pattern struct {
	ID            byte   `json:"id"`
	Name          string `json:"name"`
	ParametersHex string `json:"uninterpreted_parameters_hex"`
}

type Schedule struct {
	Slot            int    `json:"slot"`
	Enabled         bool   `json:"enabled"`
	PatternID       byte   `json:"pattern_id"`
	PatternName     string `json:"pattern_name"`
	Repetition      byte   `json:"repetition_raw"`
	RepetitionLabel string `json:"repetition_interpretation"`
	Start           string `json:"start_time"`
	Off             string `json:"off_time"`
}

type Snapshot struct {
	QueriedAt         time.Time  `json:"queried_at"`
	ClockSyncPossible bool       `json:"clock_sync_may_have_occurred"`
	Patterns          []Pattern  `json:"patterns"`
	Schedules         []Schedule `json:"daily_schedules,omitempty"`
	Playback          *Playback  `json:"playback,omitempty"`
	LayoutNote        string     `json:"layout_note"`
	StableSHA256      string     `json:"stable_sha256"`
	RawHex            string     `json:"plain_hex"`
	raw               []byte
}

// Playback describes reported state, not physical light output. Parameter matches
// are candidates: multiple saved slots can contain the same effect, and OFF can
// retain parameters without displaying them.
type Playback struct {
	ModeRaw            byte   `json:"mode_raw"`
	Mode               string `json:"mode"`
	ParametersHex      string `json:"uninterpreted_parameters_hex"`
	MatchingPatternIDs []int  `json:"matching_pattern_ids"`
}

// ParseSnapshot uses the captured pattern layout. ScheduleOffset must be explicit
// because preceding variable-length records have not been mapped. -1 omits them.
func ParseSnapshot(raw []byte, scheduleOffset int) (*Snapshot, error) {
	if len(raw) < 26 || raw[0] != 0x15 || int(binary.BigEndian.Uint16(raw[1:3])) != len(raw)-3 {
		return nil, errors.New("invalid snapshot length or opcode")
	}
	count := int(raw[25])
	end := 26 + count*182
	if end > len(raw) {
		return nil, errors.New("truncated pattern records")
	}
	s := &Snapshot{RawHex: hex.EncodeToString(raw), raw: bytes.Clone(raw), LayoutNote: "Experimental captured layout; schedule offset is supplied explicitly."}
	digest := sha256.Sum256(raw[:len(raw)-7])
	s.StableSHA256 = hex.EncodeToString(digest[:])
	names := map[byte]string{}
	for offset := 26; offset < end; offset += 182 {
		record := raw[offset : offset+182]
		name := bytes.SplitN(record[1:26], []byte{0}, 2)[0]
		if !utf8.Valid(name) {
			return nil, errors.New("invalid pattern name")
		}
		if _, exists := names[record[0]]; exists {
			return nil, errors.New("duplicate pattern ID")
		}
		names[record[0]] = string(name)
		s.Patterns = append(s.Patterns, Pattern{record[0], string(name), hex.EncodeToString(record[26:])})
	}
	if scheduleOffset == -1 {
		return s, nil
	}
	if scheduleOffset < end || scheduleOffset > len(raw)-14 {
		return nil, errors.New("schedule offset outside snapshot suffix")
	}
	for i := 0; i < 2; i++ {
		r := raw[scheduleOffset+i*7 : scheduleOffset+(i+1)*7]
		if r[0] > 1 || r[3] > 23 || r[4] > 59 || r[5] > 23 || r[6] > 59 {
			return nil, errors.New("invalid schedule fields")
		}
		label := "unknown"
		if r[2] == 0 {
			label = "today only (observed)"
		}
		if r[2] == 1 {
			label = "every day (observed)"
		}
		s.Schedules = append(s.Schedules, Schedule{i + 1, r[0] == 1, r[1], names[r[1]], r[2], label, fmt.Sprintf("%02d:%02d", r[3], r[4]), fmt.Sprintf("%02d:%02d", r[5], r[6])})
	}
	// These offsets are supported only by the observed 18-record layout and an
	// explicitly selected schedule offset. Do not extrapolate to other layouts.
	if len(raw) == 3492 && count == 18 && scheduleOffset == 3306 {
		mode := "unknown"
		switch raw[3] {
		case 0:
			mode = "off"
		case 1:
			mode = "manual"
		case 2:
			mode = "timer"
		}
		playback := &Playback{
			ModeRaw: raw[3], Mode: mode,
			ParametersHex:      hex.EncodeToString(raw[3320:3476]),
			MatchingPatternIDs: []int{},
		}
		for _, pattern := range s.Patterns {
			if pattern.ParametersHex == playback.ParametersHex {
				playback.MatchingPatternIDs = append(playback.MatchingPatternIDs, int(pattern.ID))
			}
		}
		s.Playback = playback
	}
	return s, nil
}

func probePayload(minute byte, now time.Time) ([]byte, error) {
	if minute != 3 && minute != 4 {
		return nil, errors.New("probe only permits off-minute 3 or 4")
	}
	return []byte{0x13, 0, 10, 1, 0, 1, 0, 0, 2, 0, minute, byte(now.Month()), byte(now.Day())}, nil
}

func stable(s *Snapshot) []byte { return s.raw[:len(s.raw)-7] }

func validFingerprint(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}
func validateProbeBaseline(s *Snapshot, expectedFingerprint string) error {
	if !validFingerprint(expectedFingerprint) || s.StableSHA256 != expectedFingerprint {
		return errors.New("fresh controller state differs from the reviewed baseline fingerprint; no write permitted")
	}
	expected := []byte{0, 1, 0, 0, 2, 0, 3}
	if len(s.raw) != 3492 || !bytes.Equal(s.raw[3313:3320], expected) || len(s.Patterns) != 18 || s.Patterns[0].ID != 1 || s.Patterns[17].ID != 18 {
		return errors.New("controller differs from supported disabled-slot probe layout; no write permitted")
	}
	return nil
}

func playbackModePayload(mode byte) ([]byte, error) {
	if mode != 0 && mode != 2 {
		return nil, errors.New("only captured OFF and TIMER commands are supported")
	}
	return []byte{2, 0, 1, mode}, nil
}

func savedPlaybackPayload(id byte) ([]byte, error) {
	if id < 1 || id > 18 {
		return nil, errors.New("saved playback requires a supported slot ID")
	}
	return []byte{4, 0, 1, id}, nil
}

func validatePlaybackBaseline(s *Snapshot, fingerprint string, target byte) (byte, error) {
	if !validFingerprint(fingerprint) || s.StableSHA256 != fingerprint {
		return 0, errors.New("fresh controller state differs from the reviewed baseline fingerprint; no write permitted")
	}
	if len(s.raw) != 3492 || len(s.Patterns) != 18 || s.Playback == nil || s.Playback.Mode != "timer" || len(s.Playback.MatchingPatternIDs) != 1 {
		return 0, errors.New("playback verification requires the reviewed TIMER layout and an unambiguous original effect")
	}
	for i, p := range s.Patterns {
		if int(p.ID) != i+1 {
			return 0, errors.New("playback verification requires the reviewed saved-slot order")
		}
	}
	if target < 1 || target > 18 || s.Patterns[int(target)-1].ParametersHex == s.Playback.ParametersHex {
		return 0, errors.New("select an existing target slot with parameters different from the original effect")
	}
	return byte(s.Playback.MatchingPatternIDs[0]), nil
}

// Only mode and current effect parameters may differ during the experiment.
// All other bytes (including unknown fields) remain pinned to the baseline.
func playbackInvariants(before, current *Snapshot) bool {
	return before != nil && current != nil && len(before.raw) == 3492 && len(current.raw) == 3492 &&
		bytes.Equal(before.raw[:3], current.raw[:3]) &&
		bytes.Equal(before.raw[4:3320], current.raw[4:3320]) &&
		bytes.Equal(before.raw[3476:3485], current.raw[3476:3485])
}

func verifyPlaybackState(before, current *Snapshot, mode byte, parameters []byte) error {
	if !playbackInvariants(before, current) {
		return errors.New("non-playback controller fields changed; no further control writes permitted")
	}
	if current.raw[3] != mode || !bytes.Equal(current.raw[3320:3476], parameters) {
		return errors.New("playback readback differs from intended mode or effect parameters")
	}
	return nil
}
