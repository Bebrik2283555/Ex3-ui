package controller

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"syscall"
	"testing"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/extra"
	"github.com/mhsanaei/3x-ui/v3/internal/web/locale"

	"github.com/gin-gonic/gin"
)

// TestExtraUploadHelperProcess is the long-lived child the upload test spawns
// from bin/extra-qwdtt so the file is mapped as executable text on Linux and
// a direct overwrite fails with ETXTBSY ("text file busy").
func TestExtraUploadHelperProcess(t *testing.T) {
	if os.Getenv("GO_WANT_EXTRA_UPLOAD_HELPER") != "1" {
		return
	}
	time.Sleep(30 * time.Second)
}

// TestUploadBinaryReplacesRunningCore guards the ETXTBSY bug: uploading a new
// binary while the core is running used to open bin/extra-<name> for write
// (SaveUploadedFile -> os.Create), which the kernel rejects while the file is
// executing. The upload must stage a temp file and rename it over the target.
func TestUploadBinaryReplacesRunningCore(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("ETXTBSY is a Linux/Unix behavior; replacing an executing file via rename is not possible on Windows")
	}
	dir := t.TempDir()
	t.Setenv("XUI_BIN_FOLDER", dir)

	dst := extra.WDTT.DefaultBinaryPath()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, data, 0o755); err != nil {
		t.Fatal(err)
	}

	// Start the copied test binary so dst is executing.
	child := exec.Command(dst, "-test.run=TestExtraUploadHelperProcess")
	child.Env = append(os.Environ(), "GO_WANT_EXTRA_UPLOAD_HELPER=1")
	if err := child.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = child.Process.Kill()
		_ = child.Wait()
	}()
	time.Sleep(500 * time.Millisecond)

	payload := []byte("#!/bin/sh\necho new-core-payload\n")
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)
	part, err := writer.CreateFormFile("file", "extra-qwdtt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/panel/api/extra/services/qwdtt/upload", body)
	c.Request.Header.Set("Content-Type", writer.FormDataContentType())
	c.Params = gin.Params{{Key: "name", Value: "qwdtt"}}
	c.Set("I18n", func(locale.I18nType, string, ...string) string { return "" })

	(&ExtraController{}).uploadBinary(c)

	if rec.Code != http.StatusOK {
		t.Fatalf("upload HTTP status = %d, want 200", rec.Code)
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"success":true`)) {
		t.Fatalf("upload failed: %s", rec.Body.String())
	}

	// The new payload must be on disk and no .upload temp file may remain.
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read replaced binary: %v", err)
	}
	if !bytes.Contains(got, []byte("new-core-payload")) {
		t.Errorf("binary not replaced, got: %s", got)
	}
	if _, err := os.Stat(dst + ".upload"); !os.IsNotExist(err) {
		t.Errorf("temp upload file %q left behind", dst+".upload")
	}
	// The old process must survive the swap: it still runs from the old inode.
	if err := child.Process.Signal(syscall.Signal(0)); err != nil {
		t.Errorf("running core process died during binary swap: %v", err)
	}
}
