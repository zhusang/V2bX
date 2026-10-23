package sitestat

import (
	"strings"
	"testing"
)

func TestNormalize(t *testing.T) {
	cases := []struct {
		target string
		grain  string
		want   string
	}{
		{"rr3---sn-oguesnzz.googlevideo.com:443", GranularitySite, "googlevideo.com"},
		{"www.bbc.co.uk:443", GranularitySite, "bbc.co.uk"},
		{"142.250.72.14:443", GranularitySite, "142.250.72.14"},
		{"[2001:db8::1]:443", GranularitySite, "2001:db8::1"},
		{"API2.Cursor.sh.", GranularityHost, "api2.cursor.sh"},
		{"com", GranularitySite, "com"},
		{"", GranularitySite, ""},
		{"  ", GranularitySite, ""},
	}
	for _, tc := range cases {
		if got := Normalize(tc.target, tc.grain); got != tc.want {
			t.Errorf("Normalize(%q, %q) = %q, want %q", tc.target, tc.grain, got, tc.want)
		}
	}
}

func TestNormalizeCapsLength(t *testing.T) {
	long := strings.Repeat("a", 400) + ".example"
	if got := Normalize(long, GranularityHost); len(got) != MaxSiteLen {
		t.Fatalf("len = %d, want %d", len(got), MaxSiteLen)
	}
}
