package main

import (
	"bufio"
	"cmp"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"golang.org/x/sys/unix"

	"github.com/monoedge-labs/zmail/internal/mailbox"
	"github.com/monoedge-labs/zmail/internal/store"
)

// testDial replaces the connection to the server, in tests.
var testDial func(store.Account, string) mailbox.Remote

const cliUsage = `zmail — a local mail client for agents

  zmail                                  open the window
  zmail account list [--json]            the accounts set up
  zmail account add --provider P --email ADDR [--json]
                                         add an account (P: google, aliyun, qq, icloud);
                                         the app password comes from stdin or $ZMAIL_PASSWORD
  zmail account add --provider google --email ADDR --oauth --client-id ID
                                         sign in through the browser (Google Workspace);
                                         the client secret comes from $ZMAIL_CLIENT_SECRET,
                                         or is asked for without echo
  zmail account remove --email ADDR      forget an account and its password here
  zmail sync                             fetch new mail now
  zmail connect [--json]                 where and how an agent reaches the MCP port

The password is never taken from the command line, where other programs
could read it. The mail of the server is never changed by these commands.`

func configDir() (string, error) {
	d, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(d, "Zmail"), nil
}

// runCLI handles the subcommands, and reports whether args held one.
func runCLI(args []string) bool {
	if len(args) == 0 {
		return false
	}
	switch args[0] {
	case "account", "sync", "connect", "help", "-h", "--help":
	default:
		return false
	}
	if err := cli(args, os.Stdin, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "zmail:", err)
		os.Exit(1)
	}
	return true
}

func cli(args []string, in io.Reader, out io.Writer) error {
	dir, err := configDir()
	if err != nil {
		return err
	}
	return cliIn(dir, mailbox.Secrets(keychain{}), args, in, out)
}

