package mailbox

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/monoedge-labs/zmail/internal/store"
)

// fakeIMAPServer is just enough of a server for the client's sync path:
// greeting, login, select and fetch. msgs[uid] is its answer for that UID;
// "bad" produces the kind of ENVELOPE QQ sends, where an unescaped quote
// inside the Message-ID ends the field early.
type fakeIMAPServer struct {
	msgs map[uint32]fakeMsg
	// order maps sequence numbers to UIDs (1-based, dense).
	order []uint32
	debug bool
}

type fakeMsg struct {
	subject string
	flags   string
	bad     bool // answer with a malformed ENVELOPE
	date    string
	body    string
}

func (s *fakeIMAPServer) serve(conn net.Conn) {
	defer conn.Close()
	w := bufio.NewWriter(conn)
	send := func(format string, args ...any) {
		fmt.Fprintf(w, format+"\r\n", args...)
		w.Flush()
	}
	send(`* OK fake ready`)
	r := bufio.NewReader(conn)
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimSpace(line)
		if s.debug {
			fmt.Println("S<-", line)
		}
		sp := strings.IndexByte(line, ' ')
		if sp < 0 {
			continue
		}
		tag, rest := line[:sp], line[sp+1:]
		isUID := false
		if u, a2, ok := strings.Cut(rest, " "); ok && strings.EqualFold(u, "UID") {
			isUID, rest = true, a2
		}
		verb, arg, _ := strings.Cut(rest, " ")
		switch strings.ToUpper(verb) {
		case "CAPABILITY", "NOOP", "LOGIN", "LOGOUT":
			send(`%s OK done`, tag)
		case "SELECT", "EXAMINE":
			send(`* FLAGS (\Seen \Answered)`)
			send(`* %d EXISTS`, len(s.order))
			send(`* %d RECENT`, len(s.order))
			send(`* OK [UIDVALIDITY 1] uids`)
			send(`* OK [UIDNEXT %d] next`, s.order[len(s.order)-1]+1)
			send(`%s OK [READ-ONLY] selected`, tag)
		case "FETCH":
			setS, items, _ := strings.Cut(arg, " ")
			for _, num := range parseNumSet(setS, len(s.order), s.order) {
				var uid uint32
				seq := num
				if isUID {
					uid = num
					seq = uint32(sort.SearchInts(uintSliceToInts(s.order), int(uid)) + 1)
					if int(seq) > len(s.order) || s.order[seq-1] != uid {
						continue
					}
				} else {
					if seq < 1 || int(seq) > len(s.order) {
						continue
					}
					uid = s.order[seq-1]
				}
				m, ok := s.msgs[uid]
				if !ok {
					continue
				}
				extra := s.items(items, m)
				if extra != "" {
					extra = " " + extra
				}
				send(`* %d FETCH (UID %d FLAGS (%s) INTERNALDATE "%s"%s)`, seq, uid, m.flags, m.date, extra)
			}
			send(`%s OK done`, tag)
		default:
			send(`%s BAD no such command`, tag)
		}
	}
}

func (s *fakeIMAPServer) items(asked string, m fakeMsg) string {
	var parts []string
	if strings.Contains(asked, "ENVELOPE") {
		if m.bad {
			// The bug QQ sends: an unescaped " inside the Message-ID string
			// ends the last envelope field early, and the leftover text breaks
			// the enclosing list.
			parts = append(parts, `ENVELOPE (NIL "`+m.subject+`" ((NIL NIL "x" "x.x.x")) ((NIL NIL "x" "x.x.x")) NIL NIL NIL NIL NIL "<x.x."ax@x.x"@x.x>")`)
		} else {
			parts = append(parts, `ENVELOPE (NIL "`+m.subject+`" (("Lin" NIL "lin" "x.com")) (("Lin" NIL "lin" "x.com")) NIL NIL NIL NIL NIL "<m1@x>")`)
		}
	}
	if strings.Contains(asked, "BODY") && !strings.Contains(asked, "BODYSTRUCTURE") {
		body := "From: lin@x.com\r\nSubject: " + m.subject + "\r\nContent-Type: text/plain\r\n\r\n" + m.body
		parts = append(parts, fmt.Sprintf("BODY[] {%d}\r\n%s", len(body), body))
	}
	return strings.Join(parts, " ")
}

// parseNumSet reads "1", "1:5", "5:*" into numbers. For UID sets max is
// maxUID, for seq sets the message count; order maps seq to uid.
func parseNumSet(setS string, maxSeq int, order []uint32) []uint32 {
	var out []uint32
	maxUID := uint32(0)
	if len(order) > 0 {
		maxUID = order[len(order)-1]
	}
	for _, part := range strings.Split(setS, ",") {
		a, b, hasRange := strings.Cut(part, ":")
		lo := aton(a)
		if !hasRange {
			out = append(out, lo)
			continue
		}
		hi := aton(b)
		if b == "*" {
			hi = maxUID
			if hi == 0 {
				hi = uint32(maxSeq)
			}
		}
		for n := lo; n <= hi; n++ {
			out = append(out, n)
		}
	}
	return out
}

