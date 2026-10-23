package service

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/config"
)

// certRoot is where ACME and panel certificates live on a Linux host. XHTTP and
// TLS inbounds reference files below it, so a database-only backup restores a
// panel that cannot serve those inbounds. The full backup carries them along.
const certRoot = "/root/cert"

const (
	fullBackupVersion  = 1
	maxCertBackupBytes = 64 << 20
	maxCertFileBytes   = 10 << 20
)

type fullBackupManifest struct {
	Version int      `json:"version"`
	Created string   `json:"created"`
	DBFile  string   `json:"dbFile"`
	Certs   []string `json:"certs"`
}

// FullBackup builds a ZIP with the database, every file under /root/cert and a
// manifest. The plain .db export stays available for older panels.
func (s *ServerService) FullBackup(requestHost string) ([]byte, string, error) {
	db, err := s.GetDb()
	if err != nil {
		return nil, "", err
	}
	dbName := "backup.db"
	if strings.HasSuffix(s.BackupFilename(requestHost), ".dump") {
		dbName = "backup.dump"
	}
	manifest := fullBackupManifest{
		Version: fullBackupVersion,
		Created: time.Now().Format(time.RFC3339),
		DBFile:  "db/" + dbName,
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	if err := addZipFile(zw, manifest.DBFile, db); err != nil {
		_ = zw.Close()
		return nil, "", err
	}

	if runtime.GOOS != "windows" {
		if err := appendCertDir(zw, &manifest); err != nil {
			_ = zw.Close()
			return nil, "", err
		}
	}

	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		_ = zw.Close()
		return nil, "", err
	}
	if err := addZipFile(zw, "manifest.json", manifestBytes); err != nil {
		_ = zw.Close()
		return nil, "", err
	}
	if err := zw.Close(); err != nil {
		return nil, "", err
	}

	name := strings.TrimSuffix(s.BackupFilename(requestHost), filepath.Ext(s.BackupFilename(requestHost))) + ".zip"
	return buf.Bytes(), name, nil
}

func addZipFile(zw *zip.Writer, name string, data []byte) error {
	w, err := zw.Create(name)
	if err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}

func appendCertDir(zw *zip.Writer, manifest *fullBackupManifest) error {
	return appendCertDirAt(zw, manifest, certRoot)
}

func appendCertDirAt(zw *zip.Writer, manifest *fullBackupManifest, root string) error {
	info, err := os.Stat(root)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("full backup: inspect %s: %w", root, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("full backup: %s is not a directory", root)
	}
	var total int64
	return filepath.Walk(root, func(path string, fi os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("full backup: walk %s: %w", path, walkErr)
		}
		if fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 {
			return nil
		}
		if !fi.Mode().IsRegular() {
			return nil
		}
		if fi.Size() > maxCertFileBytes {
			return fmt.Errorf("full backup: %s is %d bytes, above the %d-byte certificate limit",
				path, fi.Size(), maxCertFileBytes)
		}
		if total+fi.Size() > maxCertBackupBytes {
			return fmt.Errorf("full backup: certificates exceed the %d-byte archive limit at %s",
				maxCertBackupBytes, path)
		}
		rel, err := filepath.Rel(root, path)
		if err != nil || strings.HasPrefix(rel, "..") {
			return fmt.Errorf("full backup: keep %s inside %s", path, root)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("full backup: read %s: %w", path, err)
		}
		name := "cert/" + filepath.ToSlash(rel)
		if err := addZipFile(zw, name, data); err != nil {
			return err
		}
		manifest.Certs = append(manifest.Certs, filepath.ToSlash(rel))
		total += fi.Size()
		return nil
	})
}

// ImportBackup accepts either a plain DB/dump upload or the ZIP produced by
// FullBackup. Certificates are restored before the database so inbound paths
// already point at files that exist.
func (s *ServerService) ImportBackup(file multipart.File, keepHostSettings bool) error {
	head := make([]byte, 4)
	n, err := io.ReadFull(file, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return err
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if n < 4 || !bytes.Equal(head, []byte("PK\x03\x04")) {
		return s.ImportDB(file, keepHostSettings)
	}

	raw, err := io.ReadAll(file)
	if err != nil {
		return err
	}
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return fmt.Errorf("invalid backup archive: %w", err)
	}

	var manifest fullBackupManifest
	dbName := ""
	for _, f := range zr.File {
		if f.Name == "manifest.json" {
			rc, err := f.Open()
			if err == nil {
				_ = json.NewDecoder(rc).Decode(&manifest)
				_ = rc.Close()
			}
		}
		if dbName == "" && strings.HasPrefix(f.Name, "db/") && !f.FileInfo().IsDir() {
			dbName = f.Name
		}
	}
	if dbName == "" {
		return fmt.Errorf("backup archive has no database")
	}

	if runtime.GOOS != "windows" && len(manifest.Certs) > 0 {
		if err := restoreCerts(zr, manifest.Certs); err != nil {
			return err
		}
	}

	tmp, err := os.CreateTemp(filepath.Dir(config.GetDBPath()), ".restore-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	defer tmp.Close()

	for _, f := range zr.File {
		if f.Name != dbName {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		_, err = io.Copy(tmp, rc)
		_ = rc.Close()
		if err != nil {
			return err
		}
		break
	}
	if _, err := tmp.Seek(0, io.SeekStart); err != nil {
		return err
	}
	return s.ImportDB(tmp, keepHostSettings)
}

func restoreCerts(zr *zip.Reader, wanted []string) error {
	allow := make(map[string]bool, len(wanted))
	for _, name := range wanted {
		allow[name] = true
	}
	found := make(map[string]bool, len(wanted))
	var total int64
	for _, f := range zr.File {
		if !strings.HasPrefix(f.Name, "cert/") || f.FileInfo().IsDir() {
			continue
		}
		rel := strings.TrimPrefix(f.Name, "cert/")
		if rel == "" || strings.Contains(rel, "..") || filepath.IsAbs(rel) {
			continue
		}
		if len(allow) > 0 && !allow[rel] {
			continue
		}
		if f.UncompressedSize64 > maxCertFileBytes {
			return fmt.Errorf("backup archive: certificate %s is %d bytes, above the %d-byte limit",
				rel, f.UncompressedSize64, maxCertFileBytes)
		}
		if total+int64(f.UncompressedSize64) > maxCertBackupBytes {
			return fmt.Errorf("backup archive: certificates exceed the %d-byte limit at %s",
				maxCertBackupBytes, rel)
		}
		clean := filepath.Clean(filepath.FromSlash(rel))
		if clean == "." || strings.HasPrefix(clean, "..") {
			continue
		}
		target := filepath.Join(certRoot, clean)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
		if err != nil {
			_ = rc.Close()
			return err
		}
		_, err = io.Copy(out, io.LimitReader(rc, maxCertFileBytes))
		_ = out.Close()
		_ = rc.Close()
		if err != nil {
			return err
		}
		found[rel] = true
		total += int64(f.UncompressedSize64)
	}
	for rel := range allow {
		if !found[rel] {
			return fmt.Errorf("backup archive is missing certificate %s", rel)
		}
	}
	return nil
}
