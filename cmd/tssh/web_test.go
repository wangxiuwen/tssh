package main

import (
	"crypto/subtle"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Mirror of the auth middleware in cmdWeb, isolated for testing.
// Kept in sync with cmd/tssh/web.go.
func newTestAuthMW(token string) func(http.HandlerFunc) http.HandlerFunc {
	tokenBytes := []byte(token)
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if token != "" {
				got := ""
				if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
					got = strings.TrimPrefix(h, "Bearer ")
				} else {
					got = r.URL.Query().Get("token")
				}
				if subtle.ConstantTimeCompare([]byte(got), tokenBytes) != 1 {
					http.Error(w, `{"error":"unauthorized"}`, 401)
					return
				}
			}
			next(w, r)
		}
	}
}

func TestWebAuth_NoTokenAllowsAll(t *testing.T) {
	mw := newTestAuthMW("")
	h := mw(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	})
	req := httptest.NewRequest("GET", "/api/x", nil)
	rec := httptest.NewRecorder()
	h(rec, req)
	if rec.Code != 200 {
		t.Errorf("no-token should allow, got %d", rec.Code)
	}
}

func TestWebAuth_BearerMatch(t *testing.T) {
	mw := newTestAuthMW("s3cret")
	h := mw(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	req := httptest.NewRequest("GET", "/api/x", nil)
	req.Header.Set("Authorization", "Bearer s3cret")
	rec := httptest.NewRecorder()
	h(rec, req)
	if rec.Code != 200 {
		t.Errorf("valid Bearer should pass, got %d", rec.Code)
	}
}

func TestWebAuth_QueryTokenMatch(t *testing.T) {
	mw := newTestAuthMW("s3cret")
	h := mw(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	req := httptest.NewRequest("GET", "/api/x?token=s3cret", nil)
	rec := httptest.NewRecorder()
	h(rec, req)
	if rec.Code != 200 {
		t.Errorf("valid query token should pass, got %d", rec.Code)
	}
}

func TestWebAuth_RejectBadToken(t *testing.T) {
	mw := newTestAuthMW("s3cret")
	h := mw(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	req := httptest.NewRequest("GET", "/api/x?token=wrong", nil)
	rec := httptest.NewRecorder()
	h(rec, req)
	if rec.Code != 401 {
		t.Errorf("bad token should 401, got %d", rec.Code)
	}
}

func TestWebAuth_RejectMissing(t *testing.T) {
	mw := newTestAuthMW("s3cret")
	h := mw(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	req := httptest.NewRequest("GET", "/api/x", nil)
	rec := httptest.NewRecorder()
	h(rec, req)
	if rec.Code != 401 {
		t.Errorf("no token should 401, got %d", rec.Code)
	}
}

// Regression: an empty "Authorization: Bearer " header must not grant access
// when token is set. ConstantTimeCompare of [] vs [token] must return 0.
func TestWebAuth_RejectEmptyBearer(t *testing.T) {
	mw := newTestAuthMW("s3cret")
	h := mw(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	req := httptest.NewRequest("GET", "/api/x", nil)
	req.Header.Set("Authorization", "Bearer ")
	rec := httptest.NewRecorder()
	h(rec, req)
	if rec.Code != 401 {
		t.Errorf("empty Bearer should 401, got %d", rec.Code)
	}
}
