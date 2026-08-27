package auth

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestParseAntigravityProject(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"empty", "", ""},
		{"null", "null", ""},
		{"string", `"my-proj"`, "my-proj"},
		{"string spaced", `"  my-proj  "`, "my-proj"},
		{"resource name", `"projects/my-proj"`, "my-proj"},
		{"resource nested", `"projects/my-proj/locations/us"`, "my-proj"},
		{"object id", `{"id":"obj-proj"}`, "obj-proj"},
		{"object name", `{"name":"projects/name-proj"}`, "name-proj"},
		{"object prefers id", `{"id":"id-proj","name":"projects/other"}`, "id-proj"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var raw json.RawMessage
			if tc.raw != "" {
				raw = json.RawMessage(tc.raw)
			}
			if got := parseAntigravityProject(raw); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestNormalizeAntigravityProjectID(t *testing.T) {
	if got := normalizeAntigravityProjectID(" projects/foo/bar "); got != "foo" {
		t.Fatalf("got %q", got)
	}
	if got := normalizeAntigravityProjectID("plain"); got != "plain" {
		t.Fatalf("got %q", got)
	}
}

func TestExtractAntigravityCallback(t *testing.T) {
	cases := []struct {
		name, input, code, state string
	}{
		{"url", "http://localhost:3129/callback?code=abc&state=st", "abc", "st"},
		{"query", "?code=abc&state=st", "abc", "st"},
		{"raw", "abc", "abc", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, state := extractAntigravityCallback(tc.input)
			if code != tc.code || state != tc.state {
				t.Fatalf("got (%q, %q), want (%q, %q)", code, state, tc.code, tc.state)
			}
		})
	}
}

func TestAntigravityCallbackPageNoStore(t *testing.T) {
	rr := httptest.NewRecorder()
	writeAntigravityCallbackPage(rr, true)
	if rr.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("callback page must not be cached")
	}
	if rr.Header().Get("Content-Security-Policy") == "" {
		t.Fatal("callback page must have CSP")
	}
}

func TestManualAntigravityRequiresSession(t *testing.T) {
	if _, err := CompleteAntigravityManual("missing", "code"); err == nil {
		t.Fatal("expected unknown session to be rejected")
	}
}

// cloudCodeMock serves loadCodeAssist + onboardUser for bootstrap chain tests.
type cloudCodeMock struct {
	loadBody    string
	loadStatus  int
	onboardSeq  []string // successive onboard response bodies
	onboardHits atomic.Int32
	sawSource   atomic.Bool
}

func (m *cloudCodeMock) handler(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-Request-Source") == "local" {
		m.sawSource.Store(true)
	}
	_, _ = io.ReadAll(r.Body)
	switch {
	case strings.Contains(r.URL.Path, "loadCodeAssist"):
		st := m.loadStatus
		if st == 0 {
			st = 200
		}
		w.WriteHeader(st)
		_, _ = w.Write([]byte(m.loadBody))
	case strings.Contains(r.URL.Path, "onboardUser"):
		i := int(m.onboardHits.Add(1) - 1)
		w.WriteHeader(200)
		if i < len(m.onboardSeq) {
			_, _ = w.Write([]byte(m.onboardSeq[i]))
		} else if len(m.onboardSeq) > 0 {
			_, _ = w.Write([]byte(m.onboardSeq[len(m.onboardSeq)-1]))
		} else {
			_, _ = w.Write([]byte(`{"done":true}`))
		}
	default:
		w.WriteHeader(404)
		_, _ = w.Write([]byte(`{"error":"unexpected path ` + r.URL.Path + `"}`))
	}
}

