package mwcache

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"go.uber.org/zap"
)

// lockedBackend is a mapBackend that many requests can use at once.
type lockedBackend struct {
	mu sync.Mutex
	m  mapBackend
}

func (b *lockedBackend) get(key string) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.m.get(key)
}

func (b *lockedBackend) put(key string, val string, ttl time.Duration) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.m.put(key, val, ttl)
}

func (b *lockedBackend) delete(key string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.m.delete(key)
}

func (b *lockedBackend) close() {}

const concurrentMisses = 20

// gatedUpstream holds every request until release is closed, and numbers
// them, so a response can tell which request it answered.
type gatedUpstream struct {
	calls   atomic.Int32
	started chan struct{}
	release chan struct{}
	respond func(w http.ResponseWriter, n int32) error
}

func newGatedUpstream(respond func(w http.ResponseWriter, n int32) error) *gatedUpstream {
	return &gatedUpstream{
		started: make(chan struct{}, concurrentMisses*2),
		release: make(chan struct{}),
		respond: respond,
	}
}

func (u *gatedUpstream) ServeHTTP(w http.ResponseWriter, _ *http.Request) error {
	n := u.calls.Add(1)
	u.started <- struct{}{}
	<-u.release
	return u.respond(w, n)
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

// startMisses sends n GETs for target at once, the first one alone until it
// reaches upstream, so that it leads, and returns once the others wait on it.
func startMisses(t *testing.T, h *Handler, next caddyhttp.Handler, u *gatedUpstream, target string, n int) (*sync.WaitGroup, []*httptest.ResponseRecorder) {
	t.Helper()
	key := "example.com" + target
	var wg sync.WaitGroup
	recs := make([]*httptest.ResponseRecorder, n)
	serve := func(i int) {
		recs[i] = httptest.NewRecorder()
		wg.Go(func() {
			r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, target, nil)
			if err := h.ServeHTTP(recs[i], r, next); err != nil {
				t.Errorf("request %d: %v", i, err)
			}
		})
	}
	serve(0)
	<-u.started
	for i := 1; i < n; i++ {
		serve(i)
	}
	waitFor(t, "the misses to wait", func() bool { return misses.waiters(key) == n-1 })
	return &wg, recs
}

func newConcurrentHandler() (*Handler, *lockedBackend) {
	b := &lockedBackend{m: mapBackend{}}
	return &Handler{logger: zap.NewNop(), backend: b}, b
}

// Concurrent misses for one page reach upstream once, and all get the page.
func TestConcurrentMissesAreCoalesced(t *testing.T) {
	h, b := newConcurrentHandler()
	u := newGatedUpstream(func(w http.ResponseWriter, _ int32) error {
		w.Header().Set("Cache-Control", "s-maxage=18000, must-revalidate, max-age=0")
		w.Header().Set("Content-Type", "text/html; charset=UTF-8")
		_, err := w.Write([]byte("article"))
		return err
	})

	wg, recs := startMisses(t, h, u, u, "/w/Coalesced", concurrentMisses)
	close(u.release)
	wg.Wait()

	if n := u.calls.Load(); n != 1 {
		t.Errorf("upstream saw %d requests, want 1", n)
	}
	for i, w := range recs {
		if w.Code != http.StatusOK || w.Body.String() != "article" {
			t.Errorf("response %d: %d %q, want 200 %q", i, w.Code, w.Body.String(), "article")
		}
		if w.Header().Get("Content-Type") != "text/html; charset=UTF-8" {
			t.Errorf("response %d: Content-Type is %q", i, w.Header().Get("Content-Type"))
		}
	}
	if _, err := b.get("example.com/w/Coalesced"); err != nil {
		t.Errorf("the response was not stored: %v", err)
	}
}

// A response that may not be cached is never handed to another request. The
// waiters go upstream themselves, all at once, and later misses do not wait.
func TestUncacheableResponseIsNotShared(t *testing.T) {
	h, b := newConcurrentHandler()
	u := newGatedUpstream(func(w http.ResponseWriter, n int32) error {
		w.Header().Set("Cache-Control", "s-maxage=18000, must-revalidate, max-age=0")
		w.Header().Set("Set-Cookie", "femiwikiUserID="+strconv.Itoa(int(n)))
		_, err := w.Write([]byte("response " + strconv.Itoa(int(n))))
		return err
	})
	const target = "/w/Uncacheable"
	t.Cleanup(func() { misses.unpass("example.com" + target) })

	wg, recs := startMisses(t, h, u, u, target, concurrentMisses)
	// Upstream holds the first request, so the others can only reach it by
	// not waiting on one another.
	u.release <- struct{}{}
	for range concurrentMisses - 1 {
		select {
		case <-u.started:
		case <-time.After(5 * time.Second):
			t.Fatalf("only %d requests reached upstream at once", u.calls.Load())
		}
	}
	close(u.release)
	wg.Wait()

	if n := u.calls.Load(); n != concurrentMisses {
		t.Errorf("upstream saw %d requests, want %d", n, concurrentMisses)
	}
	seen := map[string]bool{}
	for i, w := range recs {
		cookie := w.Header().Get("Set-Cookie")
		if seen[cookie] {
			t.Errorf("response %d: %q was handed to another request too", i, cookie)
		}
		seen[cookie] = true
		if want := "response " + cookie[len("femiwikiUserID="):]; w.Body.String() != want {
			t.Errorf("response %d: body %q, want %q", i, w.Body.String(), want)
		}
	}
	if _, err := b.get("example.com" + target); err == nil {
		t.Error("the response was stored")
	}

	// Two later misses both reach upstream without one waiting on the other
	u.release = make(chan struct{})
	for range 2 {
		wg.Go(func() {
			r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, target, nil)
			if err := h.ServeHTTP(httptest.NewRecorder(), r, u); err != nil {
				t.Error(err)
			}
		})
	}
	for range 2 {
		select {
		case <-u.started:
		case <-time.After(5 * time.Second):
			t.Fatal("a miss after the uncacheable response waited")
		}
	}
	close(u.release)
	wg.Wait()
}

