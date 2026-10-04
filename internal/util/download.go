package util

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/util/netsafe"
)

// maxDownloadBytes caps a remote file fetched by URL to keep a server-side
// download (extra-core binary, zapret archive, hosts text) from exhausting RAM.
const maxDownloadBytes = 256 << 20 // 256 MiB

// DownloadTo fetches url and writes it to dst with the given mode. The client
// requires HTTPS (a plaintext download of an executable is trivially
// intercepted), follows at most 10 redirects without allowing an https->http
// downgrade, rejects non-2xx responses and enforces a size cap. The SSRF guard
// refuses to connect to loopback/private addresses. The destination is written
// atomically via a temp file.
func DownloadTo(url, dst string, mode os.FileMode) error {
	if !strings.HasPrefix(url, "https://") {
		return fmt.Errorf("unsupported URL scheme: only https is allowed")
	}
	client := &http.Client{
		Timeout: 10 * time.Minute,
		Transport: &http.Transport{
			DialContext:           netsafe.SSRFGuardedDialContext,
			MaxIdleConns:          1,
			IdleConnTimeout:       30 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: time.Second,
		},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("stopped after 10 redirects")
			}
			if req.URL.Scheme != "https" {
				return fmt.Errorf("refusing https->http redirect to %s", req.URL.Host)
			}
			return nil
		},
	}
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
