package redact

import (
	"regexp"
	"strings"
	"testing"
)

func TestText(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"mail me at Lin.Wang+x@acme.co.uk now", "mail me at ‹EMAIL_1› now"},
		{"a@b.com and c@d.com and a@b.com", "‹EMAIL_1› and ‹EMAIL_2› and ‹EMAIL_1›"},
		{"call 13800138000 or +86 139-0000-0000x", "call ‹PHONE_1› or +86 139-0000-0000x"}, // the 2nd has dashes: not a bare mobile number
		{"call 13800138000 or +8613900000000", "call ‹PHONE_1› or ‹PHONE_2›"},
		{"order 913800138000 shipped", "order 913800138000 shipped"}, // inside a longer number
		{"id 11010519491231002X ok", "id ‹ID_1› ok"},
		{"id 110105194912310021 ok", "id 110105194912310021 ok"}, // bad check digit
		{"card 4111 1111 1111 1111.", "card ‹CARD_1›."},
		{"card 4111-1111-1111-1112.", "card 4111-1111-1111-1112."}, // fails Luhn
		{"Your verification code is 482913, valid 10 min", "Your verification code is ‹CODE_1›, valid 10 min"},
		{"验证码：5821，请勿泄露", "验证码：‹CODE_1›，请勿泄露"},
		{"密码: hunter22!", "密码: ‹PASSWORD_1›"},
		{"password is Tr0ub4dor&3", "password is ‹PASSWORD_1›"},
		{"key sk-abcdefghijklmnopqrstuvwx used", "key ‹SECRET_1› used"},
		{"Authorization: Bearer abcdefghijklmnopqrstuvwxyz012345", "Authorization: Bearer ‹SECRET_1›"},
		{"AKIAIOSFODNN7EXAMPLE and ghp_" + strings.Repeat("a", 36), "‹SECRET_1› and ‹SECRET_2›"},
		{"-----BEGIN RSA PRIVATE KEY-----\nMIIB\n-----END RSA PRIVATE KEY-----", "‹SECRET_1›"},
		{"nothing to hide: 2026-11-11 12:30, #4821, 3万人参加", "nothing to hide: 2026-11-11 12:30, #4821, 3万人参加"},
		{"已收到付款 \\$20.00，合计 ¥1,280.50", "已收到付款 ‹AMOUNT_1›，合计 ‹AMOUNT_2›"},
		{"费用 3万元 和 120 美元，USD 1,200", "费用 ‹AMOUNT_1› 和 ‹AMOUNT_2›，‹AMOUNT_3›"},
		{"月薪 25000，年终奖：3万", "月薪 ‹SALARY_1›，年终奖：‹SALARY_2›"},
		{"salary: 120,000 and bonus 8k", "salary: ‹SALARY_1› and bonus ‹SALARY_2›"},
		{"实发工资\n\n18,432.50", "实发工资\n\n‹SALARY_1›"},
		// a pay slip as the mail server sends it: the word, a gap of no-break spaces, the figure
		{"月固定工资" + strings.Repeat("\u00a0", 32) + " 35,792.00 ", "月固定工资" + strings.Repeat("\u00a0", 32) + " ‹SALARY_1› "},
		{"社保个人部分" + strings.Repeat("\u00a0", 29) + " 2,929.27 ", "社保个人部分" + strings.Repeat("\u00a0", 29) + " ‹AMOUNT_1› "},
		{"个税" + strings.Repeat("\u00a0", 40) + " 438.55 ", "个税" + strings.Repeat("\u00a0", 40) + " ‹AMOUNT_1› "},
		{"累计工资总额    378,652.00\n实发工资  25,254.18", "累计工资总额    ‹SALARY_1›\n实发工资  ‹SALARY_2›"},
		{"2026年10月份薪资发放通知单 - 邓泽丰 2026年10月", "2026年10月份薪资发放通知单 - 邓泽丰 2026年10月"}, // a date is no pay
		{"收货地址：北京市海淀区中关村大街1号，电话", "收货地址：‹ADDRESS_1›，电话"},
		{"482913 is your verification code", "‹CODE_1› is your verification code"},
		{"BXZ5-P7QU is your Libraries.dev sign-in code", "‹CODE_1› is your Libraries.dev sign-in code"},
		{"This is your verification code", "This is your verification code"},
		{"", ""},
	} {
		m := New()
		if got := m.Text(c.in); got != c.want {
			t.Errorf("Text(%q)\n got %q\nwant %q", c.in, got, c.want)
		}
	}
}

func TestOverlapAndCount(t *testing.T) {
	m := New()
	// An 18-digit ID also looks like a card number; the ID keeps it.
	if got := m.Text("11010519491231002X 4111111111111111"); got != "‹ID_1› ‹CARD_1›" || m.Hidden != 2 {
		t.Errorf("got %q, hidden %d", got, m.Hidden)
	}
	// The email inside a token-looking query is hidden once, not twice.
	if got := New().Text("https://x.com/u?e=lin@acme.com"); got != "https://x.com/u?e=‹EMAIL_1›" {
		t.Errorf("got %q", got)
	}
}

