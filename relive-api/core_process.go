package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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
	FPS      float64 `json:"fps"`
	PPS      float64 `json:"pps"`
	Bitrate  float64 `json:"bitrate_kbit"`
	Sampling uint64  `json:"sampling_hz"`
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

func internalSRTSourceURL(host string, port int, streamID, token string) string {
	q := url.Values{}
	q.Set("mode", "caller")
	q.Set("transtype", "live")
	stream := "#!::r=" + streamID + ",m=request"
	if token != "" {
		stream += ",token=" + token
	}
	q.Set("streamid", stream)
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
