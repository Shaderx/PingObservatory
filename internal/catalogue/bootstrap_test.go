package catalogue

import (
	"context"
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

	bundled "github.com/Shaderx/PingObservatory/catalogue"
	"github.com/Shaderx/PingObservatory/internal/config"
	"github.com/Shaderx/PingObservatory/internal/model"
)

func assertPopulation(t *testing.T, c model.Catalogue) {
	t.Helper()
	verified, pending, slots, numeric, hostname := 0, 0, 0, 0, 0
	for _, target := range c.Targets {
		for _, family := range target.Measurement.Families {
			slots++
			if target.Admission[family].State == "verified" {
				verified++
				if target.Admission[family].Evidence.Scope == "numeric_destination" {
					numeric++
					if family != model.FamilyIPv4 || target.Measurement.Method != model.MethodICMP {
						t.Fatal("unexpected numeric admission")
					}
				} else {
					hostname++
				}
			} else {
				pending++
			}
		}
	}
	if c.SchemaVersion != 3 || len(c.Targets) != 171 || slots != 196 || verified != 138 || numeric != 116 || hostname != 22 || pending != 58 {
		t.Fatalf("unexpected effective population: schema=%d targets=%d slots=%d verified=%d pending=%d", c.SchemaVersion, len(c.Targets), slots, verified, pending)
	}
}

func productionManager(t *testing.T, handler http.HandlerFunc) (*Manager, string) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	base, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	cache := filepath.Join(t.TempDir(), "catalogue.json")
	m, err := New(cache, DefaultURL)
	if err != nil {
		t.Fatal(err)
	}
	m.client = defaultClient()
	m.client.Transport = loopbackTransport{base: base, transport: server.Client().Transport}
	return m, cache
}

func TestProductionStarterAndPublishedAliasMatch(t *testing.T) {
	starter, err := Decode(bundled.Starter())
	if err != nil {
		t.Fatal(err)
	}
	assertPopulation(t, starter)
	for _, name := range []string{"v3.json", "latest.json"} {
		data, err := os.ReadFile(filepath.Join("..", "..", "catalogue", name))
		if err != nil {
			t.Fatal(err)
		}
		c, err := Decode(data)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(starter, c) {
			t.Fatal("starter and published snapshot/alias differ", name)
		}
	}
	if DefaultURL != bundled.DefaultURL || config.Default().CatalogueURL != DefaultURL || DefaultURL != "https://raw.githubusercontent.com/Shaderx/PingObservatory/catalogue/catalogue/latest.json" {
		t.Fatal("first-run default is not the shared data channel")
	}
}

func TestProductionFirstRunRemoteCacheAndDowngrade(t *testing.T) {
	var body atomic.Value
	body.Store(string(bundled.Starter()))
	var requests atomic.Int32
	m, cache := productionManager(t, func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Path != "/Shaderx/PingObservatory/catalogue/catalogue/latest.json" {
			t.Error("unexpected download path", r.URL.Path)
		}
		fmt.Fprint(w, body.Load().(string))
	})
	c, source := m.Snapshot()
	assertPopulation(t, c)
	if source != "bundled" || requests.Load() != 0 {
		t.Fatal("opening a profile did not load the offline starter immediately")
	}
	if err := m.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	c, source = m.Snapshot()
	assertPopulation(t, c)
	if source != "remote" || requests.Load() != 1 {
		t.Fatal("default download was not activated")
	}
	loaded, err := New(cache, DefaultURL)
	if err != nil {
		t.Fatal(err)
	}
	c, source = loaded.Snapshot()
	assertPopulation(t, c)
	if source != "cache" || loaded.LastError() != "" {
		t.Fatal("same-version cache did not reopen")
	}
	// A later release may update display metadata without changing probe identity.
	c.Version++
	c.PublishedAt = time.Now().UTC()
	c.Targets[0].Label += " updated"
	body.Store(encoded(t, c))
	if err := m.Refresh(context.Background()); err != nil {
		t.Fatal(err)
	}
	loaded, err = New(cache, DefaultURL)
	if err != nil {
		t.Fatal(err)
	}
	actual, source := loaded.Snapshot()
	assertPopulation(t, actual)
	if source != "cache" || !reflect.DeepEqual(actual, c) {
		t.Fatal("newer last-valid cache lost to older bundle")
	}
	before, err := os.ReadFile(cache)
	if err != nil {
		t.Fatal(err)
	}
	body.Store(string(bundled.Starter()))
	if err := m.Refresh(context.Background()); err == nil || !strings.Contains(err.Error(), "downgrade") {
		t.Fatal("older channel response accepted", err)
	}
	body.Store(`{"schema_version":2,"version":4,"targets":[]}`)
	if err := m.Refresh(context.Background()); err == nil {
		t.Fatal("malformed update accepted")
	}
	after, err := os.ReadFile(cache)
	if err != nil {
		t.Fatal(err)
	}
	actual, _ = m.Snapshot()
	if string(before) != string(after) || !reflect.DeepEqual(actual, c) || m.LastError() == "" {
		t.Fatal("failed update replaced effective data/cache or hid its warning")
	}
}

