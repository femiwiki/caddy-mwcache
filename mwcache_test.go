package mwcache

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCIDRContainsIP(t *testing.T) {
	for i, test := range []struct {
		cidr     string
		ip       string
		expected bool
	}{
		{"10.0.0.0/8", "10.0.0.4", true},
		{"10.0.0.0/8", "111.0.0.4", false},
		{"10.0.0.0/8", "111.0.0.4:34567", false},
		{"127.0.0.1", "127.0.0.1", true},
		{"127.0.0.1", "127.0.0.1:4567", true},
	} {
		actual := CIDRContainsIP(test.cidr, test.ip)
		if test.expected != actual {
			t.Errorf("Test %d: Expected: '%t' but got '%t'", i, test.expected, actual)
		}
	}
}

func TestCreateKey(t *testing.T) {
	for _, test := range []struct {
		host     string
		target   string
		expected string
	}{
		{"femiwiki.com", "/w/Main", "femiwiki.com/w/Main"},
		{"FemiWiki.COM", "/w/Main", "femiwiki.com/w/Main"},
		{"femiwiki.com:443", "/w/Main", "femiwiki.com/w/Main"},
		{"femiwiki.com.", "/w/Main", "femiwiki.com/w/Main"},
		{"www.femiwiki.com", "/w/Main", "www.femiwiki.com/w/Main"},
		{"127.0.0.1:80", "/index.php?title=Main&action=raw", "127.0.0.1/index.php?title=Main&action=raw"},
		{"[::1]:80", "/w/Main", "::1/w/Main"},
		{"[::1]", "/w/Main", "::1/w/Main"},
		{"", "/w/Main", "/w/Main"},
	} {
		req := httptest.NewRequest(http.MethodGet, test.target, nil)
		req.Host = test.host
		if actual := createKey(req); actual != test.expected {
			t.Errorf("%q %s: expected %q but got %q", test.host, test.target, test.expected, actual)
		}
	}
}
