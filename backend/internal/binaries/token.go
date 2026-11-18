package binaries

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// rotateEngineToken runs only while the child is stopped, before every start
// (including crash recovery and core rollback). The CGI reads engine.token on
// each request; neither the token nor config contents belong in diagnostics.
// Keep the existing storage/settings and task journal across package upgrades.
func rotateEngineToken(data, cfg string) error {
	if err := os.Chmod(data, 0700); err != nil {
		return err
	}
	raw, err := os.ReadFile(cfg)
	if err != nil {
		return err
	}
	var config map[string]json.RawMessage
	if json.Unmarshal(raw, &config) != nil || config == nil {
		return errors.New("invalid private engine config")
	}
	var entropy [32]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		return errors.New("engine token generation failed")
	}
	token := hex.EncodeToString(entropy[:])
	config["apiToken"], _ = json.Marshal(token)
	if err := Atomic(cfg, config); err != nil {
		return err
	}
	// CreateTemp creates 0600 from the outset. Rename also replaces legacy
	// permissive files/symlinks without opening their targets for writing.
	f, err := os.CreateTemp(data, ".token-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, err = f.WriteString(token)
	if err == nil {
		err = f.Sync()
	}
	closed := f.Close()
	if err != nil {
		return err
	}
	if closed != nil {
		return closed
	}
	return os.Rename(f.Name(), filepath.Join(data, "engine.token"))
}
