package main

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type destinationRuntimeUpdate struct {
	Status             string  `json:"status"`
	OutputBitrateMbps  float64 `json:"output_bitrate_mbps"`
	VideoBitrateKbps   float64 `json:"video_bitrate_kbps"`
	AudioBitrateKbps   float64 `json:"audio_bitrate_kbps"`
	FPS                float64 `json:"fps"`
	AudioStatus        string  `json:"audio_status"`
	AudioPPS           float64 `json:"audio_pps"`
	ReconnectCount     int     `json:"reconnect_count"`
	LastError          string  `json:"last_error"`
}

type destinationRuntime struct {
	ID                 int64      `json:"id"`
	ChannelID          *int64     `json:"channel_id,omitempty"`
	OwnerID            string     `json:"owner_id"`
	Name               string     `json:"name"`
	Platform           string     `json:"platform"`
	Enabled            bool       `json:"enabled"`
	DesiredState       string     `json:"desired_state"`
	Status             string     `json:"status"`
	CoreProcessID      string     `json:"core_process_id,omitempty"`
	OutputBitrateMbps  float64    `json:"output_bitrate_mbps"`
	VideoBitrateKbps   float64    `json:"video_bitrate_kbps"`
	AudioBitrateKbps   float64    `json:"audio_bitrate_kbps"`
	FPS                float64    `json:"fps"`
	AudioStatus        string     `json:"audio_status"`
	AudioPPS           float64    `json:"audio_pps"`
	ReconnectCount     int        `json:"reconnect_count"`
	QualityStatus      string     `json:"quality_status"`
	QualityReasons     []string   `json:"quality_reasons"`
	SourceVideoCodec   string     `json:"source_video_codec"`
	OutputVideoCodec   string     `json:"output_video_codec"`
	SourceResolution   string     `json:"source_resolution"`
	OutputResolution   string     `json:"output_resolution"`
	SourceFPS          float64    `json:"source_fps"`
	OutputFPS          float64    `json:"output_fps"`
	SourceVideoKbps    float64    `json:"source_video_bitrate_kbps"`
	SourceAudioCodec   string     `json:"source_audio_codec"`
	OutputAudioCodec   string     `json:"output_audio_codec"`
	SourceAudioHz      uint64     `json:"source_audio_hz"`
	OutputAudioHz      uint64     `json:"output_audio_hz"`
	SourceAudioChannels uint64    `json:"source_audio_channels"`
	OutputAudioChannels uint64    `json:"output_audio_channels"`
	LastError          string     `json:"last_error,omitempty"`
	LastStatusAt       *time.Time `json:"last_status_at,omitempty"`
}

type liveTelemetry struct {
	ChannelID            int64   `json:"channel_id"`
	StreamID             string  `json:"stream_id"`
	Status               string  `json:"status"`
	RTTMs                float64 `json:"rtt_ms"`
	BandwidthMbit        float64 `json:"bandwidth_mbit"`
	RecvBufferMs         uint64  `json:"recv_buffer_ms"`
	SRTLatencyMs         uint64  `json:"srt_latency_ms"`
	RecvUniquePackets    uint64  `json:"recv_unique_packets"`
	RecvLossPackets      uint64  `json:"recv_loss_packets"`
	RecvRetransPackets   uint64  `json:"recv_retrans_packets"`
	RecvDropPackets      uint64  `json:"recv_drop_packets"`
	RecvUniqueBytes      uint64  `json:"recv_unique_bytes"`
	TimestampMs          uint64  `json:"timestamp_ms"`
	ObservedAt           string  `json:"observed_at"`
}

func (a *app) channelLiveTelemetry(w http.ResponseWriter, r *http.Request) {
	c, err := a.channelByID(r.Context(), r.PathValue("id"))
	if errors.Is(err, sql.ErrNoRows) {
		writeJSON(w, http.StatusNotFound, map[string]string{"error":"channel not found"})
		return
	}
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error":"invalid channel id"})
		return
	}

	channels, err := a.core.srt(r.Context())
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error":"core telemetry unavailable"})
		return
	}
	stats, ok := findPublisher(channels, c.StreamID)
	if !ok {
		writeJSON(w, http.StatusOK, liveTelemetry{
			ChannelID:c.ID, StreamID:c.StreamID, Status:"offline",
			ObservedAt:time.Now().UTC().Format(time.RFC3339),
		})
		return
	}

	writeJSON(w, http.StatusOK, liveTelemetry{
		ChannelID:c.ID,
		StreamID:c.StreamID,
		Status:"live",
		RTTMs:stats.RTTMs,
		BandwidthMbit:stats.BandwidthMbit,
		RecvBufferMs:stats.RecvBufferMs,
		SRTLatencyMs:stats.RecvTSBPDDelayMs,
		RecvUniquePackets:stats.RecvPktUnique,
		RecvLossPackets:stats.RecvLossPkt,
		RecvRetransPackets:stats.RecvRetransPkt,
		RecvDropPackets:stats.RecvDropPkt,
		RecvUniqueBytes:stats.RecvUniqueBytes,
		TimestampMs:stats.TimestampMs,
		ObservedAt:time.Now().UTC().Format(time.RFC3339),
	})
}

