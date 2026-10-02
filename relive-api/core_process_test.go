package main

import (
	"net/url"
	"strings"
	"testing"
)

func TestInternalSRTSourceURL(t *testing.T) {
	raw := internalSRTSourceURL("restreamer", 6000, "customer-main", "token123", "secretpass", 16)
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if u.Scheme != "srt" || u.Host != "restreamer:6000" {
		t.Fatalf("unexpected SRT URL: %s", raw)
	}
	q := u.Query()
	if q.Get("mode") != "caller" || q.Get("transtype") != "live" {
		t.Fatalf("missing caller/live options: %v", q)
	}
	if !strings.Contains(q.Get("streamid"), "r=customer-main") || !strings.Contains(q.Get("streamid"), "m=request") {
		t.Fatalf("unexpected streamid: %s", q.Get("streamid"))
	}
	if !strings.Contains(q.Get("streamid"), "token=token123") {
		t.Fatalf("missing SRT token: %s", q.Get("streamid"))
	}
	if q.Get("passphrase") != "secretpass" || q.Get("pbkeylen") != "16" {
		t.Fatalf("missing encryption options: %v", q)
	}
}

func TestDestinationOutputURL(t *testing.T) {
	got := destinationOutputURL("rtmps://example.test/live", "abc123")
	if got != "rtmps://example.test/live/abc123" {
		t.Fatalf("unexpected output URL: %s", got)
	}
}

func TestMediaFromStateHealthyAudio(t *testing.T) {
	s := coreProcessState{
		Exec: "running",
		Progress: coreProgress{
			Output: []coreProgressIO{
				{Type:"video", Bitrate:5500, FPS:50},
				{Type:"audio", Bitrate:192, PPS:48},
			},
		},
	}
	v,a,fps,pps,status := mediaFromState(s)
	if v != 5500 || a != 192 || fps != 50 || pps != 48 || status != "healthy" {
		t.Fatalf("unexpected media health: v=%v a=%v fps=%v pps=%v status=%s",v,a,fps,pps,status)
	}
}

func TestMediaFromStateMissingOutputAudio(t *testing.T) {
	s := coreProcessState{
		Exec: "running",
		Progress: coreProgress{
			Input: []coreProgressIO{{Type:"audio", Bitrate:192, PPS:48, Packet:100}},
			Output: []coreProgressIO{{Type:"video", Bitrate:5500, FPS:50}},
		},
	}
	_,_,_,_,status := mediaFromState(s)
	if status != "missing" {
		t.Fatalf("expected missing audio, got %s", status)
	}
}
