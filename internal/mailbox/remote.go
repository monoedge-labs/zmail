// Package mailbox talks to mail servers and keeps the local store in step.
// Only reading, flagging, filing and archiving exist here: nothing in it
// sends or deletes mail.
package mailbox

import (
	"regexp"
	"strings"
	"time"
)

// Folder is a mailbox of an account on its server.
type Folder struct {
	Name string
	Role string // store.Role*, or RoleSkip for folders that are not synced
}

// RoleSkip marks folders that are never synced: trash, junk, drafts and
// the virtual views some servers list.
const RoleSkip = "skip"

// Header is one message as a server describes it.
type Header struct {
	UID       uint32
	MessageID string
	Subject   string
	FromName  string
	FromAddr  string
	Date      time.Time
	Seen      bool
	Body      string // as Markdown, cut to a few tens of KB
	ListUnsub bool   // has a List-Unsubscribe header
}

// Remote is one account's mail server.
type Remote interface {
	Folders() ([]Folder, error)
	// Fetch returns up to limit messages of a folder: the newest ones when
	// afterUID is 0, else those with a higher UID.
	Fetch(folder string, afterUID uint32, limit int) ([]Header, error)
	// Flags returns whether each of a folder's newest limit messages is read.
	Flags(folder string, limit int) (map[uint32]bool, error)
	SetSeen(folder string, uids []uint32, seen bool) error
	// Move files messages in dest, creating it when missing, and returns the
	// UIDs they got there (old UID to new) when the server tells.
	Move(folder string, uids []uint32, dest string) (map[uint32]uint32, error)
	Close() error
}

var rePrefix = regexp.MustCompile(`(?i)^\s*((re|fw|fwd|回复|答复|转发)\s*[:：]\s*)+`)

// ThreadKey groups a conversation by its subject without reply prefixes.
func ThreadKey(subject string) string {
	return strings.ToLower(strings.TrimSpace(rePrefix.ReplaceAllString(subject, "")))
}

var automated = regexp.MustCompile(`(?i)(^|[._+-])(no-?reply|do-?not-?reply|notifications?|alerts?|mailer-daemon|postmaster|bounces?|billing|receipts?|invoice)([._+-]|$)`)

// Categorize puts a message in one of the inbox's groups from what is
// known without reading it: a newsletter has a List-Unsubscribe header,
// automated senders write from addresses made for it.
func Categorize(fromAddr string, listUnsub bool) string {
	if listUnsub {
		return "newsletter"
	}
	local, _, _ := strings.Cut(fromAddr, "@")
	if automated.MatchString(local) {
		return "notification"
	}
	return "person"
}

var space = regexp.MustCompile(`\s+`)

// Snippet is the first characters of a body on one line.
func Snippet(body string, n int) string {
	s := strings.TrimSpace(space.ReplaceAllString(body, " "))
	r := []rune(s)
	if len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}
