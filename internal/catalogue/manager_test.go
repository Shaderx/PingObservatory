package catalogue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Shaderx/PingObservatory/internal/model"
)

func fixture(version uint64) model.Catalogue {
	verified := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	return model.Catalogue{SchemaVersion: 1, Version: version, PublishedAt: time.Date(2026, 10, 14, 0, 0, 0, 0, time.UTC), Targets: []model.Target{{ID: "test-endpoint", Revision: 1, Label: "Test endpoint", Host: "example.com", Location: &model.Point{Latitude: 20, Longitude: 10}, VerifiedAt: &verified, VerificationSource: VerificationSource, VerificationMethod: "icmp", VerificationFamily: "ipv4", VerificationDestination: "1.1.1.1"}}}
}

func encoded(t *testing.T, value any) string {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// loopbackTransport routes the production GitHub URL to httptest. It neither
// relaxes URL validation nor makes an external network request.
type loopbackTransport struct {
	base      *url.URL
	transport http.RoundTripper
}

func (r loopbackTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	clone := request.Clone(request.Context())
	clone.URL.Scheme, clone.URL.Host = r.base.Scheme, r.base.Host
	response, err := r.transport.RoundTrip(clone)
	if response != nil {
		response.Request = request
	}
	return response, err
}

func testManager(t *testing.T, handler http.HandlerFunc) (*Manager, string) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	base, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	cache := filepath.Join(t.TempDir(), "data", "catalogue.json")
	m, err := New(cache, DefaultURL)
	if err != nil {
		t.Fatal(err)
	}
	m.client = defaultClient()
	m.client.Transport = loopbackTransport{base: base, transport: server.Client().Transport}
	return m, cache
}

func TestFallbackAndPersistence(t *testing.T) {
	var body atomic.Value
	body.Store(encoded(t, fixture(2)))
	m, cache := testManager(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != "application/json" {
			t.Error("missing JSON Accept header")
		}
		fmt.Fprint(w, body.Load().(string))
	})
	if c, source := m.Snapshot(); source != "bundled" || len(c.Targets) != 0 {
		t.Fatalf("unexpected starter %s %+v", source, c)
	}
	if err := m.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if c, source := m.Snapshot(); source != "remote" || c.Version != 2 {
		t.Fatalf("unexpected remote state %s %+v", source, c)
	}
	// Returned records and points cannot mutate collector state.
	c, _ := m.Snapshot()
	c.Targets[0].Host = "changed.example.com"
	c.Targets[0].Location.Latitude = 80
	actual, _ := m.Snapshot()
	if !reflect.DeepEqual(actual, fixture(2)) {
		t.Fatal("Snapshot shared mutable catalogue data")
	}
	loaded, err := New(cache, DefaultURL)
	if err != nil {
		t.Fatal(err)
	}
	if c, source := loaded.Snapshot(); source != "cache" || !reflect.DeepEqual(c, fixture(2)) {
		t.Fatalf("last-valid cache did not load: %s %+v", source, c)
	}
	body.Store(`{"schema_version":1,"version":3,"published_at":"2026-10-14T00:00:00Z","targets":[],"command":"run"}`)
	if err := m.Refresh(context.Background()); err == nil {
		t.Fatal("accepted executable unknown field")
	}
	if m.LastError() == "" {
		t.Fatal("missing refresh error")
	}
	if c, source := m.Snapshot(); source != "remote" || c.Version != 2 {
		t.Fatal("failed refresh changed active catalogue")
	}
	loaded, err = New(cache, "")
	if err != nil {
		t.Fatal(err)
	}
	if c, _ := loaded.Snapshot(); c.Version != 2 {
		t.Fatal("failed refresh changed last-valid cache")
	}
	body.Store(encoded(t, fixture(2)))
	if err := m.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	if m.LastError() != "" {
		t.Fatal("successful refresh did not clear error")
	}
}

func TestMalformedCacheUsesBundled(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalogue.json")
	if err := os.WriteFile(path, []byte(`{"cache_version":1}`), 0600); err != nil {
		t.Fatal(err)
	}
	m, err := New(path, DefaultURL)
	if err != nil {
		t.Fatal(err)
	}
	if c, source := m.Snapshot(); source != "bundled" || len(c.Targets) != 0 {
		t.Fatal("malformed cache did not use starter")
	}
	if !strings.Contains(m.LastError(), "cache ignored") {
		t.Fatal("malformed cache warning absent")
	}
}

