package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

type coreProcessIO struct {
	ID      string   `json:"id,omitempty"`
	Address string   `json:"address"`
	Options []string `json:"options,omitempty"`
}

type coreProcessConfig struct {
	ID             string          `json:"id,omitempty"`
	Type           string          `json:"type,omitempty"`
	Reference      string          `json:"reference,omitempty"`
	Input          []coreProcessIO `json:"input"`
	Output         []coreProcessIO `json:"output"`
	Options        []string        `json:"options,omitempty"`
	Reconnect      bool            `json:"reconnect"`
	ReconnectDelay uint64          `json:"reconnect_delay_seconds"`
	Autostart      bool            `json:"autostart"`
	StaleTimeout   uint64          `json:"stale_timeout_seconds"`
}

type coreProgressIO struct {
	ID       string  `json:"id"`
	Type     string  `json:"type"`
	Codec    string  `json:"codec"`
	Coder    string  `json:"coder"`
	Format   string  `json:"format"`
	Pixfmt   string  `json:"pix_fmt"`
	Width    uint64  `json:"width"`
	Height   uint64  `json:"height"`
	FPS      float64 `json:"fps"`
	PPS      float64 `json:"pps"`
	Bitrate  float64 `json:"bitrate_kbit"`
	Sampling uint64  `json:"sampling_hz"`
	Layout   string  `json:"layout"`
	Channels uint64  `json:"channels"`
	Packet   uint64  `json:"packet"`
}

type coreProgress struct {
	Input   []coreProgressIO `json:"inputs"`
	Output  []coreProgressIO `json:"outputs"`
	FPS     float64          `json:"fps"`
	Bitrate float64          `json:"bitrate_kbit"`
	Drop    uint64           `json:"drop"`
	Dup     uint64           `json:"dup"`
}

type coreProcessState struct {
	Order     string       `json:"order"`
	Exec      string       `json:"exec"`
	Runtime   int64        `json:"runtime_seconds"`
	Reconnect int64        `json:"reconnect_seconds"`
	LastLog   string       `json:"last_logline"`
	Progress  coreProgress `json:"progress"`
}

func (c *coreClient) doJSON(ctx context.Context, method, path string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(payload)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.username != "" {
		req.SetBasicAuth(c.username, c.password)
	}

	res, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
		return fmt.Errorf("core API %s %s returned %s: %s", method, path, res.Status, strings.TrimSpace(string(b)))
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(res.Body).Decode(out)
}

func (c *coreClient) createProcess(ctx context.Context, cfg coreProcessConfig) (coreProcessConfig, error) {
	var out coreProcessConfig
	err := c.doJSON(ctx, http.MethodPost, "/api/v3/process", cfg, &out)
	return out, err
}

func (c *coreClient) processState(ctx context.Context, id string) (coreProcessState, error) {
	var out coreProcessState
	err := c.doJSON(ctx, http.MethodGet, "/api/v3/process/"+url.PathEscape(id)+"/state", nil, &out)
	return out, err
}

func (c *coreClient) processCommand(ctx context.Context, id, command string) error {
	return c.doJSON(ctx, http.MethodPut, "/api/v3/process/"+url.PathEscape(id)+"/command", map[string]string{"command": command}, nil)
}

func (c *coreClient) deleteProcess(ctx context.Context, id string) error {
	return c.doJSON(ctx, http.MethodDelete, "/api/v3/process/"+url.PathEscape(id), nil, nil)
}

func internalSRTSourceURL(host string, port int, streamID, token, passphrase string, pbkeylen int) string {
	q := url.Values{}
	q.Set("mode", "caller")
	q.Set("transtype", "live")
	stream := "#!::r=" + streamID + ",m=request"
	if token != "" {
		stream += ",token=" + token
	}
	q.Set("streamid", stream)
	if passphrase != "" {
		q.Set("passphrase", passphrase)
		if pbkeylen == 16 || pbkeylen == 24 || pbkeylen == 32 {
			q.Set("pbkeylen", strconv.Itoa(pbkeylen))
		}
	}
	return "srt://" + host + ":" + strconv.Itoa(port) + "?" + q.Encode()
}

func destinationOutputURL(serverURL, streamKey string) string {
	serverURL = strings.TrimSpace(serverURL)
	streamKey = strings.TrimSpace(streamKey)
	if streamKey == "" {
		return serverURL
	}
	if strings.HasSuffix(serverURL, "/") {
		return serverURL + streamKey
	}
	return serverURL + "/" + streamKey
}

func passThroughOptions(platform string) []string {
	switch strings.ToLower(platform) {
	case "custom_srt":
		return []string{"-c:v", "copy", "-c:a", "copy", "-f", "mpegts"}
	default:
		return []string{"-c:v", "copy", "-c:a", "copy", "-f", "flv"}
	}
}

