package mwcache

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"go.uber.org/zap"
)

// droppingBackend refuses every write, as ristretto does when its set buffer
// is full.
type droppingBackend struct{}

func (droppingBackend) get(string) (string, error) { return "", ErrKeyNotFound }
func (droppingBackend) put(string, string) error   { return errors.New("set was dropped") }
func (droppingBackend) delete(string) error        { return nil }

func TestDroppedWriteStillServes(t *testing.T) {
	h := Handler{logger: zap.NewNop(), backend: droppingBackend{}}
	upstream := caddyhttp.HandlerFunc(func(w http.ResponseWriter, r *http.Request) error {
		w.Header().Set("Cache-Control", "s-maxage=1200, must-revalidate, max-age=0")
		w.Header().Set("Content-Type", "text/html; charset=UTF-8")
		_, err := w.Write([]byte("<p>page</p>"))
		return err
	})

	req := httptest.NewRequest(http.MethodGet, "/w/Main", nil)
	rec := httptest.NewRecorder()
	if err := h.ServeHTTP(rec, req, upstream); err != nil {
		t.Fatalf("expected the page, got error %v", err)
	}
	if rec.Code != http.StatusOK || rec.Body.String() != "<p>page</p>" {
		t.Errorf("got %d %q", rec.Code, rec.Body.String())
	}
}
