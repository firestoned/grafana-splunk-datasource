package plugin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/data"
)

// ---------------------------------------------------------------------------
// Test fixtures
// ---------------------------------------------------------------------------

// Two well-formed events with the standard meta fields.
const sampleStream = `{"preview":false,"offset":0,"result":{"_time":"2024-04-30T10:00:00.000+00:00","_raw":"app started","host":"host1","source":"/var/log/app.log","sourcetype":"app","index":"main"}}
{"preview":false,"offset":1,"result":{"_time":"2024-04-30T10:00:05.000+00:00","_raw":"login failed for bob","host":"host2","source":"/var/log/auth.log","sourcetype":"linux_audit","index":"main"}}
`

// Events with arbitrary user-defined fields (mimics `| stats count by status`).
const dynamicFieldsStream = `{"preview":false,"offset":0,"result":{"_time":"2024-04-30T10:00:00Z","_raw":"e1","status":"OK","action":"login","host":"h1"}}
{"preview":false,"offset":1,"result":{"_time":"2024-04-30T10:00:01Z","_raw":"e2","status":"FAIL","action":"login","host":"h2","extra":"x"}}
`

// Preview events should be skipped; only the second event should be in the frame.
const previewMixedStream = `{"preview":true,"offset":0,"result":{"_time":"2024-04-30T10:00:00Z","_raw":"preview event","host":"h"}}
{"preview":false,"offset":1,"result":{"_time":"2024-04-30T10:00:01Z","_raw":"real event","host":"h"}}
`

// A malformed JSONL line in the middle is silently skipped.
const malformedLineStream = `{"preview":false,"offset":0,"result":{"_time":"2024-04-30T10:00:00Z","_raw":"first","host":"h"}}
not json — this line is junk
{"preview":false,"offset":2,"result":{"_time":"2024-04-30T10:00:01Z","_raw":"third","host":"h"}}
`

// Lines without a `result` block (Splunk sometimes emits status messages).
const messageOnlyStream = `{"preview":false,"offset":0}
{"preview":false,"offset":1,"result":null}
{"preview":false,"offset":2,"result":{"_time":"2024-04-30T10:00:00Z","_raw":"r","host":"h"}}
`

func newTestDS(t *testing.T, handler http.HandlerFunc) (*Datasource, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	return &Datasource{
		baseURL:    srv.URL,
		authToken:  "test-token",
		httpClient: srv.Client(),
	}, srv
}

func makeRequest(refID string, qm queryModel) *backend.QueryDataRequest {
	queryJSON, _ := json.Marshal(qm)
	return &backend.QueryDataRequest{
		Queries: []backend.DataQuery{{
			RefID: refID,
			JSON:  queryJSON,
			TimeRange: backend.TimeRange{
				From: time.Now().Add(-time.Hour),
				To:   time.Now(),
			},
		}},
	}
}

func closedServerURL(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	return url
}

// ---------------------------------------------------------------------------
// NewDatasource
// ---------------------------------------------------------------------------

func TestNewDatasource_Success(t *testing.T) {
	settings := backend.DataSourceInstanceSettings{
		JSONData:                []byte(`{"url":"https://splunk.example.com:8089"}`),
		DecryptedSecureJSONData: map[string]string{"authToken": "secret"},
	}
	inst, err := NewDatasource(context.Background(), settings)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	ds := inst.(*Datasource)
	if ds.baseURL != "https://splunk.example.com:8089" {
		t.Errorf("baseURL: %q", ds.baseURL)
	}
	if ds.authToken != "secret" {
		t.Errorf("authToken: %q", ds.authToken)
	}
	if ds.httpClient == nil {
		t.Error("httpClient was nil")
	}
}

func TestNewDatasource_TrimsTrailingSlash(t *testing.T) {
	settings := backend.DataSourceInstanceSettings{
		JSONData: []byte(`{"url":"https://splunk.example.com:8089/"}`),
	}
	inst, err := NewDatasource(context.Background(), settings)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if got := inst.(*Datasource).baseURL; got != "https://splunk.example.com:8089" {
		t.Errorf("trailing slash not trimmed: %q", got)
	}
}

func TestNewDatasource_InvalidJSONData(t *testing.T) {
	settings := backend.DataSourceInstanceSettings{
		JSONData: []byte(`{not json`),
	}
	_, err := NewDatasource(context.Background(), settings)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "invalid jsonData") {
		t.Errorf("error: %v", err)
	}
}

