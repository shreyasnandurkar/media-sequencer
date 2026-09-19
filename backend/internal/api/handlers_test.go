package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/shreyasnandurkar/media-sequencer/backend/internal/config"
	"github.com/shreyasnandurkar/media-sequencer/backend/internal/events"
)

func testServer() http.Handler {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := config.Config{
		Port:           "0",
		AllowedOrigins: []string{"http://localhost:5173"},
		CycleMs:        60_000,
		SyncLeadMs:     1_500,
		MinSyncMs:      1_000,
		MaxSyncMs:      3_600_000,
	}
	srv := NewServer(cfg, nil, events.NewHub(log), log)
	srv.now = func() int64 { return 1_700_000_000_000 }
	return srv.Handler()
}

func do(t *testing.T, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, path, nil)
	} else {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	testServer().ServeHTTP(rec, r)
	return rec
}

func TestHealth(t *testing.T) {
	rec := do(t, http.MethodGet, "/api/health", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["status"] != "ok" {
		t.Fatalf("body %v", body)
	}
}

func TestTimeReturnsServerClock(t *testing.T) {
	rec := do(t, http.MethodGet, "/api/time", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	var body map[string]int64
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["serverTimeMs"] != 1_700_000_000_000 {
		t.Fatalf("serverTimeMs %d", body["serverTimeMs"])
	}
	if rec.Header().Get("Cache-Control") != "no-store" {
		t.Error("the clock endpoint must not be cached")
	}
}

func TestUnknownRouteIs404(t *testing.T) {
	if rec := do(t, http.MethodGet, "/api/nope", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("status %d", rec.Code)
	}
}

func TestMethodMismatchIs405(t *testing.T) {

	if rec := do(t, http.MethodDelete, "/api/health", ""); rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status %d", rec.Code)
	}
}

func TestCreateMediaValidation(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"missing name", `{"type":"image","url":"https://x.test/a.png","durationMs":1000}`},
		{"blank name", `{"name":"   ","type":"image","url":"https://x.test/a.png","durationMs":1000}`},
		{"unknown type", `{"name":"a","type":"gif","url":"https://x.test/a.png","durationMs":1000}`},
		{"zero duration", `{"name":"a","type":"image","url":"https://x.test/a.png","durationMs":0}`},
		{"negative duration", `{"name":"a","type":"image","url":"https://x.test/a.png","durationMs":-5}`},
		{"image without url", `{"name":"a","type":"image","durationMs":1000}`},
		{"video with empty url", `{"name":"a","type":"video","url":"","durationMs":1000}`},
		{"blank with url", `{"name":"a","type":"blank","url":"https://x.test/a.png","durationMs":1000}`},
		{"protocol-relative url", `{"name":"a","type":"image","url":"//evil.example/a.png","durationMs":1000}`},
		{"non-http scheme", `{"name":"a","type":"image","url":"ftp://x.test/a.png","durationMs":1000}`},
		{"malformed json", `{"name":`},
		{"unknown field", `{"name":"a","type":"blank","durationMs":1000,"colour":"red"}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := do(t, http.MethodPost, "/api/media", c.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d (%s)", rec.Code, rec.Body.String())
			}
			assertErrorShape(t, rec)
		})
	}
}

func TestValidateMediaURL(t *testing.T) {
	ok := []string{
		"https://cdn.example/clip.mp4",
		"http://localhost:5173/media/clip.mp4",
		"https://picsum.photos/id/1015/1280/720",

		"/media/clip.mp4",
		"/media/sub dir/a%20b.png",
	}
	for _, raw := range ok {
		if err := validateMediaURL(raw); err != nil {
			t.Errorf("%q should be accepted, got %v", raw, err)
		}
	}

	bad := []string{
		"",
		"media/clip.mp4",
		"//evil.example/clip.mp4",
		"ftp://example.com/a.mp4",
		"javascript:alert(1)",
		"data:text/html,hi",
		"https://",
		"http:///a",
	}
	for _, raw := range bad {
		if err := validateMediaURL(raw); err == nil {
			t.Errorf("%q should be rejected", raw)
		}
	}
}

func TestAddItemValidation(t *testing.T) {
	cases := []struct{ name, body string }{
		{"missing mediaId", `{}`},
		{"blank mediaId", `{"mediaId":"  "}`},
		{"negative position", `{"mediaId":"M1","position":-1}`},
		{"zero duration override", `{"mediaId":"M1","durationMs":0}`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := do(t, http.MethodPost, "/api/windows/W1/items", c.body)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("expected 400, got %d (%s)", rec.Code, rec.Body.String())
			}
			assertErrorShape(t, rec)
		})
	}
}

func TestDeleteItemRejectsNonNumericID(t *testing.T) {
	rec := do(t, http.MethodDelete, "/api/windows/W1/items/abc", "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
	assertErrorShape(t, rec)
}

func TestReorderRejectsEmptyList(t *testing.T) {
	rec := do(t, http.MethodPut, "/api/windows/W1/items/order", `{"itemIds":[]}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestStartSyncRequiresMediaID(t *testing.T) {
	rec := do(t, http.MethodPost, "/api/sync", `{}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
	assertErrorShape(t, rec)
}

func TestCreateWindowRequiresName(t *testing.T) {
	rec := do(t, http.MethodPost, "/api/windows", `{"name":" "}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestCORSAllowsTheConfiguredOriginOnly(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	r.Header.Set("Origin", "http://localhost:5173")
	rec := httptest.NewRecorder()
	testServer().ServeHTTP(rec, r)
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:5173" {
		t.Fatalf("allow-origin %q", got)
	}

	r = httptest.NewRequest(http.MethodGet, "/api/health", nil)
	r.Header.Set("Origin", "https://evil.example")
	rec = httptest.NewRecorder()
	testServer().ServeHTTP(rec, r)
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("unexpected allow-origin %q for a foreign origin", got)
	}
}

func TestCORSPreflight(t *testing.T) {
	r := httptest.NewRequest(http.MethodOptions, "/api/media", nil)
	r.Header.Set("Origin", "http://localhost:5173")
	rec := httptest.NewRecorder()
	testServer().ServeHTTP(rec, r)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status %d", rec.Code)
	}
	if !strings.Contains(rec.Header().Get("Access-Control-Allow-Methods"), "POST") {
		t.Error("preflight should advertise POST")
	}
}

func assertErrorShape(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	var body errorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("error body is not JSON: %v", err)
	}
	if body.Error.Code == "" || body.Error.Message == "" {
		t.Fatalf("error body missing code/message: %s", rec.Body.String())
	}
}
