package job

import (
	"io"
	"os"
	"path/filepath"

	"github.com/mhsanaei/3x-ui/v3/internal/logger"
	"github.com/mhsanaei/3x-ui/v3/internal/xray"
)

const defaultMaxXrayLogBytes int64 = 64 << 20

var maxXrayLogBytes = defaultMaxXrayLogBytes

// ClearLogsJob clears old log files to prevent disk space issues.
type ClearLogsJob struct{}

// PruneXrayLogsJob truncates oversized Xray access and error logs.
// PruneXrayLogsJob truncates the Xray access and error logs once either exceeds maxXrayLogBytes.
type PruneXrayLogsJob struct{}

// NewClearLogsJob creates a new log cleanup job instance.
func NewClearLogsJob() *ClearLogsJob {
	return new(ClearLogsJob)
}

// NewPruneXrayLogsJob creates a new Xray log pruning job instance.
func NewPruneXrayLogsJob() *PruneXrayLogsJob {
	return new(PruneXrayLogsJob)
}

// ensureFileExists creates the necessary directories and file if they don't exist
func ensureFileExists(path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return err
	}
	file.Close()
	return nil
}

// Here Run is an interface method of the Job interface
func (j *ClearLogsJob) Run() {
	logFiles := []string{xray.GetIPLimitLogPath(), xray.GetIPLimitBannedLogPath()}
	logFilesPrev := []string{xray.GetIPLimitBannedPrevLogPath()}

	// Ensure all log files and their paths exist
	for _, path := range append(logFiles, logFilesPrev...) {
		if err := ensureFileExists(path); err != nil {
			logger.Warning("Failed to ensure log file exists:", path, "-", err)
		}
	}

	// Clear log files and copy to previous logs. Each current log is opened
	// once and truncated through that same fd — reopening the path between
	// copy and truncate would hit whatever file sits there by then (TOCTOU).
	for i := range len(logFiles) {
		logFile, err := os.OpenFile(logFiles[i], os.O_RDWR, 0o644)
		if err != nil {
			logger.Warning("Failed to open current log file:", logFiles[i], "-", err)
			continue
		}

		if i > 0 {
			// Copy to previous logs
			logFilePrev, err := os.OpenFile(logFilesPrev[i-1], os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
			if err != nil {
				logger.Warning("Failed to open previous log file for writing:", logFilesPrev[i-1], "-", err)
				logFile.Close()
				continue
			}

			if _, err := logFile.Seek(0, io.SeekStart); err != nil {
				logger.Warning("Failed to seek log file:", logFiles[i], "-", err)
			} else {
				_, err = io.Copy(logFilePrev, logFile)
				if err != nil {
					logger.Warning("Failed to copy log file:", logFiles[i], "to", logFilesPrev[i-1], "-", err)
				}
			}

			logFilePrev.Close()
		}

		if err := logFile.Truncate(0); err != nil {
			logger.Warning("Failed to truncate log file:", logFiles[i], "-", err)
		}
		logFile.Close()
	}

	wipeXrayLogs()
}

func (j *PruneXrayLogsJob) Run() {
	truncateXrayLog(xray.GetAccessLogPath, maxXrayLogBytes)
	truncateXrayLog(xray.GetErrorLogPath, maxXrayLogBytes)
}

func wipeXrayLogs() {
	truncateXrayLog(xray.GetAccessLogPath, 0)
	truncateXrayLog(xray.GetErrorLogPath, 0)
}

func truncateXrayLog(pathFn func() (string, error), maxBytes int64) {
	logPath, err := pathFn()
	if err != nil || disabledLogPath(logPath) {
		return
	}
	if maxBytes > 0 {
		info, err := os.Stat(logPath)
		if err != nil {
			if !os.IsNotExist(err) {
				logger.Warning("Failed to stat Xray log:", logPath, "-", err)
			}
			return
		}
		if info.Size() <= maxBytes {
			return
		}
	}
	if err := os.Truncate(logPath, 0); err != nil && !os.IsNotExist(err) {
		logger.Warning("Failed to truncate Xray log:", logPath, "-", err)
	}
}

func disabledLogPath(path string) bool {
	return path == "" || path == "none"
}
