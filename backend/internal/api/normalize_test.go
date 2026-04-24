package api

import "testing"

func TestNormalizeDomain(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"example.com", "example.com"},
		{"  example.com  ", "example.com"},
		{"https://example.com", "example.com"},
		{"http://example.com/", "example.com"},
		{"HTTPS://Example.COM", "example.com"},
		{"//example.com", "example.com"},
		{"example.com:443", "example.com"},
		{"https://eskuvotervezo.sariakos.com/", "eskuvotervezo.sariakos.com"},
		{"https://example.com/path?q=1#frag", "example.com"},
		{"", ""},
		{"   ", ""},
	}
	for _, c := range cases {
		if got := normalizeDomain(c.in); got != c.want {
			t.Errorf("normalizeDomain(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestJoinDomainsNormalises(t *testing.T) {
	got := joinDomains([]string{
		"https://a.example.com/",
		"  b.example.com  ",
		"",
		"HTTPS://C.EXAMPLE.COM:443/x",
	})
	want := "a.example.com,b.example.com,c.example.com"
	if got != want {
		t.Errorf("joinDomains = %q, want %q", got, want)
	}
}
