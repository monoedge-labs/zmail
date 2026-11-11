package redact

import (
	"fmt"
	"regexp"
	"strconv"
)

// What a window shows in place of a hidden value is blurred text of the same
// shape (Fake): a stand-in, as the real value never reaches the window. The
// text carries the value as the placeholder, which Chunks reads back.

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

// Chip names a hidden value to a reader, as a tooltip does: "邮箱 1".
func Chip(k Kind, n int) string { return fmt.Sprintf("%s %d", Label(k), n) }

// Fake is text shaped like the value of kind k that the window blurs in its
// place: the same value, the same number, shows the same stand-in. It holds
// nothing of the value, only the look of one.
func Fake(k Kind, n int) string {
	x := uint32(n)*2654435761 + 40503
	for _, c := range string(k) {
		x = x*16777619 + uint32(c)
	}
	next := func(m int) int { x = x*1664525 + 1013904223; return int(x>>8) % m }
	pick := func(set string, n int) string {
		r := []rune(set)
		out := make([]rune, n)
		for i := range out {
			out[i] = r[next(len(r))]
		}
		return string(out)
	}
	const lower, digits, mixed = "abcdefghijkmnpqrstuvwxyz", "0123456789", "abcdefghijkmnpqrstuvwxyzABCDEFGHJKLMNPQRSTUVWXYZ23456789"
	switch k {
	case Email:
		return pick(lower, 6+next(4)) + "@" + pick(lower, 5+next(3)) + ".com"
	case Phone:
		return "1" + pick("3456789", 1) + pick(digits, 9)
	case Card:
		return pick(digits, 4) + " " + pick(digits, 4) + " " + pick(digits, 4) + " " + pick(digits, 4)
	case ID:
		return pick(digits, 17) + pick("0123456789X", 1)
	case Account:
		return pick(digits, 10+next(4))
	case Code:
		return pick(digits, 6)
	case Secret:
		return pick(mixed, 22+next(6))
	case Password:
		return pick(mixed, 8+next(4))
	case Salary, Amount:
		return pick("123456789", 1) + pick(digits, 1+next(2)) + "," + pick(digits, 3) + "." + pick(digits, 2)
	case Person:
		return pick("李王张刘陈杨赵黄周吴徐孙胡朱高林何郭马罗", 1) + pick("明华伟芳娜敏静磊洋勇艳杰涛超丽强军平", 1+next(2))
	case Address:
		return pick("北上广深杭成武南西", 1) + "市" + pick("海朝浦天江东西南", 1) + pick("区县", 1) + pick("中新长安人民", 2) + "路" + pick(digits, 2) + "号"
	}
	return pick(lower, 8)
}

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
