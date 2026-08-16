// Package profile owns connection profile persistence: the JSON record,
// UUIDv7 scope generation and validation, XDG path resolution, versioned
// AES-256-GCM at-rest encryption, secret-reference handling, and atomic
// hardened 0700/0600 saves.
//
// It has no Bubble Tea dependency; list rendering and driver display
// labels belong to the workbench connection feature.
//
// # At-rest encryption
//
// Literal passwords are stored in a versioned envelope
// ("enc:v2:" + base64(nonce|AES-256-GCM ciphertext|tag)) whose
// authenticated data binds the ciphertext to the profile's scope ID and
// the field identity ("pass" or the exact extra key). Connection
// targets that carry credentials (URL userinfo passwords, the
// libpq/pgx password query parameter, mysql: DSNs) are encrypted the
// same way under the "target" field identity; non-credential targets
// stay plaintext and readable. Pre-v2 "enc:" records (no binding)
// still load and are rewritten to the v2 envelope on the next
// successful save, and legacy plaintext credential-bearing targets are
// migrated to the encrypted form on the next successful save. Secret
// references (${ENV_VAR}, file://) are stored verbatim. A value that
// cannot be decrypted (tampering, wrong or missing key, unknown
// envelope) is never surfaced as a literal and never rewritten: Load
// retains it in the profile with an Undecryptable marker and reports
// secretFail, and Save refuses while the field still holds the
// retained blob. The key and the ciphertext both live under the same
// user-owned 0700 config directory, so the encryption protects against
// accidental disclosure and backups — not against an attacker with
// account access.
package profile

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	// MaxProfiles caps the persisted profile list.
	MaxProfiles = 20
	// encPrefix marks any at-rest encrypted value; encV2Prefix selects
	// the current versioned envelope (AES-256-GCM with a scope/field
	// binding AAD). Pre-v2 values carry only encPrefix.
	encPrefix   = "enc:"
	encV2Prefix = "enc:v2:"
	// secretKeyLen is the exact required length of secret.key.
	secretKeyLen = 32
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
	ID     string `json:"id"`
	Driver Driver `json:"driver"`
	Name   string `json:"name"`
	// Target is the driver's opener target body. It is persisted as-is
	// unless it carries credentials (URL userinfo password, a password
	// query parameter, or a mysql: DSN), in which case it is stored as a
	// v2 envelope bound to the "target" field identity and restored only
	// after authenticated decryption.
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
	// Undecryptable maps a field identity ("pass" or an exact extra key)
	// to the stored enc: value that failed to decrypt at load. It is
	// in-memory only (json:"-"; the wire format is unchanged) and marks
	// a fail-closed load: the retained ciphertext is never surfaced as a
	// literal, and Save refuses while the field still holds exactly that
	// blob. Re-entering the field's value clears the refusal.
	Undecryptable map[string]string `json:"-"`
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
// profile, and marking legacy plaintext credential-bearing targets for
// encryption. migrated reports whether any profile was reassigned or a
// legacy credential target was found, so callers can persist the
// corrected file immediately. An unreadable or corrupt file yields an
// empty list; a symlink or non-regular profiles target is refused
// (treated as unreadable) rather than followed.
//
// secretFail reports that at least one stored enc: value could not be
// decrypted (tampering, wrong or missing key). Such values are retained
// verbatim in the returned profiles with an Undecryptable marker — never
// surfaced as literals — and Save refuses to rewrite them until the user
// re-enters the field, so a transient key problem can never silently
// destroy the stored ciphertext.
func Load(path string) (profiles []Profile, migrated bool, secretFail bool) {
	if info, err := os.Lstat(path); err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return nil, false, false
		}
	} else {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return nil, false, false
		}
		// Fix permissions to 0600 even when the file pre-exists;
		// best-effort so a read-only filesystem still loads.
		_ = os.Chmod(path, 0o600)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, false, false
	}
	var connections []Profile
	if json.Unmarshal(contents, &connections) != nil {
		return nil, false, false
	}
	key, _ := loadKey()
	result := make([]Profile, 0, min(len(connections), MaxProfiles))
	seen := make(map[string]bool, len(connections))
	migrated = false
	for _, connection := range connections {
		if !connection.valid() {
			continue
		}
		// Targets are plaintext unless they carried credentials, in which
		// case they are v2 envelopes bound to the "target" field identity.
		// An encrypted target that cannot be decrypted is never surfaced
		// as a literal opener target: it is retained with the marker and
		// Save refuses to rewrite it (fail closed, non-destructive).
		if strings.HasPrefix(connection.Target, encPrefix) {
			if decrypted, ok := decryptSecret(connection.Target, key, connection.ID, "target"); ok {
				connection.Target = decrypted
			} else {
				if connection.Undecryptable == nil {
					connection.Undecryptable = map[string]string{}
				}
				connection.Undecryptable["target"] = connection.Target
				secretFail = true
			}
		} else if targetHasCredentials(connection.Target) {
			// Legacy plaintext credential-bearing target: load it for
			// compatibility and report migration so the caller persists
			// the encrypted form on the next successful save.
			migrated = true
		}
		if strings.HasPrefix(connection.Pass, encPrefix) {
			if decrypted, ok := decryptSecret(connection.Pass, key, connection.ID, "pass"); ok {
				connection.Pass = decrypted
			} else {
				// Fail closed: never surface the blob as a literal, and
				// never let Save wrap or drop it. Retain it with the
				// marker so the caller can surface the failure state.
				connection.Undecryptable = map[string]string{"pass": connection.Pass}
				secretFail = true
			}
		}
		for extraKey, value := range connection.Extras {
			if !strings.HasPrefix(value, encPrefix) {
				continue
			}
			if decrypted, ok := decryptSecret(value, key, connection.ID, extraKey); ok {
				connection.Extras[extraKey] = decrypted
			} else {
				if connection.Undecryptable == nil {
					connection.Undecryptable = map[string]string{}
				}
				connection.Undecryptable[extraKey] = value
				secretFail = true
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
	return result, migrated, secretFail
}

// renameFile is the atomic-replace step of Save (a var so tests can
// inject deterministic failures). The original file is only ever
// replaced by this final step.
var renameFile = os.Rename

// Save persists profiles atomically with 0600 permissions in a 0700
// directory, skipping invalid records and encrypting literal passwords
// into the versioned v2 envelope bound to each profile's scope and field
// identity. Secret references are stored verbatim. The profiles target
// must be a regular file (never a symlink), the directory and file
// permissions are forced to 0700/0600 even when pre-existing, the
// temporary file is fsynced before the atomic rename, and the containing
// directory is fsynced after it; a failure at any point leaves the
// original file intact.
//
// Save refuses a record while any field still holds a retained
// undecryptable blob (see Load): the ciphertext is never wrapped as a
// literal or silently rewritten. Re-entering the field clears the
// refusal.
func Save(path string, profiles []Profile) error {
	persisted := make([]Profile, 0, len(profiles))
	for _, connection := range profiles {
		if !connection.valid() {
			continue
		}
		conn := connection
		conn.Extras = maps.Clone(connection.Extras)
		// Every v2 envelope is bound to the profile's UUIDv7 scope: a
		// record without one gets a fresh scope before encryption, so no
		// ciphertext can ever carry a weak empty-scope AAD.
		if !ValidID(conn.ID) {
			id, err := NewID()
			if err != nil {
				return err
			}
			conn.ID = id
		}
		if blob, bad := conn.Undecryptable["pass"]; bad && conn.Pass == blob {
			return fmt.Errorf("profile %q has an undecryptable stored password; re-enter it to save", conn.Name)
		}
		// Targets are persisted as-is unless they carry credentials, in
		// which case they are encrypted like any other secret field.
		if blob, bad := conn.Undecryptable["target"]; bad && conn.Target == blob {
			return fmt.Errorf("profile %q has an undecryptable stored target; re-enter it to save", conn.Name)
		}
		if targetHasCredentials(conn.Target) {
			encrypted, err := encryptSecret(conn.Target, conn.ID, "target")
			if err != nil {
				return err
			}
			conn.Target = encrypted
		}
		if conn.Pass != "" && !IsSecretRef(conn.Pass) {
			encrypted, err := encryptSecret(conn.Pass, conn.ID, "pass")
			if err != nil {
				return err
			}
			conn.Pass = encrypted
		}
		// Extras are treated as sensitive like Pass: literal values are
		// encrypted at rest, secret references stay verbatim.
		for extraKey, value := range conn.Extras {
			if blob, bad := conn.Undecryptable[extraKey]; bad && value == blob {
				return fmt.Errorf("profile %q has an undecryptable stored value in %q; re-enter it to save", conn.Name, extraKey)
			}
			if value == "" || IsSecretRef(value) {
				continue
			}
			encrypted, err := encryptSecret(value, conn.ID, extraKey)
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
	if err := os.Chmod(dir, 0o700); err != nil {
		return err
	}
	// Refuse symlink and non-regular targets: the atomic rename would
	// replace a symlink rather than write through it, and a directory or
	// device target is not a profile file.
	if info, err := os.Lstat(path); err == nil {
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			return fmt.Errorf("refusing to save profiles through symlink %s", path)
		case !info.Mode().IsRegular():
			return fmt.Errorf("refusing to save profiles to non-regular target %s", path)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
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
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := renameFile(name, path); err != nil {
		return err
	}
	// fsync the containing directory so the rename itself is durable.
	dirFile, err := os.Open(dir)
	if err != nil {
		return err
	}
	if err := dirFile.Sync(); err != nil {
		_ = dirFile.Close()
		return err
	}
	return dirFile.Close()
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

// loadKey reads and validates the at-rest encryption key. It fails
// closed on anything suspicious: a symlink, a non-regular file, lax
// permission bits (any group/other access), or a length other than
// exactly secretKeyLen bytes.
func loadKey() (*[32]byte, error) {
	path, err := secretKeyPath()
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("secret.key: refusing symlink")
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("secret.key: not a regular file")
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		return nil, fmt.Errorf("secret.key: permissions %#o must be 0600", perm)
	}
	key, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(key) != secretKeyLen {
		return nil, errKeyLength
	}
	k := *(*[32]byte)(key)
	return &k, nil
}

// errKeyLength marks a key file that exists but does not yet carry
// secretKeyLen bytes — the state of a concurrent creator mid-write, so
// loadOrGenerateKey retries instead of failing.
var errKeyLength = errors.New("secret.key: unexpected length")

// waitForKey retries loadKey while a concurrent creator may still be
// writing the key file, then fails closed.
func waitForKey() (*[32]byte, error) {
	for range 20 {
		if k, err := loadKey(); err == nil {
			return k, nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return nil, errors.New("secret.key: concurrent creation did not yield a valid key")
}

// loadOrGenerateKey returns the at-rest encryption key, creating it
// race-safely on first use: the file is created with O_EXCL (never
// following or overwriting an existing entry), fsynced before use, and a
// concurrent creator loads the winner's key, retrying briefly while the
// winner may still be writing. Any other key-file problem (symlink, lax
// permissions, bad length) fails closed instead of being papered over.
func loadOrGenerateKey() (*[32]byte, error) {
	k, loadErr := loadKey()
	if loadErr == nil {
		return k, nil
	}
	// A missing key proceeds to creation; a key of the wrong length is a
	// concurrent creator mid-write (the winner is loaded). Every other
	// key-file problem (symlink, lax permissions, read errors) fails
	// closed instead of being papered over.
	if !errors.Is(loadErr, os.ErrNotExist) && !errors.Is(loadErr, errKeyLength) {
		return nil, loadErr
	}
	path, err := secretKeyPath()
	if err != nil {
		return nil, err
	}
	if errors.Is(loadErr, errKeyLength) {
		return waitForKey()
	}
	k32 := new([32]byte)
	if _, err := rand.Read(k32[:]); err != nil {
		return nil, err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		// Another creator won the race: load the winner, which may
		// still be writing the key bytes.
		return waitForKey()
	}
	if err != nil {
		return nil, err
	}
	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()
		return nil, err
	}
	if _, err := file.Write(k32[:]); err != nil {
		_ = file.Close()
		return nil, err
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return nil, err
	}
	if err := file.Close(); err != nil {
		return nil, err
	}
	return k32, nil
}

// encryptSecret returns the at-rest form of a literal secret value: the
// versioned AES-256-GCM envelope "enc:v2:" + base64(nonce|ciphertext|
// tag), authenticated with an AAD binding the ciphertext to the profile
// scope (scopeID, the connection's UUIDv7) and the field identity
// ("pass" or the exact extra key).
func encryptSecret(value string, scopeID, field string) (string, error) {
	key, err := loadOrGenerateKey()
	if err != nil {
		return "", err
	}
	encrypted, err := encrypt([]byte(value), key, secretAAD(scopeID, field))
	if err != nil {
		return "", err
	}
	return encV2Prefix + base64.StdEncoding.EncodeToString(encrypted), nil
}

// decryptSecret reverses encryptSecret for an at-rest value; ok=false
// when the value is not encrypted or cannot be decrypted (unknown
// envelope, malformed base64, wrong or missing key, tampering, or an
// AAD mismatch). Pre-v2 "enc:" records predate the scope/field binding
// and decrypt without AAD.
func decryptSecret(value string, key *[32]byte, scopeID, field string) (string, bool) {
	if key == nil {
		return "", false
	}
	var (
		body string
		aad  []byte
	)
	switch {
	case strings.HasPrefix(value, encV2Prefix):
		body = value[len(encV2Prefix):]
		aad = secretAAD(scopeID, field)
	case strings.HasPrefix(value, encPrefix):
		body = value[len(encPrefix):] // v1 envelope: no AAD
	default:
		return "", false
	}
	raw, err := base64.StdEncoding.DecodeString(body)
	if err != nil {
		return "", false
	}
	decrypted, err := decrypt(raw, key, aad)
	if err != nil {
		return "", false
	}
	return string(decrypted), true
}

// secretAAD builds the authenticated-data binding for one encrypted
// field: fixed-width length-prefixed scope ID and field identity, so no
// scope/field concatenation can collide.
func secretAAD(scopeID, field string) []byte {
	aad := make([]byte, 8, 8+len(scopeID)+len(field))
	binary.BigEndian.PutUint32(aad[0:4], uint32(len(scopeID)))
	binary.BigEndian.PutUint32(aad[4:8], uint32(len(field)))
	aad = append(aad, scopeID...)
	aad = append(aad, field...)
	return aad
}

func encrypt(plaintext []byte, key *[32]byte, aad []byte) ([]byte, error) {
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
	return gcm.Seal(nonce, nonce, plaintext, aad), nil
}

func decrypt(ciphertext []byte, key *[32]byte, aad []byte) ([]byte, error) {
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
	return gcm.Open(nil, nonce, ct, aad)
}

// targetHasCredentials reports whether a connection target carries
// credential material that must never persist in plaintext. It covers
// every credential-bearing target form the current drivers accept:
// a parseable URL with userinfo password (redis://, rediss://,
// mongodb://, mongodb+srv://, postgres://, postgresql://), a URL with a
// non-empty password query parameter (the libpq/pgx form, e.g.
// postgres://alice@h/db?password=…), a label-prefixed target wrapping
// such a URL ("redis:redis://…", "postgres:postgres://…"), and the
// go-sql-driver DSN form (mysql:user:pass@tcp(host:port)/db). Plain
// paths, database names, and userinfo-less URLs are left untouched.
func targetHasCredentials(target string) bool {
	trimmed := strings.TrimSpace(target)
	if credentialURL(trimmed) {
		return true
	}
	// Label-prefixed targets ("redis:redis://…") hide the URL from a
	// direct parse; strip the label and retry.
	if label, rest, found := strings.Cut(trimmed, ":"); found && label != "" && strings.Contains(rest, "://") {
		return credentialURL(rest)
	}
	// go-sql-driver DSN: user:password@tcp(host:port)/db. A DSN that
	// parses cannot contain an unescaped '@' in the password.
	if body, found := strings.CutPrefix(trimmed, "mysql:"); found {
		if at := strings.LastIndex(body, "@"); at > 0 {
			if strings.Index(body[:at], ":") >= 0 {
				return true
			}
		}
	}
	return false
}

// credentialURL reports whether a parseable URL carries credentials:
// userinfo with a password, or a non-empty password query parameter.
func credentialURL(s string) bool {
	u, err := url.Parse(s)
	if err != nil {
		return false
	}
	if u.User != nil {
		if _, has := u.User.Password(); has {
			return true
		}
	}
	return u.Query().Get("password") != ""
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
