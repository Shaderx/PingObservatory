package app

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Shaderx/PingObservatory/internal/catalogue"
	"github.com/Shaderx/PingObservatory/internal/collector"
	"github.com/Shaderx/PingObservatory/internal/config"
	"github.com/Shaderx/PingObservatory/internal/legal"
	"github.com/Shaderx/PingObservatory/internal/model"
	"github.com/Shaderx/PingObservatory/internal/probe"
	"github.com/Shaderx/PingObservatory/internal/storage"
	"github.com/Shaderx/PingObservatory/internal/web"
)

type App struct {
	mu             sync.RWMutex
	refreshMu      sync.Mutex
	config         model.Config
	configPath     string
	ledger         map[string]model.Target
	store          *storage.Store
	catalogue      *catalogue.Manager
	collector      *collector.Collector
	catalogueError string
	token          string
	Authority      string
}

func Open(dir string) (*App, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, "config.json")
	c, ledger, err := loadSettings(path)
	if err != nil {
		return nil, err
	}
	if err := catalogue.ValidateURL(c.CatalogueURL); err != nil {
		return nil, err
	}
	cat, err := catalogue.New(filepath.Join(dir, "catalogue.json"), c.CatalogueURL)
	if err != nil {
		return nil, err
	}
	s, err := storage.Open(filepath.Join(dir, "measurements.db"), c.RetentionDays)
	if err != nil {
		return nil, err
	}
	if err := saveSettings(path, c, ledger); err != nil {
		s.Close()
		return nil, err
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		s.Close()
		return nil, err
	}
	a := &App{config: c, configPath: path, ledger: ledger, store: s, catalogue: cat, token: hex.EncodeToString(b)}
	a.collector = &collector.Collector{Targets: a.Targets, Probe: probe.Probe, Record: s.Add, Wake: make(chan struct{}, 1)}
	return a, nil
}

func (a *App) Close() error { return a.store.Close() }

func (a *App) Targets() []model.Target {
	a.mu.RLock()
	manual := make([]model.Target, len(a.config.ManualTargets), len(a.config.ManualTargets)+catalogue.MaxTargets)
	copy(manual, a.config.ManualTargets)
	a.mu.RUnlock()
	cat, _ := a.catalogue.Snapshot()
	return append(manual, cat.Targets...)
}

func (a *App) Run(ctx context.Context) {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); a.collector.Run(ctx) }()
	wg.Add(1)
	go func() {
		defer wg.Done()
		if ctx.Err() == nil {
			a.refresh(ctx)
		}
		t := time.NewTicker(6 * time.Hour)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				a.refresh(ctx)
			}
		}
	}()
	wg.Wait()
}

func (a *App) refresh(ctx context.Context) error {
	a.refreshMu.Lock()
	defer a.refreshMu.Unlock()
	err := a.catalogue.Refresh(ctx)
	a.mu.Lock()
	if err != nil {
		a.catalogueError = err.Error()
	} else {
		a.catalogueError = ""
	}
	a.mu.Unlock()
	select {
	case a.collector.Wake <- struct{}{}:
	default:
	}
	return err
}

func (a *App) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/state", a.state)
	mux.HandleFunc("GET /api/history", a.history)
	mux.HandleFunc("PUT /api/config", a.updateConfig)
	mux.HandleFunc("POST /api/catalogue/refresh", a.refreshHTTP)
	mux.HandleFunc("GET /licenses.txt", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		io.WriteString(w, legal.Text())
	})
	mux.Handle("/", web.Handler())
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; worker-src 'self' blob:; connect-src 'self'; frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		if r.Host != a.Authority {
			http.Error(w, "invalid local host", http.StatusForbidden)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			w.Header().Set("Cache-Control", "no-store")
			if r.Method != http.MethodGet {
				if origin := r.Header.Get("Origin"); origin != "" && origin != "http://"+a.Authority {
					http.Error(w, "invalid origin", http.StatusForbidden)
					return
				}
				if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Ping-Token")), []byte(a.token)) != 1 {
					http.Error(w, "invalid session token", http.StatusForbidden)
					return
				}
			}
		}
		mux.ServeHTTP(w, r)
	})
}

func JSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, code int, err error) {
	JSON(w, code, map[string]string{"error": err.Error()})
}

func (a *App) state(w http.ResponseWriter, r *http.Request) {
	latest, err := a.store.Latest()
	if err != nil {
		fail(w, 500, err)
		return
	}
	if latest == nil {
		latest = []model.Measurement{}
	}
	a.mu.RLock()
	c := a.config
	lastError := a.catalogueError
	a.mu.RUnlock()
	cat, source := a.catalogue.Snapshot()
	if lastError == "" {
		lastError = a.catalogue.LastError()
	}
	JSON(w, 200, map[string]any{"config": c, "targets": a.Targets(), "latest": latest, "catalogue": map[string]any{"version": cat.Version, "source": source, "last_error": lastError}, "collector": a.collector.Snapshot(), "csrf_token": a.token, "limits": map[string]int{"manual_targets": 16, "catalogue_targets": 64, "max_rows": 100000, "history_minutes": 1440}})
}

