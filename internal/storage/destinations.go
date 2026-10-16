package storage

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"time"

	"github.com/Shaderx/PingObservatory/internal/model"
)

func validDestinationIP(value, family string) bool {
	ip, err := netip.ParseAddr(value)
	return err == nil && ip.String() == value && ip.Zone() == "" && !ip.Is4In6() && !ip.IsUnspecified() && !ip.IsMulticast() && ip != netip.MustParseAddr("255.255.255.255") && (family == "" || ip.Is4() == (family == model.FamilyIPv4))
}

func decodeDestination(data []byte, family string) (*model.DestinationState, error) {
	if len(data) == 0 {
		return nil, nil
	}
	bad := func() (*model.DestinationState, error) { return nil, errors.New("corrupt local destination state") }
	if len(data) > 16384 {
		return bad()
	}
	var state model.DestinationState
	if err := json.Unmarshal(data, &state); err != nil {
		return bad()
	}
	if !validDestinationIP(state.BaselineIP, family) || !validDestinationIP(state.LastIP, family) || state.BaselineObservedAt.IsZero() || state.LastObservedAt.Before(state.BaselineObservedAt) || state.ChangeCount > 9007199254740991 || state.HistoryLimit != model.DestinationHistoryLimit || state.History == nil || len(state.History) > model.DestinationHistoryLimit || uint64(len(state.History)) > state.ChangeCount {
		return bad()
	}
	if state.ChangeCount == 0 && state.BaselineIP != state.LastIP {
		return bad()
	}
	previous := state.BaselineObservedAt
	for i, change := range state.History {
		if !validDestinationIP(change.OldIP, family) || !validDestinationIP(change.NewIP, family) || change.OldIP == change.NewIP || !change.ObservedAt.After(previous) || change.ObservedAt.After(state.LastObservedAt) || i > 0 && state.History[i-1].NewIP != change.OldIP {
			return bad()
		}
		previous = change.ObservedAt
	}
	if len(state.History) > 0 && state.History[len(state.History)-1].NewIP != state.LastIP {
		return bad()
	}
	return &state, nil
}

// updateDestination runs in the same transaction as the accepted batch. An old
// record without a captured IP is never resolved or used to backfill this state.
func updateDestination(tx *sql.Tx, id int64, m model.Measurement, cutoff time.Time) error {
	if m.DestinationIP == "" {
		return nil
	}
	var data []byte
	err := tx.QueryRow("SELECT data FROM destinations WHERE series_id=?", id).Scan(&data)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	state, err := decodeDestination(data, m.Family)
	if err != nil {
		return err
	}
	if state == nil {
		state = &model.DestinationState{BaselineIP: m.DestinationIP, BaselineObservedAt: m.At, LastIP: m.DestinationIP, LastObservedAt: m.At, HistoryLimit: model.DestinationHistoryLimit, History: []model.DestinationChange{}}
	} else {
		if m.At.Before(state.LastObservedAt) {
			return nil
		}
		if m.At.Equal(state.LastObservedAt) {
			if state.LastIP != m.DestinationIP {
				return fmt.Errorf("%w: conflicting destination at the same observation time", ErrInvalid)
			}
			return nil
		}
		kept := make([]model.DestinationChange, 0, len(state.History)+1)
		for _, change := range state.History {
			if !change.ObservedAt.Before(cutoff) {
				kept = append(kept, change)
			}
		}
		if state.LastIP != m.DestinationIP {
			if state.ChangeCount == 9007199254740991 {
				return ErrCapacity
			}
			kept = append(kept, model.DestinationChange{ObservedAt: m.At, OldIP: state.LastIP, NewIP: m.DestinationIP})
			state.ChangeCount++
		}
		if len(kept) > model.DestinationHistoryLimit {
			kept = kept[len(kept)-model.DestinationHistoryLimit:]
		}
		state.History = kept
		state.LastIP, state.LastObservedAt = m.DestinationIP, m.At
	}
	data, err = json.Marshal(state)
	if err != nil {
		return err
	}
	if len(data) > 16384 {
		return ErrCapacity
	}
	_, err = tx.Exec("INSERT INTO destinations(series_id,data) VALUES(?,?) ON CONFLICT(series_id) DO UPDATE SET data=excluded.data", id, data)
	return err
}

func destinationSnapshot(data []byte, family string, start, end time.Time) (*model.DestinationState, error) {
	state, err := decodeDestination(data, family)
	if err != nil || state == nil {
		return state, err
	}
	if state.LastObservedAt.After(end) {
		return nil, nil
	}
	kept := make([]model.DestinationChange, 0, len(state.History))
	for _, change := range state.History {
		if !change.ObservedAt.Before(start) && !change.ObservedAt.After(end) {
			kept = append(kept, change)
		}
	}
	state.History = kept
	return state, nil
}

func (s *Store) destinationForSeries(target string, revision uint64, method, family string, start, end time.Time) (*model.DestinationState, error) {
	var data []byte
	err := s.db.QueryRow("SELECT d.data FROM destinations d JOIN series s ON s.id=d.series_id WHERE s.target=? AND s.revision=? AND s.method=? AND s.family=? AND s.latest_ns>=?", target, revision, method, family, s.now().UTC().Add(-s.retention).UnixNano()).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return destinationSnapshot(data, family, start, end)
}
