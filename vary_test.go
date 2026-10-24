package mwcache

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caddyserver/caddy/v2/caddyconfig/caddyfile"
	"github.com/caddyserver/caddy/v2/modules/caddyhttp"
	"go.uber.org/zap"
)

// ulsUpstream answers in the first language the request asks for, or ko, the
// way MediaWiki does with $wgULSLanguageDetection, and counts the requests.
type ulsUpstream struct {
	calls int
	vary  bool
}

func (u *ulsUpstream) ServeHTTP(w http.ResponseWriter, r *http.Request) error {
	u.calls++
	lang := "ko"
	if tag, _, _ := strings.Cut(r.Header.Get("Accept-Language"), ","); tag != "" {
		lang, _, _ = strings.Cut(tag, ";")
	}
	w.Header().Set("Cache-Control", "s-maxage=18000, must-revalidate, max-age=0")
	if u.vary {
		w.Header().Set("Vary", "Accept-Encoding, Cookie, Accept-Language")
	}
	w.WriteHeader(http.StatusOK)
	_, err := w.Write([]byte("page in " + lang))
	return err
}

func newVaryHandler() (Handler, mapBackend) {
	h, b := newTestHandler()
	h.Config.VaryAcceptLanguage = true
	return h, b
}

func getIn(t *testing.T, h Handler, next caddyhttp.Handler, method, acceptLanguage string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequestWithContext(t.Context(), method, "/w/Article", nil)
	r.RemoteAddr = "10.0.0.1:34567"
	if acceptLanguage != "" {
		r.Header.Set("Accept-Language", acceptLanguage)
	}
	w := httptest.NewRecorder()
	if err := h.ServeHTTP(w, r, next); err != nil {
		t.Fatalf("ServeHTTP: %v", err)
	}
	return w
}

// Without the option the first language a page is rendered in is what every
// reader gets, as before.
func TestVaryIsIgnoredByDefault(t *testing.T) {
	h, _ := newTestHandler()
	next := &ulsUpstream{vary: true}

	getIn(t, h, next, http.MethodGet, "en")
	if w := getIn(t, h, next, http.MethodGet, "ja"); w.Body.String() != "page in en" {
		t.Errorf("ja was served %q", w.Body.String())
	}
	if next.calls != 1 {
		t.Errorf("upstream saw %d requests, want 1", next.calls)
	}
}

func TestEachLanguageGetsItsOwnVariant(t *testing.T) {
	h, _ := newVaryHandler()
	next := &ulsUpstream{vary: true}

	for _, test := range []struct{ lang, want string }{
		{"en", "page in en"},
		{"ja", "page in ja"},
		{"", "page in ko"},
		{"EN", "page in en"},
		{"ja", "page in ja"},
		{"", "page in ko"},
	} {
		if w := getIn(t, h, next, http.MethodGet, test.lang); w.Body.String() != test.want {
			t.Errorf("%q was served %q, want %q", test.lang, w.Body.String(), test.want)
		}
	}
	if next.calls != 3 {
		t.Errorf("upstream saw %d requests, want 3", next.calls)
	}
	if w := getIn(t, h, next, http.MethodHead, "ja"); w.Code != http.StatusOK || next.calls != 3 {
		t.Errorf("HEAD in ja: %d, upstream saw %d requests, want a hit", w.Code, next.calls)
	}
}

// A response that does not vary, such as load.php's, is stored once for
// every language.
func TestResponseThatDoesNotVaryIsShared(t *testing.T) {
	h, b := newVaryHandler()
	next := &ulsUpstream{}

	getIn(t, h, next, http.MethodGet, "en")
	if w := getIn(t, h, next, http.MethodGet, "ja"); w.Body.String() != "page in en" {
		t.Errorf("ja was served %q", w.Body.String())
	}
	if next.calls != 1 || len(b) != 1 {
		t.Errorf("upstream saw %d requests and %d entries are held, want 1 and 1", next.calls, len(b))
	}
}

func TestPurgeDropsEveryVariant(t *testing.T) {
	h, _ := newVaryHandler()
	next := &ulsUpstream{vary: true}

	getIn(t, h, next, http.MethodGet, "en")
	getIn(t, h, next, http.MethodGet, "ja")
	if w := getIn(t, h, next, "PURGE", ""); w.Code != http.StatusNoContent {
		t.Fatalf("PURGE: %d", w.Code)
	}
	// The first fill after the purge marks the page again, which must not
	// bring back the other language's variant of before
	getIn(t, h, next, http.MethodGet, "en")
	getIn(t, h, next, http.MethodGet, "ja")
	if next.calls != 4 {
		t.Errorf("upstream saw %d requests, want 4", next.calls)
	}
}

