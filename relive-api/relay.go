package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type relayManager struct {
	app      *app
	interval time.Duration
	srtHost  string
	srtPort  int
	srtToken string
	srtPassphrase string
	srtPBKeyLen int
}

type relayDestination struct {
	ID              int64
	ChannelID       int64
	OwnerID         string
	Name            string
	Platform        string
	KeySource       string
	ServerURL       string
	StreamKey       string
	GeneratorRef    string
	Enabled         bool
	DesiredState    string
	Status          string
	CoreProcessID   string
	StreamID        string
	CurrentAudio    string
	LastError       string
	ReconnectCount  int
}

type destinationCommand struct {
	Command string `json:"command"`
}

func newRelayManager(a *app) *relayManager {
	port, err := strconv.Atoi(env("RELIVE_INTERNAL_SRT_PORT", "6000"))
	if err != nil || port < 1 || port > 65535 {
		port = 6000
	}
	pbkeylen, err := strconv.Atoi(env("RELIVE_INTERNAL_SRT_PBKEYLEN", "16"))
	if err != nil || (pbkeylen != 16 && pbkeylen != 24 && pbkeylen != 32) {
		pbkeylen = 16
	}
	return &relayManager{
		app:           a,
		interval:      envDuration("RELIVE_RELAY_POLL_INTERVAL", 2*time.Second),
		srtHost:       env("RELIVE_INTERNAL_SRT_HOST", "restreamer"),
		srtPort:       port,
		srtToken:      env("RELIVE_INTERNAL_SRT_TOKEN", ""),
		srtPassphrase: env("RELIVE_INTERNAL_SRT_PASSPHRASE", ""),
		srtPBKeyLen:   pbkeylen,
	}
}

func (m *relayManager) run(ctx context.Context) {
	m.reconcile(ctx)
	t := time.NewTicker(m.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			m.reconcile(ctx)
		}
	}
}

func (m *relayManager) reconcile(ctx context.Context) {
	rows, err := m.app.db.QueryContext(ctx, `
		SELECT d.id,d.channel_id,d.owner_id,d.name,d.platform,d.key_source,d.server_url,d.stream_key,
			d.generator_ref,d.enabled,d.desired_state,d.status,d.core_process_id,c.stream_id,
			d.audio_status,d.last_error,d.reconnect_count
		FROM destinations d
		JOIN channels c ON c.id=d.channel_id
		WHERE d.enabled=true AND d.channel_id IS NOT NULL
		ORDER BY d.id`)
	if err != nil {
		log.Printf("relay manager query failed: %v", err)
		return
	}
	defer rows.Close()

	var all []relayDestination
	for rows.Next() {
		var d relayDestination
		if err := rows.Scan(&d.ID,&d.ChannelID,&d.OwnerID,&d.Name,&d.Platform,&d.KeySource,&d.ServerURL,
			&d.StreamKey,&d.GeneratorRef,&d.Enabled,&d.DesiredState,&d.Status,&d.CoreProcessID,
			&d.StreamID,&d.CurrentAudio,&d.LastError,&d.ReconnectCount); err != nil {
			log.Printf("relay manager scan failed: %v", err)
			return
		}
		all = append(all,d)
	}

	for _, d := range all {
		if err := m.reconcileDestination(ctx,d); err != nil {
			log.Printf("relay destination %d (%s): %v", d.ID, d.Name, err)
		}
	}
}

