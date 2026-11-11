package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/monoedge-labs/zmail/internal/guard"
	"github.com/monoedge-labs/zmail/internal/mailbox"
	"github.com/monoedge-labs/zmail/internal/redact"
	"github.com/monoedge-labs/zmail/internal/store"
)

// cliWho is whoever runs these commands: an agent, as far as the log and the
// statistics are concerned. Everything goes through the Service, which
// checks the server first and writes the operation log.
var cliWho = mailbox.Who{Actor: store.ActorAgent, Client: "cli"}

const (
	defaultLimit = 20
	maxLimit     = 100
	bodyRunes    = 6000
)

type mailItem struct {
	ID       int64  `json:"id"`
	Account  string `json:"account"`
	Folder   string `json:"folder"`
	From     string `json:"from"`
	Subject  string `json:"subject"`
	Snippet  string `json:"snippet"`
	Date     string `json:"date"`
	Unread   bool   `json:"unread"`
	Category string `json:"category"`
	Body     string `json:"body,omitempty"`
	Redacted int    `json:"redacted,omitempty"` // values hidden in this message, when the user asked for that
}

type senderItem struct {
	Name  string `json:"name"`
	Addr  string `json:"addr"`
	Count int    `json:"count"`
}

type statsOut struct {
	Days            int            `json:"days"`
	Received        int            `json:"received"`
	Sent            int            `json:"sent"`
	Unread          int            `json:"unread_in_inbox"`
	ByAccount       map[string]int `json:"received_by_account"`
	TopSenders      []senderItem   `json:"top_senders"`
	HandledByAgents int            `json:"handled_by_agents"`
	HandledByUser   int            `json:"handled_by_user"`
}

var flagsOnly = map[string]string{
	"search":  "[--query Q] [--account ADDR] [--folder F] [--category C] [--unread] [--since-days N] [--limit N] [--json]",
	"folders": "[--json]",
	"stats":   "[--days N] [--json]",
}

// parseFlags parses flags that may stand among the positional arguments,
// and returns those.
func parseFlags(fs *flag.FlagSet, args []string) ([]string, error) {
	var rest []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		if fs.NArg() == 0 {
			return rest, nil
		}
		rest, args = append(rest, fs.Arg(0)), fs.Args()[1:]
	}
}

func parseIDs(words []string, one bool) ([]int64, error) {
	if len(words) == 0 || one && len(words) != 1 {
		if one {
			return nil, errors.New("give exactly one message ID")
		}
		return nil, errors.New("give at least one message ID")
	}
	ids := make([]int64, len(words))
	for i, w := range words {
		id, err := strconv.ParseInt(w, 10, 64)
		if err != nil || id <= 0 {
			return nil, fmt.Errorf("%q is not a message ID", w)
		}
		ids[i] = id
	}
	return ids, nil
}

