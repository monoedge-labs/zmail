package mailbox

import (
	"fmt"
	"strings"
)

// Provider describes a mail service the app can add.
type Provider struct {
	ID     string
	Name   string
	Host   string // IMAP host:port
	Secret string // what the user enters besides the address
}

// Providers are the services supported: each takes an app password
// (Google, iCloud), a client password (Aliyun) or an authorization code (QQ),
// never the account's own password.
var Providers = []Provider{
	{ID: "google", Name: "Google", Host: "imap.gmail.com:993", Secret: "应用专用密码"},
	{ID: "aliyun", Name: "阿里邮箱", Host: "imap.qiye.aliyun.com:993", Secret: "客户端专用密码"},
	{ID: "qq", Name: "QQ 邮箱", Host: "imap.qq.com:993", Secret: "授权码"},
	{ID: "icloud", Name: "iCloud 邮箱", Host: "imap.mail.me.com:993", Secret: "App 专用密码"},
}

// ProviderByID finds a provider.
func ProviderByID(id string) (Provider, bool) {
	for _, p := range Providers {
		if p.ID == id {
			return p, true
		}
	}
	return Provider{}, false
}

// HostFor returns the IMAP server of an address under a provider: personal
// Aliyun addresses use another server than company ones.
func HostFor(providerID, email string) (string, error) {
	p, ok := ProviderByID(providerID)
	if !ok {
		return "", fmt.Errorf("unknown provider %q", providerID)
	}
	if providerID == "aliyun" && strings.HasSuffix(strings.ToLower(email), "@aliyun.com") {
		return "imap.aliyun.com:993", nil
	}
	return p.Host, nil
}
