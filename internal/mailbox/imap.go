package mailbox

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"html"
	"io"
	"net"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-message"
	_ "github.com/emersion/go-message/charset" // GBK, GB18030 and the other legacy charsets of Chinese mail
	"github.com/emersion/go-message/mail"
)

const maxFetchedBody = 128 << 10 // bytes of each message read from the server

// IMAP is a Remote over one IMAP connection, opened on first use and
// opened again after a failure.
type IMAP struct {
	host, user, pass string
	token            func() (string, error) // an OAuth access token, in place of pass

	mu sync.Mutex
	c  *imapclient.Client
}

// NewIMAP returns a Remote for host ("imap.example.com:993", TLS) and a login.
func NewIMAP(host, user, pass string) *IMAP { return &IMAP{host: host, user: user, pass: pass} }

func (r *IMAP) do(f func(c *imapclient.Client) error) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.c == nil {
		c, err := dialTLS(r.host)
		if err != nil {
			return fmt.Errorf("connect %s: %w", r.host, err)
		}
		if r.token != nil {
			var tok string
			if tok, err = r.token(); err == nil {
				err = c.Authenticate(xoauth2{r.user, tok})
			}
		} else {
			err = c.Login(r.user, r.pass).Wait()
		}
		if err != nil {
			c.Close()
			return fmt.Errorf("login %s: %w", r.user, err)
		}
		r.c = c
	}
	if err := f(r.c); err != nil {
		r.c.Close() // a failed command may leave the connection unusable
		r.c = nil
		return err
	}
	return nil
}

// idleTimeout fails a read when the server sends nothing for this long, so a
// server that stops answering ends a sync with an error instead of hanging it.
var idleTimeout = 90 * time.Second

// idleConn fails a read after idleTimeout of silence. With a log, it writes
// what the server says in full and, of what the client says, only the first
// two words of each line (the tag and the command), so no password or token
// reaches the log, a password sent as a literal included.
type idleConn struct {
	net.Conn
	log io.Writer
}

func (c idleConn) Read(b []byte) (int, error) {
	c.SetReadDeadline(time.Now().Add(idleTimeout))
	n, err := c.Conn.Read(b)
	if c.log != nil {
		fmt.Fprintf(c.log, "S: %s", b[:n])
		if err != nil {
			fmt.Fprintf(c.log, "S! %v\n", err)
		}
	}
	return n, err
}

func (c idleConn) Write(b []byte) (int, error) {
	if c.log != nil {
		for _, line := range strings.Split(strings.TrimRight(string(b), "\r\n"), "\n") {
			if f := strings.Fields(line); len(f) >= 2 && imapCommand.MatchString(f[1]) {
				fmt.Fprintf(c.log, "C: %s %s …\n", f[0], f[1])
			} else {
				fmt.Fprintln(c.log, "C: (data)")
			}
		}
	}
	return c.Conn.Write(b)
}

var imapCommand = regexp.MustCompile(`^[A-Z]{2,12}$`)

// dialTLS connects as imapclient.DialTLS does, with the idle timeout, and
// logs the session to stderr when $ZMAIL_IMAP_DEBUG is set.
func dialTLS(addr string) (*imapclient.Client, error) {
	host, _, _ := net.SplitHostPort(addr)
	conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 30 * time.Second}, "tcp", addr,
		&tls.Config{ServerName: host, NextProtos: []string{"imap"}})
	if err != nil {
		return nil, err
	}
	ic := idleConn{Conn: conn}
	if os.Getenv("ZMAIL_IMAP_DEBUG") != "" {
		ic.log = os.Stderr
	}
	c := imapclient.New(ic, nil)
	// QQ Mail drops what a client sends before its greeting, and a LOGIN
	// sent that early never gets an answer: speak only after it.
	if err := c.WaitGreeting(); err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}

func (r *IMAP) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.c == nil {
		return nil
	}
	err := r.c.Close()
	r.c = nil
	return err
}

