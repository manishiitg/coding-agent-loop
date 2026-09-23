package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/mux"
)

func TestAPILatencyStatsPercentilesAndOrdering(t *testing.T) {
	s := &apiLatencyStats{routes: map[string]*apiRouteSamples{}}
	for i := 1; i <= 100; i++ {
		s.record("GET /api/hot", 200, time.Duration(i)*time.Millisecond)
	}
	s.record("GET /api/rare", 500, 2*time.Second)

	got := s.drain()
	if len(got) != 2 || got[0].Route != "GET /api/hot" {
		t.Fatalf("summaries = %+v, want hot route first by total time", got)
	}
	if got[0].P50 != 50*time.Millisecond || got[0].P95 != 95*time.Millisecond || got[0].Max != 100*time.Millisecond {
		t.Fatalf("hot percentiles = %+v", got[0])
	}
	if got[1].Errors != 1 {
		t.Fatalf("5xx not counted: %+v", got[1])
	}
	if len(s.drain()) != 0 {
		t.Fatal("drain must reset the window")
	}
}

func TestAPIRequestLogMiddlewareTimesEveryRouteByTemplate(t *testing.T) {
	apiLatency.drain()
	api := &StreamingAPI{}
	router := mux.NewRouter()
	sub := router.PathPrefix("/api").Subrouter()
	sub.Use(api.apiRequestLogMiddleware)
	sub.HandleFunc("/sessions/{session_id}/events", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("[]")) })
	sub.HandleFunc("/sessions/{session_id}/stream", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("data: x\n\n")) })

	for _, id := range []string{"a1", "b2"} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/sessions/"+id+"/events", nil))
		if st := rec.Header().Get("Server-Timing"); !strings.HasPrefix(st, "app;dur=") {
			t.Fatalf("Server-Timing = %q", st)
		}
	}
	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/sessions/a1/stream", nil))

	summaries := apiLatency.drain()
	if len(summaries) != 1 || summaries[0].Route != "GET /api/sessions/{session_id}/events" || summaries[0].Count != 2 {
		t.Fatalf("summaries = %+v, want one templated events route with 2 requests and no stream", summaries)
	}
}

func TestCORSExposesServerTiming(t *testing.T) {
	api := &StreamingAPI{config: ServerConfig{CORSOrigins: []string{"http://127.0.0.1:51734"}}}
	handler := api.corsMiddleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	req := httptest.NewRequest(http.MethodGet, "/api/sessions/x/events", nil)
	req.Header.Set("Origin", "http://127.0.0.1:51734")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Header().Get("Access-Control-Expose-Headers") != "Server-Timing" || rec.Header().Get("Timing-Allow-Origin") != "http://127.0.0.1:51734" {
		t.Fatalf("CORS timing headers = %v", rec.Header())
	}
}
