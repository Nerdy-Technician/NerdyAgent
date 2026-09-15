package tickets

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchMissingAPI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	got, note := Fetch(srv.URL, "tok", 9)
	if len(got) != 0 || note == "" {
		t.Fatalf("tickets=%v note=%q", got, note)
	}
}

func TestFetchWrappedList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tickets":[{"id":3,"title":"Printer down","status":"open"}]}`))
	}))
	defer srv.Close()
	got, note := Fetch(srv.URL, "tok", 9)
	if note != "" || len(got) != 1 || got[0].ID != 3 {
		t.Fatalf("got=%v note=%q", got, note)
	}
}
