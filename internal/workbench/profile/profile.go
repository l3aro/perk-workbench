// Package profile owns connection profile persistence: the JSON record,
// UUIDv7 scope generation and validation, XDG path resolution, AES-GCM
// password encryption, secret-reference handling, and atomic 0600 saves.
//
// It has no Bubble Tea dependency; list rendering and driver display
// labels belong to the workbench connection feature.
package profile

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

const (
	// MaxProfiles caps the persisted profile list.
	MaxProfiles = 20
	encPrefix   = "enc:"
)

// Driver identifies a database driver.
type Driver string

const (
	DriverSQLite     Driver = "sqlite"
	DriverMySQL      Driver = "mysql"
	DriverPostgreSQL Driver = "postgres"
)

// MySQLTLS is the persisted MySQL TLS mode.
type MySQLTLS string

// MySQL TLS modes, as stored in profiles and the connection form.
const (
	MySQLTLSVerify     MySQLTLS = "true"
	MySQLTLSSkipVerify MySQLTLS = "skip-verify"
	MySQLTLSDisabled   MySQLTLS = "false"
)

// PostgreSQLTLS is the persisted PostgreSQL TLS mode.
type PostgreSQLTLS string

// PostgreSQL TLS modes, as stored in profiles and the connection form.
const (
	PostgreSQLTLSVerifyFull PostgreSQLTLS = "verify-full"
	PostgreSQLTLSEncrypt    PostgreSQLTLS = "require"
	PostgreSQLTLSDisabled   PostgreSQLTLS = "disable"
)

// Profile is one persisted connection record. The JSON keys are stable
// wire format; do not rename them.
type Profile struct {
	ID            string        `json:"id"`
	Driver        Driver        `json:"driver"`
	Name          string        `json:"name"`
	Target        string        `json:"target"`
	Host          string        `json:"host,omitempty"`
	Port          string        `json:"port,omitempty"`
	User          string        `json:"user,omitempty"`
	Pass          string        `json:"pass,omitempty"`
	MySQLTLS      MySQLTLS      `json:"mysqlTLS,omitempty"`
	PostgreSQLTLS PostgreSQLTLS `json:"postgresTLS,omitempty"`
	ReadOnly      bool          `json:"readOnly,omitempty"`
	// Extras carries driver-specific form values beyond the fixed fields
	// (empty for the built-in drivers).
	Extras map[string]string `json:"extras,omitempty"`
}

// Path returns the XDG config location of the profiles file.
func Path() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "perk-workbench", "connections.json"), nil
}

// Load reads persisted profiles, assigning a fresh UUIDv7 scope to every
// legacy profile whose ID is empty, invalid, or duplicated by an earlier
// profile. migrated reports whether any profile was reassigned, so
// callers can persist the corrected file immediately. An unreadable or
// corrupt file yields an empty list.
func Load(path string) (profiles []Profile, migrated bool) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	var connections []Profile
	if json.Unmarshal(contents, &connections) != nil {
		return nil, false
	}
	key, _ := loadKey()
	result := make([]Profile, 0, min(len(connections), MaxProfiles))
	seen := make(map[string]bool, len(connections))
	migrated = false
	for _, connection := range connections {
		if !connection.valid() {
			continue
		}
		if connection.Pass != "" && key != nil {
			if decrypted, ok := decryptSecret(connection.Pass, key); ok {
				connection.Pass = decrypted
			}
		}
		for extraKey, value := range connection.Extras {
			if decrypted, ok := decryptSecret(value, key); ok {
				connection.Extras[extraKey] = decrypted
			}
		}
		if !ValidID(connection.ID) || seen[connection.ID] {
			id, err := NewID()
			if err != nil {
				// Persistence failure: keep the profile as-is and report no
				// migration so nothing unscoped is written to disk.
				result = append(result, connection)
				if len(result) == MaxProfiles {
					break
				}
				continue
			}
			connection.ID = id
			migrated = true
		}
		seen[connection.ID] = true
		result = append(result, connection)
		if len(result) == MaxProfiles {
			break
		}
	}
	return result, migrated
}

