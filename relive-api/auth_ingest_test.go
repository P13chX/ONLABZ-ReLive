package main

import (
	"strings"
	"testing"
)

func TestPasswordHashVerify(t *testing.T) {
	salt,hash,err:=hashPassword("very-long-test-password")
	if err!=nil { t.Fatal(err) }
	if !verifyPassword("very-long-test-password",salt,hash) {
		t.Fatal("expected password verification to succeed")
	}
	if verifyPassword("wrong-password",salt,hash) {
		t.Fatal("wrong password must not verify")
	}
}

func TestGeneratedStreamKey(t *testing.T) {
	a,err:=generateStreamKey()
	if err!=nil { t.Fatal(err) }
	b,err:=generateStreamKey()
	if err!=nil { t.Fatal(err) }
	if !strings.HasPrefix(a,"rlv_") || len(a)<30 {
		t.Fatalf("unexpected stream key format: %q",a)
	}
	if a==b { t.Fatal("stream keys must be random") }
}

func TestNormalizeIngestApp(t *testing.T) {
	cases:=map[string]string{
		"":"/live",
		"live":"/live",
		"/live":"/live",
		"/live/":"/live",
	}
	for in,want:=range cases {
		if got:=normalizeIngestApp(in);got!=want {
			t.Fatalf("normalizeIngestApp(%q)=%q want %q",in,got,want)
		}
	}
}

func TestFindRTMPPublisher(t *testing.T) {
	channels:=[]rtmpChannel{{Name:"/live/rlv_abc"}}
	if !findRTMPPublisher(channels,"/live","rlv_abc") {
		t.Fatal("expected current RTMP publisher to match")
	}
	if findRTMPPublisher(channels,"/live","rlv_old") {
		t.Fatal("old key must not match current publisher")
	}
}

func TestInternalRTMPSourceURL(t *testing.T) {
	got:=internalRTMPSourceURL("restreamer",1935,"/live","rlv_test","")
	if got!="rtmp://restreamer:1935/live/rlv_test" {
		t.Fatalf("unexpected RTMP source URL: %s",got)
	}
}


func TestPreviewNamesAndAuthorizationPattern(t *testing.T) {
	if got:=previewManifestName(42);got!="relive-preview-42.m3u8" {
		t.Fatalf("unexpected manifest name: %s",got)
	}
	for _,path:=range []string{
		"/preview/relive-preview-42.m3u8",
		"/preview/relive-preview-42_000001.ts",
	} {
		m:=previewChannelPattern.FindStringSubmatch(path)
		if len(m)!=2 || m[1]!="42" {
			t.Fatalf("preview channel pattern failed for %s: %v",path,m)
		}
	}
}

func TestPreviewSegmentPattern(t *testing.T) {
	got:=previewSegmentPattern(7)
	if got!="{memfs}/relive-preview-7_%06d.ts" {
		t.Fatalf("unexpected segment pattern: %s",got)
	}
}
