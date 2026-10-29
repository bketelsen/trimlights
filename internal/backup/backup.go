// Package backup creates portable, private recovery bundles without controller IO.
package backup

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/unix"
	_ "modernc.org/sqlite"
	"trimlights/internal/auth"
)

const DatabaseLimit int64 = 1 << 30

var limits = map[string]int64{"data/trimlights.db": DatabaseLimit, "config/oidc.json": 65536, "config/pocket-id.secret": 4096}
var entries = []string{"data/trimlights.db", "config/oidc.json", "config/pocket-id.secret"}

type File struct {
	Name   string `json:"name"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}
type Manifest struct {
	Format  int       `json:"format"`
	Schema  int       `json:"schema"`
	Created time.Time `json:"created"`
	Version string    `json:"version"`
	Files   []File    `json:"files"`
}
type Report struct {
	Output             string `json:"output"`
	Schema             int    `json:"schema"`
	Photos             int    `json:"photos"`
	Patterns           int    `json:"pattern_revisions"`
	Schedules          int    `json:"calendar_revisions"`
	Layouts            int    `json:"house_revisions"`
	ControllerDisabled bool   `json:"controller_disabled"`
}
type oidcFile struct {
	auth.Config
	ClientSecretFile string `json:"client_secret_file"`
}

func strictJSON(raw []byte, v any) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		return errors.New("invalid backup JSON")
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return errors.New("expected one JSON object")
	}
	return nil
}
func regular(path string, max int64) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() > max {
		return errors.New("file is not regular or exceeds the backup size limit")
	}
	return nil
}
func absent(path string) error {
	if _, err := os.Lstat(path); err == nil {
		return errors.New("output exists; refusing to overwrite")
	} else if !os.IsNotExist(err) {
		return err
	}
	return nil
}
func syncFile(path string) error {
	f, err := os.OpenFile(path, os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	return errors.Join(f.Sync(), f.Close())
}
func syncDir(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	return errors.Join(f.Sync(), f.Close())
}
func openDB(path string, readonly bool) (*sql.DB, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	u := url.URL{Scheme: "file", Path: absolute}
	q := u.Query()
	if readonly {
		q.Set("mode", "ro")
	} else {
		q.Set("mode", "rw")
	}
	q.Add("_pragma", "busy_timeout(5000)")
	q.Add("_pragma", "trusted_schema(OFF)")
	u.RawQuery = q.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	return db, nil
}
func inspect(ctx context.Context, path string) (Report, error) {
	if err := regular(path, DatabaseLimit); err != nil {
		return Report{}, err
	}
	db, err := openDB(path, true)
	if err != nil {
		return Report{}, err
	}
	defer db.Close()
	var r Report
	if err = db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&r.Schema); err != nil {
		return r, err
	}
	if r.Schema != 5 && r.Schema != 6 && r.Schema != 7 {
		return r, errors.New("backup requires supported database schema 5, 6 or 7")
	}
	var integrity string
	if err = db.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&integrity); err != nil || integrity != "ok" {
		return r, errors.New("database integrity check failed")
	}
	rows, err := db.QueryContext(ctx, "PRAGMA foreign_key_check")
	if err != nil {
		return r, err
	}
	bad := rows.Next()
	err = errors.Join(rows.Err(), rows.Close())
	if err != nil {
		return r, err
	}
	if bad {
		return r, errors.New("database contains broken foreign keys")
	}
	var objects int
	if err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM sqlite_schema WHERE type IN ('view','trigger')").Scan(&objects); err != nil {
		return r, err
	}
	if objects != 0 {
		return r, errors.New("unexpected views or triggers in application database")
	}
	columns := map[string]string{"observation": "id,body", "operations": "id,started,finished,actor,error", "login_flows": "state_hash,browser_hash,nonce,verifier,expires", "sessions": "hash,subject,name,csrf,expires", "pattern_heads": "id,revision,archived", "pattern_revisions": "id,revision,body,actor,created,archived", "pattern_imports": "slot,pattern_id", "schedule_revisions": "revision,body,actor,created", "schedule_references": "schedule_revision,rule_id,name,enabled,pattern_id,pattern_revision", "photos": "id,name,width,height,created,actor,image,thumbnail", "house_revisions": "revision,body,actor,created", "house_photo_refs": "revision,photo_id"}
	if r.Schema >= 6 {
		columns["control_audits"] = "id,body"
	}
	if r.Schema >= 7 {
		columns["calendar_runtime"] = "id,body"
		var valid int
		if err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM calendar_runtime WHERE id=1 AND json_valid(body) AND json_type(body,'$.version')='integer' AND json_extract(body,'$.version')>=1 AND json_extract(body,'$.status') IN ('disabled','active','paused')`).Scan(&valid); err != nil || valid != 1 {
			return r, errors.New("database has invalid calendar ownership state")
		}
	}
	for table, fields := range columns {
		rows, err = db.QueryContext(ctx, "SELECT "+fields+" FROM "+table+" LIMIT 0")
		if err != nil {
			return r, fmt.Errorf("missing application table or columns: %s", table)
		}
		if err = rows.Close(); err != nil {
			return r, err
		}
	}
	for table, target := range map[string]*int{"photos": &r.Photos, "pattern_revisions": &r.Patterns, "schedule_revisions": &r.Schedules, "house_revisions": &r.Layouts} {
		if err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(target); err != nil {
			return r, err
		}
	}
	if r.Schedules < 1 || r.Layouts < 1 {
		return r, errors.New("database is missing initial calendar or house revision")
	}
	return r, nil
}
func stage(output string) (string, error) {
	if output == "" {
		return "", errors.New("output is required")
	}
	if err := absent(output); err != nil {
		return "", err
	}
	return os.MkdirTemp(filepath.Dir(output), ".trimlights-backup-")
}
func writePrivate(path string, raw []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	_, err = f.Write(raw)
	return errors.Join(err, f.Sync(), f.Close())
}
func Create(ctx context.Context, dataDir, output string, config auth.Config, version string) (Report, error) {
	if err := config.Validate(); err != nil {
		return Report{}, err
	}
	if len(config.ClientSecret) > 4096 {
		return Report{}, errors.New("OIDC secret exceeds 4096 bytes")
	}
	source := filepath.Join(dataDir, "trimlights.db")
	if err := regular(source, DatabaseLimit); err != nil {
		return Report{}, err
	}
	temp, err := stage(output)
	if err != nil {
		return Report{}, err
	}
	defer os.RemoveAll(temp)
	if err = os.Mkdir(filepath.Join(temp, "data"), 0700); err != nil {
		return Report{}, err
	}
	snapshot := filepath.Join(temp, "data/trimlights.db")
	db, err := openDB(source, true)
	if err != nil {
		return Report{}, err
	}
	_, err = db.ExecContext(ctx, "PRAGMA synchronous=FULL")
	if err == nil {
		_, err = db.ExecContext(ctx, "VACUUM INTO ?", snapshot)
	}
	err = errors.Join(err, db.Close())
	if err != nil {
		return Report{}, fmt.Errorf("database snapshot: %w", err)
	}
	if err = os.Chmod(snapshot, 0600); err != nil {
		return Report{}, err
	}
	if err = syncFile(snapshot); err != nil {
		return Report{}, err
	}
	report, err := inspect(ctx, snapshot)
	if err != nil {
		return Report{}, err
	}
	raw, err := json.MarshalIndent(oidcFile{config, "pocket-id.secret"}, "", "  ")
	if err != nil {
		return Report{}, err
	}
	if len(raw) > 65536 {
		return Report{}, errors.New("OIDC configuration exceeds 64 KiB")
	}
	if err = writePrivate(filepath.Join(temp, "config/oidc.json"), raw); err != nil {
		return Report{}, err
	}
	if err = writePrivate(filepath.Join(temp, "config/pocket-id.secret"), []byte(config.ClientSecret)); err != nil {
		return Report{}, err
	}
	archive := filepath.Join(temp, "bundle.zip")
	f, err := os.OpenFile(archive, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return Report{}, err
	}
	z := zip.NewWriter(f)
	manifest := Manifest{Format: 1, Schema: report.Schema, Created: time.Now().UTC(), Version: version, Files: []File{}}
	for _, name := range entries {
		if err = ctx.Err(); err != nil {
			break
		}
		var item File
		item, err = add(z, filepath.Join(temp, name), name)
		if err != nil {
			break
		}
		manifest.Files = append(manifest.Files, item)
	}
	if err == nil {
		raw, err = json.MarshalIndent(manifest, "", "  ")
		if err == nil {
			var w io.Writer
			w, err = z.Create("manifest.json")
			if err == nil {
				_, err = w.Write(raw)
			}
		}
	}
	err = errors.Join(err, z.Close(), f.Sync(), f.Close())
	if err != nil {
		return Report{}, err
	}
	if err = ctx.Err(); err != nil {
		return Report{}, err
	}
	// Hard-link publication is atomic and refuses existing files, including symlinks.
	if err = os.Link(archive, output); err != nil {
		return Report{}, err
	}
	if err = syncDir(filepath.Dir(output)); err != nil {
		return Report{}, err
	}
	report.Output = output
	return report, nil
}
func add(z *zip.Writer, path, name string) (File, error) {
	f, err := os.Open(path)
	if err != nil {
		return File{}, err
	}
	defer f.Close()
	header := &zip.FileHeader{Name: name, Method: zip.Deflate}
	header.SetMode(0600)
	w, err := z.CreateHeader(header)
	if err != nil {
		return File{}, err
	}
	hash := sha256.New()
	n, err := io.Copy(io.MultiWriter(w, hash), f)
	return File{name, n, hex.EncodeToString(hash.Sum(nil))}, err
}
func Restore(ctx context.Context, input, output string) (Report, error) {
	if err := regular(input, DatabaseLimit+(2<<20)); err != nil {
		return Report{}, err
	}
	if err := absent(output); err != nil {
		return Report{}, err
	}
	z, err := zip.OpenReader(input)
	if err != nil {
		return Report{}, errors.New("invalid backup archive")
	}
	defer z.Close()
	files := map[string]*zip.File{}
	for _, f := range z.File {
		limit, ok := limits[f.Name]
		if f.Name == "manifest.json" {
			limit, ok = 16384, true
		}
		if !ok || files[f.Name] != nil || !f.Mode().IsRegular() || f.UncompressedSize64 > uint64(limit) {
			return Report{}, errors.New("archive contains an unexpected, duplicate, non-regular or oversized entry")
		}
		files[f.Name] = f
	}
	if len(files) != 4 || files["manifest.json"] == nil {
		return Report{}, errors.New("archive must contain a manifest and exactly three files")
	}
	raw, err := readZip(files["manifest.json"], 16384)
	if err != nil {
		return Report{}, err
	}
	var manifest Manifest
	if err = strictJSON(raw, &manifest); err != nil {
		return Report{}, err
	}
	if manifest.Format != 1 || (manifest.Schema != 5 && manifest.Schema != 6 && manifest.Schema != 7) || len(manifest.Files) != 3 || manifest.Created.IsZero() {
		return Report{}, errors.New("unsupported backup manifest")
	}
	seen := map[string]bool{}
	for _, entry := range manifest.Files {
		limit, ok := limits[entry.Name]
		hash, e := hex.DecodeString(entry.SHA256)
		if !ok || seen[entry.Name] || entry.Size < 1 || entry.Size > limit || e != nil || len(hash) != 32 || files[entry.Name] == nil || files[entry.Name].UncompressedSize64 != uint64(entry.Size) {
			return Report{}, errors.New("invalid manifest file entry")
		}
		seen[entry.Name] = true
	}
	temp, err := stage(output)
	if err != nil {
		return Report{}, err
	}
	defer os.RemoveAll(temp)
	for _, entry := range manifest.Files {
		if err = ctx.Err(); err != nil {
			return Report{}, err
		}
		if err = extract(files[entry.Name], filepath.Join(temp, entry.Name), entry); err != nil {
			return Report{}, err
		}
	}
	configRaw, err := os.ReadFile(filepath.Join(temp, "config/oidc.json"))
	if err != nil {
		return Report{}, err
	}
	var config oidcFile
	if err = strictJSON(configRaw, &config); err != nil {
		return Report{}, err
	}
	if config.ClientSecretFile != "pocket-id.secret" {
		return Report{}, errors.New("backup secret reference must be pocket-id.secret")
	}
	secret, err := os.ReadFile(filepath.Join(temp, "config/pocket-id.secret"))
	if err != nil {
		return Report{}, err
	}
	config.ClientSecret = strings.TrimSpace(string(secret))
	if err = config.Validate(); err != nil {
		return Report{}, errors.New("backup OIDC configuration is invalid")
	}
	database := filepath.Join(temp, "data/trimlights.db")
	report, err := inspect(ctx, database)
	if err != nil {
		return Report{}, err
	}
	if report.Schema != manifest.Schema {
		return Report{}, errors.New("manifest and database schema differ")
	}
	db, err := openDB(database, false)
	if err != nil {
		return Report{}, err
	}
	clear := "PRAGMA journal_mode=DELETE; PRAGMA synchronous=FULL; BEGIN; DELETE FROM sessions; DELETE FROM login_flows;"
	if report.Schema >= 7 {
		clear += `UPDATE calendar_runtime SET body=json_set(body,'$.version',json_extract(body,'$.version')+1,'$.status','paused','$.pending_operation','','$.reason','Restored ownership is paused; review one controller owner before activation.');`
	}
	_, err = db.ExecContext(ctx, clear+"COMMIT;")
	err = errors.Join(err, db.Close())
	if err != nil {
		return Report{}, err
	}
	if err = syncFile(database); err != nil {
		return Report{}, err
	}
	if err = writePrivate(filepath.Join(temp, "data/controller-disabled"), []byte("Restored instance: controller access disabled. Stop this instance and select one controller owner before deliberately removing this marker.\n")); err != nil {
		return Report{}, err
	}
	for _, dir := range []string{"data", "config", ""} {
		if err = syncDir(filepath.Join(temp, dir)); err != nil {
			return Report{}, err
		}
	}
	if err = ctx.Err(); err != nil {
		return Report{}, err
	}
	// Same-directory staging and RENAME_NOREPLACE preserve atomic, exclusive output.
	if err = unix.Renameat2(unix.AT_FDCWD, temp, unix.AT_FDCWD, output, unix.RENAME_NOREPLACE); err != nil {
		return Report{}, err
	}
	if err = syncDir(filepath.Dir(output)); err != nil {
		return Report{}, err
	}
	report.Output = output
	report.ControllerDisabled = true
	return report, nil
}
func readZip(f *zip.File, limit int64) ([]byte, error) {
	r, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer r.Close()
	b, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, errors.New("archive entry exceeds size limit")
	}
	return b, nil
}
func extract(source *zip.File, path string, entry File) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	r, err := source.Open()
	if err != nil {
		return err
	}
	defer r.Close()
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	hash := sha256.New()
	n, copyErr := io.Copy(io.MultiWriter(f, hash), io.LimitReader(r, entry.Size+1))
	err = errors.Join(copyErr, f.Sync(), f.Close())
	if err != nil {
		return err
	}
	if n != entry.Size || hex.EncodeToString(hash.Sum(nil)) != entry.SHA256 {
		return errors.New("backup file checksum or size mismatch")
	}
	return nil
}
