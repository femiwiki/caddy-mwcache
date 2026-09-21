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
		{now.Format("Mon, 2 Jan 2006 15:04:05 MST"), true},
		{now.Add(-2 * time.Hour).Format("Mon, 2 Jan 2006 15:04:05 MST"), false},
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
