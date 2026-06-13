package auth

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/adrg/xdg"
	"github.com/zalando/go-keyring"
)

type Store struct {
	service string
	profile string
}

type Credential struct {
	Token   string
	Source  string
	Profile string
}

type plainAuthFile struct {
	Profiles map[string]plainProfile `json:"profiles"`
}

type plainProfile struct {
	Token     string    `json:"token"`
	CreatedAt time.Time `json:"created_at"`
}

func NewStore(service, profile string) Store {
	return Store{service: service, profile: profile}
}

func (s Store) Resolve() (Credential, error) {
	if token := strings.TrimSpace(os.Getenv("NOTION_API_TOKEN")); token != "" {
		return Credential{Token: token, Source: "NOTION_API_TOKEN", Profile: s.profile}, nil
	}
	if token := strings.TrimSpace(os.Getenv("NOTION_TOKEN")); token != "" {
		return Credential{Token: token, Source: "NOTION_TOKEN", Profile: s.profile}, nil
	}

	if token, err := keyring.Get(s.service, s.profile); err == nil && strings.TrimSpace(token) != "" {
		return Credential{Token: strings.TrimSpace(token), Source: "system keychain", Profile: s.profile}, nil
	}

	file, path, err := s.readPlaintextFile()
	if err != nil {
		return Credential{}, err
	}
	if profile, ok := file.Profiles[s.profile]; ok && strings.TrimSpace(profile.Token) != "" {
		return Credential{Token: strings.TrimSpace(profile.Token), Source: "plaintext auth file " + path, Profile: s.profile}, nil
	}

	return Credential{}, fmt.Errorf("not authenticated; run %s auth login", s.service)
}

func (s Store) SaveKeychain(token string) error {
	return keyring.Set(s.service, s.profile, token)
}

func (s Store) SavePlaintext(token string) (string, error) {
	file, path, err := s.readPlaintextFile()
	if err != nil {
		return "", err
	}
	if file.Profiles == nil {
		file.Profiles = map[string]plainProfile{}
	}

	file.Profiles[s.profile] = plainProfile{Token: token, CreatedAt: time.Now().UTC()}

	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return "", err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", err
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return "", err
	}

	return path, nil
}

func (s Store) DeleteAll() (int, error) {
	removed := 0

	if err := keyring.Delete(s.service, s.profile); err == nil {
		removed++
	}

	file, path, err := s.readPlaintextFile()
	if err != nil {
		return removed, err
	}
	if _, ok := file.Profiles[s.profile]; ok {
		delete(file.Profiles, s.profile)
		removed++

		data, err := json.MarshalIndent(file, "", "  ")
		if err != nil {
			return removed, err
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return removed, err
		}
		if err := os.WriteFile(path, data, 0o600); err != nil {
			return removed, err
		}
	}

	return removed, nil
}

func (s Store) readPlaintextFile() (plainAuthFile, string, error) {
	path, err := xdg.ConfigFile(filepath.Join(s.service, "auth.json"))
	if err != nil {
		return plainAuthFile{}, "", err
	}

	file := plainAuthFile{Profiles: map[string]plainProfile{}}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return file, path, nil
	}
	if err != nil {
		return plainAuthFile{}, path, err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return file, path, nil
	}
	if err := json.Unmarshal(data, &file); err != nil {
		return plainAuthFile{}, path, err
	}
	if file.Profiles == nil {
		file.Profiles = map[string]plainProfile{}
	}

	return file, path, nil
}