func (r *IMAP) Folders() (out []Folder, err error) {
	err = r.do(func(c *imapclient.Client) error {
		var opts *imap.ListOptions
		if c.Caps().Has(imap.CapSpecialUse) {
			opts = &imap.ListOptions{ReturnSpecialUse: true}
		}
		list, err := c.List("", "*", opts).Collect()
		if err != nil {
			return err
		}
		for _, l := range list {
			out = append(out, Folder{Name: l.Mailbox, Role: folderRole(l.Mailbox, l.Attrs)})
		}
		return nil
	})
	return out, err
}

// folderRole decides what a folder is for from its special-use attributes,
// and from its name for servers (QQ's, among others) that give none.
func folderRole(name string, attrs []imap.MailboxAttr) string {
	if strings.EqualFold(name, "INBOX") {
		return "inbox"
	}
	for _, a := range attrs {
		switch a {
		case imap.MailboxAttrNoSelect, imap.MailboxAttrNonExistent,
			imap.MailboxAttrTrash, imap.MailboxAttrJunk, imap.MailboxAttrDrafts,
			imap.MailboxAttrFlagged, imap.MailboxAttrImportant:
			return RoleSkip
		case imap.MailboxAttrSent:
			return "sent"
		case imap.MailboxAttrArchive, imap.MailboxAttrAll:
			return "archive"
		}
	}
	n := strings.ToLower(name)
	has := func(words ...string) bool {
		for _, w := range words {
			if strings.Contains(n, w) {
				return true
			}
		}
		return false
	}
	switch {
	case has("draft", "草稿", "trash", "deleted", "已删除", "废纸篓", "junk", "spam", "垃圾"):
		return RoleSkip
	case has("sent", "已发送"):
		return "sent"
	case has("archive", "归档"):
		return "archive"
	}
	return "other"
}

func (r *IMAP) Fetch(folder string, afterUID uint32, limit int) (out []Header, err error) {
	err = r.do(func(c *imapclient.Client) error {
		sel, err := c.Select(folder, &imap.SelectOptions{ReadOnly: true}).Wait()
		if err != nil {
			return err
		}
		if sel.NumMessages == 0 {
			return nil
		}
		var set imap.NumSet
		if afterUID == 0 {
			start := uint32(1)
			if sel.NumMessages > uint32(limit) {
				start = sel.NumMessages - uint32(limit) + 1
			}
			var s imap.SeqSet
			s.AddRange(start, sel.NumMessages)
			set = s
		} else {
			var s imap.UIDSet
			s.AddRange(imap.UID(afterUID+1), 0) // 0 is "*"; the server may still return the last message
			set = s
		}
		msgs, err := c.Fetch(set, &imap.FetchOptions{
			UID: true, Flags: true, Envelope: true, InternalDate: true,
			BodySection: []*imap.FetchItemBodySection{{Peek: true, Partial: &imap.SectionPartial{Offset: 0, Size: maxFetchedBody}}},
		}).Collect()
		if err != nil {
			return err
		}
		for _, m := range msgs {
			if m.Envelope == nil || (afterUID > 0 && uint32(m.UID) <= afterUID) {
				continue
			}
			h := Header{UID: uint32(m.UID), MessageID: m.Envelope.MessageID, Subject: m.Envelope.Subject, Date: m.Envelope.Date}
			if h.Date.IsZero() {
				h.Date = m.InternalDate
			}
			if len(m.Envelope.From) > 0 {
				h.FromName, h.FromAddr = m.Envelope.From[0].Name, m.Envelope.From[0].Addr()
			}
			for _, f := range m.Flags {
				if f == imap.FlagSeen {
					h.Seen = true
				}
			}
			if len(m.BodySection) > 0 {
				h.Body, h.ListUnsub = parseBody(m.BodySection[0].Bytes)
			}
			out = append(out, h)
		}
		if len(out) > limit {
			out = out[len(out)-limit:]
		}
		return nil
	})
	return out, err
}

func (r *IMAP) Flags(folder string, limit int) (out map[uint32]bool, err error) {
	err = r.do(func(c *imapclient.Client) error {
		sel, err := c.Select(folder, &imap.SelectOptions{ReadOnly: true}).Wait()
		if err != nil || sel.NumMessages == 0 {
			return err
		}
		start := uint32(1)
		if sel.NumMessages > uint32(limit) {
			start = sel.NumMessages - uint32(limit) + 1
		}
		var set imap.SeqSet
		set.AddRange(start, sel.NumMessages)
		msgs, err := c.Fetch(set, &imap.FetchOptions{UID: true, Flags: true}).Collect()
		if err != nil {
			return err
		}
		out = make(map[uint32]bool, len(msgs))
		for _, m := range msgs {
			seen := false
			for _, f := range m.Flags {
				seen = seen || f == imap.FlagSeen
			}
			out[uint32(m.UID)] = seen
		}
		return nil
	})
	return out, err
}