func TestLoadAntigravityCodeAssistParsesProjectAndTier(t *testing.T) {
	m := &cloudCodeMock{
		loadBody: `{
			"cloudaicompanionProject": {"id": "proj-from-load"},
			"currentTier": {"id": "standard-tier", "name": "Standard"}
		}`,
	}
	srv := httptest.NewServer(http.HandlerFunc(m.handler))
	defer srv.Close()
	client := rewriteCloudCodeClient(t, srv.URL)

	projectID, tier, tierName, err := loadAntigravityCodeAssist(client, "tok")
	if err != nil {
		t.Fatal(err)
	}
	if projectID != "proj-from-load" {
		t.Fatalf("project=%q", projectID)
	}
	if tier != "standard-tier" || tierName != "Standard" {
		t.Fatalf("tier=%q name=%q", tier, tierName)
	}
	if !m.sawSource.Load() {
		t.Fatal("expected X-Request-Source: local on loadCodeAssist")
	}
}

func TestOnboardAntigravityUserCreatesProjectWhenLoadEmpty(t *testing.T) {
	oldWait, oldRetries := agOnboardWait, agOnboardRetries
	agOnboardWait = time.Millisecond
	agOnboardRetries = 5
	defer func() {
		agOnboardWait = oldWait
		agOnboardRetries = oldRetries
	}()

	m := &cloudCodeMock{
		onboardSeq: []string{
			`{"done":false}`,
			`{"done":true,"response":{"cloudaicompanionProject":{"id":"proj-from-onboard"}}}`,
		},
	}
	srv := httptest.NewServer(http.HandlerFunc(m.handler))
	defer srv.Close()
	client := rewriteCloudCodeClient(t, srv.URL)

	got, err := onboardAntigravityUser(client, "tok", "", "legacy-tier")
	if err != nil {
		t.Fatal(err)
	}
	if got != "proj-from-onboard" {
		t.Fatalf("got %q", got)
	}
	if m.onboardHits.Load() < 2 {
		t.Fatalf("expected poll, hits=%d", m.onboardHits.Load())
	}
}

func TestOnboardAntigravityUserFailsWhenDoneWithoutProject(t *testing.T) {
	oldWait, oldRetries := agOnboardWait, agOnboardRetries
	agOnboardWait = time.Millisecond
	agOnboardRetries = 2
	defer func() {
		agOnboardWait = oldWait
		agOnboardRetries = oldRetries
	}()

	m := &cloudCodeMock{
		onboardSeq: []string{`{"done":true,"response":{}}`},
	}
	srv := httptest.NewServer(http.HandlerFunc(m.handler))
	defer srv.Close()
	client := rewriteCloudCodeClient(t, srv.URL)

	_, err := onboardAntigravityUser(client, "tok", "", "legacy-tier")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "no project id") {
		t.Fatalf("err=%v", err)
	}
}

func TestResolveProjectViaLoadThenOnboard(t *testing.T) {
	oldWait, oldRetries := agOnboardWait, agOnboardRetries
	agOnboardWait = time.Millisecond
	agOnboardRetries = 5
	defer func() {
		agOnboardWait = oldWait
		agOnboardRetries = oldRetries
	}()

	m := &cloudCodeMock{
		loadBody: `{
			"cloudaicompanionProject": null,
			"allowedTiers": [{"id":"free-tier","name":"Free","isDefault":true}]
		}`,
		onboardSeq: []string{
			`{"done":true,"response":{"cloudaicompanionProject":"projects/created-proj"}}`,
		},
	}
	srv := httptest.NewServer(http.HandlerFunc(m.handler))
	defer srv.Close()
	client := rewriteCloudCodeClient(t, srv.URL)

	projectID, tier, _, err := loadAntigravityCodeAssist(client, "tok")
	if err != nil {
		t.Fatal(err)
	}
	if projectID != "" {
		t.Fatalf("expected empty project from load, got %q", projectID)
	}
	if tier != "free-tier" {
		t.Fatalf("tier=%q want free-tier", tier)
	}

	final, err := onboardAntigravityUser(client, "tok", projectID, tier)
	if err != nil {
		t.Fatal(err)
	}
	if final != "created-proj" {
		t.Fatalf("final=%q", final)
	}
}

