package probe

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/netip"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/Shaderx/PingObservatory/internal/model"
)

func probeHTTPS(ctx context.Context, t model.Target, addr netip.Addr, p model.ProbeSettings, roots *x509.CertPool) (result model.Measurement) {
	defer func() { result = captureDestination(result, addr) }()
	call, cancel := context.WithTimeout(ctx, time.Duration(p.TimeoutMS)*time.Millisecond)
	defer cancel()
	var mu sync.Mutex
	stage := "connect"
	var connected bool
	var connectStarted, tlsStarted time.Time
	timings := map[string]float64{}
	trace := &httptrace.ClientTrace{
		ConnectStart: func(_, _ string) { mu.Lock(); connectStarted = time.Now(); stage = "connect"; mu.Unlock() },
		ConnectDone: func(_, _ string, err error) {
			mu.Lock()
			defer mu.Unlock()
			timings["connect"] = float64(time.Since(connectStarted)) / float64(time.Millisecond)
			connected = err == nil
		},
		TLSHandshakeStart: func() { mu.Lock(); tlsStarted = time.Now(); stage = "tls"; mu.Unlock() },
		TLSHandshakeDone: func(_ tls.ConnectionState, _ error) {
			mu.Lock()
			timings["tls"] = float64(time.Since(tlsStarted)) / float64(time.Millisecond)
			mu.Unlock()
		},
		WroteRequest: func(_ httptrace.WroteRequestInfo) { mu.Lock(); stage = "response"; mu.Unlock() },
	}
	call = httptrace.WithClientTrace(call, trace)
	transport := &http.Transport{Proxy: nil, DisableKeepAlives: true, TLSClientConfig: &tls.Config{ServerName: t.Host, RootCAs: roots, MinVersion: tls.VersionTLS12},
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, networkFor("tcp", addr), endpoint(addr, t.Port))
		}}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	u := &url.URL{Scheme: "https", Host: endpoint(addr, t.Port), Path: t.Path}
	request, err := http.NewRequestWithContext(call, p.HTTPMethod, u.String(), nil)
	if err != nil {
		return failure(model.StatusError, "could not construct HTTPS request")
	}
	request.Host = t.Host
	started := time.Now()
	response, err := client.Do(request)
	ended := time.Now()
	mu.Lock()
	failedStage, didConnect := stage, connected
	mu.Unlock()
	if err != nil {
		sent := 0
		if didConnect {
			sent = 1
		}
		m := operationFailure(err, sent, failedStage)
		if failedStage == "connect" && m.Status == model.StatusTimeout {
			m.Sent, m.UnableToMeasure, m.HasObservation = 1, 0, true
		}
		// A completed TCP connection followed by TLS/request failure is an
		// observed HTTPS failure, never ICMP/UDP packet loss.
		if didConnect && m.Status != model.StatusPermission && m.Status != model.StatusUnsupported {
			m.UnableToMeasure, m.HasObservation = 0, true
			m.UnobservedSent = 0
		}
		return m
	}
	defer response.Body.Close()
	m := model.Measurement{Status: model.StatusDegraded, Attempted: 1, Sent: 1, HasObservation: true, HTTPStatus: response.StatusCode, FailureStage: "response"}
	read, err := io.Copy(io.Discard, io.LimitReader(response.Body, 1025))
	if err != nil {
		f := operationFailure(err, 1, "response")
		m.Status, m.Detail = f.Status, "HTTPS body read failed: "+err.Error()
		// Headers establish a real HTTP response. A truncated or timed-out
		// response body is a measured application failure; cancellation and
		// suspend are discarded by the enclosing cycle guard.
		if m.Status == model.StatusError {
			m.Status = model.StatusDegraded
		}
		return m
	}
	if read > 1024 {
		m.Detail = "HTTPS response exceeded the 1 KiB body limit"
		return m
	}
	ended = time.Now()
	mu.Lock()
	m.Timings = make(map[string]float64, len(timings)+1)
	for k, v := range timings {
		m.Timings[k] = v
	}
	mu.Unlock()
	m.Timings["request"] = float64(ended.Sub(started)) / float64(time.Millisecond)
	m.POPHint = parsePOPHint(response.Header)
	for _, expected := range p.ExpectedStatusCodes {
		if response.StatusCode == expected && !(response.StatusCode >= 300 && response.StatusCode < 400) && response.StatusCode != http.StatusForbidden {
			ms := float64(ended.Sub(started)) / float64(time.Millisecond)
			m.Status, m.Received, m.FailureStage, m.LatencyMS = model.StatusOK, 1, "", &ms
			m.Samples = []model.RTTSample{{At: ended.UTC(), LatencyMS: ms, Sequence: 1}}
			return m
		}
	}
	m.Detail = fmt.Sprintf("HTTPS returned status %d", response.StatusCode)
	return m
}

var cloudflareRaySuffix = regexp.MustCompile(`-([A-Z]{3})$`)
var fastlyPopToken = regexp.MustCompile(`(?:^|[- ])([A-Z]{3})(?:$|[, ])`)

// Only parsed serving hints are retained; headers and request identifiers are
// not persisted. These expressions retain the original LG behavior.
func parsePOPHint(headers http.Header) string {
	var pops []string
	if match := cloudflareRaySuffix.FindStringSubmatch(strings.TrimSpace(headers.Get("Cf-Ray"))); len(match) == 2 {
		pops = append(pops, match[1])
	}
	for _, value := range headers.Values("X-Served-By") {
		for _, match := range fastlyPopToken.FindAllStringSubmatch(value, -1) {
			found := false
			for _, pop := range pops {
				found = found || pop == match[1]
			}
			if !found {
				pops = append(pops, match[1])
			}
		}
	}
	return strings.Join(pops, ",")
}
