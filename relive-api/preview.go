package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type previewInfo struct {
	ChannelID       int64   `json:"channel_id"`
	Status          string  `json:"status"`
	ProcessID       string  `json:"process_id,omitempty"`
	ManifestURL     string  `json:"manifest_url,omitempty"`
	VideoCodec      string  `json:"video_codec,omitempty"`
	Width           uint64  `json:"width,omitempty"`
	Height          uint64  `json:"height,omitempty"`
	FPS             float64 `json:"fps,omitempty"`
	VideoBitrateKbps float64 `json:"video_bitrate_kbps,omitempty"`
	AudioCodec      string  `json:"audio_codec,omitempty"`
	AudioBitrateKbps float64 `json:"audio_bitrate_kbps,omitempty"`
	AudioSampleHz   uint64  `json:"audio_sample_hz,omitempty"`
	AudioChannels   uint64  `json:"audio_channels,omitempty"`
	AudioPPS        float64 `json:"audio_pps,omitempty"`
	LastError       string  `json:"last_error,omitempty"`
}

func previewProcessReference(channelID int64) string {
	return fmt.Sprintf("relive-preview:%d",channelID)
}

func previewManifestName(channelID int64) string {
	return fmt.Sprintf("relive-preview-%d.m3u8",channelID)
}

func previewSegmentPattern(channelID int64) string {
	return fmt.Sprintf("{memfs}/relive-preview-%d_%%06d.ts",channelID)
}

func (a *app) ensurePreviewProcess(ctx context.Context, ch channel) (string,error) {
	var pid string
	err:=a.db.QueryRowContext(ctx,"SELECT COALESCE(preview_process_id,'') FROM channels WHERE id=$1",ch.ID).Scan(&pid)
	if err!=nil { return "",err }
	if pid!="" {
		stateCtx,cancel:=context.WithTimeout(ctx,4*time.Second)
		_,stateErr:=a.core.processState(stateCtx,pid)
		cancel()
		if stateErr==nil { return pid,nil }
		_,_=a.db.ExecContext(ctx,"UPDATE channels SET preview_process_id='' WHERE id=$1",ch.ID)
	}

	var source string
	if strings.EqualFold(ch.IngestProtocol,"rtmp") {
		source=internalRTMPSourceURL(
			env("RELIVE_INTERNAL_RTMP_HOST","restreamer"),
			envInt("RELIVE_INTERNAL_RTMP_PORT",1935),
			ch.IngestApp,ch.StreamKey,env("RELIVE_INTERNAL_RTMP_TOKEN",""),
		)
	} else {
		source=internalSRTSourceURL(
			env("RELIVE_INTERNAL_SRT_HOST","restreamer"),
			envInt("RELIVE_INTERNAL_SRT_PORT",6000),
			ch.StreamID,env("RELIVE_INTERNAL_SRT_TOKEN",""),
			env("RELIVE_INTERNAL_SRT_PASSPHRASE",""),
			envInt("RELIVE_INTERNAL_SRT_PBKEYLEN",16),
		)
	}

	cfg:=coreProcessConfig{
		Type:"ffmpeg",
		Reference:previewProcessReference(ch.ID),
		Input:[]coreProcessIO{{Address:source}},
		Output:[]coreProcessIO{{
			Address:"{memfs}/"+previewManifestName(ch.ID),
			Options:[]string{
				"-dn","-sn",
				"-map","0:v:0","-map","0:a:0?",
				"-c:v","copy","-c:a","copy",
				"-f","hls",
				"-hls_time","1",
				"-hls_list_size","5",
				"-hls_flags","delete_segments+omit_endlist+independent_segments",
				"-hls_segment_filename",previewSegmentPattern(ch.ID),
				"-method","PUT","-y",
			},
		}},
		Reconnect:true,ReconnectDelay:2,Autostart:true,StaleTimeout:20,
	}
	createCtx,cancel:=context.WithTimeout(ctx,7*time.Second)
	created,err:=a.core.createProcess(createCtx,cfg)
	cancel()
	if err!=nil { return "",err }
	if created.ID=="" { return "","Core created preview without process ID" }
	_,err=a.db.ExecContext(ctx,"UPDATE channels SET preview_process_id=$1,updated_at=now() WHERE id=$2",created.ID,ch.ID)
	if err!=nil {
		cleanup,cancel:=context.WithTimeout(ctx,4*time.Second)
		_ = a.core.deleteProcess(cleanup,created.ID)
		cancel()
		return "",err
	}
	return created.ID,nil
}

