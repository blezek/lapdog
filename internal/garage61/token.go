package garage61

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

const maxTokenBytes = 16 * 1024

// TokenStatus deliberately reports only the source, never the credential.
type TokenStatus struct {
	Configured bool   `json:"configured"`
	Source     string `json:"source"`
}

// LocalTokenStore is a temporary token source. A future OAuth manager can
// replace it without changing the Garage61 client or scenario generation.
type LocalTokenStore struct{ path string }

func NewLocalTokenStore(path string) *LocalTokenStore { return &LocalTokenStore{path: path} }

func EnvironmentToken(context.Context) (string, error) {
	token := strings.TrimSpace(os.Getenv("GARAGE61_TOKEN"))
	if token == "" {
		return "", errors.New("add a Garage61 token on the Garage61 tab or set GARAGE61_TOKEN")
	}
	return token, nil
}

func (s *LocalTokenStore) Token(ctx context.Context) (string, error) {
	token, _, err := s.read(ctx)
	return token, err
}

func (s *LocalTokenStore) Status() (TokenStatus, error) {
	token, source, err := s.read(context.Background())
	if err != nil && source != "none" {
		return TokenStatus{}, err
	}
	return TokenStatus{Configured: token != "", Source: source}, nil
}

func (s *LocalTokenStore) read(ctx context.Context) (string, string, error) {
	info, err := os.Lstat(s.path)
	if errors.Is(err, os.ErrNotExist) {
		token, envErr := EnvironmentToken(ctx)
		if envErr != nil {
			return "", "none", envErr
		}
		return token, "environment", nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxTokenBytes+1 {
		return "", "file", errors.New("could not read the saved Garage61 token")
	}
	data, err := os.ReadFile(s.path)
	if err != nil {
		return "", "file", errors.New("could not read the saved Garage61 token")
	}
	token, err := validToken(string(data))
	if err != nil {
		return "", "file", errors.New("the saved Garage61 token is empty or invalid; replace it on the Garage61 tab")
	}
	return token, "file", nil
}

func validToken(raw string) (string, error) {
	token := strings.TrimSpace(raw)
	if token == "" || len(token) > maxTokenBytes || strings.IndexFunc(token, unicode.IsSpace) >= 0 || strings.IndexFunc(token, unicode.IsControl) >= 0 {
		return "", errors.New("enter a Garage61 token without spaces or control characters (16 KiB maximum)")
	}
	return token, nil
}

// Save replaces the token through a same-directory temporary file. CreateTemp
// requests owner-only permissions; no token is written to the database or log.
func (s *LocalTokenStore) Save(raw string) error {
	token, err := validToken(raw)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("create Garage61 token directory: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.path), ".garage61-token-*.tmp")
	if err != nil {
		return errors.New("could not create the Garage61 token file")
	}
	defer os.Remove(tmp.Name())
	if _, err = tmp.WriteString(token + "\n"); err != nil {
		tmp.Close()
		return errors.New("could not write the Garage61 token file")
	}
	if err = tmp.Sync(); err != nil {
		tmp.Close()
		return errors.New("could not sync the Garage61 token file")
	}
	if err = tmp.Close(); err != nil {
		return errors.New("could not close the Garage61 token file")
	}
	if err = os.Rename(tmp.Name(), s.path); err != nil {
		return errors.New("could not replace the Garage61 token file")
	}
	return nil
}

func (s *LocalTokenStore) Delete() error {
	if err := os.Remove(s.path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return errors.New("could not remove the Garage61 token file")
	}
	return nil
}
