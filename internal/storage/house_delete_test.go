package storage

import (
	"context"
	"errors"
	"testing"
	"trimlights/internal/house"
)

func view(photoID string) house.View {
	return house.View{PhotoID: photoID, Turns: 1, Mappings: []house.Mapping{{RunID: "front", Points: []house.Point{{X: 0, Y: 0}, {X: 1, Y: 1}}, Adjustments: []house.Adjustment{}}}}
}

func TestDeletePhotoRefusesTheCurrentLayoutAndKeepsHistory(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	image := house.PhotoImage{JPEG: []byte("sanitized JPEG fixture"), Thumbnail: []byte("thumbnail"), Width: 200, Height: 100}
	a, err := s.AddPhoto(ctx, image, "one")
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.AddPhoto(ctx, image, "one")
	if err != nil {
		t.Fatal(err)
	}
	l := house.Default()
	l.Runs = []house.Run{{ID: "front", Name: "Front", Output: "one", Start: 1, Count: 10}}
	l.Views = []house.View{view(a.ID)}
	if _, err = s.SaveHouse(ctx, 1, l, "one"); err != nil {
		t.Fatal(err)
	}
	if err = s.DeletePhoto(ctx, a.ID); !errors.Is(err, ErrPhotoInUse) {
		t.Fatal(err)
	}
	if err = s.DeletePhoto(ctx, "missing"); !errors.Is(err, ErrMissing) {
		t.Fatal(err)
	}
	l.Views = []house.View{view(b.ID)}
	if _, err = s.SaveHouse(ctx, 2, l, "one"); err != nil {
		t.Fatal(err)
	}
	if err = s.DeletePhoto(ctx, a.ID); err != nil {
		t.Fatal(err)
	}
	check := func(s *Store) {
		t.Helper()
		for _, thumbnail := range []bool{false, true} {
			if _, err := s.PhotoBytes(ctx, a.ID, thumbnail); !errors.Is(err, ErrMissing) {
				t.Fatal("deleted photo bytes", thumbnail, err)
			}
		}
		if raw, err := s.PhotoBytes(ctx, b.ID, false); err != nil || string(raw) != string(image.JPEG) {
			t.Fatal("kept photo bytes", err)
		}
		photos, err := s.Photos(ctx)
		if err != nil || len(photos) != 1 || photos[0].ID != b.ID {
			t.Fatal(photos, err)
		}
		history, err := s.HouseHistory(ctx)
		if err != nil || len(history) != 3 {
			t.Fatal(history, err)
		}
		if len(history[1].Layout.Views) != 1 || history[1].Layout.Views[0].PhotoID != a.ID {
			t.Fatal("revision 2 body changed", history[1])
		}
		var refs int
		if err = s.db.QueryRow("SELECT COUNT(*) FROM house_photo_refs WHERE photo_id=?", a.ID).Scan(&refs); err != nil || refs != 0 {
			t.Fatal("dangling photo references", refs, err)
		}
		rows, err := s.db.Query("PRAGMA foreign_key_check")
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		if rows.Next() {
			t.Fatal("broken foreign keys")
		}
		if err = rows.Err(); err != nil {
			t.Fatal(err)
		}
		if err = s.DeletePhoto(ctx, a.ID); !errors.Is(err, ErrMissing) {
			t.Fatal("second delete", err)
		}
	}
	check(s)
	// An older revision body still names the photo, so saving it again must fail.
	l.Views = []house.View{view(a.ID)}
	if _, err = s.SaveHouse(ctx, 3, l, "one"); !errors.Is(err, ErrPhotoReference) {
		t.Fatal(err)
	}
	if err = s.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	check(s)
}

func TestDeletePhotoFreesAPlaceInThePhotoLimit(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	ctx := context.Background()
	image := house.PhotoImage{JPEG: []byte("sanitized JPEG fixture"), Thumbnail: []byte("thumbnail"), Width: 200, Height: 100}
	var first Photo
	for i := 0; i < 20; i++ {
		p, err := s.AddPhoto(ctx, image, "one")
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = p
		}
	}
	if _, err = s.AddPhoto(ctx, image, "one"); !errors.Is(err, ErrPhotoLimit) {
		t.Fatal(err)
	}
	if err = s.DeletePhoto(ctx, first.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.AddPhoto(ctx, image, "one"); err != nil {
		t.Fatal("freed place is not usable", err)
	}
}
