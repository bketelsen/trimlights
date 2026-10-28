package service

import (
	"bytes"
	"encoding/json"
	"image"
	"image/png"
	"net/http/httptest"
	"strings"
	"testing"
	"trimlights/internal/house"
	"trimlights/internal/storage"
)

func TestPhotoAPIRequiresAuthAndSanitizesBeforePersistence(t *testing.T) {
	store, worker := setup(t, nil)
	token := strings.Repeat("p", 32)
	h := Handler(store, worker, token)
	call := func(method, path string, body []byte, auth bool, version string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, bytes.NewReader(body))
		if auth {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("If-Match", version)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	for _, path := range []string{"/api/v1/house", "/api/v1/photos", "/api/v1/photos/guess/image", "/api/v1/house/revisions"} {
		if r := call("GET", path, nil, false, ""); r.Code != 401 {
			t.Fatal(path, r.Code)
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 30, 20))); err != nil {
		t.Fatal(err)
	}
	if r := call("POST", "/api/v1/photos", b.Bytes(), false, ""); r.Code != 401 {
		t.Fatal(r.Code)
	}
	r := call("POST", "/api/v1/photos", b.Bytes(), true, "")
	if r.Code != 201 {
		t.Fatal(r.Code, r.Body.String())
	}
	var p storage.Photo
	if err := json.Unmarshal(r.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	r = call("GET", "/api/v1/photos/"+p.ID+"/image", nil, true, "")
	if r.Code != 200 || r.Header().Get("Content-Type") != "image/jpeg" || r.Header().Get("Cache-Control") != "no-store" || !bytes.HasPrefix(r.Body.Bytes(), []byte{0xff, 0xd8}) {
		t.Fatal(r.Code, r.Header())
	}
	l := house.Default()
	l.Views = []house.View{{PhotoID: p.ID, Mappings: []house.Mapping{}}}
	raw, _ := json.Marshal(l)
	for _, v := range []struct {
		version string
		code    int
	}{{"", 428}, {`"1"`, 200}, {`"1"`, 409}} {
		r = call("PUT", "/api/v1/house", raw, true, v.version)
		if r.Code != v.code {
			t.Fatal(v, r.Code, r.Body.String())
		}
	}
	r = call("POST", "/api/v1/photos", []byte("0000ftypheic"), true, "")
	if r.Code != 400 || !strings.Contains(r.Body.String(), "HEIC") {
		t.Fatal(r.Code, r.Body.String())
	}
	if worker.Status().Enabled {
		t.Fatal("photo API enabled hardware")
	}
}
