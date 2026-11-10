package mailbox

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"golang.org/x/oauth2"

	"github.com/monoedge-labs/zmail/internal/store"
)

// Google Workspace accounts have not accepted passwords from mail apps since
// 2025-05-01: they sign in with OAuth. The account's secret is then this JSON
// (client and refresh token), kept in the keychain like a password.
const googleScope = "https://mail.google.com/"

var googleEndpoint = oauth2.Endpoint{
	AuthURL:  "https://accounts.google.com/o/oauth2/v2/auth",
	TokenURL: "https://oauth2.googleapis.com/token",
}

type oauthSecret struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	RefreshToken string `json:"refresh_token"`
}

func (s oauthSecret) config() *oauth2.Config {
	return &oauth2.Config{ClientID: s.ClientID, ClientSecret: s.ClientSecret, Endpoint: googleEndpoint, Scopes: []string{googleScope}}
}

// dialIMAP opens an account: with a password, or with OAuth when the secret
// is the JSON above.
func dialIMAP(a store.Account, secret string) Remote {
	if !strings.HasPrefix(strings.TrimSpace(secret), "{") {
		return NewIMAP(a.Host, a.Email, secret)
	}
	var s oauthSecret
	json.Unmarshal([]byte(secret), &s)
	ts := s.config().TokenSource(context.Background(), &oauth2.Token{RefreshToken: s.RefreshToken})
	r := NewIMAP(a.Host, a.Email, "")
	r.token = func() (string, error) {
		t, err := ts.Token()
		if err != nil {
			return "", err
		}
		return t.AccessToken, nil
	}
	return r
}

// GoogleLogin signs a Google account in through the browser (code flow with
// PKCE, answered on a loopback port) and returns the secret to keep for it.
// open shows the user the address to visit.
func GoogleLogin(ctx context.Context, clientID, clientSecret, email string, open func(url string)) (string, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	s := oauthSecret{ClientID: clientID, ClientSecret: clientSecret}
	cfg := s.config()
	cfg.RedirectURL = "http://" + ln.Addr().String()

	b := make([]byte, 16)
	rand.Read(b)
	state, verifier := hex.EncodeToString(b), oauth2.GenerateVerifier()

	type reply struct {
		code string
		err  error
	}
	ch := make(chan reply, 1)
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch {
		case q.Get("state") != state:
			http.Error(w, "unexpected request", http.StatusBadRequest)
			return
		case q.Get("error") != "":
			ch <- reply{err: errors.New("google: " + q.Get("error"))}
		default:
			ch <- reply{code: q.Get("code")}
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, "<meta charset=utf-8><body style='font:16px system-ui;padding:3em'>Zmail 已收到授权，可以关闭此页面。")
	}), ReadHeaderTimeout: 10 * time.Second}
	go srv.Serve(ln)
	defer srv.Close()

	open(cfg.AuthCodeURL(state, oauth2.AccessTypeOffline, oauth2.S256ChallengeOption(verifier),
		oauth2.SetAuthURLParam("login_hint", email), oauth2.SetAuthURLParam("prompt", "consent")))

	var got reply
	select {
	case got = <-ch:
	case <-ctx.Done():
		return "", ctx.Err()
	}
	if got.err != nil {
		return "", got.err
	}
	tok, err := cfg.Exchange(ctx, got.code, oauth2.VerifierOption(verifier))
	if err != nil {
		return "", err
	}
	if tok.RefreshToken == "" {
		return "", errors.New("google gave no refresh token; remove Zmail at myaccount.google.com/permissions and try again")
	}
	s.RefreshToken = tok.RefreshToken
	out, err := json.Marshal(s)
	return string(out), err
}

// xoauth2 is the SASL XOAUTH2 mechanism Gmail's IMAP takes.
type xoauth2 struct{ user, token string }

func (x xoauth2) Start() (string, []byte, error) {
	return "XOAUTH2", []byte("user=" + x.user + "\x01auth=Bearer " + x.token + "\x01\x01"), nil
}

// Next is reached only when the server refuses the token, with the reason.
func (x xoauth2) Next(challenge []byte) ([]byte, error) {
	return nil, fmt.Errorf("xoauth2: %s", challenge)
}
