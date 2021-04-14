package mwcache

import (
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
