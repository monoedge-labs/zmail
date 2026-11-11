package redact

import "github.com/monoedge-labs/zmail/internal/guard"

// guardKinds are the kinds of what the model finds. Its urls, IP addresses
// and dates are left out: in mail they are nearly always the sender's links
// and the mail's own dates.
var guardKinds = map[string]Kind{
	"private_person":  Person,
	"private_address": Address,
	"phone":           Phone,
	"email":           Email,
	"account":         Account,
	"payment_card":    Card,
	"common_secret":   Secret,
}

// FromGuard reads texts with the model, keeping what it is sure of (the
// publisher's default threshold) and of a kind above.
func FromGuard(m *guard.Model) Model {
	return func(text string) ([]Found, bool) {
		var out []Found
		for _, s := range m.Find(text) {
			if k, ok := guardKinds[s.Label]; ok && s.Score >= guard.MinScore {
				out = append(out, Found{s.Start, s.End, k})
			}
		}
		return out, true
	}
}
