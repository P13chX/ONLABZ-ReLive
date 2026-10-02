package main

import (
	"net/http"
	"net/url"
	"strings"
	"time"
)

type destinationPlatform struct {
	ID                 string   `json:"id"`
	Name               string   `json:"name"`
	Protocols          []string `json:"protocols"`
	KeySources         []string `json:"key_sources"`
	DefaultServerURL   string   `json:"default_server_url,omitempty"`
	Experimental       bool     `json:"experimental"`
	Notes              string   `json:"notes,omitempty"`
}

type destination struct {
	ID           int64     `json:"id"`
	ChannelID    int64     `json:"channel_id"`
	OwnerID      string    `json:"owner_id"`
	Name         string    `json:"name"`
	Platform     string    `json:"platform"`
	KeySource    string    `json:"key_source"`
	ServerURL    string    `json:"server_url"`
	StreamKey    string    `json:"stream_key,omitempty"`
	GeneratorRef string    `json:"generator_ref,omitempty"`
	Enabled      bool      `json:"enabled"`
	DesiredState string    `json:"desired_state"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func (a *app) destinationPlatforms(w http.ResponseWriter, r *http.Request) {
	out := []destinationPlatform{
		{
			ID: "youtube", Name: "YouTube", Protocols: []string{"rtmp", "rtmps"},
			KeySources: []string{"manual_key"},
		},
		{
			ID: "facebook", Name: "Facebook", Protocols: []string{"rtmps", "rtmp"},
			KeySources: []string{"manual_key"},
		},
		{
			ID: "tiktok", Name: "TikTok LIVE", Protocols: []string{"rtmp"},
			KeySources: []string{"manual_key", "external_generator"},
			DefaultServerURL: "rtmp://push.rtmp.tiktok.com/live",
			Experimental: true,
			Notes: "manual_key is supported directly. external_generator is reserved for an isolated TikTok LIVE key-generation sidecar; unofficial generators may depend on Streamlabs/TikTok LIVE Studio access and can change without notice.",
		},
		{
			ID: "custom_rtmp", Name: "Custom RTMP/RTMPS", Protocols: []string{"rtmp", "rtmps"},
			KeySources: []string{"manual_key"},
		},
		{
			ID: "custom_srt", Name: "Custom SRT", Protocols: []string{"srt"},
			KeySources: []string{"manual_key"},
		},
	}
	writeJSON(w, http.StatusOK, out)
}

func (a *app) createDestination(w http.ResponseWriter, r *http.Request) {
	var d destination
	if err := decodeJSON(r, &d); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	d.OwnerID = strings.TrimSpace(d.OwnerID)
	d.Name = strings.TrimSpace(d.Name)
	d.Platform = strings.ToLower(strings.TrimSpace(d.Platform))
	d.KeySource = strings.ToLower(strings.TrimSpace(d.KeySource))
	d.ServerURL = strings.TrimSpace(d.ServerURL)
	d.GeneratorRef = strings.TrimSpace(d.GeneratorRef)

	if d.ChannelID <= 0 || d.OwnerID == "" || d.Name == "" || d.Platform == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "channel_id, owner_id, name and platform are required"})
		return
	}

	var channelOwner string
	if err := a.db.QueryRowContext(r.Context(), `SELECT owner_id FROM channels WHERE id=$1`, d.ChannelID).Scan(&channelOwner); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "channel not found"})
		return
	}
	if channelOwner != d.OwnerID {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "destination owner_id must match channel owner_id"})
		return
	}
	if d.KeySource == "" {
		d.KeySource = "manual_key"
	}
	// Destinations are operational by default. Start/stop is controlled separately
	// through desired_state so enabled can remain an administrative switch.
	d.Enabled = true

	switch d.Platform {
	case "youtube", "facebook":
		if d.KeySource != "manual_key" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "selected platform supports manual_key only"})
			return
		}
		if d.StreamKey == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "stream_key is required for this platform"})
			return
		}
	case "custom_rtmp", "custom_srt":
		if d.KeySource != "manual_key" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "selected platform supports manual_key only"})
			return
		}
	case "tiktok":
		switch d.KeySource {
		case "manual_key":
			if d.ServerURL == "" {
				d.ServerURL = "rtmp://push.rtmp.tiktok.com/live"
			}
			if d.StreamKey == "" {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "stream_key is required for TikTok manual_key mode"})
				return
			}
		case "external_generator":
			if d.GeneratorRef == "" {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "generator_ref is required for TikTok external_generator mode"})
				return
			}
			// The external provider owns account/session credentials and returns
			// short-lived push details when activation is implemented. We do not
			// import unofficial GPL generator code into the ReLive binary.
		default:
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported TikTok key_source"})
			return
		}
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unsupported destination platform"})
		return
	}

	if d.ServerURL == "" && d.KeySource == "manual_key" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "server_url is required"})
		return
	}
	if d.ServerURL != "" {
		u, err := url.Parse(d.ServerURL)
		if err != nil || u.Scheme == "" || u.Host == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid destination server_url"})
			return
		}
		scheme := strings.ToLower(u.Scheme)
		if d.Platform == "custom_srt" {
			if scheme != "srt" {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "custom_srt requires srt:// server_url"})
				return
			}
		} else if scheme != "rtmp" && scheme != "rtmps" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "RTMP destinations require rtmp:// or rtmps:// server_url"})
			return
		}
	}

	err := a.db.QueryRowContext(r.Context(), `
		INSERT INTO destinations(channel_id,owner_id,name,platform,key_source,server_url,stream_key,generator_ref,enabled,desired_state)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,'stopped')
		RETURNING id,desired_state,created_at,updated_at`,
		d.ChannelID,d.OwnerID,d.Name,d.Platform,d.KeySource,d.ServerURL,d.StreamKey,d.GeneratorRef,d.Enabled,
	).Scan(&d.ID,&d.DesiredState,&d.CreatedAt,&d.UpdatedAt)
	if err != nil {
		serverError(w, err)
		return
	}

	// Do not echo credentials back after creation.
	d.StreamKey = ""
	writeJSON(w, http.StatusCreated, d)
}

func (a *app) listDestinations(w http.ResponseWriter, r *http.Request) {
	owner := strings.TrimSpace(r.URL.Query().Get("owner_id"))
	q := `SELECT id,COALESCE(channel_id,0),owner_id,name,platform,key_source,server_url,generator_ref,enabled,desired_state,created_at,updated_at FROM destinations`
	args := []any{}
	if owner != "" {
		q += " WHERE owner_id=$1"
		args = append(args, owner)
	}
	q += " ORDER BY name"

	rows, err := a.db.QueryContext(r.Context(), q, args...)
	if err != nil {
		serverError(w, err)
		return
	}
	defer rows.Close()

	out := []destination{}
	for rows.Next() {
		var d destination
		if err := rows.Scan(&d.ID,&d.ChannelID,&d.OwnerID,&d.Name,&d.Platform,&d.KeySource,&d.ServerURL,
			&d.GeneratorRef,&d.Enabled,&d.DesiredState,&d.CreatedAt,&d.UpdatedAt); err != nil {
			serverError(w, err)
			return
		}
		out = append(out, d)
	}
	writeJSON(w, http.StatusOK, out)
}
