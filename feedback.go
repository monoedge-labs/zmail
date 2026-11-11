package main

import (
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/url"
	"runtime"
)

// Feedback goes to the project's GitHub issues. Zmail sends nothing itself:
// it builds the "new issue" page's address, with a template filled in, and
// the browser opens it. The user reads and edits the text there, and
// submits it or not.

const issuesNew = "https://github.com/monoedge-labs/zmail/issues/new"

//go:embed mygo.json
var appManifest []byte

// appVersion is the version the build is made with, from mygo.json.
func appVersion() string {
	var m struct{ Version string }
	if json.Unmarshal(appManifest, &m) != nil || m.Version == "" {
		return "unknown"
	}
	return m.Version
}

// issueDraft is the fields of an issue.
type issueDraft struct{ Title, What, Expected string }

// issueURL is the new-issue page with the draft filled in.
func issueURL(d issueDraft) string {
	if d.What == "" {
		d.What = "<!-- What went wrong? Steps to reproduce help most. -->"
	}
	body := fmt.Sprintf("## What happened\n\n%s\n\n## What you expected\n\n%s\n\n## Environment\n\n- zmail %s\n- %s/%s\n\n"+
		"<!-- Leave mail contents, addresses and passwords out. -->\n", d.What, d.Expected, appVersion(), runtime.GOOS, runtime.GOARCH)
	q := url.Values{"body": {body}}
	if d.Title != "" {
		q.Set("title", d.Title)
	}
	return issuesNew + "?" + q.Encode()
}

// openIssue opens the new-issue page in the browser.
func openIssue(d issueDraft) { openURL(issueURL(d)) }

// cliIssue is `zmail issue`: it opens the issue page, and prints the address
// too, for a terminal that cannot open a browser or an agent that hands it on.
func cliIssue(args []string, out io.Writer) error {
	const usage = "[--title T] [--body TEXT] [--print] [--json]"
	fs := flag.NewFlagSet("issue", flag.ContinueOnError)
	title := fs.String("title", "", "the issue's title")
	body := fs.String("body", "", "what happened, to start the description with")
	printOnly := fs.Bool("print", false, "only print the address; open no browser")
	asJSON := fs.Bool("json", false, "print JSON")
	words, err := parseFlags(fs, args)
	if err != nil {
		return err
	}
	if len(words) > 0 {
		return fmt.Errorf("unexpected positional argument %q: usage: zmail issue %s", words[0], usage)
	}
	u := issueURL(issueDraft{Title: *title, What: *body})
	if !*printOnly {
		openURL(u)
	}
	if *asJSON {
		return json.NewEncoder(out).Encode(map[string]any{"url": u, "opened": !*printOnly})
	}
	_, err = fmt.Fprintln(out, u)
	return err
}
