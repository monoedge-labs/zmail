package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func captureOpen(t *testing.T) *[]string {
	t.Helper()
	var opened []string
	old := openURL
	openURL = func(u string) { opened = append(opened, u) }
	t.Cleanup(func() { openURL = old })
	return &opened
}

// Mail is read-only: a clicked link is held and asked about, and opens only
// on a yes. Cancel opens nothing.
func TestLinkGuardAsks(t *testing.T) {
	opened := captureOpen(t)
	a := testApp(t)
	tt := ui.NewTester(a.view, 1360, 900)

	a.openLink("javascript:alert(1)")
	a.openLink("file:///etc/passwd")
	if a.link.open || len(*opened) != 0 {
		t.Fatalf("a link that is not http(s) or mailto must be dropped: %+v %v", a.link, *opened)
	}

	a.openLink("https://evil.example/login?next=a")
	tt.Frame() // the window redraws on its own; the test has none
	tt.Advance(300e6)
	if len(*opened) != 0 || !tt.HasText("要打开这个链接吗？") || !tt.HasText("evil.example") {
		t.Fatalf("link must wait for a yes: opened %v, texts %q", *opened, tt.Texts())
	}
	shot(t, tt, "link-guard")
	if err := tt.Click("取消"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(400e6)
	if len(*opened) != 0 || a.link.url != "" {
		t.Fatalf("cancel must open nothing: %v %+v", *opened, a.link)
	}

	a.openLink("https://evil.example/login?next=a")
	tt.Frame() // the window redraws on its own; the test has none
	tt.Advance(300e6)
	if err := tt.Click("仍然打开"); err != nil {
		t.Fatal(err)
	}
	if len(*opened) != 1 || (*opened)[0] != "https://evil.example/login?next=a" {
		t.Fatalf("confirm must open the link: %v", *opened)
	}
}

// With the guard off, in the settings, a click opens the browser at once.
func TestLinkGuardOff(t *testing.T) {
	opened := captureOpen(t)
	a := testApp(t)
	if a.priv.NoLinkGuard {
		t.Fatal("the guard must be on by default")
	}
	a.dir = t.TempDir()
	a.setPrivacy(privacy{NoLinkGuard: true})
	if !loadPrivacy(a.dir).NoLinkGuard {
		t.Error("the choice must be kept")
	}
	a.openLink("https://example.com/a")
	if a.link.open || len(*opened) != 1 {
		t.Fatalf("guard off must open at once: %+v %v", a.link, *opened)
	}
	if loadPrivacy(t.TempDir()).NoLinkGuard {
		t.Error("an empty config must keep the guard on")
	}
}

func TestIssueCLI(t *testing.T) {
	opened := captureOpen(t)
	out, err := run(t, t.TempDir(), memSecrets{}, "", "issue", "--title", "sync fails & hangs", "--body", "it hangs", "--json")
	if err != nil {
		t.Fatal(err)
	}
	var got struct {
		URL    string `json:"url"`
		Opened bool   `json:"opened"`
	}
	if json.Unmarshal([]byte(out), &got) != nil || !got.Opened || len(*opened) != 1 || (*opened)[0] != got.URL {
		t.Fatalf("issue printed %q, opened %v", out, *opened)
	}
	u, err := url.Parse(got.URL)
	if err != nil || u.Host != "github.com" || u.Path != "/monoedge-labs/zmail/issues/new" {
		t.Fatalf("bad address %q", got.URL)
	}
	q := u.Query()
	if q.Get("title") != "sync fails & hangs" || !strings.Contains(q.Get("body"), "it hangs") || !strings.Contains(q.Get("body"), "zmail "+appVersion()) {
		t.Errorf("fields lost in %q", got.URL)
	}
	if appVersion() == "unknown" {
		t.Error("the version must come from mygo.json")
	}

	*opened = nil
	if out, err = run(t, t.TempDir(), memSecrets{}, "", "issue", "--print"); err != nil || len(*opened) != 0 || !strings.HasPrefix(out, issuesNew) {
		t.Errorf("--print must open nothing: %q %v %v", out, err, *opened)
	}
	if _, err = run(t, t.TempDir(), memSecrets{}, "", "issue", "stray"); err == nil || !strings.Contains(err.Error(), "unexpected positional argument") {
		t.Errorf("a stray word must be refused: %v", err)
	}
	help, _ := run(t, t.TempDir(), memSecrets{}, "", "help")
	if !strings.Contains(help, "zmail issue") {
		t.Error("help lacks issue")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// llmServer stands in for the model, whatever address is set: it keeps what
// it was sent, and where it was meant to go, and answers with reply.
func llmServer(t *testing.T, status int, reply string) (sent *map[string]any, auth, to *string) {
	t.Helper()
	sent, auth, to = new(map[string]any), new(string), new(string)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*auth = r.Header.Get("Authorization")
		json.NewDecoder(r.Body).Decode(sent)
		w.WriteHeader(status)
		json.NewEncoder(w).Encode(map[string]any{"choices": []any{map[string]any{"message": map[string]any{"content": reply}}}})
	}))
	u, _ := url.Parse(srv.URL)
	old := llmClient.Transport
	llmClient.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		*to = r.URL.String()
		r = r.Clone(r.Context())
		r.URL.Scheme, r.URL.Host = u.Scheme, u.Host
		return http.DefaultTransport.RoundTrip(r)
	})
	t.Cleanup(func() { srv.Close(); llmClient.Transport = old })
	return sent, auth, to
}

