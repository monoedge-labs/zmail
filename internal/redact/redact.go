// Package redact hides what a reader of a mail need not see: addresses, phone
// numbers, ID and card numbers, keys and tokens, verification codes and
// passwords. It works on patterns, so it runs offline and gives the same
// answer every time.
//
// Patterns catch what has a shape. A Model, when the Masker has one, adds
// what has none: a name or an address in free text.
package redact

import (
	"fmt"
	"net/netip"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// Kind is a sort of hidden value. Its name shows in the placeholder.
type Kind string

const (
	Secret   Kind = "SECRET"
	ID       Kind = "ID"
	Email    Kind = "EMAIL"
	Phone    Kind = "PHONE"
	Card     Kind = "CARD"
	Code     Kind = "CODE"
	Password Kind = "PASSWORD"
	Salary   Kind = "SALARY"
	Amount   Kind = "AMOUNT"
	IP       Kind = "IP"
	Person   Kind = "PERSON"  // found by a Model only
	Address  Kind = "ADDRESS" // found by a Model only
	Account  Kind = "ACCOUNT" // found by a Model only
)

// Found is a stretch of a text a Model found, by byte offsets.
type Found struct {
	Start, End int
	Kind       Kind
}

// A Model finds the private stretches of a text, and says whether it could:
// ready is false while its answer is still being worked out.
type Model func(text string) (found []Found, ready bool)

// A rule finds one kind. group is the part of a match that is hidden; ok,
// when set, judges a match by the text around it.
type rule struct {
	kind  Kind
	re    *regexp.Regexp
	group int
	ok    func(s string, a, b int) bool
}

// rules run in order, and the first to claim a stretch of text keeps it: a
// key before the digits inside it, an ID number before the card it also
// looks like.
var rules = []rule{
	{Secret, regexp.MustCompile(`sk-[A-Za-z0-9_-]{20,}|gh[pousr]_[A-Za-z0-9]{30,}|github_pat_[A-Za-z0-9_]{40,}|AKIA[0-9A-Z]{16}|AIza[0-9A-Za-z_-]{35}|xox[abprs]-[A-Za-z0-9-]{10,}|eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}|-----BEGIN [A-Z ]*PRIVATE KEY-----[\s\S]*?-----END [A-Z ]*PRIVATE KEY-----`), 0, nil},
	{Secret, regexp.MustCompile(`(?i)\bbearer\s+([A-Za-z0-9._~+/=-]{20,})`), 1, nil},
	// a pay figure after the word for pay, a number of its own even where
	// the table puts the word in one cell and the figure in the next
	{Salary, regexp.MustCompile(`(?i)(?:月薪|年薪|底薪|薪资|薪水|工资|基本工资|实发|应发|税前|税后|奖金|年终奖|补贴|salary|payroll|bonus)(?:[^\d]{0,16}|[^\d\s\x{a0}]{0,10}[\s\x{a0}:：]{0,64})(\d[\d,]*(?:\.\d+)?(?:\s?(?:万元|万|元|[kK]))?)`), 1, figure},
	{ID, regexp.MustCompile(`\d{17}[\dXx]`), 0, func(s string, a, b int) bool { return alone(s, a, b) && idSum(s[a:b]) }},
	{Email, regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9-]+(?:\.[A-Za-z0-9-]+)*\.[A-Za-z]{2,}`), 0, nil},
	// IPv4 and IPv6: a candidate shape, then net/netip decides. "10:30:45" and a MAC address are not addresses.
	{IP, regexp.MustCompile(`\d{1,3}(?:\.\d{1,3}){3}|(?:[0-9A-Fa-f]{0,4}:){2,7}(?:\d{1,3}(?:\.\d{1,3}){3}|[0-9A-Fa-f]{1,4})?`), 0, isIP},
	{Phone, regexp.MustCompile(`(?:\+?86[ -]?)?1[3-9]\d{9}`), 0, alone},
	{Card, regexp.MustCompile(`\d(?:[ -]?\d){12,18}`), 0, func(s string, a, b int) bool { return alone(s, a, b) && luhn(digits(s[a:b])) }},
	// money: a sum with its currency before it or its unit after
	{Amount, regexp.MustCompile(`\\?(?:[¥￥$€£]|US\$|HK\$|NT\$|USD|RMB|CNY|EUR|GBP|HKD|JPY|人民币)\s?\d[\d,]*(?:\.\d+)?(?:\s?[万亿kKmM]\b)?|\d[\d,]*(?:\.\d+)?\s?(?:万元|亿元|元|块钱|美元|美金|欧元|英镑|日元|港元|港币|RMB|CNY|USD|EUR|GBP)`), 0, nil},
	// the figures of a pay slip that name no pay word: grouped by thousands,
	// or a decimal sum at the end of a line after a gap of blanks (no-break ones too)
	{Amount, regexp.MustCompile(`\d{1,3}(?:,\d{3})+(?:\.\d+)?`), 0, alone},
	{Amount, regexp.MustCompile(`(?m)[ \t\x{a0}\x{3000}]{3,}(\d+\.\d{2})[ \t\x{a0}\x{3000}]*$`), 1, alone},
	{Code, regexp.MustCompile(`(?i)(?:验证码|校验码|动态码|verification code|security code|one-time (?:password|code)|otp|code)(?:\s*(?:is|为|是))?[\s:：]*(\d{4,8})\b`), 1, nil},
	// a code sent first: "482913 is your verification code", "BXZ5-P7QU is your sign-in code"
	{Code, regexp.MustCompile(`(?i)\b([A-Z0-9][A-Z0-9-]{3,11})\s+(?:is|为|是)\s+(?:your|the|您的|你的)\s+(?:[\w.\- ]{0,30}?)(?:verification|security|sign-?in|log-?in|one-time|confirmation|auth(?:entication)?)\s+code`), 1, hasDigit},
	{Address, regexp.MustCompile(`(?i)(?:收货地址|详细地址|联系地址|家庭地址|公司地址|住址|地址|address)\s*[:：]\s*([^\n，。;；]{4,80})`), 1, nil},
	{Password, regexp.MustCompile(`(?i)(?:密码|password|passwd|pwd)\s*(?:is|为|是|[:：=])\s*(\S{4,64})`), 1, nil},
}

// figure judges the number a pay word led to: a sum, not a year or a date.
func figure(s string, a, b int) bool {
	num := s[a:b]
	if len(digits(num)) < 4 && !strings.ContainsAny(num, "万kK元") {
		return false
	}
	if b < len(s) {
		r, _ := utf8.DecodeRuneInString(s[b:])
		if strings.ContainsRune("年月日号时分秒%", r) {
			return false
		}
		if (r == '-' || r == '/') && b+1 < len(s) && isDigit(s[b+1]) {
			return false
		}
	}
	return true
}

func latin(c byte) bool { return c|0x20 >= 'a' && c|0x20 <= 'z' }

func hasDigit(s string, a, b int) bool { return strings.ContainsAny(s[a:b], "0123456789") }

// alone reports whether s[a:b] stands apart from other digits.
func alone(s string, a, b int) bool {
	return (a == 0 || !isDigit(s[a-1])) && (b == len(s) || !isDigit(s[b]))
}

// isIP reports whether s[a:b] is an IP address that stands apart from the
// letters, digits, dots and colons around it (so "std::string" and "1.2.3.4.5" stay).
func isIP(s string, a, b int) bool {
	if _, err := netip.ParseAddr(s[a:b]); err != nil {
		return false
	}
	edge := func(c byte) bool {
		return isDigit(c) || c == ':' || c == '.' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
	}
	return (a == 0 || !edge(s[a-1])) && (b == len(s) || !edge(s[b]) || s[b] == '.' && (b+1 == len(s) || !edge(s[b+1])))
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func digits(s string) string {
	return strings.Map(func(r rune) rune {
		if r >= '0' && r <= '9' {
			return r
		}
		return -1
	}, s)
}

// luhn is the check a card number passes.
func luhn(d string) bool {
	if len(d) < 13 || len(d) > 19 {
		return false
	}
	sum := 0
	for i := range d {
		n := int(d[len(d)-1-i] - '0')
		if i%2 == 1 {
			if n *= 2; n > 9 {
				n -= 9
			}
		}
		sum += n
	}
	return sum%10 == 0
}

// idSum is the check digit of a mainland China resident ID number.
func idSum(id string) bool {
	w := [17]int{7, 9, 10, 5, 8, 4, 2, 1, 6, 3, 7, 9, 10, 5, 8, 4, 2}
	sum := 0
	for i := 0; i < 17; i++ {
		sum += int(id[i]-'0') * w[i]
	}
	return "10X98765432"[sum%11] == byte(strings.ToUpper(id)[17])
}

type span struct {
	a, b int
	kind Kind
}

// find returns the stretches of s to hide, by position.
func find(s string) []span {
	var out []span
	for _, r := range rules {
		for _, m := range r.re.FindAllStringSubmatchIndex(s, -1) {
			a, b := m[2*r.group], m[2*r.group+1]
			if a < 0 || (r.ok != nil && !r.ok(s, a, b)) {
				continue
			}
			if !slices.ContainsFunc(out, func(x span) bool { return a < x.b && x.a < b }) {
				out = append(out, span{a, b, r.kind})
			}
		}
	}
	slices.SortFunc(out, func(x, y span) int { return x.a - y.a })
	return out
}

// A Masker swaps hidden values for numbered placeholders such as ‹EMAIL_1›.
// One value keeps its number across the texts a Masker sees, so a reader can
// still tell who wrote to whom. Not safe for concurrent use.
type Masker struct {
	Hidden int // how many stretches have been replaced

	// Model, when set, finds more than the patterns do; its spans give way
	// to theirs where the two meet.
	Model Model
	// Pending is set when Model was not ready for a text: what Text and
	// HTML returned then holds the patterns' work alone, and is not to be shown.
	Pending bool

	tags  map[string]string
	count map[Kind]int
}

func New() *Masker { return &Masker{tags: map[string]string{}, count: map[Kind]int{}} }

func (m *Masker) tag(k Kind, v string) string {
	key := string(k) + "\x00" + v
	t, ok := m.tags[key]
	if !ok {
		m.count[k]++
		t = fmt.Sprintf("‹%s_%d›", k, m.count[k])
		m.tags[key] = t
	}
	m.Hidden++
	return t
}

// spans are the stretches of s to hide: the patterns', and the model's
// where they leave room.
func (m *Masker) spans(s string) []span {
	out := find(s)
	if m.Model == nil {
		return out
	}
	found, ready := m.Model(s)
	if !ready {
		m.Pending = true
		return out
	}
	for _, f := range found {
		a, b := f.Start, f.End
		for a < b && (s[a] == ' ' || s[a] == '\n' || s[a] == '\t') {
			a++
		}
		for b > a && (s[b-1] == ' ' || s[b-1] == '\n' || s[b-1] == '\t') {
			b--
		}
		if f.Kind == Person && b <= len(s) { // "邓泽丰-Zephyr": the model stops at the dash
			for b+1 < len(s) && s[b] == '-' && latin(s[b+1]) {
				for b++; b < len(s) && latin(s[b]); b++ {
				}
			}
		}
		if a < b && b <= len(s) && !slices.ContainsFunc(out, func(x span) bool { return a < x.b && x.a < b }) {
			out = append(out, span{a, b, f.Kind})
		}
	}
	slices.SortFunc(out, func(x, y span) int { return x.a - y.a })
	return out
}

// sep joins the texts of a page for one reading by the model, which reads
// them faster than many short ones; no span is taken across it.
const sep = "\n\n"

// Texts hides what ss hold, each read on its own: the model loses a name at
// the head of a subject when a snippet follows in the same reading. A value
// keeps its number across them.
func (m *Masker) Texts(ss ...string) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = m.Joined(s)[0]
	}
	return out
}

// Joined hides what ss hold as one text, which the model reads once, and
// patterns that run across them find (a code after its label across markup).
func (m *Masker) Joined(ss ...string) []string {
	joined := strings.Join(ss, sep)
	spans := m.spans(joined)
	if len(spans) == 0 {
		return ss
	}
	out := make([]string, len(ss))
	off, k := 0, 0
	for i, s := range ss {
		end := off + len(s)
		for k < len(spans) && spans[k].a < off { // in the separator
			k++
		}
		var b strings.Builder
		last := 0
		for k < len(spans) && spans[k].a < end {
			x := spans[k]
			k++
			if x.b > end { // crosses into the next text: left as it is
				continue
			}
			b.WriteString(s[last : x.a-off])
			b.WriteString(m.tag(x.kind, joined[x.a:x.b]))
			last = x.b - off
		}
		b.WriteString(s[last:])
		out[i] = b.String()
		off = end + len(sep)
	}
	return out
}

// Distinct is how many different values have been hidden.
func (m *Masker) Distinct() int { return len(m.tags) }

// Text hides what s holds of the kinds above.
func (m *Masker) Text(s string) string { return m.Joined(s)[0] }

// HTML hides the same in a page's text, the texts of its attributes that
// show (alt, title, aria-label, placeholder, value) and its mailto, tel and
// sms links, and leaves its layout as it is. Its scripts and styles are not
// text and stay as they are, and so do its web links, which a mail needs to
// work.
func (m *Masker) HTML(markup string) string {
	doc, err := html.Parse(strings.NewReader(markup))
	if err != nil {
		return m.Text(markup)
	}
	var texts []*string    // the page's texts, to be hidden in one reading
	var shown []*html.Node // those that are text on the page, which become chips
	var walk func(n *html.Node)
	walk = func(n *html.Node) {
		switch n.Type {
		case html.TextNode:
			// A blank text holds nothing to hide, and a page is mostly blanks
			// between its tags: the model must not read them.
			if p := n.Parent; strings.TrimSpace(n.Data) != "" && (p == nil || (p.Data != "script" && p.Data != "style")) {
				texts = append(texts, &n.Data)
				if p != nil && !rawText[p.Data] {
					shown = append(shown, n)
				}
			}
		case html.ElementNode:
			for i, a := range n.Attr {
				switch a.Key {
				case "alt", "title", "aria-label", "placeholder", "value":
					texts = append(texts, &n.Attr[i].Val)
				case "href":
					if v := strings.ToLower(a.Val); strings.HasPrefix(v, "mailto:") || strings.HasPrefix(v, "tel:") || strings.HasPrefix(v, "sms:") {
						texts = append(texts, &n.Attr[i].Val)
					}
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	vals := make([]string, len(texts))
	for i, p := range texts {
		vals[i] = *p
	}
	for i, v := range m.Joined(vals...) {
		*texts[i] = v
	}
	for _, n := range shown {
		chipify(n)
	}
	var b strings.Builder
	if err := html.Render(&b, doc); err != nil {
		return m.Text(markup)
	}
	return b.String()
}

// rawText are the elements whose text is no markup could be put in.
var rawText = map[string]bool{"title": true, "textarea": true, "noscript": true, "option": true, "template": true, "script": true, "style": true}

// chipify swaps the placeholders in a text node for <mark class="zm"> elements,
// which the page styles as chips: data-k holds the kind, the title names it.
func chipify(n *html.Node) {
	chunks := Chunks(n.Data)
	if len(chunks) == 1 && chunks[0].Kind == "" {
		return
	}
	for _, c := range chunks {
		if c.Kind == "" {
			n.Parent.InsertBefore(&html.Node{Type: html.TextNode, Data: c.Text}, n)
			continue
		}
		mark := &html.Node{Type: html.ElementNode, Data: "mark", DataAtom: atom.Mark, Attr: []html.Attribute{
			{Key: "class", Val: "zm"}, {Key: "data-k", Val: string(c.Kind)}, {Key: "title", Val: Label(c.Kind)}}}
		mark.AppendChild(&html.Node{Type: html.TextNode, Data: Fake(c.Kind, c.N)})
		n.Parent.InsertBefore(mark, n)
	}
	n.Parent.RemoveChild(n)
}
