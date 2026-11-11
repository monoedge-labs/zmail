package mailbox

import (
	"bytes"
	"encoding/base64"
	"io"
	"net/url"
	"regexp"
	"strings"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

// A mail names the pictures it carries by Content-ID (<img src="cid:…">).
// They sit in parts of the message that the first read, cut after its first
// parts, mostly lacks, so the parts are fetched once the markup asks for
// them and put in the markup as data: addresses.

const (
	maxInlineImage = 8 << 20  // bytes of one picture: a screenshot, a poster
	maxInlineTotal = 16 << 20 // bytes of the pictures of one message
)

var reCID = regexp.MustCompile(`(?i)\bcid:([^"'\s)>]+)`)

// pictureTypes are the pictures worth showing: a vector one could carry markup.
var pictureTypes = map[string]bool{"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true}

func hasCID(markup string) bool { return reCID.MatchString(markup) }

// cidKey is a Content-ID as the markup spells it: no brackets, unescaped.
func cidKey(id string) string {
	id = strings.Trim(strings.TrimSpace(id), "<>")
	if u, err := url.PathUnescape(id); err == nil {
		id = u
	}
	return strings.ToLower(id)
}

type inlinePart struct {
	path []int
	cid  string
	mime string
	enc  string
}

// inlineParts lists the pictures of a message that carry a Content-ID.
func inlineParts(bs imap.BodyStructure) []inlinePart {
	var out []inlinePart
	bs.Walk(func(path []int, p imap.BodyStructure) bool {
		sp, ok := p.(*imap.BodyStructureSinglePart)
		if !ok || sp.ID == "" || !pictureTypes[sp.MediaType()] {
			return true
		}
		if sp.Size > maxInlineImage*4/3+1024 { // base64 is a third larger
			return true
		}
		out = append(out, inlinePart{append([]int(nil), path...), cidKey(sp.ID), sp.MediaType(), strings.ToLower(sp.Encoding)})
		return true
	})
	return out
}

// dataURI is a part's body, as it was sent, as an address; ok is false for
// an encoding that is not known.
func dataURI(mime, enc string, body []byte) (uri string, ok bool) {
	switch enc {
	case "base64":
		raw, err := io.ReadAll(base64.NewDecoder(base64.StdEncoding, bytes.NewReader(body)))
		if err != nil || len(raw) == 0 || len(raw) > maxInlineImage {
			return "", false
		}
		return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(raw), true
	case "7bit", "8bit", "binary", "":
		if len(body) == 0 || len(body) > maxInlineImage {
			return "", false
		}
		return "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(body), true
	}
	return "", false
}

// replaceCID puts the pictures in the markup; those it has none for stay.
func replaceCID(markup string, pics map[string]string) string {
	return reCID.ReplaceAllStringFunc(markup, func(m string) string {
		if uri, ok := pics[cidKey(m[len("cid:"):])]; ok {
			return uri
		}
		return m
	})
}

// inlineImages fetches the pictures markup names by Content-ID. It returns
// the markup as it was when there is a failure: the mail shows without them.
func inlineImages(c *imapclient.Client, uid imap.UID, markup string) string {
	set := imap.UIDSetNum(uid)
	st, err := c.Fetch(set, &imap.FetchOptions{BodyStructure: &imap.FetchItemBodyStructure{}}).Collect()
	if err != nil || len(st) == 0 || st[0].BodyStructure == nil {
		return markup
	}
	parts := inlineParts(st[0].BodyStructure)
	if len(parts) == 0 {
		return markup
	}
	items := make([]*imap.FetchItemBodySection, len(parts))
	for i, p := range parts {
		items[i] = &imap.FetchItemBodySection{Part: p.path, Peek: true}
	}
	got, err := c.Fetch(set, &imap.FetchOptions{BodySection: items}).Collect()
	if err != nil || len(got) == 0 {
		return markup
	}
	pics, total := map[string]string{}, 0
	for i, p := range parts {
		uri, ok := dataURI(p.mime, p.enc, got[0].FindBodySection(items[i]))
		if !ok || total+len(uri) > maxInlineTotal*4/3 {
			continue
		}
		total += len(uri)
		pics[p.cid] = uri
	}
	return replaceCID(markup, pics)
}