func cliIn(dir string, sec mailbox.Secrets, args []string, in io.Reader, out io.Writer) error {
	switch args[0] {
	case "help", "-h", "--help":
		fmt.Fprintln(out, cliUsage)
		return nil
	case "connect":
		return cliConnect(dir, args[1:], out)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	st, err := store.Open(filepath.Join(dir, "zmail.db"))
	if err != nil {
		return err
	}
	defer st.Close()
	svc := mailbox.New(st, sec)
	if testDial != nil {
		svc.Dial = testDial
	}

	if args[0] == "sync" {
		return svc.SyncAll()
	}
	if len(args) < 2 {
		return fmt.Errorf("account needs list, add or remove\n\n%s", cliUsage)
	}
	fs := flag.NewFlagSet("account "+args[1], flag.ContinueOnError)
	provider := fs.String("provider", "", "google, aliyun, qq or icloud")
	email := fs.String("email", "", "the address")
	asJSON := fs.Bool("json", false, "print JSON")
	oauth := fs.Bool("oauth", false, "google: sign in through the browser (Workspace accounts)")
	clientID := fs.String("client-id", "", "google: OAuth client ID (or $ZMAIL_CLIENT_ID)")
	if err := fs.Parse(args[2:]); err != nil {
		return err
	}
	show := func(v any, text string) error {
		if *asJSON {
			return json.NewEncoder(out).Encode(v)
		}
		_, err := fmt.Fprintln(out, text)
		return err
	}

	switch args[1] {
	case "list":
		accs, err := st.Accounts()
		if err != nil {
			return err
		}
		unread, _ := st.UnreadCounts()
		type row struct {
			Provider string `json:"provider"`
			Email    string `json:"email"`
			Host     string `json:"host"`
			Unread   int    `json:"unread"`
		}
		rows := []row{}
		var lines []string
		for _, a := range accs {
			rows = append(rows, row{a.Provider, a.Email, a.Host, unread[a.ID]})
			lines = append(lines, fmt.Sprintf("%-8s %-32s %d unread", a.Provider, a.Email, unread[a.ID]))
		}
		if len(lines) == 0 {
			lines = []string{"no accounts"}
		}
		return show(rows, strings.Join(lines, "\n"))

	case "add":
		if *provider == "" || *email == "" {
			return fmt.Errorf("account add needs --provider and --email")
		}
		secret := strings.TrimSpace(os.Getenv("ZMAIL_PASSWORD"))
		if *oauth {
			if *provider != "google" {
				return fmt.Errorf("--oauth is for --provider google")
			}
			id := cmp.Or(*clientID, os.Getenv("ZMAIL_CLIENT_ID"))
			if id == "" {
				return fmt.Errorf("--oauth needs the OAuth client ID: --client-id or $ZMAIL_CLIENT_ID")
			}
			cs := os.Getenv("ZMAIL_CLIENT_SECRET")
			if cs == "" {
				fmt.Fprint(os.Stderr, "OAuth client secret: ")
				var err error
				cs, err = readSecretLine(in)
				fmt.Fprintln(os.Stderr)
				if err != nil {
					return err
				}
			}
			if cs == "" {
				return fmt.Errorf("--oauth needs the OAuth client secret, on stdin or in $ZMAIL_CLIENT_SECRET")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			var err error
			secret, err = googleLogin(ctx, id, cs, *email, func(u string) {
				fmt.Fprintf(out, "opening the browser to sign in; if it does not open, visit:\n%s\n", u)
				openURL(u)
			})
			if err != nil {
				return err
			}
		}
		if secret == "" {
			b, err := io.ReadAll(in)
			if err != nil {
				return err
			}
			secret = strings.TrimSpace(string(b))
		}
		if secret == "" {
			return fmt.Errorf("give the app password on stdin or in $ZMAIL_PASSWORD")
		}
		acc, err := svc.AddAccount(*provider, *email, secret)
		if err != nil {
			return err
		}
		syncErr := svc.SyncAccount(acc)
		type result struct {
			Account string `json:"account"`
			Synced  bool   `json:"synced"`
			Error   string `json:"error,omitempty"`
		}
		r := result{Account: acc.Email, Synced: syncErr == nil}
		text := "added " + acc.Email
		if syncErr != nil {
			r.Error = syncErr.Error()
			text += " (first sync failed: " + syncErr.Error() + ")"
		}
		return show(r, text)

	case "remove":
		if *email == "" {
			return fmt.Errorf("account remove needs --email")
		}
		accs, err := st.Accounts()
		if err != nil {
			return err
		}
		for _, a := range accs {
			if strings.EqualFold(a.Email, *email) {
				if err := svc.RemoveAccount(a.ID); err != nil {
					return err
				}
				return show(map[string]string{"removed": a.Email}, "removed "+a.Email)
			}
		}
		return fmt.Errorf("no account %s", *email)
	}
	return fmt.Errorf("unknown account command %q\n\n%s", args[1], cliUsage)
}

// readSecretLine reads one line from in, with the terminal's echo off when in
// is a terminal, so that a pasted secret is not shown.
// ponytail: TIOCGETA is the macOS/BSD request; Linux would need TCGETS.
func readSecretLine(in io.Reader) (string, error) {
	if f, ok := in.(*os.File); ok {
		fd := int(f.Fd())
		if t, err := unix.IoctlGetTermios(fd, unix.TIOCGETA); err == nil {
			quiet := *t
			quiet.Lflag &^= unix.ECHO
			if unix.IoctlSetTermios(fd, unix.TIOCSETA, &quiet) == nil {
				defer unix.IoctlSetTermios(fd, unix.TIOCSETA, t)
			}
		}
	}
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

// googleLogin and openURL are variables so that tests need no browser.
var googleLogin = mailbox.GoogleLogin

func openURL(u string) {
	if runtime.GOOS == "darwin" {
		exec.Command("open", u).Start()
	}
}

// cliConnect says how an agent reaches the MCP port of the window.
func cliConnect(dir string, args []string, out io.Writer) error {
	fs := flag.NewFlagSet("connect", flag.ContinueOnError)
	addr := fs.String("addr", "127.0.0.1:8765", "where the window serves agents")
	asJSON := fs.Bool("json", false, "print JSON")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	token, err := loadToken(dir)
	if err != nil {
		return err
	}
	url := "http://" + *addr + "/mcp"
	cmd := fmt.Sprintf(`claude mcp add --transport http zmail %s --header "Authorization: Bearer %s"`, url, token)
	if *asJSON {
		return json.NewEncoder(out).Encode(map[string]string{"url": url, "token": token, "header": "Authorization: Bearer " + token, "claude_code": cmd})
	}
	fmt.Fprintf(out, "url    %s\ntoken  %s\n\n%s\n", url, token, cmd)
	return nil
}