func cliMail(svc *mailbox.Service, priv privacy, dir string, args []string, out io.Writer) error {
	fs := flag.NewFlagSet(args[0], flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "print JSON")
	var (
		query, account, folder, category *string
		unread, create                   *bool
		sinceDays, limit, days           *int
	)
	switch args[0] {
	case "search":
		query, account = fs.String("query", "", "text to find in subject, sender or body"), fs.String("account", "", "an account's address")
		folder, category = fs.String("folder", "", "a folder name, as folders gives"), fs.String("category", "", "person, notification or newsletter")
		unread = fs.Bool("unread", false, "unread mail only")
		sinceDays = fs.Int("since-days", 0, "only mail from the last N days")
		limit = fs.Int("limit", defaultLimit, "at most this many, 100 at most")
	case "stats":
		days = fs.Int("days", 7, "the last N days")
	case "mark-read":
		unread = fs.Bool("unread", false, "mark unread instead")
	case "move":
		folder = fs.String("folder", "", "the destination: a name from folders, or junk for the account's junk folder")
		create = fs.Bool("create", false, "make the folder when the account has none of that name")
	}
	words, err := parseFlags(fs, args[1:])
	if err != nil {
		return err
	}
	// These take flags only: a stray word would silently widen the query.
	if usage, ok := flagsOnly[args[0]]; ok && len(words) > 0 {
		return fmt.Errorf("unexpected positional argument %q: usage: zmail %s %s", words[0], args[0], usage)
	}
	show := func(v any, text string) error {
		if *asJSON {
			return json.NewEncoder(out).Encode(v)
		}
		_, err := fmt.Fprintln(out, strings.TrimRight(text, "\n"))
		return err
	}
	st := svc.Store
	// The commands that only read log themselves here; those that change mail
	// are logged by the Service, with what they did.
	logRead := func(tool string, err error) {
		st.LogOp(store.Op{Actor: cliWho.Actor, Client: cliWho.Client, Tool: tool, OK: err == nil})
	}
	emails := func() map[string]string {
		accs, _ := st.Accounts()
		byID := map[string]string{}
		for _, a := range accs {
			byID[a.ID] = a.Email
		}
		return byID
	}
	done := func(n int, verb string) error {
		return show(map[string]int{"affected": n}, fmt.Sprintf("%s %d messages", verb, n))
	}

	switch args[0] {
	case "search":
		q := store.Query{Text: *query, Folder: *folder, Category: *category, Unread: *unread, Limit: *limit}
		if q.Limit <= 0 {
			q.Limit = defaultLimit
		}
		q.Limit = min(q.Limit, maxLimit)
		if *sinceDays > 0 {
			q.Since = time.Now().AddDate(0, 0, -*sinceDays)
		}
		byID := emails()
		if *account != "" {
			for id, email := range byID {
				if strings.EqualFold(email, *account) {
					q.AccountID = id
				}
			}
			if q.AccountID == "" {
				err := fmt.Errorf("no account %q", *account)
				logRead("search_mail", err)
				return err
			}
		}
		ms, err := st.Search(q)
		logRead("search_mail", err)
		if err != nil {
			return err
		}
		hide, err := hider(priv, dir, st)
		if err != nil {
			return err
		}
		items := mailItems(ms, byID, false, hide)
		return show(map[string]any{"messages": items}, mailLines(items, false))

	case "read":
		ids, err := parseIDs(words, true)
		if err != nil {
			return err
		}
		ms, err := st.Thread(ids[0])
		logRead("read_thread", err)
		if err != nil {
			return fmt.Errorf("message %d: %w", ids[0], err)
		}
		hide, err := hider(priv, dir, st)
		if err != nil {
			return err
		}
		items := mailItems(ms, emails(), true, hide)
		return show(map[string]any{"subject": ms[0].Subject, "messages": items}, mailLines(items, true))

	case "folders":
		accs, err := st.Accounts()
		out := map[string][]string{}
		roles := map[string]map[string]string{}
		var lines []string
		for _, a := range accs {
			fs, _ := st.Folders(a.ID)
			names, label := []string{}, []string{}
			roles[a.Email] = map[string]string{}
			for name, role := range fs {
				if role != mailbox.RoleSkip {
					names = append(names, name)
					roles[a.Email][name] = role
				}
			}
			slices.Sort(names)
			for _, name := range names {
				if r := roles[a.Email][name]; r != store.RoleOther {
					name += " (" + r + ")"
				}
				label = append(label, name)
			}
			out[a.Email] = names
			lines = append(lines, a.Email+": "+strings.Join(label, ", "))
		}
		logRead("list_folders", err)
		if err != nil {
			return err
		}
		return show(map[string]any{"folders": out, "roles": roles}, strings.Join(lines, "\n"))

	case "stats":
		d := *days
		if d <= 0 {
			d = 7
		}
		s, err := st.Stats(time.Now().AddDate(0, 0, -d))
		logRead("get_stats", err)
		if err != nil {
			return err
		}
		o := statsOut{Days: d, Received: s.Received, Sent: s.Sent, Unread: s.Unread, ByAccount: s.ByAccount,
			TopSenders: []senderItem{}, HandledByAgents: s.AgentHandled, HandledByUser: s.MeHandled}
		for _, c := range s.TopSenders {
			o.TopSenders = append(o.TopSenders, senderItem(c))
		}
		text := fmt.Sprintf("last %d days: %d received, %d sent, %d unread in inbox; handled by agents %d, by you %d",
			d, o.Received, o.Sent, o.Unread, o.HandledByAgents, o.HandledByUser)
		return show(o, text)

	case "mark-read":
		ids, err := parseIDs(words, false)
		if err != nil {
			return err
		}
		if err := svc.MarkRead(cliWho, ids, !*unread); err != nil {
			return err
		}
		verb := "marked read"
		if *unread {
			verb = "marked unread"
		}
		return done(len(ids), verb)

	case "archive":
		ids, err := parseIDs(words, true)
		if err != nil {
			return err
		}
		ms, err := st.Thread(ids[0])
		if err != nil {
			return fmt.Errorf("message %d: %w", ids[0], err)
		}
		var todo []int64
		for _, m := range ms {
			if m.Role != store.RoleArchive && m.Role != store.RoleSent {
				todo = append(todo, m.ID)
			}
		}
		if err := svc.Archive(cliWho, todo); err != nil {
			return err
		}
		return done(len(todo), "archived")

	case "move":
		if strings.TrimSpace(*folder) == "" {
			return errors.New("move needs --folder")
		}
		ids, err := parseIDs(words, false)
		if err != nil {
			return err
		}
		// One account's mail goes to that account's folder: a name means a
		// folder the account has, unless --create says to make one.
		var accs []string
		byAcc := map[string][]int64{}
		for _, id := range ids {
			m, err := st.Get(id)
			if err != nil {
				return fmt.Errorf("message %d: %w", id, err)
			}
			if _, ok := byAcc[m.AccountID]; !ok {
				accs = append(accs, m.AccountID)
			}
			byAcc[m.AccountID] = append(byAcc[m.AccountID], id)
		}
		dest := map[string]string{}
		for _, acc := range accs {
			if dest[acc], err = resolveFolder(st, acc, strings.TrimSpace(*folder), *create); err != nil {
				return err
			}
		}
		for _, acc := range accs {
			if err := svc.MoveToFolder(cliWho, byAcc[acc], dest[acc]); err != nil {
				return err
			}
		}
		return done(len(ids), "moved")
	}
	return nil
}

