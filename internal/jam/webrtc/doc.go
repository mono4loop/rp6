//go:build nojam || js || android || ios

// Package webrtc provides the WebRTC-based network Transport for rp6 shared jam
// sessions. The real implementation is compiled by default on desktop builds;
// it is replaced by this stub when jam is disabled (-tags nojam) or on targets
// where the pion/webrtc dependency doesn't apply (web/wasm and mobile), keeping
// those builds free of it.
package webrtc