func TestNewDatasource_MissingURL(t *testing.T) {
	settings := backend.DataSourceInstanceSettings{
		JSONData: []byte(`{}`),
	}
	_, err := NewDatasource(context.Background(), settings)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "url is required") {
		t.Errorf("error: %v", err)
	}
}

func TestNewDatasource_NoToken(t *testing.T) {
	settings := backend.DataSourceInstanceSettings{
		JSONData: []byte(`{"url":"https://splunk.example.com:8089"}`),
	}
	inst, err := NewDatasource(context.Background(), settings)
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if inst.(*Datasource).authToken != "" {
		t.Error("expected empty token")
	}
}

// ---------------------------------------------------------------------------
// QueryData / handleQuery
// ---------------------------------------------------------------------------

func TestQueryData_Success(t *testing.T) {
	ds, _ := newTestDS(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("auth header: got %q", got)
		}
		if r.URL.Path != "/services/search/jobs/export" {
			t.Errorf("path: got %q", r.URL.Path)
		}
		if got := r.Header.Get("Content-Type"); got != "application/x-www-form-urlencoded" {
			t.Errorf("content-type: got %q", got)
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse form: %v", err)
		}
		if got := r.PostForm.Get("search"); got != "search index=main error" {
			t.Errorf("search: got %q (expected `search ` prefix)", got)
		}
		if got := r.PostForm.Get("output_mode"); got != "json" {
			t.Errorf("output_mode: got %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(sampleStream))
	})

	resp, err := ds.QueryData(context.Background(), makeRequest("A", queryModel{Search: "index=main error", MaxResults: 100}))
	if err != nil {
		t.Fatalf("QueryData error: %v", err)
	}
	r := resp.Responses["A"]
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
	if frame.RefID != "A" {
		t.Errorf("frame RefID: %q", frame.RefID)
	}
	bodyField, _ := frame.FieldByName("body")
	if bodyField == nil {
		t.Fatal("missing body field")
	}
	if got := bodyField.At(0); got != "app started" {
		t.Errorf("body[0]: got %v", got)
	}
}

func TestQueryData_EmptySearch(t *testing.T) {
	ds, _ := newTestDS(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("upstream should not be called for empty search")
	})
	resp, err := ds.QueryData(context.Background(), makeRequest("A", queryModel{}))
	if err != nil {
		t.Fatalf("QueryData error: %v", err)
	}
	if resp.Responses["A"].Error != nil {
		t.Errorf("empty search should be a no-op, got: %v", resp.Responses["A"].Error)
	}
}

func TestQueryData_WhitespaceOnlySearch(t *testing.T) {
	ds, _ := newTestDS(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("upstream should not be called for whitespace-only search")
	})
	resp, _ := ds.QueryData(context.Background(), makeRequest("A", queryModel{Search: "   \n\t  "}))
	if resp.Responses["A"].Error != nil {
		t.Errorf("whitespace search should be a no-op")
	}
}

func TestQueryData_InvalidJSON(t *testing.T) {
	ds, _ := newTestDS(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("upstream should not be called for invalid query JSON")
	})
	req := &backend.QueryDataRequest{
		Queries: []backend.DataQuery{{
			RefID:     "A",
			JSON:      []byte(`{not json`),
			TimeRange: backend.TimeRange{From: time.Now().Add(-time.Hour), To: time.Now()},
		}},
	}
	resp, _ := ds.QueryData(context.Background(), req)
	if resp.Responses["A"].Error == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(resp.Responses["A"].Error.Error(), "invalid query json") {
		t.Errorf("got: %v", resp.Responses["A"].Error)
	}
}

func TestQueryData_MultipleQueries(t *testing.T) {
	calls := 0
	ds, _ := newTestDS(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, _ = w.Write([]byte(sampleStream))
	})
	queryA, _ := json.Marshal(queryModel{Search: "index=a"})
	queryB, _ := json.Marshal(queryModel{Search: "index=b"})
	req := &backend.QueryDataRequest{
		Queries: []backend.DataQuery{
			{RefID: "A", JSON: queryA, TimeRange: backend.TimeRange{From: time.Now().Add(-time.Hour), To: time.Now()}},
			{RefID: "B", JSON: queryB, TimeRange: backend.TimeRange{From: time.Now().Add(-time.Hour), To: time.Now()}},
		},
	}
	resp, _ := ds.QueryData(context.Background(), req)
	if calls != 2 {
		t.Errorf("expected 2 upstream calls, got %d", calls)
	}
	if _, ok := resp.Responses["A"]; !ok {
		t.Error("missing A")
	}
	if _, ok := resp.Responses["B"]; !ok {
		t.Error("missing B")
	}
}