// rewriteCloudCodeClient returns an http.Client that rewrites requests to the
// real cloudcode-pa host toward the given mock base URL.
func rewriteCloudCodeClient(t *testing.T, mockBase string) *http.Client {
	t.Helper()
	mockURL, err := url.Parse(mockBase)
	if err != nil {
		t.Fatal(err)
	}
	return &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			u := *req.URL
			u.Scheme = mockURL.Scheme
			u.Host = mockURL.Host
			r2 := req.Clone(req.Context())
			r2.URL = &u
			r2.Host = u.Host
			return http.DefaultTransport.RoundTrip(r2)
		}),
		Timeout: 10 * time.Second,
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestParseAntigravityProjectEmptyObject(t *testing.T) {
	if got := parseAntigravityProject(json.RawMessage(`{}`)); got != "" {
		t.Fatalf("got %q", got)
	}
	// Production onboard body shape.
	raw := json.RawMessage(`{"@type":"type.googleapis.com/google.internal.cloud.code.v1internal.OnboardUserResponse"}`)
	if got := parseAntigravityProject(raw); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestResolveAntigravityProjectSyntheticWhenOnboardEmpty(t *testing.T) {
	oldWait, oldRetries := agOnboardWait, agOnboardRetries
	agOnboardWait = time.Millisecond
	agOnboardRetries = 2
	defer func() {
		agOnboardWait = oldWait
		agOnboardRetries = oldRetries
	}()

	// load: no project; onboard: done with empty {} (production failure mode).
	m := &cloudCodeMock{
		loadBody: `{
			"cloudaicompanionProject": null,
			"allowedTiers": [{"id":"free-tier","name":"Free","isDefault":true}]
		}`,
		onboardSeq: []string{
			`{"done":true,"response":{"@type":"type.googleapis.com/google.internal.cloud.code.v1internal.OnboardUserResponse","cloudaicompanionProject":{}}}`,
		},
	}
	srv := httptest.NewServer(http.HandlerFunc(m.handler))
	defer srv.Close()
	client := rewriteCloudCodeClient(t, srv.URL)

	load, err := loadAntigravityCodeAssistDetailed(client, "tok")
	if err != nil {
		t.Fatal(err)
	}
	if load.ProjectID != "" {
		t.Fatalf("expected empty load project, got %q", load.ProjectID)
	}

	projectID, tier, _ := resolveAntigravityProject(client, "tok", "user@example.com", load)
	if projectID == "" {
		t.Fatal("expected synthetic non-empty project id")
	}
	// Synthetic shape: adj-noun-xxxxx
	parts := strings.Split(projectID, "-")
	if len(parts) < 3 {
		t.Fatalf("synthetic shape unexpected: %q", projectID)
	}
	if tier == "" {
		t.Fatal("tier empty")
	}
}

func TestResolveAntigravityProjectUsesRealAfterOnboard(t *testing.T) {
	oldWait, oldRetries := agOnboardWait, agOnboardRetries
	agOnboardWait = time.Millisecond
	agOnboardRetries = 3
	defer func() {
		agOnboardWait = oldWait
		agOnboardRetries = oldRetries
	}()

	m := &cloudCodeMock{
		loadBody: `{"cloudaicompanionProject": null, "allowedTiers": [{"id":"legacy-tier","isDefault":true}]}`,
		onboardSeq: []string{
			`{"done":true,"response":{"cloudaicompanionProject":{"id":"real-gcp-proj"}}}`,
		},
	}
	srv := httptest.NewServer(http.HandlerFunc(m.handler))
	defer srv.Close()
	client := rewriteCloudCodeClient(t, srv.URL)

	load, err := loadAntigravityCodeAssistDetailed(client, "tok")
	if err != nil {
		t.Fatal(err)
	}
	projectID, _, _ := resolveAntigravityProject(client, "tok", "u@e.com", load)
	if projectID != "real-gcp-proj" {
		t.Fatalf("got %q want real-gcp-proj", projectID)
	}
}

func TestGenerateAntigravityProjectIDShape(t *testing.T) {
	id := GenerateAntigravityProjectID()
	if id == "" || !strings.Contains(id, "-") {
		t.Fatalf("bad id %q", id)
	}
	id2 := GenerateAntigravityProjectID()
	// Extremely unlikely equal; if equal still ok shape-wise.
	_ = id2
}
