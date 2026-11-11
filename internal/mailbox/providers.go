package mailbox

import (
	"fmt"
	"strings"
)

// Provider describes a mail service the app can add.
type Provider struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Host     string `json:"imap_host"`  // IMAP host:port
	Secret   string `json:"credential"` // what the user enters besides the address
	Help     string `json:"help_url"`   // an official page on getting that credential
	HelpNote string `json:"help_note"`  // what the page is, and what to do on it
}

// Providers are the services supported: each takes an app password
// (Google, iCloud), a client password (Aliyun) or an authorization code (QQ),
// never the account's own password.
var Providers = []Provider{
	{ID: "google", Name: "Google", Host: "imap.gmail.com:993", Secret: "应用专用密码",
		Help:     "https://support.google.com/accounts/answer/185833",
		HelpNote: "Google 账号帮助页：开启两步验证后，在其中的链接创建应用专用密码；Workspace 账号需要管理员允许"},
	{ID: "aliyun", Name: "阿里邮箱", Host: "imap.qiye.aliyun.com:993", Secret: "客户端专用密码",
		Help:     "https://help.aliyun.com/zh/document_detail/444269.html",
		HelpNote: "阿里云帮助中心：在网页版“设置 > 账户与安全”生成三方客户端安全密码；个人 @aliyun.com 邮箱用登录密码"},
	{ID: "qq", Name: "QQ 邮箱", Host: "imap.qq.com:993", Secret: "授权码",
		Help:     "https://help.mail.qq.com/detail/0/1087",
		HelpNote: "QQ 邮箱帮助中心：在网页版“设置 > 账号与安全”开启 IMAP 服务并生成授权码"},
	{ID: "icloud", Name: "iCloud 邮箱", Host: "imap.mail.me.com:993", Secret: "App 专用密码",
		Help:     "https://support.apple.com/102654",
		HelpNote: "Apple 支持页：在 account.apple.com 的“登录与安全”生成 App 专用密码"},
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
