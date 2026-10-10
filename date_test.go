package mwcache

import (
	"net/http"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestIsFreshParsesHTTPDates(t *testing.T) {
	h := Handler{logger: zap.NewNop()}
	now := time.Now().UTC()

	for i, test := range []struct {
		date     string
		expected bool
	}{
		{now.Format(http.TimeFormat), true},
		{now.Add(-30 * time.Minute).Format(http.TimeFormat), true},
		{now.Add(-2 * time.Hour).Format(http.TimeFormat), false},
		{"not a date", true},
	} {
		header := http.Header{}
		header.Set("Cache-Control", "s-maxage=3600, must-revalidate, max-age=0")
		header.Set("Date", test.date)
		if actual := h.isFresh(header); actual != test.expected {
			t.Errorf("Test %d: %q: expected '%t' but got '%t'", i, test.date, test.expected, actual)
		}
	}
}

func TestStampedDateIsIMFFixdate(t *testing.T) {
	ht := newHeadTest(t, "public, max-age=300, s-maxage=300")

	for _, name := range []string{"miss", "hit"} {
		got := ht.serve(t, http.MethodGet).Header().Get("Date")
		date, err := time.Parse(http.TimeFormat, got)
		if err != nil || date.Format(http.TimeFormat) != got {
			t.Errorf("%s: Date %q is not an IMF-fixdate", name, got)
		}
	}
	if len(ht.calls) != 1 {
		t.Errorf("expected one call upstream but got %d", len(ht.calls))
	}
}
