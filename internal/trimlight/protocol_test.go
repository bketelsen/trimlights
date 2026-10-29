package trimlight

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"
)

func fixture(t *testing.T) []byte {
	t.Helper()
	b, e := os.ReadFile("testdata/snapshot.bin")
	if e != nil {
		t.Fatal(e)
	}
	return b
}

func TestSyntheticProtocolFrames(t *testing.T) {
	b, e := os.ReadFile("testdata/frames.json")
	if e != nil {
		t.Fatal(e)
	}
	var vectors []struct {
		Command        byte
		XOR            byte
		Payload, Frame string
	}
	if e = json.Unmarshal(b, &vectors); e != nil {
		t.Fatal(e)
	}
	if len(vectors) != 22 {
		t.Fatalf("expected 22 synthetic frames, got %d", len(vectors))
	}
	for _, v := range vectors {
		payload, e := hex.DecodeString(v.Payload)
		if e != nil {
			t.Fatal(e)
		}
		frame, e := encodeFrame(v.Command, payload, v.XOR)
		if e != nil {
			t.Fatal(e)
		}
		if hex.EncodeToString(frame) != v.Frame {
			t.Fatal("Go encoding differs from synthetic reference frame")
		}
		if v.Command == 4 {
			decoded, e := readSnapshotFrame(bytes.NewReader(frame))
			if e != nil || !bytes.Equal(decoded, payload) {
				t.Fatalf("round-trip: %v", e)
			}
			frame[len(frame)-1] ^= 1
			if _, e = readSnapshotFrame(bytes.NewReader(frame)); e == nil {
				t.Fatal("corrupted checksum accepted")
			}
		}
	}
}

func TestSnapshotAndBounds(t *testing.T) {
	raw := fixture(t)
	s, e := ParseSnapshot(raw, 3306)
	if e != nil {
		t.Fatal(e)
	}
	if e = validateProbeBaseline(s, s.StableSHA256); e != nil {
		t.Fatal(e)
	}
	if s.Schedules[0].PatternName != "Example 18" || s.Schedules[0].Off != "21:00" || s.Schedules[1].Enabled || s.Schedules[1].Off != "00:03" {
		t.Fatal(s.Schedules)
	}
	for _, offset := range []int{-2, 0, 3300, len(raw) - 13, int(^uint(0) >> 1)} {
		if _, e = ParseSnapshot(raw, offset); e == nil {
			t.Fatalf("accepted offset %d", offset)
		}
	}
	for _, size := range []int{0, 2, 25, len(raw) - 1} {
		if _, e = ParseSnapshot(raw[:size], 3306); e == nil {
			t.Fatal("accepted truncated snapshot")
		}
	}
	raw[3313] = 1
	s, e = ParseSnapshot(raw, 3306)
	if e != nil {
		t.Fatal(e)
	}
	if validateProbeBaseline(s, s.StableSHA256) == nil {
		t.Fatal("enabled schedule allowed for probe")
	}
}

func TestClockAndSaveVectors(t *testing.T) {
	now := time.Date(2026, 10, 28, 10, 15, 16, 0, time.UTC)
	q, e := clockQuery(now)
	if e != nil || hex.EncodeToString(q) != "1500071a0a1c030a0f10" {
		t.Fatalf("clock: %x %v", q, e)
	}
	for minute, want := range map[byte]string{3: "13000a01000100000200030a1c", 4: "13000a01000100000200040a1c"} {
		p, e := probePayload(minute, now)
		if e != nil || hex.EncodeToString(p) != want {
			t.Fatalf("save: %x %v", p, e)
		}
	}
	if _, e = probePayload(5, now); e == nil {
		t.Fatal("unbounded probe")
	}
	if _, e = clockQuery(time.Date(2026, 11, 1, 10, 0, 0, 0, time.UTC)); e == nil {
		t.Fatal("unverified Sunday accepted")
	}
}

func TestPlaybackReportedState(t *testing.T) {
	for value, mode := range map[byte]string{0: "off", 1: "manual", 2: "timer", 255: "unknown"} {
		t.Run(mode, func(t *testing.T) {
			raw := fixture(t)
			raw[3] = value
			// Entirely synthetic parameters from slot 7, including the retained
			// parameters when OFF. This is not evidence of emitted light.
			copy(raw[3320:3476], raw[26+6*182+26:26+7*182])
			s, err := ParseSnapshot(raw, 3306)
			if err != nil {
				t.Fatal(err)
			}
			p := s.Playback
			if p == nil || p.ModeRaw != value || p.Mode != mode || p.ParametersHex != s.Patterns[6].ParametersHex || len(p.MatchingPatternIDs) != 1 || p.MatchingPatternIDs[0] != 7 {
				t.Fatalf("incorrect playback: %+v", p)
			}
			encoded, err := json.Marshal(p)
			if err != nil || !bytes.Contains(encoded, []byte(`"matching_pattern_ids":[7]`)) {
				t.Fatalf("pattern IDs must be a JSON number array: %s %v", encoded, err)
			}
		})
	}
}