// When upstream fails, the waiters try upstream themselves, and the key is
// not marked, since an error does not say the next response cannot be shared.
func TestUpstreamErrorIsNotShared(t *testing.T) {
	h, _ := newConcurrentHandler()
	failure := errors.New("php-fpm is down")
	u := newGatedUpstream(func(w http.ResponseWriter, n int32) error {
		if n == 1 {
			return failure
		}
		w.Header().Set("Cache-Control", "s-maxage=18000, must-revalidate, max-age=0")
		_, err := w.Write([]byte("article"))
		return err
	})
	const target = "/w/Failing"
	key := "example.com" + target

	var wg sync.WaitGroup
	var leaderErr error
	wg.Go(func() {
		r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, target, nil)
		leaderErr = h.ServeHTTP(httptest.NewRecorder(), r, u)
	})
	<-u.started
	waiter := httptest.NewRecorder()
	wg.Go(func() {
		r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, target, nil)
		if err := h.ServeHTTP(waiter, r, u); err != nil {
			t.Errorf("waiter: %v", err)
		}
	})
	waitFor(t, "the waiter", func() bool { return misses.waiters(key) == 1 })
	close(u.release)
	wg.Wait()

	if !errors.Is(leaderErr, failure) {
		t.Errorf("leader got %v, want %v", leaderErr, failure)
	}
	if waiter.Body.String() != "article" {
		t.Errorf("waiter got %q, want %q", waiter.Body.String(), "article")
	}
	if f, leader := misses.join(key); f != nil || !leader {
		t.Error("the key was marked after an upstream error")
	} else {
		misses.land(key, nil, true)
	}
}

// A waiter whose client goes away stops waiting.
func TestWaiterGivesUpWithItsClient(t *testing.T) {
	h, _ := newConcurrentHandler()
	u := newGatedUpstream(func(w http.ResponseWriter, _ int32) error {
		w.Header().Set("Cache-Control", "s-maxage=18000, must-revalidate, max-age=0")
		_, err := w.Write([]byte("article"))
		return err
	})
	const target = "/w/Abandoned"

	var wg sync.WaitGroup
	wg.Go(func() {
		r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, target, nil)
		if err := h.ServeHTTP(httptest.NewRecorder(), r, u); err != nil {
			t.Error(err)
		}
	})
	<-u.started
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error)
	go func() {
		r := httptest.NewRequestWithContext(ctx, http.MethodGet, target, nil)
		done <- h.ServeHTTP(httptest.NewRecorder(), r, u)
	}()
	waitFor(t, "the waiter", func() bool { return misses.waiters("example.com"+target) == 1 })
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("waiter returned %v, want %v", err, context.Canceled)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the waiter kept waiting")
	}
	close(u.release)
	wg.Wait()
}

// A key marked as pass skips the wait for passTTL, and a response that can be
// shared again lifts the mark early.
func TestPassMarkExpires(t *testing.T) {
	now := time.Unix(0, 0)
	c := newCoalescer()
	c.now = func() time.Time { return now }

	if _, leader := c.join("k"); !leader {
		t.Fatal("the first miss does not lead")
	}
	c.land("k", nil, false)
	if f, leader := c.join("k"); f != nil || leader {
		t.Fatal("a miss right after an unshared response did not skip the wait")
	}

	now = now.Add(passTTL)
	if _, leader := c.join("k"); !leader {
		t.Fatal("the mark outlived passTTL")
	}
	c.land("k", []byte("entry"), false)
	if _, ok := c.pass["k"]; ok {
		t.Error("a shared response left the key marked")
	}

	c.land("other", nil, false)
	c.unpass("other")
	if _, ok := c.pass["other"]; ok {
		t.Error("unpass left the key marked")
	}
}

// Marks nobody asks about again are swept once they expire.
func TestPassMarksAreSwept(t *testing.T) {
	now := time.Unix(0, 0)
	c := newCoalescer()
	c.now = func() time.Time { return now }
	c.land("old", nil, false)
	now = now.Add(passTTL + time.Second)
	c.land("new", nil, false)
	if _, ok := c.pass["old"]; ok {
		t.Error("an expired mark was not swept")
	}
	if _, ok := c.pass["new"]; !ok {
		t.Error("a fresh mark was swept")
	}
}

// A HEAD miss neither leads nor waits, as its response has no body to share.
func TestHeadMissIsNotCoalesced(t *testing.T) {
	h, _ := newConcurrentHandler()
	next := cacheableUpstream()
	r := httptest.NewRequestWithContext(t.Context(), http.MethodHead, "/w/Head", nil)
	if err := h.ServeHTTP(httptest.NewRecorder(), r, next); err != nil {
		t.Fatal(err)
	}
	if f, leader := misses.join("example.com/w/Head"); f != nil || !leader {
		t.Error("a HEAD miss left a flight or a mark behind")
	} else {
		misses.land("example.com/w/Head", nil, true)
	}
}
