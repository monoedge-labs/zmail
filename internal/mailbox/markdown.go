package mailbox

import (
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

// A body is kept as Markdown: the window shows its links and pictures, an
// agent reads it as text. Mail's HTML is mostly layout tables, so the
// conversion keeps blocks, line breaks, links, pictures, lists, headings and
// bold, and drops the rest of the styling.

var (
	reURL      = regexp.MustCompile(`https?://[^\s<>"'）】」，。]+[^\s<>"'）】」，。.,;:!?)]`)
	reMDEscape = regexp.MustCompile("([\\\\`*_\\[\\]<>#~|$])")
	reSpaces   = regexp.MustCompile(`[ \t\r\n\f\v\x{00a0}\x{2000}-\x{200a}\x{3000}]+`)
	reHidden   = regexp.MustCompile(`[\x{00ad}\x{034f}\x{180e}\x{200b}-\x{200f}\x{2060}-\x{2064}\x{feff}]`) // fill of preheaders
	reNoShow   = regexp.MustCompile(`(?i)display\s*:\s*none|visibility\s*:\s*hidden|max-height\s*:\s*0`)
	reLines    = regexp.MustCompile(`\n{3,}`)
)

// escapeMD makes s literal Markdown, its web addresses links.
func escapeMD(s string) string {
	var b strings.Builder
	last := 0
	for _, m := range reURL.FindAllStringIndex(s, -1) {
		b.WriteString(reMDEscape.ReplaceAllString(s[last:m[0]], `\$1`))
		b.WriteString("<" + s[m[0]:m[1]] + ">")
		last = m[1]
	}
	b.WriteString(reMDEscape.ReplaceAllString(s[last:], `\$1`))
	return b.String()
}

// textMarkdown is a text/plain body as Markdown, its lines kept.
func textMarkdown(s string) string {
	lines := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	for i, l := range lines {
		l = strings.TrimSpace(l)
		if l == "" {
			lines[i] = ""
			continue
		}
		l = escapeMD(l)
		if strings.ContainsRune("-+=", rune(l[0])) || startsNumbered(l) {
			l = `\` + l // not a list, nor a heading's underline
		}
		lines[i] = l + "  " // a hard break
	}
	return tidy(strings.Join(lines, "\n"))
}

func startsNumbered(l string) bool {
	i := strings.IndexAny(l, ".)")
	if i <= 0 || i > 9 {
		return false
	}
	_, err := strconv.Atoi(l[:i])
	return err == nil
}

// htmlMarkdown is a text/html body as Markdown.
func htmlMarkdown(s string) string {
	doc, err := html.Parse(strings.NewReader(s))
	if err != nil {
		return textMarkdown(htmlText(s))
	}
	var b strings.Builder
	walk(&b, doc)
	return tidy(b.String())
}

func tidy(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		l = strings.TrimLeft(l, " ") // indented 4 spaces would be code
		if strings.TrimSpace(l) == "" {
			l = ""
		}
		lines[i] = l
	}
	return strings.TrimSpace(reLines.ReplaceAllString(strings.Join(lines, "\n"), "\n\n"))
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return strings.TrimSpace(a.Val)
		}
	}
	return ""
}

func hasAttr(n *html.Node, key string) bool {
	for _, a := range n.Attr {
		if a.Key == key {
			return true
		}
	}
	return false
}

// webURL is u when a link may go there, quoted for a Markdown destination.
func webURL(u string) (string, bool) {
	l := strings.ToLower(u)
	if !strings.HasPrefix(l, "http://") && !strings.HasPrefix(l, "https://") && !strings.HasPrefix(l, "mailto:") {
		return "", false
	}
	return strings.NewReplacer(" ", "%20", "(", "%28", ")", "%29", "<", "%3C", ">", "%3E").Replace(u), true
}

func children(n *html.Node) string {
	var b strings.Builder
	for k := n.FirstChild; k != nil; k = k.NextSibling {
		walk(&b, k)
	}
	return b.String()
}

func walk(b *strings.Builder, n *html.Node) {
	switch n.Type {
	case html.TextNode:
		b.WriteString(escapeMD(reSpaces.ReplaceAllString(reHidden.ReplaceAllString(n.Data, ""), " ")))
		return
	case html.DocumentNode:
		b.WriteString(children(n))
		return
	case html.ElementNode:
	default:
		return
	}
	if reNoShow.MatchString(attr(n, "style")) || hasAttr(n, "hidden") {
		return // a preheader, or what a client never shows
	}
	switch n.Data {
	case "head", "style", "script", "title", "meta", "noscript", "template":
	case "br":
		b.WriteString("  \n")
	case "hr":
		b.WriteString("\n\n---\n\n")
	case "img":
		src, ok := webURL(attr(n, "src"))
		w, _ := strconv.Atoi(attr(n, "width"))
		h, _ := strconv.Atoi(attr(n, "height"))
		// Markdown shows a picture as large as it is: the icons and avatars
		// a mail draws small would show large, so they go with the
		// tracking pixels. A part of the message (cid:) was not fetched.
		if !ok || strings.HasPrefix(src, "mailto:") || (w > 0 && w <= 48) || (h > 0 && h <= 48) {
			return
		}
		alt := reSpaces.ReplaceAllString(attr(n, "alt"), " ")
		b.WriteString("![" + reMDEscape.ReplaceAllString(alt, `\$1`) + "](" + src + ")")
	case "a":
		inner := strings.TrimSpace(reSpaces.ReplaceAllString(children(n), " "))
		href, ok := webURL(attr(n, "href"))
		images := strings.Join(reMDImage.FindAllString(inner, -1), "\n\n")
		if images != "" && strings.TrimSpace(reMDImage.ReplaceAllString(inner, "")) == "" {
			// A picture that is a link, as a banner: the picture shows, since
			// Markdown shows a picture inside a link as its description.
			b.WriteString("\n\n" + images + "\n\n")
			return
		}
		inner = strings.TrimSpace(reMDImage.ReplaceAllString(inner, "")) // the icons of a text link
		switch {
		case inner == "":
		case !ok || strings.HasPrefix(inner, "<"): // no address, or an address shown as one
			b.WriteString(inner)
		default:
			b.WriteString("[" + inner + "](" + href + ")")
		}
	case "b", "strong":
		if inner := strings.TrimSpace(children(n)); inner != "" && !strings.Contains(inner, "\n") {
			b.WriteString("**" + inner + "**")
		} else {
			b.WriteString(inner)
		}
	case "li":
		b.WriteString("\n- " + strings.TrimSpace(reSpaces.ReplaceAllString(children(n), " ")) + "\n")
	case "h1", "h2", "h3", "h4", "h5", "h6":
		if inner := strings.TrimSpace(reSpaces.ReplaceAllString(children(n), " ")); inner != "" {
			b.WriteString("\n\n" + strings.Repeat("#", int(n.Data[1]-'0')) + " " + inner + "\n\n")
		}
	case "blockquote":
		inner := tidy(children(n))
		b.WriteString("\n\n> " + strings.ReplaceAll(inner, "\n", "\n> ") + "\n\n")
	case "td", "th":
		b.WriteString(children(n) + " ")
	case "p", "div", "table", "tr", "ul", "ol", "section", "article", "header", "footer", "center", "pre", "tbody", "thead", "dl", "dt", "dd", "address", "figure":
		b.WriteString("\n\n" + children(n) + "\n\n")
	default:
		b.WriteString(children(n))
	}
}

var (
	reMDImage = regexp.MustCompile(`!\[[^\]]*\]\([^)]*\)`)
	reMDLink  = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
	reMDAuto  = regexp.MustCompile(`<(https?://[^>]+)>`)
	reMDMarks = regexp.MustCompile(`\*\*|^#+ |\n#+ |^> |\n> `)
	reMDEsc   = regexp.MustCompile(`\\(.)`)
)

// PlainText is a Markdown body as text: its links' labels, no pictures.
func PlainText(md string) string {
	s := reMDImage.ReplaceAllString(md, "")
	s = reMDLink.ReplaceAllString(s, "$1")
	s = reMDAuto.ReplaceAllString(s, "$1")
	s = reMDMarks.ReplaceAllString(s, " ")
	return reMDEsc.ReplaceAllString(s, "$1")
}
