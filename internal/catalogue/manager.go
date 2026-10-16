// Package catalogue validates and updates data-only endpoint catalogues.
package catalogue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"sync"
	"time"

	bundled "github.com/Shaderx/PingObservatory/catalogue"
	"github.com/Shaderx/PingObservatory/internal/model"
)

const (
	maxIdentities = 4096
	maxCacheBytes = 16 * 1024 * 1024
	fetchTimeout  = 15 * time.Second
)

type cacheState struct {
	CacheVersion int             `json:"cache_version"`
	Catalogue    model.Catalogue `json:"catalogue"`
	Identities   []model.Target  `json:"identities"`
}

// Manager keeps a last-valid catalogue and a bounded revision ledger. All
// methods are safe for concurrent collector, API, and updater calls.
type Manager struct {
	mu         sync.RWMutex
	refreshMu  sync.Mutex
	cachePath  string
	url        string
	generation uint64
	current    model.Catalogue
	identities map[string]model.Target
	source     string
	lastError  string
	client     *http.Client
}

// New loads the bundled starter, then a valid last-known catalogue if present.
// A missing or malformed cache falls back to the starter; LastError exposes a
// malformed-cache warning. Invalid configuration or starter data is fatal.
func New(cachePath, url string) (*Manager, error) {
	if cachePath == "" {
		return nil, errors.New("catalogue cache path is required")
	}
	if err := ValidateURL(url); err != nil {
		return nil, err
	}
	c, err := Decode(bundled.Starter())
	if err != nil {
		return nil, fmt.Errorf("invalid bundled catalogue: %w", err)
	}
	m := &Manager{cachePath: cachePath, url: url, current: c, source: "bundled", identities: map[string]model.Target{}, client: defaultClient()}
	for _, target := range c.Targets {
		m.identities[target.ID] = cloneTarget(target)
	}
	state, err := readCache(cachePath)
	if errors.Is(err, os.ErrNotExist) {
		return m, nil
	}
	if err != nil {
		m.lastError = "catalogue cache ignored: " + err.Error()
		return m, nil
	}
	if err = validateTransition(c, m.identities, state.Catalogue); err != nil {
		m.lastError = "catalogue cache ignored: " + err.Error()
		return m, nil
	}
	identities := state.identitiesMap()
	for _, target := range c.Targets {
		old, ok := identities[target.ID]
		if !ok || old.Revision < target.Revision || (old.Revision == target.Revision && !sameProbeDefinition(old, target)) {
			m.lastError = "catalogue cache ignored: inconsistent starter identity"
			return m, nil
		}
	}
	m.current, m.identities, m.source = state.Catalogue, identities, "cache"
	return m, nil
}

// Snapshot returns an independent copy and its source: bundled, cache, or remote.
func (m *Manager) Snapshot() (model.Catalogue, string) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return cloneCatalogue(m.current), m.source
}

// LastError returns the latest fetch/cache warning; a successful refresh clears it.
func (m *Manager) LastError() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.lastError
}

func (m *Manager) SetURL(raw string) error {
	if err := ValidateURL(raw); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.url != raw {
		m.url = raw
		m.generation++
	}
	return nil
}

