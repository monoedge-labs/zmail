package mailbox

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/monoedge-labs/zmail/internal/store"
)

func TestGoogleLogin(t *testing.T) {
	var sawVerifier, sawCode string
	tokens := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		sawVerifier, sawCode = r.Form.Get("code_verifier"), r.Form.Get("code")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"access_token": "at", "refresh_token": "rt-1", "token_type": "Bearer", "expires_in": 3600})
	}))
	defer tokens.Close()
	old := googleEndpoint
	googleEndpoint.TokenURL = tokens.URL
	defer func() { googleEndpoint = old }()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	secret, err := GoogleLogin(ctx, "id", "cs", "me@example.com", func(authURL string) {
		u, _ := url.Parse(authURL)
		q := u.Query()
		if q.Get("login_hint") != "me@example.com" || q.Get("code_challenge_method") != "S256" ||
			q.Get("access_type") != "offline" || q.Get("scope") != googleScope {
			t.Errorf("auth url = %s", authURL)
		}
		go func() { // the browser, coming back to the loopback address with the code
			res, err := http.Get(q.Get("redirect_uri") + "/?code=abc&state=" + q.Get("state"))
			if err == nil {
				res.Body.Close()
			}
		}()
	})
	if err != nil {
		t.Fatal(err)
	}
	var s oauthSecret
	if json.Unmarshal([]byte(secret), &s) != nil || s.RefreshToken != "rt-1" || s.ClientID != "id" || s.ClientSecret != "cs" {
		t.Errorf("secret = %s", secret)
	}
	if sawCode != "abc" || sawVerifier == "" {
		t.Errorf("token request: code %q verifier %q", sawCode, sawVerifier)
	}

	// An OAuth secret opens the account with a token; a password does not.
	a := store.Account{Host: "imap.gmail.com:993", Email: "me@example.com"}
	if r := dialIMAP(a, secret).(*IMAP); r.token == nil {
		t.Error("an OAuth secret must sign in with a token")
	}
	if r := dialIMAP(a, "abcd efgh").(*IMAP); r.token != nil || r.pass != "abcd efgh" {
		t.Error("a password must sign in as before")
	}
}

func TestGoogleLoginDenied(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_, err := GoogleLogin(ctx, "id", "cs", "me@x.com", func(authURL string) {
		u, _ := url.Parse(authURL)
		q := u.Query()
		go http.Get(q.Get("redirect_uri") + "/?error=access_denied&state=" + q.Get("state"))
	})
	if err == nil {
		t.Fatal("a denied sign-in must fail")
	}
}

func TestXOAuth2(t *testing.T) {
	mech, ir, _ := xoauth2{"a@b.c", "tok"}.Start()
	if mech != "XOAUTH2" || string(ir) != "user=a@b.c\x01auth=Bearer tok\x01\x01" {
		t.Errorf("%s %q", mech, ir)
	}
}

func TestDebugLogHidesSecrets(t *testing.T) {
	var log strings.Builder
	c := idleConn{Conn: nopConn{}, log: &log}
	c.Write([]byte("T1 LOGIN \"me@qq.com\" \"s3cret\"\r\n"))
	c.Write([]byte("s3cret-literal\r\n"))
	c.Write([]byte("T2 AUTHENTICATE XOAUTH2 dG9rZW4=\r\n"))
	if strings.Contains(log.String(), "s3cret") || strings.Contains(log.String(), "dG9rZW4") || strings.Contains(log.String(), "me@qq.com") {
		t.Errorf("log leaks: %q", log.String())
	}
}

type nopConn struct{ net.Conn }

func (nopConn) Write(b []byte) (int, error) { return len(b), nil }