func (a *app) listDestinationRuntime(w http.ResponseWriter, r *http.Request) {
	channelID := strings.TrimSpace(r.URL.Query().Get("channel_id"))
	owner := strings.TrimSpace(r.URL.Query().Get("owner_id"))
	q := `SELECT id,channel_id,owner_id,name,platform,enabled,desired_state,status,core_process_id,
		output_bitrate_mbps,video_bitrate_kbps,audio_bitrate_kbps,fps,audio_status,audio_pps,
		reconnect_count,quality_status,quality_reasons,source_video_codec,output_video_codec,
		source_resolution,output_resolution,source_fps,output_fps,source_video_bitrate_kbps,
		source_audio_codec,output_audio_codec,source_audio_hz,output_audio_hz,
		source_audio_channels,output_audio_channels,last_error,last_status_at FROM destinations`
	args := []any{}
	if channelID != "" {
		id, err := strconv.ParseInt(channelID,10,64)
		if err != nil {
			writeJSON(w,http.StatusBadRequest,map[string]string{"error":"invalid channel_id"})
			return
		}
		q += " WHERE channel_id=$1"
		args = append(args,id)
	} else if owner != "" {
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

	out := []destinationRuntime{}
	for rows.Next() {
		var d destinationRuntime
		if err := rows.Scan(&d.ID,&d.ChannelID,&d.OwnerID,&d.Name,&d.Platform,&d.Enabled,&d.DesiredState,
			&d.Status,&d.CoreProcessID,&d.OutputBitrateMbps,&d.VideoBitrateKbps,&d.AudioBitrateKbps,
			&d.FPS,&d.AudioStatus,&d.AudioPPS,&d.ReconnectCount,&d.QualityStatus,&d.QualityReasons,
			&d.SourceVideoCodec,&d.OutputVideoCodec,&d.SourceResolution,&d.OutputResolution,
			&d.SourceFPS,&d.OutputFPS,&d.SourceVideoKbps,&d.SourceAudioCodec,&d.OutputAudioCodec,
			&d.SourceAudioHz,&d.OutputAudioHz,&d.SourceAudioChannels,&d.OutputAudioChannels,
			&d.LastError,&d.LastStatusAt); err != nil {
			serverError(w, err)
			return
		}
		out = append(out,d)
	}
	writeJSON(w,http.StatusOK,out)
}

func (a *app) updateDestinationRuntime(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"),10,64)
	if err != nil {
		writeJSON(w,http.StatusBadRequest,map[string]string{"error":"invalid destination id"})
		return
	}
	var in destinationRuntimeUpdate
	if err := decodeJSON(r,&in); err != nil {
		writeJSON(w,http.StatusBadRequest,map[string]string{"error":err.Error()})
		return
	}
	in.Status = strings.ToLower(strings.TrimSpace(in.Status))
	switch in.Status {
	case "idle","connecting","live","degraded","reconnecting","failed","disabled","unknown":
	default:
		writeJSON(w,http.StatusBadRequest,map[string]string{"error":"unsupported destination status"})
		return
	}
	if in.OutputBitrateMbps < 0 {
		writeJSON(w,http.StatusBadRequest,map[string]string{"error":"output_bitrate_mbps must be >= 0"})
		return
	}
	if in.AudioStatus == "" { in.AudioStatus = "unknown" }
	_, err = a.db.ExecContext(r.Context(), `
		UPDATE destinations
		SET status=$1,output_bitrate_mbps=$2,video_bitrate_kbps=$3,audio_bitrate_kbps=$4,
			fps=$5,audio_status=$6,audio_pps=$7,reconnect_count=$8,last_error=$9,
			last_status_at=now(),updated_at=now()
		WHERE id=$10`,
		in.Status,in.OutputBitrateMbps,in.VideoBitrateKbps,in.AudioBitrateKbps,in.FPS,
		in.AudioStatus,in.AudioPPS,in.ReconnectCount,strings.TrimSpace(in.LastError),id)
	if err != nil {
		serverError(w,err)
		return
	}
	writeJSON(w,http.StatusOK,map[string]any{"id":id,"status":in.Status})
}