func TestHTML(t *testing.T) {
	in := `<html><head><style>.a{width:13800138000px}</style></head><body bgcolor="#eee">` +
		`<p class="a">Hi <b>Lin</b>, code <i>482913</i>? no: verification code is 482913. Write lin@acme.com</p>` +
		`<a href="mailto:lin@acme.com" title="lin@acme.com">mail</a><a href="https://x.com/?e=lin@acme.com">web</a>` +
		`<img alt="13800138000" src="https://x.com/a.png"><script>var p="13800138000"</script></body></html>`
	m := New()
	got := m.HTML(in)
	for _, want := range []string{
		"width:13800138000px", // CSS is left alone
		`bgcolor="#eee"`,      // layout survives
		`verification code is <mark class="zm" data-k="CODE" title="验证码">` + Fake(Code, 1) + `</mark>.`, // text, as a stand-in
		`Write <mark class="zm" data-k="EMAIL" title="邮箱">` + Fake(Email, 1) + `</mark>`,                // the same value, the same number
		`href="mailto:‹EMAIL_1›"`,              // mailto links
		`title="‹EMAIL_1›"`,                    // titles
		`alt="‹PHONE_1›"`,                      // alt texts
		`href="https://x.com/?e=lin@acme.com"`, // other links stay usable
		`var p="13800138000"`,                  // script text is left alone
	} {
		if !strings.Contains(got, want) {
			t.Errorf("HTML lacks %q:\n%s", want, got)
		}
	}
	// the texts are read as one, so a code set apart by markup is still a code
	if !strings.Contains(got, `<p class="a">Hi <b>Lin</b>, code <i><mark class="zm" data-k="CODE" title="验证码">`+Fake(Code, 1)+`</mark></i>?`) {
		t.Errorf("a code after its label across markup, and the markup itself:\n%s", got)
	}
}

// A model's spans join the patterns': they give way where they meet, and
// are read once for a page's texts as a whole.
func TestModel(t *testing.T) {
	calls := 0
	m := New()
	m.Model = func(text string) ([]Found, bool) {
		calls++
		var out []Found
		for _, w := range []struct {
			s string
			k Kind
		}{{"王小明", Person}, {"lin@acme.com", Email}, {"北京市海淀区", Address}} {
			if i := strings.Index(text, w.s); i >= 0 {
				out = append(out, Found{i, i + len(w.s), w.k})
			}
		}
		return out, true
	}
	got := m.HTML(`<p>我是 王小明，住在<b>北京市海淀区</b>，邮箱 lin@acme.com</p>`)
	for _, want := range []string{
		`我是 <mark class="zm" data-k="PERSON" title="姓名">` + Fake(Person, 1) + `</mark>，住在<b><mark class="zm" data-k="ADDRESS" title="地址">` + Fake(Address, 1) + `</mark></b>，邮箱 <mark class="zm" data-k="EMAIL" title="邮箱">` + Fake(Email, 1) + `</mark>`} {
		if !strings.Contains(got, want) {
			t.Errorf("got %s", got)
		}
	}
	if calls != 1 {
		t.Errorf("the model read %d times, want once for the page", calls)
	}

	p := New()
	p.Model = func(string) ([]Found, bool) { return nil, false }
	if got := p.Text("王小明 13800138000"); !p.Pending || !strings.Contains(got, "‹PHONE_1›") {
		t.Errorf("pending = %v, text %q", p.Pending, got)
	}
}

func TestChunks(t *testing.T) {
	got := Chunks("写给 ‹EMAIL_2›，验证码 ‹CODE_1›。‹NOPE_1›")
	want := []Chunk{{"写给 ", "", 0}, {"‹EMAIL_2›", Email, 2}, {"，验证码 ", "", 0}, {"‹CODE_1›", Code, 1}, {"。‹NOPE_1›", "", 0}}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("chunk %d = %+v, want %+v", i, got[i], want[i])
		}
	}
	if c := Chunks("plain"); len(c) != 1 || c[0].Kind != "" || Marked("plain") || !Marked("a ‹PHONE_1›") {
		t.Errorf("plain: %+v", c)
	}
	if Chip(Person, 3) != "姓名 3" {
		t.Errorf("chip = %q", Chip(Person, 3))
	}
}

func TestPersonKeepsLatinName(t *testing.T) {
	m := New()
	m.Model = func(s string) ([]Found, bool) { return []Found{{Start: 0, End: 9, Kind: Person}}, true } // "邓泽丰" only
	if got := m.Text("邓泽丰-Zephyr，你好"); got != "‹PERSON_1›，你好" {
		t.Errorf("got %q", got)
	}
}

// A stand-in is the same for the same value, takes the shape of its kind and
// holds nothing of a value.
func TestFake(t *testing.T) {
	if Fake(Email, 1) != Fake(Email, 1) || Fake(Email, 1) == Fake(Email, 2) {
		t.Error("a stand-in must follow the value's number")
	}
	for _, c := range []struct {
		k  Kind
		re string
	}{
		{Email, `^[a-z]+@[a-z]+\.com$`}, {Phone, `^1[3-9]\d{9}$`}, {Code, `^\d{6}$`}, {ID, `^\d{17}[\dX]$`},
		{Salary, `^\d+,\d{3}\.\d{2}$`}, {Card, `^(\d{4} ){3}\d{4}$`},
	} {
		for n := 1; n < 20; n++ {
			if !regexp.MustCompile(c.re).MatchString(Fake(c.k, n)) {
				t.Errorf("Fake(%s, %d) = %q", c.k, n, Fake(c.k, n))
			}
		}
	}
	if Fake(Person, 1) == "" || Fake(Address, 1) == "" || Fake("OTHER", 1) == "" {
		t.Error("empty stand-in")
	}
}

// A page is mostly blanks between its tags; the model reads none of them.
func TestHTMLModelSkipsBlanks(t *testing.T) {
	var asked string
	m := New()
	m.Model = func(s string) ([]Found, bool) { asked = s; return nil, true }
	m.HTML("<table>\n  <tr>\n    <td>你好</td>\n    <td>   </td>\n    <td>世界</td>\n  </tr>\n</table>")
	if want := "你好" + sep + "世界"; asked != want {
		t.Fatalf("model read %q, want %q", asked, want)
	}
}
