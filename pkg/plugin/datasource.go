package plugin

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/grafana/grafana-plugin-sdk-go/backend"
	"github.com/grafana/grafana-plugin-sdk-go/backend/httpclient"
	"github.com/grafana/grafana-plugin-sdk-go/backend/instancemgmt"
	"github.com/grafana/grafana-plugin-sdk-go/backend/log"
	"github.com/grafana/grafana-plugin-sdk-go/data"
)

// Compile-time interface checks.
var (
	_ backend.QueryDataHandler      = (*Datasource)(nil)
	_ backend.CheckHealthHandler    = (*Datasource)(nil)
	_ instancemgmt.InstanceDisposer = (*Datasource)(nil)
)

// Datasource is one configured Splunk data source instance.
type Datasource struct {
	baseURL    string
	authToken  string
	httpClient *http.Client
}

// settings is the shape of the non-secret JSON sent by the frontend
// ConfigEditor.
type settings struct {
	URL string `json:"url"`
}

// NewDatasource is the factory called by the SDK on instance create / update.
func NewDatasource(ctx context.Context, s backend.DataSourceInstanceSettings) (instancemgmt.Instance, error) {
	var cfg settings
	if err := json.Unmarshal(s.JSONData, &cfg); err != nil {
		return nil, fmt.Errorf("invalid jsonData: %w", err)
	}
	if cfg.URL == "" {
		return nil, errors.New("url is required")
	}

	opts, err := s.HTTPClientOptions(ctx)
	if err != nil {
		return nil, fmt.Errorf("http client options: %w", err)
	}
	// Searches can be slow; raise the default if the user hasn't configured one.
	if opts.Timeouts != nil && opts.Timeouts.Timeout < 60*time.Second {
		opts.Timeouts.Timeout = 60 * time.Second
	}

	client, err := httpclient.New(opts)
	if err != nil {
		return nil, fmt.Errorf("http client: %w", err)
	}

	token := s.DecryptedSecureJSONData["authToken"]

	return &Datasource{
		baseURL:    strings.TrimRight(cfg.URL, "/"),
		authToken:  token,
		httpClient: client,
	}, nil
}

func (d *Datasource) Dispose() {
	d.httpClient.CloseIdleConnections()
}

// queryModel matches the frontend SplunkQuery type.
type queryModel struct {
	Search       string `json:"search"`
	MaxResults   int    `json:"maxResults"`
	EarliestTime string `json:"earliestTime,omitempty"`
	LatestTime   string `json:"latestTime,omitempty"`
}

func (d *Datasource) QueryData(ctx context.Context, req *backend.QueryDataRequest) (*backend.QueryDataResponse, error) {
	resp := backend.NewQueryDataResponse()
	for _, q := range req.Queries {
		resp.Responses[q.RefID] = d.handleQuery(ctx, q)
	}
	return resp, nil
}

func (d *Datasource) handleQuery(ctx context.Context, q backend.DataQuery) backend.DataResponse {
	var qm queryModel
	if err := json.Unmarshal(q.JSON, &qm); err != nil {
		return backend.ErrDataResponse(backend.StatusBadRequest, "invalid query json: "+err.Error())
	}
	if strings.TrimSpace(qm.Search) == "" {
		return backend.DataResponse{}
	}

	frame, err := d.runSearch(ctx, qm, q.TimeRange)
	if err != nil {
		log.DefaultLogger.Error("splunk search failed", "err", err)
		return backend.ErrDataResponse(backend.StatusBadGateway, err.Error())
	}
	frame.RefID = q.RefID
	return backend.DataResponse{Frames: data.Frames{frame}}
}

// runSearch calls Splunk's synchronous search export endpoint and parses
// the streamed JSONL response into a logs-typed Grafana data frame.
//
// See: https://docs.splunk.com/Documentation/Splunk/latest/RESTREF/RESTsearch#search.2Fjobs.2Fexport
func (d *Datasource) runSearch(ctx context.Context, qm queryModel, tr backend.TimeRange) (*data.Frame, error) {
	endpoint := d.baseURL + "/services/search/jobs/export"

	// SPL convention: queries that don't start with a generating command must
	// be prefixed with `search `.
	spl := strings.TrimSpace(qm.Search)
	if !startsWithCommand(spl) {
		spl = "search " + spl
	}

	form := url.Values{}
	form.Set("search", spl)
	form.Set("output_mode", "json")
	form.Set("earliest_time", firstNonEmpty(qm.EarliestTime, tr.From.Format(time.RFC3339)))
	form.Set("latest_time", firstNonEmpty(qm.LatestTime, tr.To.Format(time.RFC3339)))
	if qm.MaxResults > 0 {
		form.Set("count", strconv.Itoa(qm.MaxResults))
	} else if qm.MaxResults == 0 {
		// Default cap to avoid runaway responses if the user left it blank.
		form.Set("count", "1000")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+d.authToken)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("splunk HTTP error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("splunk returned %d: %s", resp.StatusCode, truncate(string(body), 500))
	}

	return parseExportStream(resp.Body)
}

// exportLine is one JSONL row from /search/jobs/export.
type exportLine struct {
	Preview bool                       `json:"preview"`
	Offset  int                        `json:"offset"`
	Result  map[string]json.RawMessage `json:"result"`
	// LastRow / Messages also exist; ignored here.
}

