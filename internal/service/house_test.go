package service

import (
	"bytes"
	"context"
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
	h := Handler(store, worker, token, nil)
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
	for _, body := range [][]byte{[]byte(`null`), []byte(`{"schema":1,"runs":[],"views":[],"calibrated":true}`), []byte(`{"schema":1,"runs":[],"views":[{"photo_id":"missing","turns":0,"mappings":[]}]}`), []byte(strings.Repeat(" ", 512<<10) + "{}")} {
		response := call("POST", "/api/v1/house/validate", body, true, "")
		if response.Code != 400 {
			t.Fatal("invalid import accepted", response.Code, response.Body.String())
		}
	}
	if response := call("POST", "/api/v1/house/validate", raw, false, ""); response.Code != 401 {
		t.Fatal(response.Code)
	}
	response := call("POST", "/api/v1/house/validate", raw, true, "")
	if response.Code != 200 {
		t.Fatal(response.Code, response.Body.String())
	}
	var validated struct {
		Layout house.Layout `json:"layout"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &validated); err != nil || validated.Layout.Views[0].PhotoID != p.ID {
		t.Fatal("invalid validated layout", err)
	}
	before, err := store.HouseHistory(context.Background())
	if err != nil || len(before) != 1 {
		t.Fatal("validation mutated revisions", err)
	}
	operations, err := store.Operations(context.Background())
	if err != nil || len(operations) != 0 {
		t.Fatal("validation contacted controller", err)
	}
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

func TestPhotoDeleteAPIRefusesTheSavedHouseAndRemovesAnUnusedPhoto(t *testing.T) {
	store, worker := setup(t, nil)
	token := strings.Repeat("p", 32)
	h := Handler(store, worker, token, nil)
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
	if r := call("DELETE", "/api/v1/photos/guess", nil, false, ""); r.Code != 401 {
		t.Fatal(r.Code)
	}
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 30, 20))); err != nil {
		t.Fatal(err)
	}
	r := call("POST", "/api/v1/photos", b.Bytes(), true, "")
	if r.Code != 201 {
		t.Fatal(r.Code, r.Body.String())
	}
	var p storage.Photo
	if err := json.Unmarshal(r.Body.Bytes(), &p); err != nil {
		t.Fatal(err)
	}
	l := house.Default()
	l.Views = []house.View{{PhotoID: p.ID, Mappings: []house.Mapping{}}}
	raw, _ := json.Marshal(l)
	if r = call("PUT", "/api/v1/house", raw, true, `"1"`); r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	r = call("DELETE", "/api/v1/photos/"+p.ID, nil, true, "")
	var failure struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(r.Body.Bytes(), &failure); err != nil || r.Code != 409 || !strings.Contains(failure.Error, "remove its view") {
		t.Fatal(r.Code, err, failure.Error)
	}
	if r = call("DELETE", "/api/v1/photos/missing", nil, true, ""); r.Code != 404 {
		t.Fatal(r.Code, r.Body.String())
	}
	l.Views = []house.View{}
	raw, _ = json.Marshal(l)
	if r = call("PUT", "/api/v1/house", raw, true, `"2"`); r.Code != 200 {
		t.Fatal(r.Code, r.Body.String())
	}
	if r = call("DELETE", "/api/v1/photos/"+p.ID, nil, true, ""); r.Code != 204 || r.Body.Len() != 0 {
		t.Fatal(r.Code, r.Body.String())
	}
	if r = call("GET", "/api/v1/photos/"+p.ID+"/image", nil, true, ""); r.Code != 404 {
		t.Fatal(r.Code, r.Body.String())
	}
	r = call("GET", "/api/v1/photos", nil, true, "")
	var listed struct {
		Photos []storage.Photo `json:"photos"`
	}
	if err := json.Unmarshal(r.Body.Bytes(), &listed); err != nil || len(listed.Photos) != 0 {
		t.Fatal(r.Code, err, listed.Photos)
	}
	operations, err := store.Operations(context.Background())
	if err != nil || len(operations) != 0 {
		t.Fatal("photo deletion contacted controller", err)
	}
	if worker.Status().Enabled {
		t.Fatal("photo deletion enabled hardware")
	}
}