func TestQueryData_ServerError(t *testing.T) {
	ds, _ := newTestDS(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"messages":[{"text":"internal"}]}`))
	})
	resp, _ := ds.QueryData(context.Background(), makeRequest("A", queryModel{Search: "x"}))
	if resp.Responses["A"].Error == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(resp.Responses["A"].Error.Error(), "500") {
		t.Errorf("expected 500 in error, got: %v", resp.Responses["A"].Error)
	}
}

func TestQueryData_4xx(t *testing.T) {
	ds, _ := newTestDS(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"messages":[{"text":"invalid SPL"}]}`))
	})
	resp, _ := ds.QueryData(context.Background(), makeRequest("A", queryModel{Search: "x"}))
	if resp.Responses["A"].Error == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(resp.Responses["A"].Error.Error(), "invalid SPL") {
		t.Errorf("expected error body in message, got: %v", resp.Responses["A"].Error)
	}
}

func TestQueryData_NetworkError(t *testing.T) {
	ds := &Datasource{
		baseURL:    closedServerURL(t),
		authToken:  "tok",
		httpClient: &http.Client{Timeout: 500 * time.Millisecond},
	}
	resp, _ := ds.QueryData(context.Background(), makeRequest("A", queryModel{Search: "x"}))
	if resp.Responses["A"].Error == nil {
		t.Error("expected network error")
	}
}

func TestQueryData_ContextCancellation(t *testing.T) {
	ds, _ := newTestDS(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	resp, _ := ds.QueryData(ctx, makeRequest("A", queryModel{Search: "x"}))
	if resp.Responses["A"].Error == nil {
		t.Error("expected context deadline error")
	}
}

// ---------------------------------------------------------------------------
// runSearch — form parameters
// ---------------------------------------------------------------------------

func TestRunSearch_AllParams(t *testing.T) {
	var captured *http.Request
	ds, _ := newTestDS(t, func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		captured = r
		_, _ = w.Write([]byte(""))
	})
	_, _ = ds.QueryData(context.Background(), makeRequest("A", queryModel{
		Search:       "index=foo",
		MaxResults:   50,
		EarliestTime: "-15m",
		LatestTime:   "now",
	}))
	form := captured.PostForm
	if form.Get("count") != "50" {
		t.Errorf("count: %q", form.Get("count"))
	}
	if form.Get("earliest_time") != "-15m" {
		t.Errorf("earliest_time: %q", form.Get("earliest_time"))
	}
	if form.Get("latest_time") != "now" {
		t.Errorf("latest_time: %q", form.Get("latest_time"))
	}
}

func TestRunSearch_DefaultMaxResults(t *testing.T) {
	var form map[string][]string
	ds, _ := newTestDS(t, func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		form = r.PostForm
		_, _ = w.Write([]byte(""))
	})
	_, _ = ds.QueryData(context.Background(), makeRequest("A", queryModel{Search: "x"}))
	if form["count"][0] != "1000" {
		t.Errorf("expected default count=1000, got %q", form["count"][0])
	}
}

func TestRunSearch_TimeRangeFallback(t *testing.T) {
	var form map[string][]string
	ds, _ := newTestDS(t, func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		form = r.PostForm
		_, _ = w.Write([]byte(""))
	})
	_, _ = ds.QueryData(context.Background(), makeRequest("A", queryModel{Search: "x"}))
	if _, err := time.Parse(time.RFC3339, form["earliest_time"][0]); err != nil {
		t.Errorf("earliest_time not RFC3339: %q (%v)", form["earliest_time"][0], err)
	}
	if _, err := time.Parse(time.RFC3339, form["latest_time"][0]); err != nil {
		t.Errorf("latest_time not RFC3339: %q (%v)", form["latest_time"][0], err)
	}
}

