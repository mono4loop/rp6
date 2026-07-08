//go:build !nojam && !js && !android && !ios

package webrtc

import "testing"

func TestNormalizeURL(t *testing.T) {
	cases := map[string]string{
		"rp6-signal.example.com":         "wss://rp6-signal.example.com",
		"rp6-signal.example.com/":        "wss://rp6-signal.example.com",
		"host:1337":                      "wss://host:1337",
		"ws://localhost:1337/":           "ws://localhost:1337",
		"wss://rp6-signal.example.com":   "wss://rp6-signal.example.com",
		"http://localhost:1337":          "ws://localhost:1337",
		"https://rp6-signal.example.com": "wss://rp6-signal.example.com",
		"  rp6-signal.example.com  ":     "wss://rp6-signal.example.com",
		"":                               "",
	}
	for in, want := range cases {
		if got := NormalizeURL(in); got != want {
			t.Errorf("NormalizeURL(%q) = %q, want %q", in, got, want)
		}
	}
}
