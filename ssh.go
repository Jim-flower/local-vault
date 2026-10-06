package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"regexp"
	"strings"
)

// SSHConnection is encrypted as a whole, including connection metadata.
type SSHConnection struct {
	Host       string `json:"Host"`
	Port       int    `json:"Port"`
	PrivateKey string `json:"PrivateKey"`
	Passphrase string `json:"Passphrase"`
	PublicKey  string `json:"PublicKey"`
}

var sshHostPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]*$`)
var sshUserPattern = regexp.MustCompile(`^[a-zA-Z0-9_][a-zA-Z0-9_.-]*$`)

func validSSHHost(host string) bool {
	ip, zone, zoned := strings.Cut(host, "%")
	if net.ParseIP(ip) != nil {
		return !zoned || sshHostPattern.MatchString(zone)
	}
	return !zoned && sshHostPattern.MatchString(host)
}

func normalizeEntry(e *Entry) error {
	e.Name = strings.TrimSpace(e.Name)
	if e.Name == "" {
		return errors.New("name is required")
	}
	if e.Type == "" {
		e.Type = "password"
	}
	switch e.Type {
	case "password":
		if e.SSH != nil {
			return errors.New("password entry cannot contain SSH settings")
		}
		if e.Password == "" {
			return errors.New("password is required")
		}
	case "ssh":
		if e.SSH == nil {
			return errors.New("SSH settings are required")
		}
		e.SSH.Host = strings.TrimSpace(e.SSH.Host)
		e.Username = strings.TrimSpace(e.Username)
		if !validSSHHost(e.SSH.Host) || !sshUserPattern.MatchString(e.Username) {
			return errors.New("SSH requires a valid hostname/IP and username (without spaces or shell characters)")
		}
		if e.SSH.Port == 0 {
			e.SSH.Port = 22
		}
		if e.SSH.Port < 1 || e.SSH.Port > 65535 {
			return errors.New("SSH port must be between 1 and 65535")
		}
		if len(e.SSH.PrivateKey) > 1<<20 || len(e.SSH.PublicKey) > 1<<20 || len(e.SSH.Passphrase) > 1<<16 {
			return errors.New("SSH key or passphrase is too large")
		}
	default:
		return fmt.Errorf("unsupported entry type %q", e.Type)
	}
	return nil
}

func (s *Store) encryptSSH(value *SSHConnection) (string, error) {
	if value == nil {
		return "", nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return s.encryptField(string(data))
}

func (s *Store) decryptSSH(value string) (*SSHConnection, error) {
	if value == "" {
		return nil, nil
	}
	plain, err := s.decryptField(value)
	if err != nil {
		return nil, err
	}
	var result SSHConnection
	if err := json.Unmarshal([]byte(plain), &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// SaveVaultEntry accepts the same typed entry for both desktop and HTTP clients.
// IDs and timestamps from the caller are ignored.
func (a *App) SaveVaultEntry(oldName string, input Entry) error {
	if a.store == nil {
		return errors.New("store not available")
	}
	if oldName == "" {
		return a.store.AddEntryRecord(&input)
	}
	previous, err := a.store.GetEntry(oldName)
	if err != nil {
		return err
	}
	input.ID = previous.ID
	return a.store.SaveEntry(&input)
}
