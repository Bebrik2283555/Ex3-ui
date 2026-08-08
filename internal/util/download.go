package util

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// maxDownloadBytes caps a remote file fetched by URL to keep a server-side
// download (extra-core binary, zapret archive, hosts text) from exhausting RAM.
const maxDownloadBytes = 256 << 20 // 256 MiB

// DownloadTo fetches url and writes it to dst with the given mode. The client
// follows redirects (Google Drive / CDN links), rejects non-2xx responses and
// enforces a size cap. The destination is written atomically via a temp file.
func DownloadTo(url, dst string, mode os.FileMode) error {
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return fmt.Errorf("unsupported URL scheme: only http/https are allowed")
	}
	client := &http.Client{Timeout: 10 * time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed: HTTP %d", resp.StatusCode)
	}

	tmp := dst + ".download"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, io.LimitReader(resp.Body, maxDownloadBytes+1)); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	size, err := os.Stat(tmp)
	if err != nil {
		os.Remove(tmp)
		return err
	}
	if size.Size() > maxDownloadBytes {
		os.Remove(tmp)
		return fmt.Errorf("file exceeds the %d MiB download cap", maxDownloadBytes>>20)
	}
	if err := os.Chmod(tmp, mode); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}