// A page that stops varying replaces its marker, and its variants are no
// longer read.
func TestResponseThatStopsVaryingReplacesTheVariants(t *testing.T) {
	h, b := newVaryHandler()
	next := &ulsUpstream{vary: true}

	getIn(t, h, next, http.MethodGet, "en")
	getIn(t, h, next, "PURGE", "")
	next.vary = false
	getIn(t, h, next, http.MethodGet, "ja")
	if w := getIn(t, h, next, http.MethodGet, "en"); w.Body.String() != "page in ja" {
		t.Errorf("en was served %q", w.Body.String())
	}
	if _, ok := markerGeneration(b[articleKey]); ok {
		t.Error("the page is still marked as varying")
	}
}

// Misses in two languages for one page are not coalesced, or the second would
// be handed the first one's language.
func TestMissesInOtherLanguagesAreNotCoalesced(t *testing.T) {
	b := &lockedBackend{m: mapBackend{}}
	h := &Handler{logger: zap.NewNop(), backend: b, Config: Config{VaryAcceptLanguage: true}}
	u := newGatedUpstream(func(w http.ResponseWriter, n int32) error {
		w.Header().Set("Cache-Control", "s-maxage=18000, must-revalidate, max-age=0")
		w.Header().Set("Vary", "Accept-Language")
		_, err := w.Write([]byte("response " + strconv.Itoa(int(n))))
		return err
	})

	var wg sync.WaitGroup
	recs := map[string]*httptest.ResponseRecorder{}
	for _, lang := range []string{"en", "ja"} {
		recs[lang] = httptest.NewRecorder()
		r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/w/Coalesce-lang", nil)
		r.Header.Set("Accept-Language", lang)
		wg.Go(func() {
			if err := h.ServeHTTP(recs[lang], r, u); err != nil {
				t.Errorf("%s: %v", lang, err)
			}
		})
		// Each reaches upstream itself instead of waiting on the other
		select {
		case <-u.started:
		case <-time.After(5 * time.Second):
			close(u.release)
			wg.Wait()
			t.Fatalf("%s waited on the miss in another language", lang)
		}
	}
	close(u.release)
	wg.Wait()
	if recs["en"].Body.String() == recs["ja"].Body.String() {
		t.Errorf("both were served %q", recs["en"].Body.String())
	}
}

func TestNormalizeAcceptLanguage(t *testing.T) {
	for _, test := range []struct{ in, want string }{
		{"", ""},
		{"ko-KR,ko;q=0.9,en-US;q=0.8,en;q=0.7", "ko-kr,ko;q=0.9,en-us;q=0.8,en;q=0.7"},
		{" ko-KR , ko ; q = 0.9 ", "ko-kr,ko;q=0.9"},
		{"ko,\ten", "ko,en"},
		// Not one tag: MediaWiki reads e and n
		{"e n", "e n"},
		// U+212A, which strings.ToLower would make a k
		{"Ko", "Ko"},
		// The order and the q-values decide the language, and so stay
		{"en;q=0.5,ko", "en;q=0.5,ko"},
		{"zh-TW", "zh-tw"},
	} {
		r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
		if test.in != "" {
			r.Header.Set("Accept-Language", test.in)
		}
		if got := normalizeAcceptLanguage(r); got != test.want {
			t.Errorf("%q: got %q, want %q", test.in, got, test.want)
		}
	}

	r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
	r.Header.Add("Accept-Language", "ko")
	r.Header.Add("Accept-Language", "en")
	if got := normalizeAcceptLanguage(r); got != "ko,en" {
		t.Errorf("two fields: got %q", got)
	}
}

func TestVariesOnAcceptLanguage(t *testing.T) {
	for _, test := range []struct {
		vary []string
		want bool
	}{
		{nil, false},
		{[]string{"Accept-Encoding, Cookie"}, false},
		{[]string{"Accept-Encoding, accept-language,Cookie"}, true},
		{[]string{"Accept-Encoding", "Accept-Language"}, true},
		{[]string{"Accept-Languages"}, false},
	} {
		if got := variesOnAcceptLanguage(http.Header{"Vary": test.vary}); got != test.want {
			t.Errorf("%q: got %t", test.vary, got)
		}
	}
}

func TestVaryAcceptLanguageDirective(t *testing.T) {
	h := &Handler{}
	if err := h.UnmarshalCaddyfile(caddyfile.NewTestDispenser(`mwcache {
		vary_accept_language
	}`)); err != nil {
		t.Fatal(err)
	}
	if !h.Config.VaryAcceptLanguage {
		t.Error("vary_accept_language is off")
	}
	if err := (&Handler{}).UnmarshalCaddyfile(caddyfile.NewTestDispenser(`mwcache {
		vary_accept_language yes
	}`)); err == nil {
		t.Error("an argument is accepted")
	}
}