func (m *relayManager) reconcileDestination(ctx context.Context, d relayDestination) error {
	if d.DesiredState != "running" {
		if d.CoreProcessID != "" {
			stopCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			_ = m.app.core.deleteProcess(stopCtx,d.CoreProcessID)
			cancel()
		}
		return m.updateRuntime(ctx,d,"idle","",0,0,0,0,"unknown",0,"")
	}

	if d.KeySource == "external_generator" {
		return m.updateRuntime(ctx,d,"failed","",0,0,0,0,"unknown",d.ReconnectCount,
			"external TikTok generator activation is not implemented yet")
	}

	if d.CoreProcessID == "" {
		if strings.TrimSpace(d.ServerURL) == "" {
			return m.updateRuntime(ctx,d,"failed","",0,0,0,0,"unknown",d.ReconnectCount,"destination server URL is empty")
		}

		source := internalSRTSourceURL(m.srtHost,m.srtPort,d.StreamID,m.srtToken,m.srtPassphrase,m.srtPBKeyLen)
		output := destinationOutputURL(d.ServerURL,d.StreamKey)
		cfg := coreProcessConfig{
			Type:"ffmpeg",
			Reference:fmt.Sprintf("relive-destination:%d",d.ID),
			Input:[]coreProcessIO{{Address:source}},
			Output:[]coreProcessIO{{Address:output,Options:passThroughOptions(d.Platform)}},
			Reconnect:true,
			ReconnectDelay:2,
			Autostart:true,
		}

		createCtx,cancel := context.WithTimeout(ctx,7*time.Second)
		created,err := m.app.core.createProcess(createCtx,cfg)
		cancel()
		if err != nil {
			msg := sanitizeRelayError(err.Error(),d)
			return m.updateRuntime(ctx,d,"failed","",0,0,0,0,"unknown",d.ReconnectCount,msg)
		}
		if created.ID == "" {
			return m.updateRuntime(ctx,d,"failed","",0,0,0,0,"unknown",d.ReconnectCount,"Core created relay without process ID")
		}
		return m.updateRuntime(ctx,d,"connecting",created.ID,0,0,0,0,"unknown",d.ReconnectCount,"")
	}

	stateCtx,cancel := context.WithTimeout(ctx,5*time.Second)
	state,err := m.app.core.processState(stateCtx,d.CoreProcessID)
	cancel()
	if err != nil {
		msg := sanitizeRelayError(err.Error(),d)
		if strings.Contains(msg,"404") || strings.Contains(strings.ToLower(msg),"unknown process") {
			return m.updateRuntime(ctx,d,"reconnecting","",0,0,0,0,"unknown",d.ReconnectCount+1,"relay process disappeared; recreating")
		}
		return m.updateRuntime(ctx,d,"degraded",d.CoreProcessID,0,0,0,0,"unknown",d.ReconnectCount,msg)
	}

	videoK,audioK,fps,audioPPS,audioStatus := mediaFromState(state)
	status := "unknown"
	lastError := ""
	reconnectCount := d.ReconnectCount

	switch strings.ToLower(state.Exec) {
	case "starting":
		status = "connecting"
	case "running":
		if state.Reconnect > 0 {
			status = "reconnecting"
			if d.Status != "reconnecting" {
				reconnectCount++
			}
		} else if audioStatus == "missing" {
			status = "degraded"
			lastError = "audio input detected but no audio packets are reaching output"
		} else {
			status = "live"
		}
	case "finishing":
		status = "reconnecting"
	case "failed":
		status = "failed"
		lastError = state.LastLog
	case "finished","killed":
		status = "reconnecting"
		lastError = state.LastLog
	default:
		status = "unknown"
	}

	if lastError == "" && (status == "degraded" || status == "reconnecting" || status == "failed") {
		lastError = state.LastLog
	}
	lastError = sanitizeRelayError(lastError,d)

	return m.updateRuntime(ctx,d,status,d.CoreProcessID,(videoK+audioK)/1000.0,videoK,audioK,fps,audioStatus,reconnectCount,lastError,
		audioPPS)
}

func (m *relayManager) updateRuntime(ctx context.Context, d relayDestination, status, processID string,
	outputMbps,videoKbps,audioKbps,fps float64,audioStatus string,reconnects int,lastError string, extra ...float64) error {
	audioPPS := 0.0
	if len(extra)>0 { audioPPS=extra[0] }

	statusChanged := d.Status != status
	audioChanged := d.CurrentAudio != audioStatus

	_,err := m.app.db.ExecContext(ctx,`
		UPDATE destinations SET
			status=$1,core_process_id=$2,output_bitrate_mbps=$3,video_bitrate_kbps=$4,
			audio_bitrate_kbps=$5,fps=$6,audio_status=$7,audio_pps=$8,reconnect_count=$9,
			last_error=$10,last_status_at=now(),updated_at=now()
		WHERE id=$11`,
		status,processID,outputMbps,videoKbps,audioKbps,fps,audioStatus,audioPPS,reconnects,lastError,d.ID)
	if err != nil { return err }

	if statusChanged {
		severity := "info"
		if status=="degraded" || status=="reconnecting" { severity="warning" }
		if status=="failed" { severity="critical" }
		message := d.Name+" "+strings.ToUpper(status)
		if lastError!="" { message += ": "+lastError }
		_ = m.app.recordIncident(ctx,d.ChannelID,d.ID,severity,"destination_status",message)
	}
	if audioChanged && audioStatus=="missing" {
		_ = m.app.recordIncident(ctx,d.ChannelID,d.ID,"critical","audio_missing",d.Name+": audio packets missing")
	}
	if audioChanged && d.CurrentAudio=="missing" && audioStatus=="healthy" {
		_ = m.app.recordIncident(ctx,d.ChannelID,d.ID,"info","audio_recovered",d.Name+": audio recovered")
	}
	return nil
}

