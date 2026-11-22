package webapp

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const maxConvertUpload = 50 << 20

func serveConverterHTML(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/converter" {
		http.NotFound(w, r)
		return
	}
	content, err := staticFS.ReadFile("static/converter.html")
	if err != nil {
		http.Error(w, "converter page unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(content)
}

func convertOwnedMediaHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxConvertUpload)
	if err := r.ParseMultipartForm(maxConvertUpload); err != nil {
		http.Error(w, "File too large or invalid upload (maximum 50 MB).", http.StatusRequestEntityTooLarge)
		return
	}
	src, header, err := r.FormFile("media")
	if err != nil {
		http.Error(w, "Choose a video or audio file first.", http.StatusBadRequest)
		return
	}
	defer src.Close()

	ext := strings.ToLower(filepath.Ext(filepath.Base(header.Filename)))
	switch ext {
	case ".mp4", ".m4v", ".mov", ".mkv", ".webm", ".avi", ".mp3", ".wav", ".m4a", ".ogg", ".flac":
	default:
		http.Error(w, "Unsupported media format.", http.StatusBadRequest)
		return
	}

	input, err := os.CreateTemp("", "mrperfect-media-*"+ext)
	if err != nil {
		http.Error(w, "Could not prepare conversion.", http.StatusInternalServerError)
		return
	}
	inputPath := input.Name()
	defer os.Remove(inputPath)
	if _, err = io.Copy(input, src); err != nil {
		input.Close()
		http.Error(w, "Upload failed.", http.StatusBadRequest)
		return
	}
	if err = input.Close(); err != nil {
		http.Error(w, "Upload failed.", http.StatusInternalServerError)
		return
	}

	output, err := os.CreateTemp("", "mrperfect-audio-*.mp3")
	if err != nil {
		http.Error(w, "Could not prepare MP3.", http.StatusInternalServerError)
		return
	}
	outputPath := output.Name()
	output.Close()
	defer os.Remove(outputPath)

	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "ffmpeg", "-nostdin", "-v", "error", "-y", "-i", inputPath, "-vn", "-codec:a", "libmp3lame", "-q:a", "2", outputPath)
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			http.Error(w, "Conversion timed out.", http.StatusGatewayTimeout)
		} else {
			http.Error(w, "This file could not be converted. Check that it contains playable audio.", http.StatusUnprocessableEntity)
		}
		return
	}

	file, err := os.Open(outputPath)
	if err != nil {
		http.Error(w, "MP3 output unavailable.", http.StatusInternalServerError)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		http.Error(w, "Could not read MP3 output.", http.StatusInternalServerError)
		return
	}
	base := strings.TrimSuffix(filepath.Base(header.Filename), filepath.Ext(header.Filename))
	base = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, base)
	if base == "" {
		base = "audio"
	}
	w.Header().Set("Content-Type", "audio/mpeg")
	w.Header().Set("Content-Length", fmt.Sprint(info.Size()))
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", base+".mp3"))
	http.ServeContent(w, r, base+".mp3", info.ModTime(), file)
}
