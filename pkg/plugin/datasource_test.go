package plugin

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/data"
)

// Two events in the JSONL format /services/search/jobs/export emits.
const sampleStream = `{"preview":false,"offset":0,"result":{"_time":"2024-04-30T10:00:00.000+00:00","_raw":"app started","host":"host1","source":"/var/log/app.log","sourcetype":"app","index":"main"}}
{"preview":false,"offset":1,"result":{"_time":"2024-04-30T10:00:05.000+00:00","_raw":"login failed for bob","host":"host2","source":"/var/log/auth.log","sourcetype":"linux_audit","index":"main"}}
`

func newTestDS(t *testing.T, handler http.HandlerFunc) (*Datasource, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	return &Datasource{
		baseURL:   srv.URL,
		authToken: "test-token",
		httpClient: &http.Client{
			Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}, //nolint:gosec
		},
	}, srv
}

func TestQueryData_Success(t *testing.T) {
	ds, _ := newTestDS(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("auth header: got %q", got)
		}
		if r.URL.Path != "/services/search/jobs/export" {
			t.Errorf("path: got %q", r.URL.Path)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		if got := r.PostForm.Get("search"); got != "search index=main error" {
			t.Errorf("search: got %q (expected `search ` prefix to be added)", got)
		}
		if got := r.PostForm.Get("output_mode"); got != "json" {
			t.Errorf("output_mode: got %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(sampleStream))
	})

	q := queryModel{Search: "index=main error", MaxResults: 100}
	queryJSON, _ := json.Marshal(q)
	req := &backend.QueryDataRequest{
		Queries: []backend.DataQuery{{
			RefID: "A",
			JSON:  queryJSON,
			TimeRange: backend.TimeRange{
				From: time.Now().Add(-time.Hour),
				To:   time.Now(),
			},
		}},
	}

	resp, err := ds.QueryData(context.Background(), req)
	if err != nil {
		t.Fatalf("QueryData error: %v", err)
	}
	r, ok := resp.Responses["A"]
	if !ok {
		t.Fatal("missing response for refID A")
	}
	if r.Error != nil {
		t.Fatalf("response error: %v", r.Error)
	}
	if len(r.Frames) != 1 {
		t.Fatalf("expected 1 frame, got %d", len(r.Frames))
	}
	frame := r.Frames[0]
	if frame.Rows() != 2 {
		t.Errorf("expected 2 rows, got %d", frame.Rows())
	}
	if frame.Meta == nil || frame.Meta.Type != data.FrameTypeLogLines {
		t.Errorf("expected logs frame meta, got %+v", frame.Meta)
	}
	// Spot-check body
	_, bodyField := frame.FieldByName("body")
	if bodyField == nil {
		t.Fatal("missing body field")
	}
	if got := bodyField.At(0); got != "app started" {
		t.Errorf("body[0]: got %v", got)
	}
}

func TestStartsWithCommand(t *testing.T) {
	cases := map[string]bool{
		"search index=main":   true,
		"index=main":          false,
		"| stats count":       true,
		"tstats count":        true,
		"  | inputlookup foo": true,
		"foo bar":             false,
	}
	for in, want := range cases {
		if got := startsWithCommand(in); got != want {
			t.Errorf("startsWithCommand(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestSearchPrefixed(t *testing.T) {
	var captured string
	ds, _ := newTestDS(t, func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		captured = r.PostForm.Get("search")
		_, _ = w.Write([]byte(""))
	})
	queryJSON, _ := json.Marshal(queryModel{Search: "| stats count by host"})
	_, _ = ds.QueryData(context.Background(), &backend.QueryDataRequest{
		Queries: []backend.DataQuery{{
			RefID: "A", JSON: queryJSON,
			TimeRange: backend.TimeRange{From: time.Now().Add(-time.Hour), To: time.Now()},
		}},
	})
	if !strings.HasPrefix(captured, "| stats") {
		t.Errorf("pipe-prefixed query should not get `search ` prepended, got %q", captured)
	}
}

func TestCheckHealth_OK(t *testing.T) {
	ds, _ := newTestDS(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"entry":[]}`))
	})
	res, err := ds.CheckHealth(context.Background(), nil)
	if err != nil {
		t.Fatalf("CheckHealth error: %v", err)
	}
	if res.Status != backend.HealthStatusOk {
		t.Errorf("expected Ok, got %v: %s", res.Status, res.Message)
	}
}

func TestCheckHealth_Unauthorized(t *testing.T) {
	ds, _ := newTestDS(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	})
	res, _ := ds.CheckHealth(context.Background(), nil)
	if res.Status != backend.HealthStatusError {
		t.Errorf("expected Error, got %v", res.Status)
	}
	if !strings.Contains(res.Message, "Authentication failed") {
		t.Errorf("expected auth error message, got %q", res.Message)
	}
}

func TestCheckHealth_NoToken(t *testing.T) {
	ds := &Datasource{baseURL: "https://example.com", authToken: "", httpClient: http.DefaultClient}
	res, _ := ds.CheckHealth(context.Background(), nil)
	if res.Status != backend.HealthStatusError {
		t.Errorf("expected Error for missing token, got %v", res.Status)
	}
}

func TestParseSplunkTime(t *testing.T) {
	cases := []struct {
		in   string
		want bool // whether it should parse
	}{
		{"2024-04-30T10:00:00.000+00:00", true},
		{"2024-04-30T10:00:00Z", true},
		{"1714435200.500", true}, // epoch float
		{"", false},
		{"not-a-time", false},
	}
	for _, c := range cases {
		got := parseSplunkTime(c.in)
		if c.want && got.IsZero() {
			t.Errorf("parseSplunkTime(%q): expected parse, got zero", c.in)
		}
		if !c.want && !got.IsZero() {
			t.Errorf("parseSplunkTime(%q): expected zero, got %v", c.in, got)
		}
	}
}
