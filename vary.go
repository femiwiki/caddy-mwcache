package mwcache

import (
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
)

// The page's key holds this marker and a generation, which the variants' keys
// carry, so a purge of the page key strands every variant at once.
const varyMarkerPrefix = "\x00mwcache-vary-accept-language\x00"

// generations numbers the markers. The cache lives in memory, so numbers
// that are unique within the process are enough.
var generations atomic.Uint64

// varyMarker returns the marker of a new generation. It begins with a NUL,
// which a gob stream, the start of every entry, never does.
func varyMarker() (marker, generation string) {
	generation = strconv.FormatUint(generations.Add(1), 10)
	return varyMarkerPrefix + generation, generation
}

// markerGeneration returns the generation val marks, or false if val is an
// entry.
func markerGeneration(val string) (string, bool) {
	return strings.CutPrefix(val, varyMarkerPrefix)
}

// variantKey is the key of the variant of the page at key for the
// Accept-Language r sends, under generation.
func variantKey(key string, generation string, r *http.Request) string {
	return key + "\x00" + generation + "\x00" + normalizeAcceptLanguage(r)
}

// languageKey keeps misses for a page apart by Accept-Language, so a waiter is
// never handed another language's page.
func languageKey(key string, r *http.Request) string {
	return key + "\x00" + normalizeAcceptLanguage(r)
}

// normalizeAcceptLanguage lowercases ASCII letters and drops whitespace around
// separators only, as ULS reads the whole list in q order (see README.md).
func normalizeAcceptLanguage(r *http.Request) string {
	v := strings.Join(r.Header.Values("Accept-Language"), ",")
	var b strings.Builder
	b.Grow(len(v))
	var last byte
	space := false
	for i := range len(v) {
		c := v[i]
		switch c {
		case ' ', '\t':
			space = true
			continue
		case ',', ';', '=':
		default:
			// ASCII only: strings.ToLower would turn the Kelvin sign into a
			// k, which MediaWiki's [a-z] never matches.
			if 'A' <= c && c <= 'Z' {
				c += 'a' - 'A'
			}
			// Whitespace inside a value still splits it, as it does for
			// MediaWiki, which reads "e n" as e and n, not en.
			if space && last != 0 && last != ',' && last != ';' && last != '=' {
				b.WriteByte(' ')
			}
		}
		space = false
		b.WriteByte(c)
		last = c
	}
	return b.String()
}

// variesOnAcceptLanguage reports whether a Vary field of header names
// Accept-Language.
func variesOnAcceptLanguage(header http.Header) bool {
	for _, v := range header.Values("Vary") {
		for name := range strings.SplitSeq(v, ",") {
			if strings.EqualFold(strings.TrimSpace(name), "Accept-Language") {
				return true
			}
		}
	}
	return false
}
