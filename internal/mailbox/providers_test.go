package mailbox

import (
	"net/url"
	"strings"
	"testing"
)

// The help links lead to the providers' own sites, with nothing of the user in them.
func TestProviderHelpLinks(t *testing.T) {
	official := map[string][]string{
		"google": {"support.google.com", "myaccount.google.com"},
		"aliyun": {"help.aliyun.com", "www.alibabacloud.com"},
		"qq":     {"help.mail.qq.com", "service.mail.qq.com"},
		"icloud": {"support.apple.com", "account.apple.com"},
	}
	for _, p := range Providers {
		u, err := url.Parse(p.Help)
		if err != nil || u.Scheme != "https" || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
			t.Errorf("%s: help %q must be a plain https URL", p.ID, p.Help)
			continue
		}
		ok := false
		for _, h := range official[p.ID] {
			ok = ok || u.Hostname() == h
		}
		if !ok {
			t.Errorf("%s: %s is not an official host", p.ID, u.Hostname())
		}
		if strings.Contains(p.Help, "@") || p.HelpNote == "" {
			t.Errorf("%s: help %q, note %q", p.ID, p.Help, p.HelpNote)
		}
	}
}