func TestPlaybackMatchesDoNotInventSlotIdentity(t *testing.T) {
	raw := fixture(t)
	copy(raw[3320:3476], bytes.Repeat([]byte{0xfe}, 156))
	s, err := ParseSnapshot(raw, 3306)
	if err != nil || s.Playback == nil || s.Playback.MatchingPatternIDs == nil || len(s.Playback.MatchingPatternIDs) != 0 {
		t.Fatalf("unmatched parameters: %+v %v", s, err)
	}
	// Two different slot names/IDs, identical parameters: return both candidates.
	copy(raw[26+26:26+182], raw[3320:3476])
	copy(raw[26+182+26:26+2*182], raw[3320:3476])
	s, err = ParseSnapshot(raw, 3306)
	if err != nil || s.Playback == nil || len(s.Playback.MatchingPatternIDs) != 2 || s.Playback.MatchingPatternIDs[0] != 1 || s.Playback.MatchingPatternIDs[1] != 2 {
		t.Fatalf("ambiguous parameters: %+v %v", s, err)
	}
}

func TestPlaybackRequiresReviewedLayout(t *testing.T) {
	for _, change := range []struct {
		name   string
		offset int
		mutate func([]byte) []byte
	}{
		{"no explicit offset", -1, func(b []byte) []byte { return b }},
		{"different offset", 3307, func(b []byte) []byte { clear(b[3307:3321]); return b }},
		{"different count", 3306, func(b []byte) []byte { b[25] = 17; return b }},
		{"different length", 3306, func(b []byte) []byte {
			b = append(b, 0)
			binary.BigEndian.PutUint16(b[1:3], uint16(len(b)-3))
			return b
		}},
	} {
		t.Run(change.name, func(t *testing.T) {
			s, err := ParseSnapshot(change.mutate(fixture(t)), change.offset)
			if err != nil {
				t.Fatal(err)
			}
			if s.Playback != nil {
				t.Fatal("inferred playback for an unreviewed layout")
			}
		})
	}
}

func FuzzSnapshot(f *testing.F) {
	b, _ := os.ReadFile("testdata/snapshot.bin")
	f.Add(b, 3306)
	f.Add([]byte{}, 0)
	f.Fuzz(func(t *testing.T, b []byte, offset int) { _, _ = ParseSnapshot(b, offset) })
}

func fixtureFingerprint(t *testing.T) string {
	t.Helper()
	s, err := ParseSnapshot(fixture(t), 3306)
	if err != nil {
		t.Fatal(err)
	}
	return s.StableSHA256
}

func TestPublicSnapshotIsEntirelySynthetic(t *testing.T) {
	raw := fixture(t)
	expected := make([]byte, 3492)
	expected[0] = 0x15
	expected[1] = 0x0d
	expected[2] = 0xa1
	expected[25] = 18
	for i := 0; i < 18; i++ {
		offset := 26 + i*182
		expected[offset] = byte(i + 1)
		copy(expected[offset+1:offset+26], fmt.Sprintf("Example %02d", i+1))
		for j := 0; j < 156; j++ {
			expected[offset+26+j] = byte((i*3 + j) % 32)
		}
	}
	copy(expected[3306:3320], []byte{1, 18, 1, 18, 0, 21, 0, 0, 1, 0, 0, 2, 0, 3})
	copy(expected[len(expected)-7:], []byte{0, 1, 5, 3, 12, 0, 0})
	if !bytes.Equal(raw, expected) {
		t.Fatal("public snapshot contains bytes outside its synthetic specification")
	}
	var vectors []struct{ Payload string }
	b, err := os.ReadFile("testdata/frames.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(b, &vectors); err != nil {
		t.Fatal(err)
	}
	for i, v := range vectors {
		payload, err := hex.DecodeString(v.Payload)
		if err != nil {
			t.Fatal(err)
		}
		if len(payload) == 3492 {
			want := bytes.Clone(expected)
			want[len(want)-1] = byte(i)
			if !bytes.Equal(payload, want) {
				t.Fatal("snapshot frame includes non-synthetic fields")
			}
		} else {
			for _, v := range payload {
				if v > 32 {
					t.Fatal("unexpected byte in synthetic short payload")
				}
			}
		}
	}
}
