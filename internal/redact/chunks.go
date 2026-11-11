package redact

import (
	"fmt"
	"regexp"
	"strconv"
)

// What a window shows in place of a hidden value is a chip: the kind's name
// and the value's number, in the kind's color. The text carries it as the
// placeholder, which Chunks reads back.

var labels = map[Kind]string{
	Secret: "密钥", ID: "证件号", Email: "邮箱", Phone: "手机号", Card: "银行卡",
	Code: "验证码", Password: "密码", Person: "姓名", Address: "地址", Account: "账号",
	Salary: "薪资", Amount: "金额",
}

// Label names a kind to a reader.
func Label(k Kind) string {
	if l, ok := labels[k]; ok {
		return l
	}
	return string(k)
}

// Chip is the text of a chip: "邮箱 1".
func Chip(k Kind, n int) string { return fmt.Sprintf("%s %d", Label(k), n) }

var rePlace = regexp.MustCompile(`‹([A-Z]+)_(\d+)›`)

// A Chunk is a stretch of masked text: plain, when Kind is empty, or one
// hidden value, its number N.
type Chunk struct {
	Text string
	Kind Kind
	N    int
}

// Chunks cuts masked text at its placeholders.
func Chunks(s string) []Chunk {
	var out []Chunk
	last := 0
	for _, m := range rePlace.FindAllStringSubmatchIndex(s, -1) {
		k := Kind(s[m[2]:m[3]])
		if _, ok := labels[k]; !ok {
			continue
		}
		n, _ := strconv.Atoi(s[m[4]:m[5]])
		if m[0] > last {
			out = append(out, Chunk{Text: s[last:m[0]]})
		}
		out = append(out, Chunk{Text: s[m[0]:m[1]], Kind: k, N: n})
		last = m[1]
	}
	if last < len(s) || len(out) == 0 {
		out = append(out, Chunk{Text: s[last:]})
	}
	return out
}

// Marked says whether s holds a placeholder.
func Marked(s string) bool { return len(Chunks(s)) > 1 || Chunks(s)[0].Kind != "" }