// Save persists profiles atomically with 0600 permissions, skipping
// invalid records and encrypting literal passwords. Secret references are
// stored verbatim.
func Save(path string, profiles []Profile) error {
	persisted := make([]Profile, 0, len(profiles))
	for _, connection := range profiles {
		if !connection.valid() {
			continue
		}
		conn := connection
		conn.Extras = maps.Clone(connection.Extras)
		if conn.Pass != "" && !IsSecretRef(conn.Pass) {
			encrypted, err := encryptSecret(conn.Pass)
			if err != nil {
				return err
			}
			conn.Pass = encrypted
		}
		// Extras are treated as sensitive like Pass: literal values are
		// encrypted at rest, secret references stay verbatim.
		for extraKey, value := range conn.Extras {
			if value == "" || IsSecretRef(value) {
				continue
			}
			encrypted, err := encryptSecret(value)
			if err != nil {
				return err
			}
			conn.Extras[extraKey] = encrypted
		}
		persisted = append(persisted, conn)
	}
	contents, err := json.MarshalIndent(persisted, "", "\t")
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	file, err := os.CreateTemp(dir, "recent-*.json")
	if err != nil {
		return err
	}
	name := file.Name()
	defer os.Remove(name)
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return err
	}
	if _, err := file.Write(contents); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}

// NewID returns a fresh UUIDv7 scope for a connection profile.
func NewID() (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", err
	}
	return id.String(), nil
}

// ValidID reports whether id is a parseable UUIDv7, the only scope form
// this application generates for profiles.
func ValidID(id string) bool {
	parsed, err := uuid.Parse(id)
	return err == nil && parsed.Version() == 7
}

// valid reports whether the record carries the fields its driver needs.
// Known drivers have strict per-driver requirements; any other driver
// (a future registered or plugin driver) survives with the generic
// target floor, and its form-level requirements are enforced by the
// connection layer.
func (p Profile) valid() bool {
	if p.Name == "" {
		return false
	}
	switch p.Driver {
	case DriverSQLite:
		return p.Target != ""
	case DriverMySQL, DriverPostgreSQL:
		return p.Host != "" && p.Port != "" && p.User != ""
	default:
		return p.Target != ""
	}
}

func secretKeyPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "perk-workbench", "secret.key"), nil
}

func loadKey() (*[32]byte, error) {
	path, err := secretKeyPath()
	if err != nil {
		return nil, err
	}
	key, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(key) != 32 {
		return nil, errors.New("secret.key: unexpected length")
	}
	k := *(*[32]byte)(key)
	return &k, nil
}

func loadOrGenerateKey() (*[32]byte, error) {
	k, err := loadKey()
	if err == nil {
		return k, nil
	}
	// Generate new key on first use
	path, err := secretKeyPath()
	if err != nil {
		return nil, err
	}
	k32 := new([32]byte)
	if _, err := rand.Read(k32[:]); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	if err := os.WriteFile(path, k32[:], 0o600); err != nil {
		return nil, err
	}
	return k32, nil
}

// encryptSecret returns the at-rest form of a literal secret value.
func encryptSecret(value string) (string, error) {
	key, err := loadOrGenerateKey()
	if err != nil {
		return "", err
	}
	encrypted, err := encrypt([]byte(value), key)
	if err != nil {
		return "", err
	}
	return encPrefix + base64.StdEncoding.EncodeToString(encrypted), nil
}

// decryptSecret reverses encryptSecret for an at-rest value; ok=false
// when the value is not encrypted or cannot be decrypted.
func decryptSecret(value string, key *[32]byte) (string, bool) {
	if key == nil || !strings.HasPrefix(value, encPrefix) {
		return "", false
	}
	raw, err := base64.StdEncoding.DecodeString(value[len(encPrefix):])
	if err != nil {
		return "", false
	}
	decrypted, err := decrypt(raw, key)
	if err != nil {
		return "", false
	}
	return string(decrypted), true
}

func encrypt(plaintext []byte, key *[32]byte) ([]byte, error) {
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

func decrypt(ciphertext []byte, key *[32]byte) ([]byte, error) {
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return nil, errors.New("ciphertext too short")
	}
	nonce, ct := ciphertext[:nonceSize], ciphertext[nonceSize:]
	return gcm.Open(nil, nonce, ct, nil)
}

// IsSecretRef returns true if pass is a reference (${ENV_VAR} or
// file:///path) rather than a literal password.
func IsSecretRef(pass string) bool {
	return strings.HasPrefix(pass, "${") || strings.HasPrefix(pass, "file://")
}

// ResolveSecretRef expands ${ENV_VAR} and file:///path references.
func ResolveSecretRef(pass string) string {
	if strings.HasPrefix(pass, "${") && strings.HasSuffix(pass, "}") {
		return os.Getenv(pass[2 : len(pass)-1])
	}
	if strings.HasPrefix(pass, "file://") {
		data, err := os.ReadFile(pass[7:])
		if err != nil {
			return pass
		}
		return strings.TrimRight(string(data), "\n\r")
	}
	return pass
}