const goodReply = "```json\n" + `{"title":"同步 QQ 邮箱\n卡住","what_happened":"点击同步没有反应","expected":"同步完成"}` + "\n```"

// The feedback sheet sends nothing until "AI 润色" is clicked; then only the
// typed text goes, with its private values hidden, and the fields come back
// for the user to edit before the browser opens.
func TestFeedbackSheetPolish(t *testing.T) {
	opened := captureOpen(t)
	sent, auth, _ := llmServer(t, 200, goodReply)
	a := testApp(t)
	tt := ui.NewTester(a.view, 1360, 900)
	if err := tt.Click("反馈问题"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(300e6)
	if !tt.HasText("AI 润色") || len(*opened) != 0 || *auth != "" {
		t.Fatalf("opening the sheet must send and open nothing: %q", tt.Texts())
	}
	if !tt.HasText("润色由 Agnes AI 免费提供") || !tt.HasText(defaultModel+" 目前限免，有每分钟请求数限制，以公告为准。") {
		t.Fatalf("the sheet must say who provides the model: %q", tt.Texts())
	}
	if err := tt.Click("查看公告"); err != nil {
		t.Fatal(err)
	}
	if len(*opened) != 1 || (*opened)[0] != llmNotice {
		t.Fatalf("the notice link opened %v", *opened)
	}
	*opened = nil

	a.fb.text = "同步卡住，联系 me@example.com"
	if err := tt.Click("AI 润色"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(300e6)
	if *auth != "Bearer "+defaultKey || (*sent)["model"] != defaultModel {
		t.Errorf("request: auth %q, sent %v", *auth, *sent)
	}
	if msgs, _ := json.Marshal((*sent)["messages"]); strings.Contains(string(msgs), "me@example.com") {
		t.Errorf("an address went to the model: %s", msgs)
	}
	if !a.fb.polished || a.fb.draft.Title != "同步 QQ 邮箱 卡住" || !tt.HasText("现象") {
		t.Fatalf("fields not shown: %+v", a.fb)
	}
	shot(t, tt, "feedback-polished")

	if err := tt.Click("打开 Issue"); err != nil {
		t.Fatal(err)
	}
	if len(*opened) != 1 || !strings.Contains((*opened)[0], url.QueryEscape("同步完成")) || a.fb.open {
		t.Fatalf("opened %v, sheet %+v", *opened, a.fb)
	}
}

// A model that fails or breaks the format costs nothing: the text goes
// to the issue as it was typed.
func TestFeedbackPolishFallback(t *testing.T) {
	for name, c := range map[string]struct {
		status int
		reply  string
	}{
		"server error": {500, ""},
		"not JSON":     {200, "sorry"},
		"no title":     {200, `{"title":"","what_happened":"x"}`},
	} {
		t.Run(name, func(t *testing.T) {
			opened := captureOpen(t)
			llmServer(t, c.status, c.reply)
			a := testApp(t)
			a.fb = feedbackState{open: true, text: "it hangs"}
			a.polishFeedback()
			if a.fb.polished || a.fb.err == "" || a.fb.busy {
				t.Fatalf("a bad answer must show an error and no fields: %+v", a.fb)
			}
			a.sendFeedback()
			if len(*opened) != 1 || !strings.Contains((*opened)[0], "it+hangs") {
				t.Fatalf("the typed text must still go: %v", *opened)
			}
		})
	}
}

func TestParseDraftClips(t *testing.T) {
	long := strings.Repeat("长", 500)
	d, err := parseDraft(`{"title":"` + long + `","what_happened":"` + strings.Repeat("x", 5000) + `"}`)
	if err != nil || len([]rune(d.Title)) != titleRunes || len([]rune(d.What)) != fieldRunes {
		t.Errorf("lengths not capped: %v %d %d", err, len([]rune(d.Title)), len([]rune(d.What)))
	}
}

// The sidebar's entry opens the sheet, which opens the issue page: with the
// text as typed when the model was not asked, and with no mail in it.
func TestFeedbackButton(t *testing.T) {
	opened := captureOpen(t)
	a := testApp(t)
	tt := ui.NewTester(a.view, 1360, 900)
	if err := tt.Click("反馈问题"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(300e6)
	if len(*opened) != 0 {
		t.Fatalf("the sheet must open nothing by itself: %v", *opened)
	}
	a.fb.text = "它卡住了"
	if err := tt.Click("打开 Issue"); err != nil {
		t.Fatal(err)
	}
	if len(*opened) != 1 || !strings.HasPrefix((*opened)[0], issuesNew+"?") || !strings.Contains((*opened)[0], url.QueryEscape("它卡住了")) {
		t.Fatalf("opened %v", *opened)
	}
}

// The user's own service: the address and model go to llm.json, the key to
// the keychain, and a request uses all three. Changing the address forgets
// the key, so that it cannot be sent where it was not meant to go.
func TestLLMSettings(t *testing.T) {
	t.Setenv("ZMAIL_LLM_KEY", "")
	t.Setenv("ZMAIL_LLM_BASE", "")
	t.Setenv("ZMAIL_LLM_MODEL", "")
	dir, sec := t.TempDir(), memSecrets{}
	if s := resolveLLM(dir, sec); !s.free() || s.Key != defaultKey || s.Model != defaultModel {
		t.Fatalf("nothing set must be the built-in: %+v", s)
	}

	if err := setLLM(dir, sec, llmConfig{BaseURL: "http://evil.example/v1"}, "k"); err == nil {
		t.Error("http to another computer must be refused")
	}
	if err := setLLM(dir, sec, llmConfig{BaseURL: "https://u:p@x.example/v1"}, "k"); err == nil {
		t.Error("credentials in the address must be refused")
	}
	if err := setLLM(dir, sec, llmConfig{BaseURL: "https://api.example.com/v1/", Model: "m1"}, "sk-mine"); err != nil {
		t.Fatal(err)
	}
	s := resolveLLM(dir, sec)
	if s.Base != "https://api.example.com/v1" || s.Model != "m1" || s.Key != "sk-mine" || s.KeySource != "custom" || s.free() {
		t.Fatalf("settings: %+v", s)
	}
	if b, _ := os.ReadFile(dir + "/llm.json"); strings.Contains(string(b), "sk-mine") {
		t.Error("the key must not be in llm.json")
	}

	// The model alone changes: the key stays. The address changes: it goes.
	if err := setLLM(dir, sec, llmConfig{BaseURL: "https://api.example.com/v1", Model: "m2"}, ""); err != nil || sec[llmKeyID] != "sk-mine" {
		t.Fatalf("a new model must keep the key: %v %v", err, sec)
	}
	if err := setLLM(dir, sec, llmConfig{BaseURL: "http://localhost:11434/v1", Model: "m2"}, ""); err != nil {
		t.Fatal(err)
	}
	if _, ok := sec[llmKeyID]; ok || resolveLLM(dir, sec).KeySource != "builtin" {
		t.Errorf("a new address must forget the key: %v", sec)
	}

	t.Setenv("ZMAIL_LLM_MODEL", "from-env")
	if resolveLLM(dir, sec).Model != "from-env" {
		t.Error("the environment must win")
	}
	if err := resetLLM(dir, sec); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ZMAIL_LLM_MODEL", "")
	if s := resolveLLM(dir, sec); !s.free() || s.Model != defaultModel {
		t.Errorf("reset must be the built-in: %+v", s)
	}
}

func TestLLMCLI(t *testing.T) {
	t.Setenv("ZMAIL_LLM_KEY", "")
	t.Setenv("ZMAIL_LLM_BASE", "")
	t.Setenv("ZMAIL_LLM_MODEL", "")
	dir, sec := t.TempDir(), memSecrets{}
	show := func(args ...string) (got struct{ BaseURL, Model, Key string }) {
		t.Helper()
		out, err := run(t, dir, sec, "", append(args, "--json")...)
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]string
		json.Unmarshal([]byte(out), &m)
		got.BaseURL, got.Model, got.Key = m["base_url"], m["model"], m["key"]
		if strings.Contains(out, defaultKey) {
			t.Fatal("show must not print a key")
		}
		return got
	}
	if g := show("llm", "show"); g.BaseURL != defaultBase || g.Key != "builtin" {
		t.Fatalf("show: %+v", g)
	}

	out, err := run(t, dir, sec, "sk-cli\n", "llm", "set", "--base-url", "https://api.example.com/v1", "--model", "m1", "--key-stdin")
	if err != nil || strings.Contains(out, "sk-cli") || !strings.Contains(out, "yours, in the keychain") {
		t.Fatalf("set printed %q, %v", out, err)
	}
	if sec[llmKeyID] != "sk-cli" {
		t.Errorf("the key must go to the keychain: %v", sec)
	}
	if g := show("llm", "set", "--model", "m2"); g.Model != "m2" || g.BaseURL != "https://api.example.com/v1" || g.Key != "custom" {
		t.Errorf("--model alone must change only the model: %+v", g)
	}
	// Moving the address without a new key forgets the old one.
	if g := show("llm", "set", "--base-url", "https://other.example.com/v1"); g.Key != "builtin" {
		t.Errorf("a moved address must forget the key: %+v", g)
	}
	if g := show("llm", "set", "--base-url", ""); g.BaseURL != defaultBase {
		t.Errorf("an empty --base-url is the built-in: %+v", g)
	}
	if _, err := run(t, dir, sec, "", "llm", "set"); err == nil {
		t.Error("set with nothing must be refused")
	}
	if _, err := run(t, dir, sec, "", "llm", "set", "--key-stdin"); err == nil {
		t.Error("an empty key on stdin must be refused")
	}
	if _, err := run(t, dir, sec, "", "llm", "set", "--base-url", "http://evil.example"); err == nil {
		t.Error("a plain http address must be refused")
	}
	if _, err := run(t, dir, sec, "", "llm", "show", "stray"); err == nil || !strings.Contains(err.Error(), "unexpected positional argument") {
		t.Errorf("a stray word must be refused: %v", err)
	}
	run(t, dir, sec, "k\n", "llm", "set", "--key-stdin", "--model", "x")
	if g := show("llm", "reset"); g.Model != defaultModel || g.Key != "builtin" || len(sec) != 0 {
		t.Errorf("reset: %+v %v", g, sec)
	}
	help, _ := run(t, dir, sec, "", "help")
	if !strings.Contains(help, "zmail llm set") {
		t.Error("help lacks llm")
	}
}

// The sheet's settings save to the same place, and a request then goes to
// the user's address with the user's key, and says so.
func TestFeedbackSheetLLMSettings(t *testing.T) {
	t.Setenv("ZMAIL_LLM_KEY", "")
	t.Setenv("ZMAIL_LLM_BASE", "")
	t.Setenv("ZMAIL_LLM_MODEL", "")
	captureOpen(t)
	sent, auth, to := llmServer(t, 200, goodReply)
	a := testApp(t)
	sec := memSecrets{}
	a.dir, a.sec = t.TempDir(), sec
	tt := ui.NewTester(a.view, 1360, 900)
	if err := tt.Click("反馈问题"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(300e6)
	if err := tt.Click("模型设置"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(300e6)
	if !tt.HasText("API Key") || !tt.HasText("接口地址") {
		t.Fatalf("settings not shown: %q", tt.Texts())
	}
	v := &a.fb.set.f.Values
	v.Base, v.Model, v.Key = "https://llm.example.com/v1", "my-model", "sk-window"
	shot(t, tt, "feedback-llm-settings")
	a.fb.draft, a.fb.polished = issueDraft{"标题", "现象", "期望"}, true
	tt.Frame()
	shot(t, tt, "feedback-all-open")
	a.fb.polished = false
	tt.Frame()
	if err := tt.Click("保存"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(300e6)
	if a.fb.set.err != "" || a.fb.set.open || sec[llmKeyID] != "sk-window" {
		t.Fatalf("not saved: %q %v", a.fb.set.err, sec)
	}
	if !tt.HasText("使用你配置的模型服务") || !tt.HasText("llm.example.com，模型 my-model") || !tt.HasText("只有点“AI 润色”，上面的文字才会发给第三方 llm.example.com（已隐去邮箱、手机号等）；不点就不会发送。") || tt.HasText("查看公告") {
		t.Fatalf("the sheet must name the user's service, not Agnes: %q", tt.Texts())
	}

	a.fb.text = "它卡住了"
	if err := tt.Click("AI 润色"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(300e6)
	if *to != "https://llm.example.com/v1/chat/completions" || *auth != "Bearer sk-window" || (*sent)["model"] != "my-model" {
		t.Errorf("request went to %q with %q: %v", *to, *auth, *sent)
	}

	// A bad address is refused, with the reason in the form.
	a.toggleLLMForm()
	a.fb.set.f.Values.Base = "http://evil.example/v1"
	a.saveLLM()
	if a.fb.set.err == "" || !a.fb.set.open {
		t.Errorf("a plain http address must be refused in the form: %q", a.fb.set.err)
	}
	a.resetLLMForm()
	if !a.llmNow().free() || len(sec) != 0 {
		t.Errorf("reset must be the built-in: %+v %v", a.llmNow(), sec)
	}
}

// The sheets' buttons follow the system's: the default one cannot act before
// there is something to send, ⌘Return is its key, and Return in the link
// alert is the safe answer, not the click-through.
func TestSheetButtons(t *testing.T) {
	opened := captureOpen(t)
	a := testApp(t)
	tt := ui.NewTester(a.view, 1360, 900)
	if err := tt.Click("反馈问题"); err != nil {
		t.Fatal(err)
	}
	tt.Advance(300e6)
	if err := tt.Click("打开 Issue"); err != nil {
		t.Fatal(err)
	}
	if len(*opened) != 0 || a.fb.err != "" {
		t.Fatalf("an empty sheet must not act, nor scold: %v %q", *opened, a.fb.err)
	}
	tt.Key(ui.Cmd, ui.KeyEnter)
	if len(*opened) != 0 {
		t.Fatal("⌘Return on an empty sheet must do nothing")
	}
	a.fb.text = "它卡住了"
	tt.Frame()
	tt.Key(ui.Cmd, ui.KeyEnter)
	if len(*opened) != 1 || a.fb.open {
		t.Fatalf("⌘Return must open the issue: %v %+v", *opened, a.fb)
	}

	*opened = nil
	a.openLink("https://evil.example/x")
	tt.Frame()
	tt.Advance(300e6)
	tt.Key(0, ui.KeyEnter)
	tt.Advance(400e6)
	if a.link.open || len(*opened) != 0 {
		t.Fatalf("Return in the alert must cancel: %+v %v", a.link, *opened)
	}
}

// The announcement is the page, not its Markdown source.
func TestNoticeIsHTML(t *testing.T) {
	if strings.HasSuffix(llmNotice, ".md") || !strings.HasPrefix(llmNotice, "https://") {
		t.Errorf("llmNotice = %q", llmNotice)
	}
}

// The sheets' ground is opaque, light or dark: nothing of the window shows through.
func TestSheetOpaque(t *testing.T) {
	for _, dark := range []bool{false, true} {
		if pop := palFor(dark).pop; pop.A != 255 {
			t.Errorf("dark=%v: the sheet's ground has alpha %d", dark, pop.A)
		}
		a := testApp(t)
		tt := ui.NewTester(a.view, 1360, 900)
		tt.SetDark(dark)
		if err := tt.Click("反馈问题"); err != nil {
			t.Fatal(err)
		}
		tt.Advance(300e6)
		shot(t, tt, fmt.Sprintf("feedback-dark%v", dark))
	}
}
