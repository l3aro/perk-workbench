package workbench

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"charm.land/bubbles/v2/list"
)

const (
	maxRecentConnections = 20
	encPrefix            = "enc:"
)

type recentConnection struct {
	Driver        connectionDriver `json:"driver"`
	Name          string           `json:"name"`
	Target        string           `json:"target"`
	Host          string           `json:"host,omitempty"`
	Port          string           `json:"port,omitempty"`
	User          string           `json:"user,omitempty"`
	Pass          string           `json:"pass,omitempty"`
	MySQLTLS      mysqlTLSMode     `json:"mysqlTLS,omitempty"`
	PostgreSQLTLS postgresTLSMode  `json:"postgresTLS,omitempty"`
	ReadOnly      bool             `json:"readOnly,omitempty"`
}

func (c recentConnection) FilterValue() string { return c.Name + " " + c.Target }
func (c recentConnection) Title() string       { return safeText(c.Name) }
func (c recentConnection) Description() string {
	desc := ""
	if c.Driver != driverSQLite {
		desc = safeText(c.driverName() + ": " + c.User + "@" + c.Host + ":" + c.Port + "/" + c.Target)
	} else {
		desc = safeText(c.driverName() + ": " + c.Target)
	}
	if c.ReadOnly {
		desc += " [RO]"
	}
	return desc
}

func (c recentConnection) driverName() string {
	switch c.Driver {
	case driverMySQL:
		return "MySQL"
	case driverPostgreSQL:
		return "PostgreSQL"
	default:
		return "SQLite"
	}
}

func recentConnectionsPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "perk-workbench", "connections.json"), nil
}

func loadRecentConnections(path string) []recentConnection {
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var connections []recentConnection
	if json.Unmarshal(contents, &connections) != nil {
		return nil
	}
	key, _ := loadKey()
	result := make([]recentConnection, 0, min(len(connections), maxRecentConnections))
	for _, connection := range connections {
		if !connection.valid() {
			continue
		}
		if connection.Pass != "" && key != nil {
			if strings.HasPrefix(connection.Pass, encPrefix) {
				raw, err := base64.StdEncoding.DecodeString(connection.Pass[len(encPrefix):])
				if err == nil {
					if decrypted, err := decrypt(raw, key); err == nil {
						connection.Pass = string(decrypted)
					}
				}
			}
		}
		result = append(result, connection)
		if len(result) == maxRecentConnections {
			break
		}
	}
	return result
}

func saveRecentConnections(path string, connections []recentConnection) error {
	persisted := make([]recentConnection, 0, len(connections))
	for _, connection := range connections {
		if !connection.valid() {
			continue
		}
		conn := connection
		if conn.Pass != "" && !isSecretRef(conn.Pass) {
			key, err := loadOrGenerateKey()
			if err != nil {
				return err
			}
			encrypted, err := encrypt([]byte(conn.Pass), key)
			if err != nil {
				return err
			}
			conn.Pass = encPrefix + base64.StdEncoding.EncodeToString(encrypted)
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

func (c recentConnection) valid() bool {
	if c.Name == "" {
		return false
	}
	switch c.Driver {
	case driverSQLite:
		return c.Target != ""
	case driverMySQL, driverPostgreSQL:
		return c.Host != "" && c.Port != "" && c.User != ""
	default:
		return false
	}
}

func recentListItems(connections []recentConnection) []list.Item {
	items := make([]list.Item, len(connections))
	for index, connection := range connections {
		items[index] = connection
	}
	return items
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

// isSecretRef returns true if pass is a reference (${ENV_VAR} or file:///path)
// rather than a literal password.
func isSecretRef(pass string) bool {
	return strings.HasPrefix(pass, "${") || strings.HasPrefix(pass, "file://")
}

// resolveSecretRef expands ${ENV_VAR} and file:///path references.
func resolveSecretRef(pass string) string {
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