func TestProductionFailuresKeepPopulatedStarter(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusServiceUnavailable} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			m, cache := productionManager(t, func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status) })
			if err := m.Refresh(context.Background()); err == nil {
				t.Fatal("failed download accepted")
			}
			c, source := m.Snapshot()
			assertPopulation(t, c)
			if source != "bundled" || m.LastError() == "" {
				t.Fatal("failed first download lost the populated fallback/warning")
			}
			if _, err := os.Stat(cache); !os.IsNotExist(err) {
				t.Fatal("failed response wrote a cache")
			}
		})
	}
	path := filepath.Join(t.TempDir(), "catalogue.json")
	for _, data := range []string{`{"cache_version":1}`, `{"broken":true}`} {
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
		m, err := New(path, "")
		if err != nil {
			t.Fatal(err)
		}
		c, source := m.Snapshot()
		assertPopulation(t, c)
		if source != "bundled" || m.LastError() == "" {
			t.Fatal("malformed cache did not warn and use populated bundle")
		}
		warning := m.LastError()
		if err := m.Refresh(context.Background()); err != nil || m.LastError() != warning {
			t.Fatal("disabled updater hid the rejected-cache warning", err)
		}
	}
	legacy, err := legacyManager(filepath.Join(t.TempDir(), "legacy.json"), "")
	if err != nil {
		t.Fatal(err)
	}
	old, _ := legacy.Snapshot()
	if err := writeCache(path, old, map[string]model.Target{}); err != nil {
		t.Fatal(err)
	}
	m, err := New(path, "")
	if err != nil {
		t.Fatal(err)
	}
	c, source := m.Snapshot()
	assertPopulation(t, c)
	if source != "bundled" || !strings.Contains(m.LastError(), "downgrade") {
		t.Fatal("legacy empty cache downgraded the real starter")
	}
	data, err := os.ReadFile(filepath.Join("..", "..", "catalogue", "v2.json"))
	if err != nil {
		t.Fatal(err)
	}
	v2, err := Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	identities := map[string]model.Target{}
	for _, target := range v2.Targets {
		identities[target.ID] = target
	}
	removed := fixtureV2(2).Targets[0]
	removed.ID = "removed-preview-target"
	identities[removed.ID] = removed
	if err := writeCache(path, v2, identities); err != nil {
		t.Fatal(err)
	}
	m, err = New(path, "")
	if err != nil {
		t.Fatal(err)
	}
	c, source = m.Snapshot()
	assertPopulation(t, c)
	if source != "bundled" || !strings.Contains(m.LastError(), "downgrade") {
		t.Fatal("older populated v2 cache downgraded schema3 starter")
	}
	retained, exists := m.identities[removed.ID]
	if !exists || retained.Revision != removed.Revision || retained.Admission["ipv4"].Evidence.Scope != "numeric_destination" {
		t.Fatal("new starter discarded an older cache's removed-ID revision guard")
	}
	next := cloneCatalogue(c)
	next.Version++
	retained.Host = "changed.example.com"
	next.Targets = append(next.Targets, retained)
	if err := validateTransition(c, m.identities, next); err == nil {
		t.Fatal("removed ID could reset its identity after bundled-schema migration")
	}
}

func TestProductionDownloadDeadlineKeepsPopulatedStarter(t *testing.T) {
	m, cache := productionManager(t, func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() })
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	if err := m.Refresh(ctx); err == nil {
		t.Fatal("deadline did not fail the download")
	}
	c, source := m.Snapshot()
	assertPopulation(t, c)
	if source != "bundled" || m.LastError() == "" {
		t.Fatal("timed-out first download lost data or warning")
	}
	if _, err := os.Stat(cache); !os.IsNotExist(err) {
		t.Fatal("timed-out download persisted a cache")
	}
}