// Refresh fetches at most 4 MiB with a 15-second deadline, validates the entire
// document and revision ledger, and replaces the cache before exposing new data.
// Fetch failures leave the previous catalogue and its source unchanged.
func (m *Manager) Refresh(ctx context.Context) (err error) {
	m.refreshMu.Lock()
	defer m.refreshMu.Unlock()
	defer func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		if err != nil {
			m.lastError = err.Error()
		} else {
			m.lastError = ""
		}
	}()
	m.mu.RLock()
	raw, generation, client := m.url, m.generation, m.client
	m.mu.RUnlock()
	if raw == "" {
		return nil
	}
	if err = ValidateURL(raw); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "PingObservatory/1")
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("fetch catalogue: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("fetch catalogue: HTTP %d", response.StatusCode)
	}
	if response.ContentLength > MaxCatalogueBytes {
		return errors.New("catalogue exceeds size limit")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, MaxCatalogueBytes+1))
	if err != nil {
		return fmt.Errorf("read catalogue: %w", err)
	}
	candidate, err := Decode(data)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.generation != generation {
		return errors.New("catalogue URL changed during refresh")
	}
	if err := validateTransition(m.current, m.identities, candidate); err != nil {
		return err
	}
	identities := make(map[string]model.Target, len(m.identities)+len(candidate.Targets))
	for id, target := range m.identities {
		identities[id] = cloneTarget(target)
	}
	for _, target := range candidate.Targets {
		identities[target.ID] = cloneTarget(target)
	}
	if len(identities) > maxIdentities {
		return errors.New("catalogue identity ledger is full")
	}
	if err := writeCache(m.cachePath, candidate, identities); err != nil {
		return fmt.Errorf("persist catalogue: %w", err)
	}
	m.current, m.identities, m.source = candidate, identities, "remote"
	return nil
}

func defaultClient() *http.Client {
	return &http.Client{Timeout: fetchTimeout, CheckRedirect: func(request *http.Request, via []*http.Request) error {
		if len(via) >= 3 {
			return errors.New("too many catalogue redirects")
		}
		return ValidateURL(request.URL.String())
	}}
}

func validateTransition(current model.Catalogue, identities map[string]model.Target, next model.Catalogue) error {
	if next.Version < current.Version {
		return errors.New("catalogue version downgrade rejected")
	}
	if next.SchemaVersion < current.SchemaVersion {
		return errors.New("catalogue schema downgrade rejected")
	}
	if next.Version == current.Version && !reflect.DeepEqual(next, current) {
		return errors.New("catalogue version equivocation rejected")
	}
	if next.PublishedAt.Before(current.PublishedAt) {
		return errors.New("catalogue publication time regression rejected")
	}
	for _, target := range next.Targets {
		if old, ok := identities[target.ID]; ok {
			if target.Revision < old.Revision {
				return fmt.Errorf("target %q revision downgrade rejected", target.ID)
			}
			unchanged := sameProbeDefinition(target, old)
			if next.SchemaVersion == 1 {
				unchanged = reflect.DeepEqual(target, old)
			}
			if target.Revision == old.Revision && !unchanged {
				return fmt.Errorf("target %q changed without a new revision", target.ID)
			}
		}
	}
	return nil
}

func (state cacheState) identitiesMap() map[string]model.Target {
	identities := make(map[string]model.Target, len(state.Identities))
	for _, target := range state.Identities {
		identities[target.ID] = cloneTarget(target)
	}
	return identities
}

