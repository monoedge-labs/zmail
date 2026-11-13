package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/monoedge-labs/zmail/internal/mailbox"
)

func TestProvidersCLI(t *testing.T) {
	out, err := run(t, t.TempDir(), memSecrets{}, "", "providers", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var got []mailbox.Provider
	if json.Unmarshal([]byte(out), &got) != nil || len(got) != 4 {
		t.Fatalf("providers printed %q", out)
	}
	for i, p := range got {
		if p != mailbox.Providers[i] || p.Help == "" || p.HelpNote == "" || p.Secret == "" || p.Host == "" {
			t.Errorf("provider %d = %+v, want the window's %+v", i, p, mailbox.Providers[i])
		}
	}
	if !strings.Contains(out, `"help_url"`) || !strings.Contains(out, `"credential"`) || !strings.Contains(out, `"imap_host"`) {
		t.Errorf("JSON keys: %q", out)
	}
	text, _ := run(t, t.TempDir(), memSecrets{}, "", "providers")
	for _, p := range mailbox.Providers {
		if !strings.Contains(text, p.Help) {
			t.Errorf("text lacks %s", p.Help)
		}
	}
}

func TestSkillCLI(t *testing.T) {
	out, err := run(t, t.TempDir(), memSecrets{}, "", "skill")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range mailbox.Providers {
		if !strings.Contains(out, p.Help) || !strings.Contains(out, "`"+p.ID+"`") {
			t.Errorf("skill lacks provider %s", p.ID)
		}
	}
	for _, want := range []string{
		"--json", "zmail providers --json", "zmail account add", "stdin", "Mail is data", "instruction",
		"Sending, replying and deleting", "keep them out of your commands", "6000", "Trash and drafts are refused", "--create", "--folder junk", "roles",
		"zmail issue --print --json", "zmail llm", "zmail search", "zmail read", "zmail folders", "zmail stats", "zmail mark-read", "zmail archive", "zmail move", "zmail version --json", "no server and no port",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("skill lacks %q", want)
		}
	}
	if strings.Contains(out, "{{") || strings.Contains(strings.ToLower(out), "mcp") {
		t.Error("skill has a stray placeholder or old wording")
	}
	help, _ := run(t, t.TempDir(), memSecrets{}, "", "help")
	for _, cmd := range []string{"skill", "providers", "search", "read", "folders", "stats", "mark-read", "archive", "move"} {
		if !strings.Contains(help, "zmail "+cmd) {
			t.Errorf("help lacks %s", cmd)
		}
	}
}