func (a *App) history(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	id := q.Get("target_id")
	rev, err := strconv.ParseUint(q.Get("revision"), 10, 64)
	if err != nil || rev == 0 {
		fail(w, 400, errors.New("revision must be positive"))
		return
	}
	minutes := 60
	if q.Get("minutes") != "" {
		minutes, err = strconv.Atoi(q.Get("minutes"))
		if err != nil {
			fail(w, 400, errors.New("invalid minutes"))
			return
		}
	}
	if minutes < 1 || minutes > 1440 || len(id) == 0 || len(id) > 80 {
		fail(w, 400, errors.New("invalid history window or target_id"))
		return
	}
	end := time.Now().UTC().Truncate(time.Minute).Add(time.Minute)
	buckets, err := a.store.Buckets(id, rev, end.Add(-time.Duration(minutes)*time.Minute), end)
	if err != nil {
		fail(w, 500, err)
		return
	}
	JSON(w, 200, buckets)
}

func (a *App) updateConfig(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
		fail(w, 415, errors.New("Content-Type must be application/json"))
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64*1024)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	var c model.Config
	if err := d.Decode(&c); err != nil {
		fail(w, 400, err)
		return
	}
	if err := d.Decode(new(any)); !errors.Is(err, io.EOF) {
		fail(w, 400, errors.New("request contains trailing data"))
		return
	}
	if err := config.Validate(c); err != nil {
		fail(w, 400, err)
		return
	}
	if err := catalogue.ValidateURL(c.CatalogueURL); err != nil {
		fail(w, 400, err)
		return
	}
	a.refreshMu.Lock()
	defer a.refreshMu.Unlock()
	a.mu.Lock()
	defer a.mu.Unlock()
	ledger := make(map[string]model.Target, len(a.ledger)+len(c.ManualTargets))
	for k, v := range a.ledger {
		ledger[k] = v
	}
	for _, t := range c.ManualTargets {
		if old, ok := ledger[t.ID]; ok {
			if t.Revision < old.Revision || t.Revision == old.Revision && !reflect.DeepEqual(t, old) {
				fail(w, 400, errors.New("target changes require a higher revision; revisions cannot decrease"))
				return
			}
		}
		ledger[t.ID] = t
	}
	if len(ledger) > 4096 {
		fail(w, 400, errors.New("manual identity ledger limit reached"))
		return
	}
	// Configuration and identity history are one atomic document, so a crash
	// cannot commit one without the other.
	if err := saveSettings(a.configPath, c, ledger); err != nil {
		fail(w, 500, err)
		return
	}
	a.config = c
	a.ledger = ledger
	if err := a.catalogue.SetURL(c.CatalogueURL); err != nil {
		fail(w, 500, err)
		return
	}
	if err := a.store.SetRetention(c.RetentionDays); err != nil {
		fail(w, 500, err)
		return
	}
	select {
	case a.collector.Wake <- struct{}{}:
	default:
	}
	JSON(w, 200, c)
}

type settings struct {
	SchemaVersion int                     `json:"schema_version"`
	Config        model.Config            `json:"config"`
	Identities    map[string]model.Target `json:"manual_identities"`
}

func loadSettings(path string) (model.Config, map[string]model.Target, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return config.Default(), map[string]model.Target{}, nil
	}
	if err != nil {
		return model.Config{}, nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return model.Config{}, nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > 2*1024*1024 {
		return model.Config{}, nil, errors.New("settings must be a bounded regular file")
	}
	d := json.NewDecoder(io.LimitReader(f, 2*1024*1024+1))
	d.DisallowUnknownFields()
	var s settings
	if err := d.Decode(&s); err != nil {
		return model.Config{}, nil, fmt.Errorf("read settings: %w", err)
	}
	if err := d.Decode(new(any)); !errors.Is(err, io.EOF) {
		return model.Config{}, nil, errors.New("settings contain trailing data")
	}
	if s.SchemaVersion != 1 || s.Identities == nil || len(s.Identities) > 4096 {
		return model.Config{}, nil, errors.New("invalid settings schema or identity ledger")
	}
	if err := config.Validate(s.Config); err != nil {
		return model.Config{}, nil, err
	}
	for id, t := range s.Identities {
		check := config.Default()
		check.ManualTargets = []model.Target{t}
		if id != t.ID {
			return model.Config{}, nil, errors.New("identity ledger key mismatch")
		}
		if err := config.Validate(check); err != nil {
			return model.Config{}, nil, err
		}
	}
	for _, t := range s.Config.ManualTargets {
		if old, ok := s.Identities[t.ID]; !ok || !reflect.DeepEqual(old, t) {
			return model.Config{}, nil, errors.New("config target disagrees with revision ledger")
		}
	}
	return s.Config, s.Identities, nil
}

func saveSettings(path string, c model.Config, ledger map[string]model.Target) error {
	b, err := json.MarshalIndent(settings{1, c, ledger}, "", "  ")
	if err != nil {
		return err
	}
	if len(b)+1 > 2*1024*1024 {
		return errors.New("settings exceed size limit")
	}
	return writeAtomic(path, append(b, '\n'))
}

func writeAtomic(path string, b []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".ledger-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err := f.Chmod(0600); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), path)
}

func (a *App) refreshHTTP(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	if err := a.refresh(ctx); err != nil {
		fail(w, 502, err)
		return
	}
	JSON(w, 200, map[string]string{"status": "updated"})
}

func Listen(address string) (net.Listener, error) {
	host, _, err := net.SplitHostPort(address)
	if err != nil || host != "127.0.0.1" && host != "::1" {
		return nil, errors.New("listen address must be 127.0.0.1:port or [::1]:port")
	}
	return net.Listen("tcp", address)
}
