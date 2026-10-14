package app

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/Shaderx/PingObservatory/internal/config"
	"github.com/Shaderx/PingObservatory/internal/model"
)

func request(a *App, method, path string, body []byte, token, origin string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://"+a.Authority+path, bytes.NewReader(body))
	r.Host = a.Authority
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("X-Ping-Token", token)
	r.Header.Set("Origin", origin)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	return w
}

func TestSettingsRejectRevisionMismatchAndFailedSaveIsAtomic(t *testing.T) {
	dir := t.TempDir()
	a, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	a.Authority = "127.0.0.1:8742"
	c := config.Default()
	c.ManualTargets = []model.Target{{ID: "manual-local", Revision: 1, Label: "Local", Host: "127.0.0.1"}}
	b, _ := json.Marshal(c)
	if w := request(a, "PUT", "/api/config", b, a.token, ""); w.Code != 200 {
		t.Fatal(w.Body)
	}
	realPath := a.configPath
	a.configPath = dir
	c.ManualTargets[0].Revision = 2
	c.ManualTargets[0].Host = "127.0.0.2"
	b, _ = json.Marshal(c)
	if w := request(a, "PUT", "/api/config", b, a.token, ""); w.Code != 500 {
		t.Fatalf("failed persistence accepted %d", w.Code)
	}
	a.configPath = realPath
	if a.config.ManualTargets[0].Revision != 1 || a.ledger["manual-local"].Revision != 1 {
		t.Fatal("failed save changed live settings")
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	stored, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var s settings
	if err := json.Unmarshal(stored, &s); err != nil {
		t.Fatal(err)
	}
	s.Config.ManualTargets[0].Host = "127.0.0.3"
	stored, _ = json.Marshal(s)
	if err := os.WriteFile(realPath, stored, 0600); err != nil {
		t.Fatal(err)
	}
	if bad, err := Open(dir); err == nil {
		bad.Close()
		t.Fatal("same revision mismatch accepted after restart")
	}
	s.Config.ManualTargets[0].Host = "127.0.0.1"
	s.Config.ManualTargets[0].Revision = 0
	stored, _ = json.Marshal(s)
	os.WriteFile(realPath, stored, 0600)
	if bad, err := Open(dir); err == nil {
		bad.Close()
		t.Fatal("lower revision accepted after restart")
	}
}

func TestHTTPBoundariesAndManualRevisionLedger(t *testing.T) {
	dir := t.TempDir()
	a, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	a.Authority = "127.0.0.1:8742"
	c := config.Default()
	c.ManualTargets = []model.Target{{ID: "manual-local", Revision: 1, Label: "Local", Host: "127.0.0.1"}}
	b, _ := json.Marshal(c)
	if w := request(a, http.MethodPut, "/api/config", b, "", "http://"+a.Authority); w.Code != 403 {
		t.Fatalf("missing token accepted %d", w.Code)
	}
	if w := request(a, http.MethodPut, "/api/config", b, a.token, "https://untrusted.example"); w.Code != 403 {
		t.Fatalf("foreign origin accepted %d", w.Code)
	}
	if w := request(a, http.MethodPut, "/api/config", b, a.token, "http://"+a.Authority); w.Code != 200 {
		t.Fatalf("valid config %d %s", w.Code, w.Body)
	}
	c.ManualTargets[0].Host = "127.0.0.2"
	b, _ = json.Marshal(c)
	if w := request(a, http.MethodPut, "/api/config", b, a.token, ""); w.Code != 400 {
		t.Fatalf("same revision target change accepted %d", w.Code)
	}
	c.ManualTargets = nil
	b, _ = json.Marshal(c)
	if w := request(a, http.MethodPut, "/api/config", b, a.token, ""); w.Code != 200 {
		t.Fatal(w.Body)
	}
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	a, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	a.Authority = "127.0.0.1:8742"
	c.ManualTargets = []model.Target{{ID: "manual-local", Revision: 1, Label: "Changed", Host: "127.0.0.2"}}
	b, _ = json.Marshal(c)
	if w := request(a, http.MethodPut, "/api/config", b, a.token, ""); w.Code != 400 {
		t.Fatalf("deleted identity reused after restart %d", w.Code)
	}
	c.ManualTargets[0].Revision = 2
	b, _ = json.Marshal(c)
	if w := request(a, http.MethodPut, "/api/config", b, a.token, ""); w.Code != 200 {
		t.Fatalf("higher revision rejected %d %s", w.Code, w.Body)
	}
	if w := request(a, http.MethodGet, "/api/history?target_id=manual-local&revision=2&minutes=3", nil, "", ""); w.Code != 200 {
		t.Fatal(w.Body)
	} else {
		var buckets []model.Bucket
		if err := json.Unmarshal(w.Body.Bytes(), &buckets); err != nil {
			t.Fatal(err)
		}
		if len(buckets) != 3 {
			t.Fatal("wrong bucket count")
		}
		for _, v := range buckets {
			if v.Status != "na" || v.Measurement != nil {
				t.Fatal("missing measurement converted into failure")
			}
		}
	}
}

func TestHTTPRejectsBadHostCatalogueAndOversizedInput(t *testing.T) {
	a, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	a.Authority = "127.0.0.1:8742"
	r := httptest.NewRequest("GET", "http://attacker.example/api/state", nil)
	w := httptest.NewRecorder()
	a.Handler().ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("untrusted host accepted")
	}
	c := config.Default()
	c.CatalogueURL = "http://127.0.0.1/private"
	b, _ := json.Marshal(c)
	if w := request(a, "PUT", "/api/config", b, a.token, ""); w.Code != 400 {
		t.Fatal("non-GitHub URL accepted")
	}
	if w := request(a, "PUT", "/api/config", bytes.Repeat([]byte(" "), 65537), a.token, ""); w.Code != 400 {
		t.Fatalf("oversize accepted %d", w.Code)
	}
	if w := request(a, "GET", "/api/history?target_id=x&revision=1&minutes=1441", nil, "", ""); w.Code != 400 {
		t.Fatal("unbounded history accepted")
	}
	if l, err := Listen("0.0.0.0:0"); err == nil {
		l.Close()
		t.Fatal("public interface listener accepted")
	}
}

func TestEmptyStateCollectionsAreArrays(t *testing.T) {
	a, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	a.Authority = "127.0.0.1:8742"
	w := request(a, "GET", "/api/state", nil, "", "")
	if w.Code != 200 {
		t.Fatal(w.Body)
	}
	var state map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &state); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"targets", "latest"} {
		if string(state[field]) != "[]" {
			t.Fatalf("%s should be an empty array, got %s", field, state[field])
		}
	}
}
