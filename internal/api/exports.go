package api

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/dude2k/MultiSpeed/internal/database"
	"github.com/dude2k/MultiSpeed/internal/models"
)

func (s *Server) exportResultsCSV(w http.ResponseWriter, r *http.Request) {
	filter, err := parseResultFilter(r)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "INVALID_FILTER", err.Error())
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="multispeed-results.csv"`)
	writer := csv.NewWriter(w)
	_ = writer.Write([]string{"id", "task_id", "provider", "trigger", "status", "started_at", "finished_at", "download_bps", "upload_bps", "latency_ms", "jitter_ms", "packet_loss_percent", "interface", "source_ip", "public_ip", "server_id", "server_name", "error"})
	err = iterateResults(r.Context(), s.store, filter, func(item models.Result) error {
		row := []string{item.ID, item.TaskID, string(item.Provider), string(item.Trigger), string(item.Status), timeText(item.StartedAt), timeText(item.FinishedAt), int64Text(item.DownloadBitsPerSecond), int64Text(item.UploadBitsPerSecond), floatText(item.LatencyMilliseconds), floatText(item.JitterMilliseconds), floatText(item.PacketLossPercent), item.SelectedInterface, item.SelectedSourceIP, item.DetectedPublicIP, item.ServerID, item.ServerName, item.SanitizedError}
		for index := range row {
			row[index] = spreadsheetSafe(row[index])
		}
		return writer.Write(row)
	})
	writer.Flush()
	if writerErr := writer.Error(); err == nil {
		err = writerErr
	}
	if err != nil {
		s.logger.Error("CSV export failed", "request_id", requestIDFrom(r.Context()), "error", err)
	}
}
func (s *Server) exportResultsJSON(w http.ResponseWriter, r *http.Request) {
	filter, err := parseResultFilter(r)
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "INVALID_FILTER", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="multispeed-results.json"`)
	_, _ = w.Write([]byte("["))
	first := true
	encoder := json.NewEncoder(w)
	err = iterateResults(r.Context(), s.store, filter, func(item models.Result) error {
		if !first {
			_, _ = w.Write([]byte(","))
		}
		first = false
		return encoder.Encode(item)
	})
	_, _ = w.Write([]byte("]\n"))
	if err != nil {
		s.logger.Error("JSON export failed", "request_id", requestIDFrom(r.Context()), "error", err)
	}
}
func iterateResults(ctx context.Context, store *database.Store, filter database.ResultFilter, visit func(models.Result) error) error {
	return store.WalkResults(ctx, filter, visit)
}

func (s *Server) backup(w http.ResponseWriter, r *http.Request) {
	directory, err := os.MkdirTemp(filepath.Dir(s.store.Path()), ".multispeed-backup-")
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "BACKUP_FAILED", "A temporary backup directory could not be created.")
		return
	}
	defer func() { _ = os.RemoveAll(directory) }()
	destination := filepath.Join(directory, "multispeed-backup.db")
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	if err := s.store.Backup(ctx, destination); err != nil {
		s.logger.Error("SQLite backup failed", "request_id", requestIDFrom(r.Context()), "error", err)
		writeError(w, r, http.StatusInternalServerError, "BACKUP_FAILED", "The database backup could not be created.")
		return
	}
	file, err := os.Open(destination)
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "BACKUP_FAILED", "The completed backup could not be opened.")
		return
	}
	defer func() { _ = file.Close() }()
	info, err := file.Stat()
	if err != nil {
		writeError(w, r, http.StatusInternalServerError, "BACKUP_FAILED", "The completed backup could not be inspected.")
		return
	}
	w.Header().Set("Content-Disposition", `attachment; filename="multispeed-backup.db"`)
	w.Header().Set("Content-Type", "application/vnd.sqlite3")
	http.ServeContent(w, r, "multispeed-backup.db", info.ModTime(), file)
}

func timeText(value *time.Time) string {
	if value == nil {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}
func int64Text(value *int64) string {
	if value == nil {
		return ""
	}
	return strconv.FormatInt(*value, 10)
}
func floatText(value *float64) string {
	if value == nil {
		return ""
	}
	return strconv.FormatFloat(*value, 'f', -1, 64)
}

func spreadsheetSafe(value string) string {
	trimmed := strings.TrimLeft(value, " \t\r\n")
	if trimmed != "" && strings.ContainsRune("=+-@", rune(trimmed[0])) {
		return "'" + value
	}
	return value
}

var _ = fmt.Sprint