// resolveFolder names the folder of an account that a move to name means:
// the one with that name, in any case, or for "junk" or "spam" the account's
// junk folder, whatever its server calls it. A folder the account lacks is
// an error, as a typo must not make one, unless create is set.
func resolveFolder(st *store.Store, acc, name string, create bool) (string, error) {
	fs, err := st.Folders(acc)
	if err != nil {
		return "", err
	}
	var names []string
	for f, role := range fs {
		if role == mailbox.RoleSkip {
			continue
		}
		names = append(names, f)
		if strings.EqualFold(f, name) {
			return f, nil
		}
	}
	if l := strings.ToLower(name); l == "junk" || l == "spam" {
		for _, f := range names {
			if fs[f] == store.RoleJunk {
				return f, nil
			}
		}
	}
	if _, ok := fs[name]; ok { // Trash, drafts: the service refuses them
		return name, nil
	}
	if create {
		return name, nil
	}
	slices.Sort(names)
	return "", fmt.Errorf("no folder %q: choose one of %s, or add --create to make it", name, strings.Join(names, ", "))
}

// hider is the masker for what the agent reads, nil when the user hides
// nothing. With the local model turned on it reads with the model too, and a
// model that is missing or fails to load is an error: the user asked for
// more than the patterns, and gets none of it silently. What the model finds
// is kept in st, so a text it read before is not read again.
func hider(p privacy, dir string, st *store.Store) (*redact.Masker, error) {
	if !p.Agent {
		return nil, nil
	}
	m := redact.New()
	if p.model() {
		d := modelDir(dir)
		if !guard.Installed(d) {
			return nil, errors.New("the masking model is not downloaded yet (opening Zmail starts it, on the statistics page): wait for it, or turn the model off there")
		}
		g, err := guard.Load(d)
		if err != nil {
			return nil, fmt.Errorf("the masking model did not load: %w", err)
		}
		m.Model = storedModel(st, filepath.Base(d), redact.FromGuard(g))
	}
	return m, nil
}

// maskMargin is how much past the cut of a body is hidden too, so that a
// value the cut runs through is hidden whole before it is cut.
const maskMargin = 256

// mailItems describes messages for the agent. With a masker, the subject,
// snippet and body come with their private values hidden, before the body is
// cut; the sender stays, as the agent needs it to tell mail apart.
func mailItems(ms []store.Message, accounts map[string]string, withBody bool, hide *redact.Masker) []mailItem {
	out := make([]mailItem, 0, len(ms))
	for _, m := range ms {
		from := m.FromAddr
		if m.FromName != "" {
			from = fmt.Sprintf("%s <%s>", m.FromName, m.FromAddr)
		}
		before, cut := 0, false
		body := m.Body
		if hide != nil {
			before = hide.Hidden
			if !withBody {
				body = ""
			} else if r := []rune(body); len(r) > bodyRunes+maskMargin {
				body, cut = string(r[:bodyRunes+maskMargin]), true
			}
			t := hide.Texts(m.Subject, m.Snippet, body)
			m.Subject, m.Snippet, body = t[0], t[1], t[2]
		}
		it := mailItem{ID: m.ID, Account: accounts[m.AccountID], Folder: m.Folder, From: from, Subject: m.Subject,
			Snippet: m.Snippet, Date: m.Date.Format(time.RFC3339), Unread: !m.Seen, Category: m.Category}
		if withBody {
			it.Body = truncate(body, bodyRunes)
			if cut && !strings.HasSuffix(it.Body, "[cut]") {
				it.Body += "\n[cut]"
			}
		}
		if hide != nil {
			it.Redacted = hide.Hidden - before
		}
		out = append(out, it)
	}
	return out
}

func truncate(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "\n[cut]"
	}
	return s
}

func mailLines(items []mailItem, withBody bool) string {
	var b strings.Builder
	for _, it := range items {
		mark := " "
		if it.Unread {
			mark = "*"
		}
		fmt.Fprintf(&b, "%s %d  %s  %s  %s  %s\n", mark, it.ID, it.Date, it.Account, it.From, it.Subject)
		if withBody {
			fmt.Fprintf(&b, "\n%s\n\n", it.Body)
		} else if it.Snippet != "" {
			fmt.Fprintf(&b, "    %s\n", it.Snippet)
		}
	}
	if len(items) == 0 {
		return "no messages"
	}
	return b.String()
}