func uidSet(uids []uint32) imap.UIDSet {
	var s imap.UIDSet
	for _, u := range uids {
		s.AddNum(imap.UID(u))
	}
	return s
}

func (r *IMAP) SetSeen(folder string, uids []uint32, seen bool) error {
	return r.do(func(c *imapclient.Client) error {
		if _, err := c.Select(folder, nil).Wait(); err != nil {
			return err
		}
		op := imap.StoreFlagsAdd
		if !seen {
			op = imap.StoreFlagsDel
		}
		return c.Store(uidSet(uids), &imap.StoreFlags{Op: op, Silent: true, Flags: []imap.Flag{imap.FlagSeen}}, nil).Close()
	})
}

func (r *IMAP) Move(folder string, uids []uint32, dest string) (mapped map[uint32]uint32, err error) {
	err = r.do(func(c *imapclient.Client) error {
		if found, err := c.List("", dest, nil).Collect(); err != nil {
			return err
		} else if len(found) == 0 {
			if err := c.Create(dest, nil).Wait(); err != nil {
				return fmt.Errorf("create %q: %w", dest, err)
			}
		}
		if _, err := c.Select(folder, nil).Wait(); err != nil {
			return err
		}
		md, err := c.Move(uidSet(uids), dest).Wait()
		if err != nil {
			return err
		}
		src, ok1 := md.SourceUIDs.(imap.UIDSet)
		dst, ok2 := md.DestUIDs.(imap.UIDSet)
		if ok1 && ok2 {
			s, _ := src.Nums()
			d, _ := dst.Nums()
			if len(s) == len(d) {
				mapped = make(map[uint32]uint32, len(s))
				for i := range s {
					mapped[uint32(s[i])] = uint32(d[i])
				}
			}
		}
		return nil
	})
	return mapped, err
}

// parseBody returns the text of a message cut off after its first parts: its
// text/plain part, or its text/html one with the tags taken out.
func parseBody(raw []byte) (text string, listUnsub bool) {
	mr, err := mail.CreateReader(bytes.NewReader(raw))
	if err != nil && !message.IsUnknownCharset(err) || mr == nil {
		return "", false
	}
	listUnsub = mr.Header.Get("List-Unsubscribe") != ""
	var plain, markup string
	for {
		p, err := mr.NextPart()
		if err != nil { // io.EOF, or the cut message's end
			break
		}
		h, ok := p.Header.(*mail.InlineHeader)
		if !ok {
			continue
		}
		ct, _, _ := h.ContentType()
		b, _ := io.ReadAll(io.LimitReader(p.Body, 64<<10))
		switch ct {
		case "text/plain":
			if plain == "" {
				plain = string(b)
			}
		case "text/html":
			if markup == "" {
				markup = string(b)
			}
		}
	}
	if strings.TrimSpace(plain) != "" {
		return strings.TrimSpace(plain), listUnsub
	}
	return htmlText(markup), listUnsub
}

var (
	reBlocks = regexp.MustCompile(`(?is)<(style|script|head)[^>]*>.*?</(style|script|head)>`)
	reBreaks = regexp.MustCompile(`(?i)<(br|/p|/div|/tr|/li|/h[1-6])[^>]*>`)
	reTags   = regexp.MustCompile(`(?s)<[^>]*>`)
	reBlank  = regexp.MustCompile(`\n{3,}`)
)

func htmlText(s string) string {
	s = reBlocks.ReplaceAllString(s, "")
	s = reBreaks.ReplaceAllString(s, "\n")
	s = html.UnescapeString(reTags.ReplaceAllString(s, ""))
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSpace(l)
	}
	return strings.TrimSpace(reBlank.ReplaceAllString(strings.Join(lines, "\n"), "\n\n"))
}