func (a *app) channelPreview(w http.ResponseWriter,r *http.Request) {
	u,ok:=currentUser(r.Context())
	if !ok { writeJSON(w,http.StatusUnauthorized,map[string]string{"error":"unauthorized"});return }
	id,err:=strconv.ParseInt(r.PathValue("id"),10,64)
	if err!=nil { writeJSON(w,http.StatusBadRequest,map[string]string{"error":"invalid channel id"});return }
	if !a.canAccessChannel(r.Context(),u,id) { writeJSON(w,http.StatusForbidden,map[string]string{"error":"forbidden"});return }
	ch,err:=a.channelByID(r.Context(),r.PathValue("id"))
	if errors.Is(err,sql.ErrNoRows) { writeJSON(w,http.StatusNotFound,map[string]string{"error":"channel not found"});return }
	if err!=nil { serverError(w,err);return }

	pid,err:=a.ensurePreviewProcess(r.Context(),ch)
	if err!=nil {
		writeJSON(w,http.StatusOK,previewInfo{ChannelID:ch.ID,Status:"offline",LastError:sanitizePreviewError(err.Error(),ch)})
		return
	}
	stateCtx,cancel:=context.WithTimeout(r.Context(),5*time.Second)
	state,err:=a.core.processState(stateCtx,pid)
	cancel()
	if err!=nil {
		writeJSON(w,http.StatusOK,previewInfo{ChannelID:ch.ID,Status:"reconnecting",ProcessID:pid,LastError:sanitizePreviewError(err.Error(),ch)})
		return
	}

	out:=previewInfo{
		ChannelID:ch.ID,ProcessID:pid,Status:strings.ToLower(state.Exec),
		ManifestURL:"/preview/"+previewManifestName(ch.ID),
		LastError:sanitizePreviewError(state.LastLog,ch),
	}
	if v,ok:=mediaPair(state.Progress.Input,"video");ok {
		out.VideoCodec=v.Codec;out.Width=v.Width;out.Height=v.Height;out.FPS=v.FPS;out.VideoBitrateKbps=v.Bitrate
	}
	if audio,ok:=mediaPair(state.Progress.Input,"audio");ok {
		out.AudioCodec=audio.Codec;out.AudioBitrateKbps=audio.Bitrate;out.AudioSampleHz=audio.Sampling;out.AudioChannels=audio.Channels;out.AudioPPS=audio.PPS
	}
	if strings.EqualFold(state.Exec,"running") { out.Status="live" }
	writeJSON(w,http.StatusOK,out)
}

func (a *app) stopChannelPreview(w http.ResponseWriter,r *http.Request) {
	u,ok:=currentUser(r.Context())
	if !ok { writeJSON(w,http.StatusUnauthorized,map[string]string{"error":"unauthorized"});return }
	id,err:=strconv.ParseInt(r.PathValue("id"),10,64)
	if err!=nil { writeJSON(w,http.StatusBadRequest,map[string]string{"error":"invalid channel id"});return }
	if !a.canAccessChannel(r.Context(),u,id) { writeJSON(w,http.StatusForbidden,map[string]string{"error":"forbidden"});return }
	var pid string
	err=a.db.QueryRowContext(r.Context(),"SELECT COALESCE(preview_process_id,'') FROM channels WHERE id=$1",id).Scan(&pid)
	if err!=nil { serverError(w,err);return }
	if pid!="" {
		ctx,cancel:=context.WithTimeout(r.Context(),5*time.Second)
		_ = a.core.deleteProcess(ctx,pid)
		cancel()
	}
	_,_=a.db.ExecContext(r.Context(),"UPDATE channels SET preview_process_id='',updated_at=now() WHERE id=$1",id)
	writeJSON(w,http.StatusOK,map[string]bool{"ok":true})
}

func sanitizePreviewError(msg string,ch channel) string {
	msg=strings.TrimSpace(msg)
	if ch.StreamKey!="" { msg=strings.ReplaceAll(msg,ch.StreamKey,"***") }
	if len(msg)>600 { msg=msg[:600] }
	return msg
}

func envInt(key string,fallback int) int {
	raw:=strings.TrimSpace(env(key,""))
	if raw=="" { return fallback }
	n,err:=strconv.Atoi(raw)
	if err!=nil { return fallback }
	return n
}