func readCache(path string) (cacheState, error) {
	var state cacheState
	info, err := os.Lstat(path)
	if err != nil {
		return state, err
	}
	if !info.Mode().IsRegular() || info.Size() > maxCacheBytes {
		return state, errors.New("cache must be a bounded regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return state, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxCacheBytes+1))
	if err != nil {
		return state, err
	}
	if len(data) > maxCacheBytes {
		return state, errors.New("cache exceeds size limit")
	}
	if err := decodeStrict(data, &state); err != nil {
		return state, err
	}
	if state.CacheVersion != 1 || state.Identities == nil || len(state.Identities) > maxIdentities {
		return state, errors.New("invalid cache schema or identity count")
	}
	var raw struct {
		Catalogue  json.RawMessage `json:"catalogue"`
		Identities json.RawMessage `json:"identities"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return state, err
	}
	state.Catalogue, err = Decode(raw.Catalogue)
	if err != nil {
		return state, err
	}
	if err := requireTargetFieldsForSchema(raw.Identities, state.Catalogue.SchemaVersion); err != nil {
		return state, fmt.Errorf("invalid cached identities: %w", err)
	}
	seen := make(map[string]model.Target, len(state.Identities))
	for i, target := range state.Identities {
		if target.VerifiedAt != nil {
			at := target.VerifiedAt.UTC()
			target.VerifiedAt = &at
			state.Identities[i] = target
		}
		var err error
		if state.Catalogue.SchemaVersion == 2 {
			normalizeTarget(&target)
			state.Identities[i] = target
			err = validateTargetV2(target, state.Catalogue.PublishedAt)
		} else {
			err = validateTarget(target, state.Catalogue.PublishedAt)
		}
		if err != nil {
			return state, err
		}
		if _, ok := seen[target.ID]; ok {
			return state, errors.New("duplicate cached identity")
		}
		seen[target.ID] = target
	}
	for _, target := range state.Catalogue.Targets {
		if old, ok := seen[target.ID]; !ok || !reflect.DeepEqual(old, target) {
			return state, errors.New("cache catalogue and identity ledger disagree")
		}
	}
	return state, nil
}

func writeCache(path string, c model.Catalogue, identities map[string]model.Target) error {
	state := cacheState{CacheVersion: 1, Catalogue: c, Identities: make([]model.Target, 0, len(identities))}
	for _, target := range identities {
		state.Identities = append(state.Identities, target)
	}
	sort.Slice(state.Identities, func(i, j int) bool { return state.Identities[i].ID < state.Identities[j].ID })
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	if len(data) > maxCacheBytes {
		return errors.New("cache exceeds size limit")
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	file, err := os.CreateTemp(dir, ".catalogue-*")
	if err != nil {
		return err
	}
	name := file.Name()
	defer os.Remove(name)
	if err := file.Chmod(0600); err != nil {
		file.Close()
		return err
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

func cloneCatalogue(c model.Catalogue) model.Catalogue {
	targets := make([]model.Target, len(c.Targets))
	for i, target := range c.Targets {
		targets[i] = cloneTarget(target)
	}
	c.Targets = targets
	return c
}

func cloneTarget(t model.Target) model.Target {
	if t.Location != nil {
		point := *t.Location
		t.Location = &point
	}
	if t.VerifiedAt != nil {
		at := *t.VerifiedAt
		t.VerifiedAt = &at
	}
	t.Tags = append([]string(nil), t.Tags...)
	t.Measurement.Families = append([]string(nil), t.Measurement.Families...)
	t.Measurement.ExpectedStatusCodes = append([]int(nil), t.Measurement.ExpectedStatusCodes...)
	t.Measurement.DNSExpectedAnswers = append([]string(nil), t.Measurement.DNSExpectedAnswers...)
	if t.Measurement.ProviderIPv6Support != nil {
		support := *t.Measurement.ProviderIPv6Support
		t.Measurement.ProviderIPv6Support = &support
	}
	if t.Admission != nil {
		admissions := make(map[string]model.Admission, len(t.Admission))
		for family, admission := range t.Admission {
			if admission.Evidence != nil {
				evidence := *admission.Evidence
				admission.Evidence = &evidence
			}
			admissions[family] = admission
		}
		t.Admission = admissions
	}
	return t
}

// Revision tracks the intrinsic probe definition. A higher catalogue version
// can refresh display metadata or per-family admission/evidence at the same
// target revision without splitting the user's measurement history.
func sameProbeDefinition(a, b model.Target) bool {
	return a.Host == b.Host && a.Port == b.Port && a.Scheme == b.Scheme && a.Path == b.Path && reflect.DeepEqual(a.Measurement, b.Measurement)
}

func normalizeTarget(t *model.Target) {
	if t.VerifiedAt != nil {
		at := t.VerifiedAt.UTC()
		t.VerifiedAt = &at
	}
	for family, admission := range t.Admission {
		if admission.Evidence != nil {
			evidence := *admission.Evidence
			evidence.ObservedAt = evidence.ObservedAt.UTC()
			admission.Evidence = &evidence
			t.Admission[family] = admission
		}
	}
}