func TestVersionAndRevisionContinuity(t *testing.T) {
	var body atomic.Value
	body.Store(encoded(t, fixture(2)))
	m, cache := testManager(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body.Load().(string)) })
	if err := m.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	changed := fixture(2)
	changed.Targets[0].Label = "Changed"
	body.Store(encoded(t, changed))
	if err := m.Refresh(context.Background()); err == nil || !strings.Contains(err.Error(), "equivocation") {
		t.Fatalf("same version changed: %v", err)
	}
	body.Store(encoded(t, fixture(1)))
	if err := m.Refresh(context.Background()); err == nil || !strings.Contains(err.Error(), "downgrade") {
		t.Fatalf("lower version accepted: %v", err)
	}
	changed.Version = 3
	body.Store(encoded(t, changed))
	if err := m.Refresh(context.Background()); err == nil || !strings.Contains(err.Error(), "revision") {
		t.Fatalf("same revision changed: %v", err)
	}
	changed.Targets[0].Revision = 2
	body.Store(encoded(t, changed))
	if err := m.Refresh(context.Background()); err != nil {
		t.Fatalf("new target revision rejected: %v", err)
	}
	removed := fixture(4)
	removed.Targets = []model.Target{}
	body.Store(encoded(t, removed))
	if err := m.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Removal cannot reset a stable identity, including after an app restart.
	loaded, err := New(cache, DefaultURL)
	if err != nil {
		t.Fatal(err)
	}
	loaded.client = m.client
	body.Store(encoded(t, fixture(5)))
	if err := loaded.Refresh(context.Background()); err == nil || !strings.Contains(err.Error(), "revision downgrade") {
		t.Fatalf("removed identity downgrade accepted: %v", err)
	}
	changed.Version = 5
	body.Store(encoded(t, changed))
	if err := loaded.Refresh(context.Background()); err != nil {
		t.Fatalf("unchanged removed identity could not return: %v", err)
	}
}

func TestFetchLimitsAndStatus(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		body   string
		length string
	}{
		{"unknown length too large", 200, strings.Repeat(" ", MaxCatalogueBytes+1), ""},
		{"declared too large", 200, "", fmt.Sprint(MaxCatalogueBytes + 1)},
		{"not found", 404, encoded(t, fixture(2)), ""},
		{"bad json", 200, "{", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			m, cache := testManager(t, func(w http.ResponseWriter, r *http.Request) {
				if test.length != "" {
					w.Header().Set("Content-Length", test.length)
				}
				w.WriteHeader(test.status)
				fmt.Fprint(w, test.body)
			})
			if err := m.Refresh(context.Background()); err == nil {
				t.Fatal("invalid response accepted")
			}
			if _, source := m.Snapshot(); source != "bundled" {
				t.Fatal("failed response changed starter")
			}
			if _, err := os.Stat(cache); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("failed response persisted cache")
			}
		})
	}
}

func TestContextDeadline(t *testing.T) {
	m, _ := testManager(t, func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() })
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := m.Refresh(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline not preserved: %v", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("request ignored deadline")
	}
}

func TestRedirectOutsideAllowlist(t *testing.T) {
	var requests atomic.Int32
	m, _ := testManager(t, func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Redirect(w, r, "https://example.com/catalogue.json", http.StatusFound)
	})
	if err := m.Refresh(context.Background()); err == nil {
		t.Fatal("external redirect accepted")
	}
	if requests.Load() != 1 {
		t.Fatal("redirect made another network request")
	}
}

func TestURLChangeDuringRefresh(t *testing.T) {
	started, finish := make(chan struct{}), make(chan struct{})
	defer func() {
		select {
		case <-finish:
		default:
			close(finish)
		}
	}()
	body := encoded(t, fixture(2))
	m, _ := testManager(t, func(w http.ResponseWriter, r *http.Request) { close(started); <-finish; fmt.Fprint(w, body) })
	done := make(chan error, 1)
	go func() { done <- m.Refresh(context.Background()) }()
	select {
	case <-started:
	case err := <-done:
		t.Fatalf("refresh ended before the loopback handler started: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("loopback handler did not start before the test deadline")
	}
	if err := m.SetURL(""); err != nil {
		t.Fatal(err)
	}
	// Even a change back to the original URL invalidates the in-flight fetch.
	if err := m.SetURL(DefaultURL); err != nil {
		t.Fatal(err)
	}
	close(finish)
	if err := <-done; err == nil || !strings.Contains(err.Error(), "URL changed") {
		t.Fatalf("stale URL response committed: %v", err)
	}
	if _, source := m.Snapshot(); source != "bundled" {
		t.Fatal("stale response changed current catalogue")
	}
}

func TestCacheFailurePreservesCurrent(t *testing.T) {
	body := encoded(t, fixture(2))
	m, cache := testManager(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) })
	if err := os.MkdirAll(cache, 0700); err != nil {
		t.Fatal(err)
	}
	if err := m.Refresh(context.Background()); err == nil {
		t.Fatal("cache write failure was ignored")
	}
	if _, source := m.Snapshot(); source != "bundled" {
		t.Fatal("unpersisted catalogue became active")
	}
	entries, err := os.ReadDir(filepath.Dir(cache))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".catalogue-") {
			t.Fatal("failed cache write leaked temporary file")
		}
	}
}

func TestRemovedCachedIdentityRequiresCompleteRecord(t *testing.T) {
	c := fixture(2)
	c.Targets = []model.Target{}
	state := cacheState{CacheVersion: 1, Catalogue: c, Identities: fixture(2).Targets}
	data := strings.Replace(encoded(t, state), `"latitude":20,`, "", 1)
	path := filepath.Join(t.TempDir(), "catalogue.json")
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	m, err := New(path, DefaultURL)
	if err != nil {
		t.Fatal(err)
	}
	if _, source := m.Snapshot(); source != "bundled" || m.LastError() == "" {
		t.Fatal("incomplete removed identity was accepted from cache")
	}
}
