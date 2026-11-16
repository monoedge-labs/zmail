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

// GoogleClientID and GoogleClientSecret are the OAuth client Zmail is built
// with, of the Desktop app type: what --oauth signs in with when no other is
// given. Google treats an installed app's client pair as public — anyone can
// read it from the binary — so shipping both is the accepted practice
// (Thunderbird et al. do the same); the real secret is the refresh token.
const GoogleClientID = "826672560130-o7gortqf86q76er3iaknpafdrqguf6tr.apps.googleusercontent.com"
const GoogleClientSecret = "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"

type oauthSecret struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	RefreshToken string `json:"refresh_token"`
}

func (s oauthSecret) config() *oauth2.Config {
	return &oauth2.Config{ClientID: s.ClientID, ClientSecret: s.ClientSecret, Endpoint: googleEndpoint,
		Scopes: []string{googleScope}}
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
// GoogleLogin signs a Google account in at a page it opens, the code
// coming back over a loopback redirect. It returns the OAuth secret JSON
// and the address the sign-in was with, which the account's name is:
// the email argument is only a login hint, and the sign-in may be with
// another of the browser's accounts.
func GoogleLogin(ctx context.Context, clientID, clientSecret, email string, open func(url string)) (secret, signed string, err error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", "", err
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
		return "", "", ctx.Err()
	}
	if got.err != nil {
		return "", "", got.err
	}
	tok, err := cfg.Exchange(ctx, got.code, oauth2.VerifierOption(verifier))
	if err != nil {
		return "", "", err
	}
	if tok.RefreshToken == "" {
		return "", "", errors.New("google gave no refresh token; remove Zmail at myaccount.google.com/permissions and try again")
	}
	s.RefreshToken = tok.RefreshToken
	out, err := json.Marshal(s)
	if err != nil {
		return "", "", err
	}
	if e, err := gmailEmail(ctx, tok.AccessToken); err == nil {
		signed = e
	}
	if signed == "" {
		signed = email
	}
	if signed == "" {
		return "", "", errors.New("google signed in but the account's address is unknown; type the Gmail address and try again")
	}
	return string(out), signed, nil
}

// gmailProfile is the endpoint an access token reads its account's
// address from; a var so a test can point it at a stub.
var gmailProfile = "https://gmail.googleapis.com/gmail/v1/users/me/profile"

// gmailEmail is the address an access token signs in as, from the Gmail
// profile endpoint, which the mail scope alone can read (an ID token
// would need an extra OpenID grant, and a granted one is not refreshed).
func gmailEmail(ctx context.Context, accessToken string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, gmailProfile, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	var p struct {
		Email string `json:"emailAddress"`
	}
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("profile: %s", res.Status)
	}
	if err := json.NewDecoder(res.Body).Decode(&p); err != nil {
		return "", err
	}
	return p.Email, nil
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
