package main

import (
	"cmp"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/zalando/go-keyring"

	"github.com/monoedge-labs/zmail/internal/mailbox"
)

// Which model service polishes feedback is the user's to choose, in the
// window or with `zmail llm`. The address and the model name are kept in
// llm.json beside the database; the key is a secret and goes to the keychain,
// as the mail passwords do. What is not set is the built-in free service.
// An environment variable wins over both, to try something once.

const (
	defaultBase  = "https://apihub.agnes-ai.com/v1"
	defaultModel = "agnes-3.0-flash"
	// Agnes AI's free key, meant to be public: it sits in the source and in
	// the binary, and when it is abused it is replaced and shipped again.
	defaultKey = "sk-5orQwZnghehgLf4QplHw3qpPvgEd0DtDhuzdmn0uWgtWXuee"
	// llmKeyID names the user's key in the keychain; account ids hold a ":".
	llmKeyID = "llm"
)

// llmConfig is what llm.json holds; "" is the built-in.
type llmConfig struct {
	BaseURL string `json:"base_url,omitempty"`
	Model   string `json:"model,omitempty"`
}

// llmSettings is what a request uses.
type llmSettings struct {
	Base, Model, Key string
	KeySource        string // "builtin", "custom" (the keychain) or "env"
}

func loadLLMConfig(dir string) (c llmConfig) {
	if dir == "" {
		return c
	}
	if b, err := os.ReadFile(filepath.Join(dir, "llm.json")); err == nil {
		json.Unmarshal(b, &c)
	}
	return c
}

// resolveLLM is the service in use: the environment, then the user's choice,
// then the built-in. sec may be nil.
func resolveLLM(dir string, sec mailbox.Secrets) llmSettings {
	c := loadLLMConfig(dir)
	s := llmSettings{
		Base:      cmp.Or(os.Getenv("ZMAIL_LLM_BASE"), c.BaseURL, defaultBase),
		Model:     cmp.Or(os.Getenv("ZMAIL_LLM_MODEL"), c.Model, defaultModel),
		Key:       defaultKey,
		KeySource: "builtin",
	}
	if k := os.Getenv("ZMAIL_LLM_KEY"); k != "" {
		s.Key, s.KeySource = k, "env"
	} else if sec != nil {
		if k, err := sec.Get(llmKeyID); err == nil && k != "" {
			s.Key, s.KeySource = k, "custom"
		}
	}
	return s
}

// free reports whether the built-in free service is in use.
func (s llmSettings) free() bool { return s.Base == defaultBase && s.KeySource == "builtin" }

func (s llmSettings) host() string {
	if u, err := url.Parse(s.Base); err == nil && u.Hostname() != "" {
		return u.Hostname()
	}
	return s.Base
}

// who names the receiver of the text, for the notice that it leaves the computer.
func (s llmSettings) who() string {
	switch {
	case s.Base == defaultBase:
		return "第三方 Agnes AI"
	case loopback(s.host()):
		return "本机的模型服务 " + s.host()
	}
	return "第三方 " + s.host()
}

func loopback(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

// checkBase accepts an https address, or http for a model on this computer:
// the key travels with every request, and must not go in the clear.
func checkBase(base string) error {
	u, err := url.Parse(base)
	switch {
	case err != nil || u.Host == "" || u.User != nil:
		return fmt.Errorf("接口地址无效：%q", base)
	case u.Scheme == "https", u.Scheme == "http" && loopback(u.Hostname()):
		return nil
	}
	return fmt.Errorf("接口地址要用 https（本机的服务可用 http）：%q", base)
}

// forgetLLMKey drops the user's key from the keychain; there being none is fine.
func forgetLLMKey(sec mailbox.Secrets) error {
	if sec == nil {
		return nil
	}
	if err := sec.Delete(llmKeyID); err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return err
	}
	return nil
}

// setLLM keeps the user's choice: c, whose empty fields are the built-in,
// and key when it is not empty. A key belongs to the address it was given
// for, so when the address changes and no new key comes, the old one is
// forgotten: changing the address cannot send a key somewhere it was not meant to go.
func setLLM(dir string, sec mailbox.Secrets, c llmConfig, key string) error {
	if dir == "" {
		return errors.New("没有配置目录，无法保存")
	}
	c.BaseURL, c.Model = strings.TrimRight(strings.TrimSpace(c.BaseURL), "/"), strings.TrimSpace(c.Model)
	if c.BaseURL != "" {
		if err := checkBase(c.BaseURL); err != nil {
			return err
		}
	}
	switch old := loadLLMConfig(dir); {
	case key != "":
		if sec == nil {
			return errors.New("没有系统钥匙串，无法保存 key")
		}
		if err := sec.Set(llmKeyID, key); err != nil {
			return err
		}
	case c.BaseURL != old.BaseURL:
		if err := forgetLLMKey(sec); err != nil {
			return err
		}
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	b, err := json.Marshal(c)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "llm.json"), b, 0o600)
}

// resetLLM goes back to the built-in service.
func resetLLM(dir string, sec mailbox.Secrets) error {
	if err := forgetLLMKey(sec); err != nil {
		return err
	}
	if err := os.Remove(filepath.Join(dir, "llm.json")); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// cliLLM is `zmail llm`. It never prints the key, and takes it from stdin
// only, never from the command line, where other programs could read it.
func cliLLM(dir string, sec mailbox.Secrets, args []string, in io.Reader, out io.Writer) error {
	const usage = "zmail llm show [--json] | set [--base-url URL] [--model M] [--key-stdin] | reset"
	if len(args) == 0 {
		return fmt.Errorf("llm needs show, set or reset: %s", usage)
	}
	fs := flag.NewFlagSet("llm "+args[0], flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "print JSON")
	base := fs.String("base-url", "", "the service's OpenAI-compatible address; empty for the built-in")
	model := fs.String("model", "", "the model's name; empty for the built-in")
	keyStdin := fs.Bool("key-stdin", false, "read the API key from stdin")
	words, err := parseFlags(fs, args[1:])
	if err != nil {
		return err
	}
	if len(words) > 0 {
		return fmt.Errorf("unexpected positional argument %q: usage: %s", words[0], usage)
	}

	switch args[0] {
	case "show":
	case "reset":
		if err := resetLLM(dir, sec); err != nil {
			return err
		}
	case "set":
		c, changed := loadLLMConfig(dir), false
		fs.Visit(func(f *flag.Flag) {
			switch f.Name {
			case "base-url":
				c.BaseURL, changed = *base, true
			case "model":
				c.Model, changed = *model, true
			}
		})
		key := ""
		if *keyStdin {
			if key, err = readSecretLine(in); err != nil {
				return err
			}
			if key == "" {
				return errors.New("--key-stdin: no key on stdin")
			}
			changed = true
		}
		if !changed {
			return fmt.Errorf("nothing to set: %s", usage)
		}
		if err := setLLM(dir, sec, c, key); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown llm command %q: %s", args[0], usage)
	}

	s := resolveLLM(dir, sec)
	if *asJSON {
		return json.NewEncoder(out).Encode(map[string]string{"base_url": s.Base, "model": s.Model, "key": s.KeySource})
	}
	key := map[string]string{"builtin": "built-in free key", "custom": "yours, in the keychain", "env": "$ZMAIL_LLM_KEY"}[s.KeySource]
	_, err = fmt.Fprintf(out, "base-url  %s\nmodel     %s\nkey       %s\n", s.Base, s.Model, key)
	return err
}
