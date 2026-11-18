package mailbox

import (
	"regexp"
	"strings"
)

// PageHTML is a message's markup as a page, for a web view: a policy that
// leaves it styles, pictures and fonts and no script, frame, form or
// plug-in. A mail of plain paragraphs gets room around its text; one laid
// out in a table of its own, as newsletters and notifications are, keeps
// the whole page, to its edges. extraCSS adds the app's own rules after
// the page's (the window's redaction mask, empty for the phone app).
func PageHTML(markup, extraCSS string) string {
	return `<!doctype html><meta charset=utf-8>` +
		`<meta name=viewport content="width=device-width">` +
		`<meta http-equiv=Content-Security-Policy content="default-src 'none'; img-src https: http: data:; style-src 'unsafe-inline' https: http:; font-src https: http: data:">` +
		`<style>` + pageLook(markup) + `body{margin:0;padding:` + pagePad + `}` + ownLayout + `{padding:0}` + dataTable + `img{max-width:100%;height:auto}img[src^="cid:" i]{display:none}` + extraCSS + `</style>` +
		markup
}

// pageLook is the page's ground and type. A mail with colors of its own is
// drawn for white, so it gets white. One without is the app's to dress: a
// soft ground and larger type, the app's blue for links, and the dark
// ground of the system's dark appearance.
func pageLook(markup string) string {
	const font = `font:14px/1.5 -apple-system,"PingFang SC",sans-serif`
	if authored(markup) {
		return `html{background:#fff;color:#202124;color-scheme:light;` + font + `}`
	}
	return `html{background:#fcfcfd;color:#1f2430;color-scheme:light;font:15px/1.7 -apple-system,"PingFang SC",sans-serif}` +
		`a{color:#2558d9}table[border],table[border] :is(th,td){border-color:#d5dae3}th{background:#f3f5f9}` +
		`@media (prefers-color-scheme:dark){html{background:#20232f;color:#e4e8f0;color-scheme:dark}a{color:#7fa0ff}` +
		`table[border],table[border] :is(th,td){border-color:rgba(255,255,255,.2)}th{background:rgba(255,255,255,.07)}}`
}

// authoredRE finds what gives a mail colors, sizes or pictures of its own: a
// style or class, a font or color attribute, a background, a picture, or a
// table with a width. Where it finds none, the mail is plain text in markup.
var authoredRE = regexp.MustCompile(`(?i)\b(?:style|class|bgcolor|color|background)\s*=|<font\b|background|<img\b|<table\b[^>]*\bwidth\s*=`)

// styledRE finds a rule of a style block that colors a plain element, as
// `p { color: red }` does; a rule of a class cannot reach a body without classes.
var styledRE = regexp.MustCompile(`(?is)(?:^|[},\s])(?:html|body|p|a|td|th|table|div|span|li|h[1-6])\s*(?:,[^{]*)?\{[^}]*(?:color|background)`)

var styleBlockRE = regexp.MustCompile(`(?is)<style\b[^>]*>(.*?)</style>`)

// authored reports whether a mail brings colors of its own.
func authored(markup string) bool {
	body := markup
	if i := strings.Index(strings.ToLower(markup), "<body"); i >= 0 {
		body = markup[i:]
	}
	if authoredRE.MatchString(body) {
		return true
	}
	for _, m := range styleBlockRE.FindAllStringSubmatch(markup, -1) {
		if styledRE.MatchString(m[1]) {
			return true
		}
	}
	return false
}

// pagePad is the room around the text of a mail that brings no layout.
const pagePad = "20px 24px"

// dataTable draws a table the sender gave a border attribute, which a browser
// shows as double lines with the text against them, as a plain grid.
const dataTable = `table[border]{border-collapse:collapse}table[border] :is(th,td){padding:4px 8px}`

// ownLayout selects the body of a mail laid out in a table of its own, which
// takes the page to its edges.
const ownLayout = `body:has(>table,>center,>div>table,>div>center,>div>div>table):not(:has(>p,>h1,>h2,>h3,>h4,>ul,>ol,>pre,>blockquote))`