func TestRunSearch_PrefixedAndUnprefixed(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain search prepended", "index=main", "search index=main"},
		{"explicit search untouched", "search index=main", "search index=main"},
		{"pipe-prefixed untouched", "| stats count by host", "| stats count by host"},
		{"tstats untouched", "tstats count where index=foo", "tstats count where index=foo"},
		{"leading whitespace pipe", "  | inputlookup foo", "| inputlookup foo"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var captured string
			ds, _ := newTestDS(t, func(w http.ResponseWriter, r *http.Request) {
				_ = r.ParseForm()
				captured = r.PostForm.Get("search")
				_, _ = w.Write([]byte(""))
			})
			_, _ = ds.QueryData(context.Background(), makeRequest("A", queryModel{Search: c.in}))
			if captured != c.want {
				t.Errorf("got %q, want %q", captured, c.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// parseExportStream
// ---------------------------------------------------------------------------

func TestParseExportStream_BasicEvents(t *testing.T) {
	frame, err := parseExportStream(strings.NewReader(sampleStream))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if frame.Rows() != 2 {
		t.Errorf("expected 2 rows, got %d", frame.Rows())
	}
	if frame.Meta == nil || frame.Meta.Type != data.FrameTypeLogLines {
		t.Errorf("expected logs frame meta, got %+v", frame.Meta)
	}
	if frame.Meta.PreferredVisualization != data.VisTypeLogs {
		t.Errorf("expected VisTypeLogs preferred viz")
	}
}

func TestParseExportStream_DynamicFields(t *testing.T) {
	// Events have arbitrary user fields (`status`, `action`, `extra`) that
	// should appear as columns alongside the well-known meta.
	frame, err := parseExportStream(strings.NewReader(dynamicFieldsStream))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	for _, want := range []string{"timestamp", "body", "host", "status", "action", "extra"} {
		f, _ := frame.FieldByName(want)
		if f == nil {
			t.Errorf("missing field %q", want)
		}
	}
}

func TestParseExportStream_FieldOrderStable(t *testing.T) {
	// Well-known meta (host/source/sourcetype/index) come first in declaration
	// order, then the rest sorted alphabetically.
	stream := `{"preview":false,"offset":0,"result":{"_time":"2024-04-30T10:00:00Z","_raw":"e","host":"h","z":"1","sourcetype":"s","action":"a","index":"i","source":"src"}}` + "\n"
	frame, err := parseExportStream(strings.NewReader(stream))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	var names []string
	for _, f := range frame.Fields {
		names = append(names, f.Name)
	}
	want := []string{"timestamp", "body", "host", "source", "sourcetype", "index", "action", "z"}
	for i, w := range want {
		if i >= len(names) || names[i] != w {
			t.Errorf("field order: got %v, want %v", names, want)
			break
		}
	}
}

func TestParseExportStream_PreviewSkipped(t *testing.T) {
	frame, err := parseExportStream(strings.NewReader(previewMixedStream))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if frame.Rows() != 1 {
		t.Errorf("expected 1 row (preview skipped), got %d", frame.Rows())
	}
	body, _ := frame.FieldByName("body")
	if got := body.At(0); got != "real event" {
		t.Errorf("body[0]: got %v, want 'real event'", got)
	}
}

func TestParseExportStream_MalformedLineSkipped(t *testing.T) {
	frame, err := parseExportStream(strings.NewReader(malformedLineStream))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if frame.Rows() != 2 {
		t.Errorf("expected 2 rows (malformed line skipped), got %d", frame.Rows())
	}
}

func TestParseExportStream_NullOrMissingResultSkipped(t *testing.T) {
	frame, err := parseExportStream(strings.NewReader(messageOnlyStream))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if frame.Rows() != 1 {
		t.Errorf("expected 1 row, got %d", frame.Rows())
	}
}

func TestParseExportStream_EmptyStream(t *testing.T) {
	frame, err := parseExportStream(strings.NewReader(""))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if frame.Rows() != 0 {
		t.Errorf("expected 0 rows, got %d", frame.Rows())
	}
	// Even with no events, the frame should have at minimum timestamp and body.
	if f, _ := frame.FieldByName("timestamp"); f == nil {
		t.Error("missing timestamp field on empty stream")
	}
	if f, _ := frame.FieldByName("body"); f == nil {
		t.Error("missing body field on empty stream")
	}
}

func TestParseExportStream_LargeLine(t *testing.T) {
	// Splunk events can have huge `_raw`. The scanner buffer is bumped to 16MB,
	// so a 2MB line should parse without "token too long".
	huge := strings.Repeat("X", 2*1024*1024)
	stream := `{"preview":false,"offset":0,"result":{"_time":"2024-04-30T10:00:00Z","_raw":"` + huge + `","host":"h"}}` + "\n"
	frame, err := parseExportStream(strings.NewReader(stream))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if frame.Rows() != 1 {
		t.Errorf("expected 1 row, got %d", frame.Rows())
	}
	body, _ := frame.FieldByName("body")
	if got := body.At(0).(string); len(got) != len(huge) {
		t.Errorf("body length mismatch: got %d, want %d", len(got), len(huge))
	}
}

func TestParseExportStream_NumericFieldsCoercedToString(t *testing.T) {
	// Splunk emits some fields as numbers (counts, durations). rawString
	// should extract them as string.
	stream := `{"preview":false,"offset":0,"result":{"_time":"2024-04-30T10:00:00Z","_raw":"e","count":42,"ratio":0.5,"flag":true}}` + "\n"
	frame, err := parseExportStream(strings.NewReader(stream))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	count, _ := frame.FieldByName("count")
	if got := count.At(0).(string); got != "42" {
		t.Errorf("count: got %q, want '42'", got)
	}
	ratio, _ := frame.FieldByName("ratio")
	if got := ratio.At(0).(string); got != "0.5" {
		t.Errorf("ratio: got %q", got)
	}
}

// ---------------------------------------------------------------------------
// startsWithCommand
// ---------------------------------------------------------------------------

func TestStartsWithCommand(t *testing.T) {
	cases := map[string]bool{
		"search index=main":      true,
		"index=main":             false,
		"| stats count":          true,
		"tstats count":           true,
		"  | inputlookup foo":    true,
		"foo bar":                false,
		"":                       false,
		"SEARCH index=main":      true, // case-insensitive
		"mstats count where idx": true,
		"datamodel foo":          true,
		"savedsearch my_search":  true,
		"unknown_command foo":    false,
	}
	for in, want := range cases {
		if got := startsWithCommand(in); got != want {
			t.Errorf("startsWithCommand(%q) = %v, want %v", in, got, want)
		}
	}
}

// ---------------------------------------------------------------------------
// firstNonEmpty
// ---------------------------------------------------------------------------

func TestFirstNonEmpty(t *testing.T) {
	cases := []struct {
		in   []string
		want string
	}{
		{[]string{}, ""},
		{[]string{""}, ""},
		{[]string{"", ""}, ""},
		{[]string{"a"}, "a"},
		{[]string{"", "b"}, "b"},
		{[]string{"a", "b"}, "a"},
		{[]string{"", "", "c"}, "c"},
	}
	for _, c := range cases {
		if got := firstNonEmpty(c.in...); got != c.want {
			t.Errorf("firstNonEmpty(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

// ---------------------------------------------------------------------------
// rawString
// ---------------------------------------------------------------------------

func TestRawString(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"empty raw message", "", ""},
		{"json string", `"hello"`, "hello"},
		{"json number", `42`, "42"},
		{"json float", `3.14`, "3.14"},
		{"json bool", `true`, "true"},
		{"json null becomes empty string", `null`, ""},
		{"empty string", `""`, ""},
		{"string with spaces", `"hello world"`, "hello world"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := rawString(json.RawMessage(c.raw))
			if got != c.want {
				t.Errorf("rawString(%q) = %q, want %q", c.raw, got, c.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// parseSplunkTime
// ---------------------------------------------------------------------------

func TestParseSplunkTime(t *testing.T) {
	cases := []struct {
		name      string
		in        string
		wantParse bool
	}{
		{"RFC3339Nano with offset", "2024-04-30T10:00:00.123456789+00:00", true},
		{"RFC3339Nano millis with offset", "2024-04-30T10:00:00.000+00:00", true},
		{"RFC3339 Z", "2024-04-30T10:00:00Z", true},
		{"epoch float", "1714435200.500", true},
		{"epoch int as string", "1714435200", true},
		{"empty", "", false},
		{"garbage", "not-a-time", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := parseSplunkTime(c.in)
			if c.wantParse && got.IsZero() {
				t.Errorf("expected parse, got zero")
			}
			if !c.wantParse && !got.IsZero() {
				t.Errorf("expected zero, got %v", got)
			}
		})
	}
}

func TestParseSplunkTime_EpochAccuracy(t *testing.T) {
	got := parseSplunkTime("1714435200.500")
	want := time.Unix(1714435200, 500_000_000).UTC()
	if !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// ---------------------------------------------------------------------------
// CheckHealth
// ---------------------------------------------------------------------------

func TestCheckHealth_OK(t *testing.T) {
	ds, _ := newTestDS(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Errorf("auth header: %q", got)
		}
		if r.URL.Path != "/services/server/info" {
			t.Errorf("path: %q", r.URL.Path)
		}
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
	if !strings.Contains(res.Message, "Connected") {
		t.Errorf("expected success message, got %q", res.Message)
	}
}

func TestCheckHealth_NoToken(t *testing.T) {
	ds := &Datasource{baseURL: "https://example.com", authToken: "", httpClient: http.DefaultClient}
	res, _ := ds.CheckHealth(context.Background(), nil)
	if res.Status != backend.HealthStatusError {
		t.Errorf("expected Error for missing token, got %v", res.Status)
	}
	if !strings.Contains(res.Message, "Auth token is missing") {
		t.Errorf("expected missing-token message, got %q", res.Message)
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
		t.Errorf("expected auth error, got %q", res.Message)
	}
	if !strings.Contains(res.Message, "401") {
		t.Errorf("expected 401 in message, got %q", res.Message)
	}
}

func TestCheckHealth_Forbidden(t *testing.T) {
	ds, _ := newTestDS(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	})
	res, _ := ds.CheckHealth(context.Background(), nil)
	if res.Status != backend.HealthStatusError {
		t.Errorf("expected Error for 403")
	}
	if !strings.Contains(res.Message, "Forbidden") {
		t.Errorf("expected 'Forbidden' in message, got %q", res.Message)
	}
	if !strings.Contains(res.Message, "rest_properties_get") {
		t.Errorf("expected hint about Splunk capability, got %q", res.Message)
	}
}

func TestCheckHealth_OtherClientError(t *testing.T) {
	ds, _ := newTestDS(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"messages":[{"text":"bad request"}]}`))
	})
	res, _ := ds.CheckHealth(context.Background(), nil)
	if res.Status != backend.HealthStatusError {
		t.Errorf("expected Error for 400")
	}
	if !strings.Contains(res.Message, "400") {
		t.Errorf("expected 400 in message, got %q", res.Message)
	}
}

func TestCheckHealth_ServerError(t *testing.T) {
	ds, _ := newTestDS(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	res, _ := ds.CheckHealth(context.Background(), nil)
	if res.Status != backend.HealthStatusError {
		t.Errorf("expected Error for 500")
	}
}

func TestCheckHealth_NetworkError(t *testing.T) {
	ds := &Datasource{
		baseURL:    closedServerURL(t),
		authToken:  "tok",
		httpClient: &http.Client{Timeout: 500 * time.Millisecond},
	}
	res, _ := ds.CheckHealth(context.Background(), nil)
	if res.Status != backend.HealthStatusError {
		t.Error("expected Error on network failure")
	}
	if !strings.Contains(res.Message, "Could not reach Splunk") {
		t.Errorf("expected 'Could not reach' message, got %q", res.Message)
	}
}

// ---------------------------------------------------------------------------
// truncate
// ---------------------------------------------------------------------------

func TestTruncate(t *testing.T) {
	cases := []struct {
		s    string
		n    int
		want string
	}{
		{"", 5, ""},
		{"abc", 5, "abc"},
		{"abcde", 5, "abcde"},
		{"abcdef", 5, "abcde..."},
		{"long-string-here", 4, "long..."},
		{"x", 0, "..."},
	}
	for _, c := range cases {
		if got := truncate(c.s, c.n); got != c.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", c.s, c.n, got, c.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Dispose
// ---------------------------------------------------------------------------

func TestDispose_NoPanic(t *testing.T) {
	ds := &Datasource{httpClient: &http.Client{}}
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Dispose panicked: %v", r)
		}
	}()
	ds.Dispose()
}

func TestDispose_AfterUse(t *testing.T) {
	ds, _ := newTestDS(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(""))
	})
	_, _ = ds.QueryData(context.Background(), makeRequest("A", queryModel{Search: "x"}))
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Dispose panicked after use: %v", r)
		}
	}()
	ds.Dispose()
}
