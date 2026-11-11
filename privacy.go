package main

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/monoedge-labs/zmail/internal/guard"
)

// privacy is what the user chose to hide in mail: from what the agent's
// commands print, and in the window. Both default to off, as a verification
// code is often just what the user asks the agent to read. It lives in
// privacy.json beside the mail database; the window changes it, the commands
// only read it.
type privacy struct {
	Agent bool `json:"agent"`
	View  bool `json:"view"`
	// ModelOff turns the local model off. It is on by default: names and
	// addresses are most of what patterns cannot find.
	ModelOff bool `json:"model_off"`
	// NoLinkGuard lets a click on a link in mail open the browser at once. By
	// default mail is read-only: a click asks first, as a link in mail is a
	// common way to lure someone to a fake page.
	NoLinkGuard bool `json:"no_link_guard"`
}

func (p privacy) model() bool { return !p.ModelOff }

// modelDir is where the local model's files are kept, under the config directory.
func modelDir(dir string) string { return guard.Dir(filepath.Join(dir, "models")) }

func loadPrivacy(dir string) (p privacy) {
	if b, err := os.ReadFile(filepath.Join(dir, "privacy.json")); err == nil {
		json.Unmarshal(b, &p)
	}
	return p
}

func (p privacy) save(dir string) error {
	b, err := json.Marshal(p)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "privacy.json"), b, 0o600)
}