func mediaFromState(s coreProcessState) (videoKbps, audioKbps, fps, audioPPS float64, audioStatus string) {
	audioStatus = "unknown"
	for _, io := range s.Progress.Output {
		switch strings.ToLower(io.Type) {
		case "video":
			videoKbps += io.Bitrate
			if io.FPS > fps {
				fps = io.FPS
			}
		case "audio":
			audioKbps += io.Bitrate
			audioPPS += io.PPS
			if io.PPS > 0 || io.Bitrate > 0 {
				audioStatus = "healthy"
			}
		}
	}
	if audioStatus == "unknown" {
		inputAudio := false
		for _, io := range s.Progress.Input {
			if strings.EqualFold(io.Type, "audio") && (io.PPS > 0 || io.Bitrate > 0 || io.Packet > 0) {
				inputAudio = true
				break
			}
		}
		if inputAudio && strings.EqualFold(s.Exec, "running") {
			audioStatus = "missing"
		}
	}
	return
}


type bitstreamIntegrity struct {
	Status                 string
	SourceVideoCodec       string
	OutputVideoCodec       string
	SourceResolution       string
	OutputResolution       string
	SourceFPS              float64
	OutputFPS              float64
	SourceVideoBitrateKbps float64
	OutputVideoBitrateKbps float64
	SourceAudioCodec       string
	OutputAudioCodec       string
	SourceAudioKbps        float64
	OutputAudioKbps        float64
	SourceAudioHz          uint64
	OutputAudioHz          uint64
	SourceAudioChannels    uint64
	OutputAudioChannels    uint64
	Reasons                []string
}

func mediaPair(streams []coreProgressIO, mediaType string) (coreProgressIO, bool) {
	for _, s := range streams {
		if strings.EqualFold(s.Type, mediaType) {
			return s, true
		}
	}
	return coreProgressIO{}, false
}

func evaluateBitstreamIntegrity(s coreProcessState) bitstreamIntegrity {
	q := bitstreamIntegrity{Status: "unknown", Reasons: []string{}}
	inV, hasInV := mediaPair(s.Progress.Input, "video")
	outV, hasOutV := mediaPair(s.Progress.Output, "video")
	inA, hasInA := mediaPair(s.Progress.Input, "audio")
	outA, hasOutA := mediaPair(s.Progress.Output, "audio")

	if hasInV {
		q.SourceVideoCodec = inV.Codec
		q.SourceResolution = fmt.Sprintf("%dx%d", inV.Width, inV.Height)
		q.SourceFPS = inV.FPS
		q.SourceVideoBitrateKbps = inV.Bitrate
	}
	if hasOutV {
		q.OutputVideoCodec = outV.Codec
		q.OutputResolution = fmt.Sprintf("%dx%d", outV.Width, outV.Height)
		q.OutputFPS = outV.FPS
		q.OutputVideoBitrateKbps = outV.Bitrate
	}
	if hasInA {
		q.SourceAudioCodec = inA.Codec
		q.SourceAudioKbps = inA.Bitrate
		q.SourceAudioHz = inA.Sampling
		q.SourceAudioChannels = inA.Channels
	}
	if hasOutA {
		q.OutputAudioCodec = outA.Codec
		q.OutputAudioKbps = outA.Bitrate
		q.OutputAudioHz = outA.Sampling
		q.OutputAudioChannels = outA.Channels
	}

	if !hasInV || !hasOutV {
		return q
	}

	changed := false
	if !strings.EqualFold(inV.Codec, outV.Codec) {
		changed = true
		q.Reasons = append(q.Reasons, "video codec changed")
	}
	if inV.Width != outV.Width || inV.Height != outV.Height {
		changed = true
		q.Reasons = append(q.Reasons, "resolution changed")
	}
	if inV.FPS > 0 && outV.FPS > 0 && math.Abs(inV.FPS-outV.FPS) > 0.5 {
		changed = true
		q.Reasons = append(q.Reasons, "frame rate changed")
	}
	if hasInA != hasOutA {
		changed = true
		q.Reasons = append(q.Reasons, "audio stream presence changed")
	}
	if hasInA && hasOutA {
		if !strings.EqualFold(inA.Codec, outA.Codec) {
			changed = true
			q.Reasons = append(q.Reasons, "audio codec changed")
		}
		if inA.Sampling > 0 && outA.Sampling > 0 && inA.Sampling != outA.Sampling {
			changed = true
			q.Reasons = append(q.Reasons, "audio sample rate changed")
		}
		if inA.Channels > 0 && outA.Channels > 0 && inA.Channels != outA.Channels {
			changed = true
			q.Reasons = append(q.Reasons, "audio channel count changed")
		}
	}
	if changed {
		q.Status = "changed"
	} else {
		q.Status = "preserved"
	}
	return q
}