func aton(s string) uint32 {
	n, _ := strconv.Atoi(s)
	return uint32(n)
}

func uintSliceToInts(u []uint32) []int {
	out := make([]int, len(u))
	for i, v := range u {
		out[i] = int(v)
	}
	return out
}

// newFakeIMAP wires an IMAP remote to an in-memory server, one per dial.
func newFakeIMAP(srv *fakeIMAPServer) *IMAP {
	return &IMAP{
		host: "fake:993", user: "u", pass: "p",
		dial: func(addr string) (*imapclient.Client, error) {
			server, client := net.Pipe()
			go srv.serve(server)
			c := imapclient.New(client, nil)
			if err := c.WaitGreeting(); err != nil {
				return nil, err
			}
			return c, nil
		},
	}
}

// TestMalformedEnvelopeSkipsOneMessage reproduces monoedge-labs/zmail#18: a
// server that writes an unescaped quote inside ENVELOPE kills a batch fetch,
// and the sync must keep the other messages instead of failing on the bad
// one every round.
func TestMalformedEnvelopeSkipsOneMessage(t *testing.T) {
	srv := &fakeIMAPServer{
		order: []uint32{101, 102, 103},
		debug: true,
		msgs: map[uint32]fakeMsg{
			101: {subject: "Good one", flags: `\Seen`, date: "18-Nov-2026 10:00:00 +0800", body: "hi"},
			102: {subject: "Broken envelope", flags: `\Seen`, bad: true, date: "18-Nov-2026 10:05:00 +0800"},
			103: {subject: "Good two", flags: ``, date: "18-Nov-2026 10:10:00 +0800", body: "yo"},
		},
	}
	r := newFakeIMAP(srv)
	hs, err := r.Fetch("INBOX", 100, 500)
	if err != nil {
		t.Fatalf("Fetch with one bad message failed the batch: %v", err)
	}
	got := map[uint32]Header{}
	for _, h := range hs {
		got[h.UID] = h
	}
	if !got[101].Seen || got[101].Subject != "Good one" {
		t.Errorf("uid 101 wrong: %+v", got[101])
	}
	if got[103].Subject != "Good two" {
		t.Errorf("uid 103 (after the bad one) missing: %+v", hs)
	}
	if !got[102].Malformed || got[102].FetchErr == "" {
		t.Errorf("uid 102 not a tombstone: %+v", got[102])
	}
	if got[102].Subject != "" {
		t.Errorf("tombstone has no parsed subject, got %q", got[102].Subject)
	}

	// And a sync over the store marks the bad UID as unreadable and moves on.
	st, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	svc := New(st, memSecrets{"qq:a@qq.com": "x"})
	fake := newFake()
	fake.folders["INBOX"] = []Header{
		{UID: 7, Malformed: true, FetchErr: "in envelope: imapwire: expected ')', got \"a\"", Date: time.Now()},
		{UID: 8, Subject: "Fine", FromAddr: "a@b.c", Date: time.Now(), Body: "ok"},
	}
	svc.Dial = func(store.Account, string) Remote { return fake }
	a, err := svc.AddAccount("qq", "a@qq.com", "x")
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.SyncAccount(a); err != nil {
		t.Fatalf("sync: %v", err)
	}
	if last := st.LastUID(a.ID, "INBOX"); last != 8 {
		t.Errorf("watermark = %d, want 8", last)
	}
	m, _ := st.Get(st.MessageID(a.ID, "INBOX", 7))
	if m.Subject != unreadableSubject {
		t.Errorf("tombstone subject = %q", m.Subject)
	}
	if err := svc.SyncAccount(a); err != nil {
		t.Fatalf("resync: %v", err)
	}
}

func TestDegradableSplitsBadDataFromDeadConnection(t *testing.T) {
	for _, err := range []error{
		fmt.Errorf("in response-data: in envelope: imapwire: expected ')', got \"a\""),
		fmt.Errorf("in response-data: in body-type-1part: imapwire: expected atom, got \"(\""),
	} {
		if !degradable(err) {
			t.Errorf("degradable(%v) = false", err)
		}
	}
	for _, err := range []error{
		io.EOF, io.ErrUnexpectedEOF,
		&net.DNSError{IsTimeout: true},
	} {
		if degradable(err) {
			t.Errorf("degradable(%v) = true", err)
		}
	}
	// i/o timeout wrapped as a net.OpError
	var opErr error = &net.OpError{Op: "read", Err: errors.New("i/o timeout")}
	if degradable(fmt.Errorf("in envelope: %w", opErr)) {
		t.Error("a read timeout is a dead connection, not bad data")
	}
}