func sanitizeRelayError(msg string, d relayDestination) string {
	msg = strings.TrimSpace(msg)
	if d.StreamKey!="" {
		msg = strings.ReplaceAll(msg,d.StreamKey,"***")
	}
	full := destinationOutputURL(d.ServerURL,d.StreamKey)
	if full!="" && full!=d.ServerURL {
		msg = strings.ReplaceAll(msg,full,d.ServerURL+"/***")
	}
	if len(msg)>1000 { msg=msg[:1000] }
	return msg
}

func (a *app) destinationCommand(w http.ResponseWriter, r *http.Request) {
	id,err := strconv.ParseInt(r.PathValue("id"),10,64)
	if err!=nil {
		writeJSON(w,http.StatusBadRequest,map[string]string{"error":"invalid destination id"})
		return
	}
	var in destinationCommand
	if err:=decodeJSON(r,&in);err!=nil {
		writeJSON(w,http.StatusBadRequest,map[string]string{"error":err.Error()})
		return
	}
	in.Command=strings.ToLower(strings.TrimSpace(in.Command))

	var processID string
	err=a.db.QueryRowContext(r.Context(),`SELECT core_process_id FROM destinations WHERE id=$1`,id).Scan(&processID)
	if errors.Is(err,sql.ErrNoRows) {
		writeJSON(w,http.StatusNotFound,map[string]string{"error":"destination not found"})
		return
	}
	if err!=nil { serverError(w,err);return }

	switch in.Command {
	case "start":
		_,err=a.db.ExecContext(r.Context(),`UPDATE destinations SET desired_state='running',status='connecting',last_error='',updated_at=now() WHERE id=$1`,id)
	case "stop":
		_,err=a.db.ExecContext(r.Context(),`UPDATE destinations SET desired_state='stopped',updated_at=now() WHERE id=$1`,id)
	case "restart":
		_,err=a.db.ExecContext(r.Context(),`UPDATE destinations SET desired_state='running',status='reconnecting',updated_at=now() WHERE id=$1`,id)
		if err==nil && processID!="" {
			cmdCtx,cancel:=context.WithTimeout(r.Context(),5*time.Second)
			cmdErr:=a.core.processCommand(cmdCtx,processID,"restart")
			cancel()
			if cmdErr!=nil {
				_,_ = a.db.ExecContext(r.Context(),`UPDATE destinations SET core_process_id='',last_error=$1 WHERE id=$2`,sanitizeRelayError(cmdErr.Error(),relayDestination{ID:id}),id)
			}
		}
	default:
		writeJSON(w,http.StatusBadRequest,map[string]string{"error":"command must be start, stop or restart"})
		return
	}
	if err!=nil { serverError(w,err);return }
	writeJSON(w,http.StatusAccepted,map[string]any{"id":id,"command":in.Command})
}

type incident struct {
	ID            int64      `json:"id"`
	ChannelID     *int64     `json:"channel_id,omitempty"`
	DestinationID *int64     `json:"destination_id,omitempty"`
	Severity      string     `json:"severity"`
	Code          string     `json:"code"`
	Message       string     `json:"message"`
	CreatedAt     time.Time  `json:"created_at"`
}

func (a *app) recordIncident(ctx context.Context, channelID,destinationID int64,severity,code,message string) error {
	var destination any
	if destinationID > 0 {
		destination = destinationID
	}
	_,err:=a.db.ExecContext(ctx,`
		INSERT INTO incident_events(channel_id,destination_id,severity,code,message)
		VALUES($1,$2,$3,$4,$5)`,channelID,destination,severity,code,message)
	return err
}

func (a *app) listIncidents(w http.ResponseWriter,r *http.Request) {
	channelID,err:=strconv.ParseInt(r.URL.Query().Get("channel_id"),10,64)
	if err!=nil || channelID<=0 {
		writeJSON(w,http.StatusBadRequest,map[string]string{"error":"channel_id is required"})
		return
	}
	limit:=100
	if raw:=r.URL.Query().Get("limit");raw!="" {
		if n,e:=strconv.Atoi(raw);e==nil && n>0 && n<=500 { limit=n }
	}
	rows,err:=a.db.QueryContext(r.Context(),`
		SELECT id,channel_id,destination_id,severity,code,message,created_at
		FROM incident_events WHERE channel_id=$1 ORDER BY created_at DESC LIMIT $2`,channelID,limit)
	if err!=nil { serverError(w,err);return }
	defer rows.Close()
	out:=[]incident{}
	for rows.Next() {
		var x incident
		if err:=rows.Scan(&x.ID,&x.ChannelID,&x.DestinationID,&x.Severity,&x.Code,&x.Message,&x.CreatedAt);err!=nil {
			serverError(w,err);return
		}
		out=append(out,x)
	}
	writeJSON(w,http.StatusOK,out)
}

func jsonBytes(v any) []byte {
	b,_:=json.Marshal(v)
	return b
}
