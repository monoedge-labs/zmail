package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/monoedge-labs/zmail/internal/mailbox"
	"github.com/monoedge-labs/zmail/internal/store"
)

func run(t *testing.T, dir string, sec mailbox.Secrets, stdin string, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	err := cliIn(dir, sec, args, strings.NewReader(stdin), &out)
	return out.String(), err
}

func TestAccountCLI(t *testing.T) {
	testDial = func(store.Account, string) mailbox.Remote { return quiet{} }
	t.Cleanup(func() { testDial = nil })
	t.Setenv("ZMAIL_PASSWORD", "")
	dir, sec := t.TempDir(), memSecrets{}

	if _, err := run(t, dir, sec, "", "account", "add", "--provider", "icloud", "--email", "me@icloud.com"); err == nil {
		t.Error("an account without a password must be refused")
	}
	out, err := run(t, dir, sec, "app-pass\n", "account", "add", "--provider", "icloud", "--email", "me@icloud.com", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var added struct {
		Account string
		Synced  bool
	}
	if json.Unmarshal([]byte(out), &added) != nil || added.Account != "me@icloud.com" || !added.Synced {
		t.Fatalf("add printed %q", out)
	}
	if sec["icloud:me@icloud.com"] != "app-pass" {
		t.Error("the password must go to the keychain, trimmed")
	}
	if b, _ := os.ReadFile(dir + "/zmail.db"); strings.Contains(string(b), "app-pass") {
		t.Error("the password must not be in the database")
	}

	out, _ = run(t, dir, sec, "", "account", "list", "--json")
	var rows []struct{ Provider, Email, Host string }
	if json.Unmarshal([]byte(out), &rows) != nil || len(rows) != 1 || rows[0].Host != "imap.mail.me.com:993" {
		t.Fatalf("list printed %q", out)
	}

	if _, err := run(t, dir, sec, "", "account", "add", "--provider", "nope", "--email", "a@b.c"); err == nil {
		t.Error("an unknown provider must be refused")
	}
	if _, err := run(t, dir, sec, "", "account", "remove", "--email", "me@icloud.com"); err != nil {
		t.Fatal(err)
	}
	if _, ok := sec["icloud:me@icloud.com"]; ok {
		t.Error("removing an account must forget its password")
	}
	out, _ = run(t, dir, sec, "", "account", "list")
	if !strings.Contains(out, "no accounts") {
		t.Errorf("list after remove: %q", out)
	}
}

func TestOAuthSecretFromStdin(t *testing.T) {
	testDial = func(store.Account, string) mailbox.Remote { return quiet{} }
	t.Setenv("ZMAIL_CLIENT_SECRET", "")
	var gotID, gotSecret string
	login := googleLogin
	googleLogin = func(_ context.Context, id, secret, _ string, _ func(string)) (string, error) {
		gotID, gotSecret = id, secret
		return `{"refresh_token":"r"}`, nil
	}
	t.Cleanup(func() { testDial, googleLogin = nil, login })
	dir, sec := t.TempDir(), memSecrets{}

	if _, err := run(t, dir, sec, "", "account", "add", "--provider", "google", "--email", "me@example.com", "--oauth", "--client-id", "cid"); err == nil {
		t.Error("an empty client secret must be refused")
	}
	if _, err := run(t, dir, sec, "  s3cret \n", "account", "add", "--provider", "google", "--email", "me@example.com", "--oauth", "--client-id", "cid"); err != nil {
		t.Fatal(err)
	}
	if gotID != "cid" || gotSecret != "s3cret" {
		t.Errorf("login got id %q secret %q", gotID, gotSecret)
	}

	// With no client named, the one zmail is built with signs in; $ZMAIL_CLIENT_ID comes before it.
	t.Setenv("ZMAIL_CLIENT_ID", "")
	if _, err := run(t, dir, sec, "s3cret\n", "account", "add", "--provider", "google", "--email", "two@example.com", "--oauth"); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(mailbox.GoogleClientID, ".apps.googleusercontent.com") || gotID != mailbox.GoogleClientID {
		t.Errorf("login got id %q, want the built-in %q", gotID, mailbox.GoogleClientID)
	}
	t.Setenv("ZMAIL_CLIENT_ID", "envid")
	if _, err := run(t, dir, sec, "s3cret\n", "account", "add", "--provider", "google", "--email", "three@example.com", "--oauth"); err != nil {
		t.Fatal(err)
	}
	if gotID != "envid" {
		t.Errorf("login got id %q, want $ZMAIL_CLIENT_ID", gotID)
	}
}
