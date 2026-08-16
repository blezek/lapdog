package updater

import (
	"archive/zip"
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/Masterminds/semver/v3"
)

const (
	maxArchiveBytes    = 128 << 20
	maxChecksumBytes   = 1 << 20
	maxExecutableBytes = 256 << 20
)

func newer(candidate, current string) bool {
	a, err := semver.NewVersion(candidate)
	if err != nil {
		return false
	}
	b, err := semver.NewVersion(current)
	if err != nil {
		return false
	}
	return a.GreaterThan(b)
}
func equalVersion(a, b string) bool {
	av, e := semver.NewVersion(a)
	if e != nil {
		return false
	}
	bv, e := semver.NewVersion(b)
	return e == nil && av.Equal(bv)
}

func stage(ctx context.Context, client *http.Client, dir string, rel Release) (string, error) {
	if rel.AssetURL == "" || rel.ChecksumURL == "" {
		return "", errors.New("release is missing the update archive or SHA256SUMS")
	}
	archive, err := downloadLimited(ctx, client, rel.AssetURL, maxArchiveBytes)
	if err != nil {
		return "", fmt.Errorf("archive: %w", err)
	}
	sums, err := downloadLimited(ctx, client, rel.ChecksumURL, maxChecksumBytes)
	if err != nil {
		return "", fmt.Errorf("checksums: %w", err)
	}
	want, err := checksumFor(assetName, sums)
	if err != nil {
		return "", err
	}
	got := sha256.Sum256(archive)
	if !strings.EqualFold(want, hex.EncodeToString(got[:])) {
		return "", errors.New("archive checksum does not match SHA256SUMS")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	archivePath := filepath.Join(dir, "download.zip")
	if err := os.WriteFile(archivePath, archive, 0o600); err != nil {
		return "", err
	}
	defer os.Remove(archivePath)
	dst := filepath.Join(dir, "staged-lapdog.exe")
	if err := extractExecutable(archivePath, dst); err != nil {
		return "", err
	}
	return dst, nil
}

func downloadLimited(ctx context.Context, client *http.Client, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("server returned %s", resp.Status)
	}
	if resp.ContentLength > limit {
		return nil, fmt.Errorf("response exceeds %d bytes", limit)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, fmt.Errorf("response exceeds %d bytes", limit)
	}
	return b, nil
}

func checksumFor(name string, data []byte) (string, error) {
	s := bufio.NewScanner(strings.NewReader(string(data)))
	found := ""
	for s.Scan() {
		fields := strings.Fields(s.Text())
		if len(fields) != 2 {
			continue
		}
		file := strings.TrimPrefix(fields[1], "*")
		if file != name {
			continue
		}
		if found != "" {
			return "", fmt.Errorf("SHA256SUMS contains duplicate entries for %s", name)
		}
		if len(fields[0]) != sha256.Size*2 {
			return "", errors.New("SHA256SUMS contains an invalid hash")
		}
		if _, err := hex.DecodeString(fields[0]); err != nil {
			return "", errors.New("SHA256SUMS contains an invalid hash")
		}
		found = fields[0]
	}
	if err := s.Err(); err != nil {
		return "", err
	}
	if found == "" {
		return "", fmt.Errorf("SHA256SUMS has no entry for %s", name)
	}
	return found, nil
}

func extractExecutable(archivePath, dst string) error {
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return fmt.Errorf("open update ZIP: %w", err)
	}
	defer zr.Close()
	var chosen *zip.File
	for _, f := range zr.File {
		clean := filepath.ToSlash(filepath.Clean(f.Name))
		if clean != f.Name || strings.HasPrefix(clean, "../") || strings.HasPrefix(clean, "/") || strings.Contains(clean, ":") {
			return fmt.Errorf("unsafe ZIP path %q", f.Name)
		}
		if f.FileInfo().IsDir() {
			continue
		}
		if clean != "lapdog.exe" {
			return fmt.Errorf("unexpected file %q in update ZIP", f.Name)
		}
		if chosen != nil {
			return errors.New("update ZIP contains duplicate lapdog.exe files")
		}
		if f.UncompressedSize64 > maxExecutableBytes {
			return errors.New("lapdog.exe in update ZIP is too large")
		}
		if f.Mode()&os.ModeSymlink != 0 {
			return errors.New("lapdog.exe in update ZIP is a symbolic link")
		}
		chosen = f
	}
	if chosen == nil {
		return errors.New("update ZIP does not contain lapdog.exe")
	}
	r, err := chosen.Open()
	if err != nil {
		return err
	}
	defer r.Close()
	tmp := dst + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o700)
	if err != nil {
		return err
	}
	n, copyErr := io.Copy(out, io.LimitReader(r, maxExecutableBytes+1))
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(tmp)
		return copyErr
	}
	if closeErr != nil {
		_ = os.Remove(tmp)
		return closeErr
	}
	if n > maxExecutableBytes {
		_ = os.Remove(tmp)
		return errors.New("lapdog.exe in update ZIP is too large")
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
