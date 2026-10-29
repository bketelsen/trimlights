package backup

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"trimlights/internal/auth"
	"trimlights/internal/house"
	"trimlights/internal/pattern"
	"trimlights/internal/schedule"
	"trimlights/internal/storage"
	"trimlights/internal/trimlight"
)

func configuration() auth.Config {
	return auth.Config{Origin: "https://lights.example.invalid", Issuer: "https://id.example.invalid", ClientID: "fixture-client", ClientSecret: "fixture-secret", AllowedSubjects: []string{"one", "two"}}
}
func fixture(t *testing.T) (*storage.Store, string) {
	t.Helper()
	dir := t.TempDir()
	s, err := storage.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	ctx := context.Background()
	d := pattern.Design{Schema: 1, Name: "Recovery fixture", Effect: "static", Brightness: 50, Palette: []pattern.Run{{Color: "#ff0000", Count: 3}}}
	p, err := s.CreatePattern(ctx, d, "one")
	if err != nil {
		t.Fatal(err)
	}
	d.Name = "Second revision"
	if _, err = s.UpdatePattern(ctx, p.ID, 1, d, "two"); err != nil {
		t.Fatal(err)
	}
	plan := schedule.Default()
	plan.Rules = []schedule.Rule{{ID: "evening", Name: "Evening", Enabled: true, Kind: "weekly", Weekdays: []int{0, 1, 2, 3, 4, 5, 6}, Start: schedule.Boundary{Kind: "clock", Time: "18:00"}, End: schedule.Boundary{Kind: "clock", Time: "21:00"}, Action: schedule.Action{Kind: "pattern", PatternID: p.ID, Revision: 1}}}
	if _, err = s.SaveSchedule(ctx, 1, plan, "one"); err != nil {
		t.Fatal(err)
	}
	var raw bytes.Buffer
	_ = png.Encode(&raw, image.NewRGBA(image.Rect(0, 0, 80, 40)))
	photo, err := house.Sanitize(raw.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	saved, err := s.AddPhoto(ctx, photo, "two")
	if err != nil {
		t.Fatal(err)
	}
	layout := house.Default()
	layout.Runs = []house.Run{{ID: "front", Name: "Front", Output: "one", Start: 1, Count: 5}}
	layout.Views = []house.View{{PhotoID: saved.ID, Mappings: []house.Mapping{{RunID: "front", Points: []house.Point{{X: 0, Y: 0}, {X: 1, Y: 1}}, Adjustments: []house.Adjustment{}}}}}
	if _, err = s.SaveHouse(ctx, 1, layout, "one"); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err = s.Record(ctx, storage.Observation{Snapshot: &trimlight.Snapshot{QueriedAt: now}, Source: "fixture"}, storage.Operation{ID: "fixture", Started: now, Finished: now, Actor: "one", Control: &storage.ControlAudit{Request: trimlight.ControlRequest{Action: "off"}, Outcome: "verified", WriteAttempted: true}}); err != nil {
		t.Fatal(err)
	}
	if err = s.SaveSession(ctx, storage.Session{Hash: "old-session", Subject: "one", CSRF: "fixture", Expires: now.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if err = s.SaveFlow(ctx, storage.LoginFlow{StateHash: "state", BrowserHash: "browser", Nonce: "nonce", Verifier: "verifier", Expires: now.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	return s, dir
}

func TestSchemaFiveBackupStillRestoresAndMigrates(t *testing.T) {
	source, dir := fixture(t)
	if err := source.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := openDB(filepath.Join(dir, "trimlights.db"), false)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec("DROP TABLE control_audits; PRAGMA user_version=5")
	db.Close()
	if err != nil {
		t.Fatal(err)
	}
	archive := filepath.Join(t.TempDir(), "schema-five.zip")
	r, err := Create(context.Background(), dir, archive, configuration(), "test")
	if err != nil || r.Schema != 5 {
		t.Fatal(r, err)
	}
	output := filepath.Join(t.TempDir(), "restored")
	r, err = Restore(context.Background(), archive, output)
	if err != nil || r.Schema != 5 {
		t.Fatal(r, err)
	}
	s, err := storage.Open(filepath.Join(output, "data"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if _, err = s.Operations(context.Background()); err != nil {
		t.Fatal(err)
	}
}
func TestLiveBackupRestoresAllHouseholdStateAndClearsAuthentication(t *testing.T) {
	source, dir := fixture(t)
	ctx := context.Background()
	out := filepath.Join(t.TempDir(), "house.zip")
	report, err := Create(ctx, dir, out, configuration(), "test")
	if err != nil {
		t.Fatal(err)
	}
	if report.Photos != 1 || report.Patterns != 2 || report.Schedules != 2 || report.Layouts != 2 {
		t.Fatal(report)
	}
	// The source remains open with its ownership lock and authentication intact.
	if _, err = source.Session(ctx, "old-session", time.Now()); err != nil {
		t.Fatal(err)
	}
	restored := filepath.Join(t.TempDir(), "recovered")
	report, err = Restore(ctx, out, restored)
	if err != nil {
		t.Fatal(err)
	}
	if !report.ControllerDisabled {
		t.Fatal(report)
	}
	store, err := storage.Open(filepath.Join(restored, "data"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	check := func(a, b any, e1, e2 error) {
		t.Helper()
		if e1 != nil || e2 != nil || !reflect.DeepEqual(a, b) {
			t.Fatal("restored data differs", e1, e2)
		}
	}
	a, e1 := source.Patterns(ctx, true)
	b, e2 := store.Patterns(ctx, true)
	check(a, b, e1, e2)
	for _, p := range a {
		x, e1 := source.PatternHistory(ctx, p.ID)
		y, e2 := store.PatternHistory(ctx, p.ID)
		check(x, y, e1, e2)
	}
	c, e1 := source.ScheduleHistory(ctx)
	d, e2 := store.ScheduleHistory(ctx)
	check(c, d, e1, e2)
	e, e1 := source.HouseHistory(ctx)
	f, e2 := store.HouseHistory(ctx)
	check(e, f, e1, e2)
	photos, e1 := source.Photos(ctx)
	other, e2 := store.Photos(ctx)
	check(photos, other, e1, e2)
	for _, p := range photos {
		for _, thumb := range []bool{false, true} {
			x, e1 := source.PhotoBytes(ctx, p.ID, thumb)
			y, e2 := store.PhotoBytes(ctx, p.ID, thumb)
			check(x, y, e1, e2)
		}
	}
	o, e1 := source.Observation(ctx)
	oo, e2 := store.Observation(ctx)
	check(o, oo, e1, e2)
	ops, e1 := source.Operations(ctx)
	ops2, e2 := store.Operations(ctx)
	check(ops, ops2, e1, e2)
	if _, err = store.Session(ctx, "old-session", time.Now()); !errors.Is(err, storage.ErrMissing) {
		t.Fatal(err)
	}
	if _, err = store.ConsumeFlow(ctx, "state", "browser", time.Now()); !errors.Is(err, storage.ErrMissing) {
		t.Fatal(err)
	}
	secret, err := os.ReadFile(filepath.Join(restored, "config/pocket-id.secret"))
	if err != nil || string(secret) != configuration().ClientSecret {
		t.Fatal("secret mismatch", err)
	}
	for _, path := range []string{out, filepath.Join(restored, "data/trimlights.db"), filepath.Join(restored, "config/oidc.json"), filepath.Join(restored, "config/pocket-id.secret"), filepath.Join(restored, "data/controller-disabled")} {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != 0600 {
			t.Fatal(path, err)
		}
	}
	if _, err = Create(ctx, dir, out, configuration(), "test"); err == nil {
		t.Fatal("overwrote archive")
	}
	if _, err = Restore(ctx, out, restored); err == nil {
		t.Fatal("overwrote restore")
	}
}
func TestRestoreRejectsTamperingAndUnsafeArchives(t *testing.T) {
	_, dir := fixture(t)
	ctx := context.Background()
	archive := filepath.Join(t.TempDir(), "source.zip")
	if _, err := Create(ctx, dir, archive, configuration(), "test"); err != nil {
		t.Fatal(err)
	}
	z, err := zip.OpenReader(archive)
	if err != nil {
		t.Fatal(err)
	}
	defer z.Close()
	original := map[string][]byte{}
	for _, f := range z.File {
		r, _ := f.Open()
		original[f.Name], err = io.ReadAll(r)
		r.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, kind := range []string{"traversal", "duplicate", "symlink", "checksum", "missing", "schema", "database", "config-path", "manifest-size"} {
		t.Run(kind, func(t *testing.T) {
			entries := map[string][]byte{}
			for n, b := range original {
				entries[n] = append([]byte(nil), b...)
			}
			var manifest Manifest
			_ = json.Unmarshal(entries["manifest.json"], &manifest)
			rehash := func(name string) {
				for i := range manifest.Files {
					if manifest.Files[i].Name == name {
						hash := sha256.Sum256(entries[name])
						manifest.Files[i].Size = int64(len(entries[name]))
						manifest.Files[i].SHA256 = hex.EncodeToString(hash[:])
					}
				}
			}
			switch kind {
			case "checksum":
				entries["data/trimlights.db"][100] ^= 1
			case "missing":
				delete(entries, "config/pocket-id.secret")
			case "schema":
				manifest.Schema = 999
			case "database":
				entries["data/trimlights.db"] = []byte("invalid database")
				rehash("data/trimlights.db")
			case "config-path":
				entries["config/oidc.json"] = bytes.ReplaceAll(entries["config/oidc.json"], []byte("pocket-id.secret"), []byte("../../escape"))
				rehash("config/oidc.json")
			}
			entries["manifest.json"], _ = json.Marshal(manifest)
			if kind == "manifest-size" {
				entries["manifest.json"] = make([]byte, 16385)
			}
			path := filepath.Join(t.TempDir(), "bad.zip")
			file, _ := os.Create(path)
			writer := zip.NewWriter(file)
			for name, raw := range entries {
				h := &zip.FileHeader{Name: name, Method: zip.Deflate}
				if kind == "symlink" && name == "config/pocket-id.secret" {
					h.SetMode(os.ModeSymlink | 0600)
				}
				w, _ := writer.CreateHeader(h)
				_, _ = w.Write(raw)
			}
			if kind == "traversal" {
				w, _ := writer.Create("../escape")
				_, _ = w.Write([]byte("no"))
			}
			if kind == "duplicate" {
				w, _ := writer.Create("config/pocket-id.secret")
				_, _ = w.Write([]byte("duplicate"))
			}
			_ = writer.Close()
			_ = file.Close()
			dest := filepath.Join(t.TempDir(), "output")
			if _, err := Restore(ctx, path, dest); err == nil {
				t.Fatal("accepted invalid archive")
			}
			if _, err := os.Stat(dest); !os.IsNotExist(err) {
				t.Fatal("failed restore published output")
			}
			left, _ := filepath.Glob(filepath.Join(filepath.Dir(dest), ".trimlights-backup-*"))
			if len(left) != 0 {
				t.Fatal("staging files retained")
			}
		})
	}
}
func TestBackupMissingSourceAndCancellationLeaveNoOutput(t *testing.T) {
	ctx := context.Background()
	dest := filepath.Join(t.TempDir(), "bundle.zip")
	missing := t.TempDir()
	if _, err := Create(ctx, missing, dest, configuration(), "test"); err == nil {
		t.Fatal("created missing database")
	}
	if _, err := os.Stat(filepath.Join(missing, "trimlights.db")); !os.IsNotExist(err) {
		t.Fatal(err)
	}
	_, dir := fixture(t)
	ctx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := Create(ctx, dir, dest, configuration(), "test"); err == nil {
		t.Fatal("ignored cancellation")
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Fatal(err)
	}
}