// parseExportStream reads JSONL events and builds a Grafana logs frame.
// Field columns are derived from the union of keys across all events, so
// SPL queries that emit arbitrary fields (e.g. `| stats count by status`)
// keep their columns instead of being silently dropped.
func parseExportStream(r io.Reader) (*data.Frame, error) {
	type event struct {
		t    time.Time
		raw  string
		kv   map[string]string
	}

	var events []event
	fieldSet := map[string]struct{}{}

	scanner := bufio.NewScanner(r)
	// Splunk lines can be large; bump the buffer.
	scanner.Buffer(make([]byte, 1024*1024), 16*1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var ev exportLine
		if err := json.Unmarshal(line, &ev); err != nil {
			// Skip malformed lines rather than failing the whole search;
			// Splunk occasionally interleaves messages.
			continue
		}
		if ev.Preview || ev.Result == nil {
			continue
		}

		e := event{
			t:   parseSplunkTime(rawString(ev.Result["_time"])),
			raw: rawString(ev.Result["_raw"]),
			kv:  make(map[string]string, len(ev.Result)),
		}
		for k, v := range ev.Result {
			if k == "_time" || k == "_raw" {
				continue
			}
			e.kv[k] = rawString(v)
			fieldSet[k] = struct{}{}
		}
		events = append(events, e)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read stream: %w", err)
	}

	times := make([]time.Time, len(events))
	bodies := make([]string, len(events))
	for i, e := range events {
		times[i] = e.t
		bodies[i] = e.raw
	}

	// Order columns: well-known meta first (host/source/sourcetype/index),
	// then any remaining fields sorted alphabetically. Stable order keeps
	// dashboards from reflowing on each refresh.
	knownMeta := []string{"host", "source", "sourcetype", "index"}
	seen := map[string]bool{}
	fieldOrder := make([]string, 0, len(fieldSet))
	for _, k := range knownMeta {
		if _, ok := fieldSet[k]; ok {
			fieldOrder = append(fieldOrder, k)
			seen[k] = true
		}
	}
	rest := make([]string, 0, len(fieldSet))
	for k := range fieldSet {
		if !seen[k] {
			rest = append(rest, k)
		}
	}
	sort.Strings(rest)
	fieldOrder = append(fieldOrder, rest...)

	fields := make([]*data.Field, 0, 2+len(fieldOrder))
	fields = append(fields,
		data.NewField("timestamp", nil, times),
		data.NewField("body", nil, bodies),
	)
	for _, name := range fieldOrder {
		col := make([]string, len(events))
		for i, e := range events {
			col[i] = e.kv[name]
		}
		fields = append(fields, data.NewField(name, nil, col))
	}

	frame := data.NewFrame("logs", fields...)
	frame.Meta = &data.FrameMeta{
		Type:                   data.FrameTypeLogLines,
		PreferredVisualization: data.VisTypeLogs,
	}
	return frame, nil
}

// CheckHealth is hit by the "Save & Test" button. We call a cheap endpoint
// (`/services/server/info`) that any read-only token can reach.
func (d *Datasource) CheckHealth(ctx context.Context, _ *backend.CheckHealthRequest) (*backend.CheckHealthResult, error) {
	if d.authToken == "" {
		return &backend.CheckHealthResult{
			Status:  backend.HealthStatusError,
			Message: "Auth token is missing — set one in the data source config and Save & Test again.",
		}, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, d.baseURL+"/services/server/info?output_mode=json", nil)
	if err != nil {
		return &backend.CheckHealthResult{Status: backend.HealthStatusError, Message: err.Error()}, nil
	}
	req.Header.Set("Authorization", "Bearer "+d.authToken)
	req.Header.Set("Accept", "application/json")

	resp, err := d.httpClient.Do(req)
	if err != nil {
		return &backend.CheckHealthResult{
			Status:  backend.HealthStatusError,
			Message: "Could not reach Splunk: " + err.Error(),
		}, nil
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return &backend.CheckHealthResult{
			Status:  backend.HealthStatusError,
			Message: "Authentication failed (401). Check that the token is a Splunk auth token (not a HEC token) and is not expired.",
		}, nil
	case resp.StatusCode == http.StatusForbidden:
		return &backend.CheckHealthResult{
			Status:  backend.HealthStatusError,
			Message: "Forbidden (403). The token's user lacks permission for /services/server/info — try a token bound to an admin or a role with `rest_properties_get`.",
		}, nil
	case resp.StatusCode >= 400:
		body, _ := io.ReadAll(resp.Body)
		return &backend.CheckHealthResult{
			Status:  backend.HealthStatusError,
			Message: fmt.Sprintf("Splunk returned %d: %s", resp.StatusCode, truncate(string(body), 200)),
		}, nil
	}

	return &backend.CheckHealthResult{
		Status:  backend.HealthStatusOk,
		Message: "Connected to Splunk successfully.",
	}, nil
}

// --- helpers ---

// startsWithCommand reports whether an SPL string begins with a generating
// command (so we shouldn't prepend `search `). Cheap heuristic — Splunk
// itself is the source of truth, so on a false negative the prepend is
// harmless because `search search ...` errors clearly.
func startsWithCommand(spl string) bool {
	first := strings.ToLower(strings.SplitN(strings.TrimSpace(spl), " ", 2)[0])
	switch first {
	case "search", "|", "tstats", "metadata", "inputlookup", "savedsearch",
		"datamodel", "from", "mstats", "loadjob", "pivot", "rest":
		return true
	}
	return strings.HasPrefix(strings.TrimSpace(spl), "|")
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// rawString unwraps a JSON value that might be string, number, bool, or
// missing, into a plain string.
func rawString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	// Number/bool/etc — return as-is, trimmed of quotes if any.
	return strings.Trim(string(raw), `"`)
}

// parseSplunkTime accepts the formats Splunk emits in `_time`. The export
// endpoint with output_mode=json typically gives RFC3339-ish, but older
// stacks return epoch floats.
func parseSplunkTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		sec := int64(f)
		nsec := int64((f - float64(sec)) * 1e9)
		return time.Unix(sec, nsec).UTC()
	}
	return time.Time{}
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
