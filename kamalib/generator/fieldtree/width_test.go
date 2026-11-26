package fieldtree

import (
	"strings"
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/golden"
)

func TestWidth(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want int
	}{
		{"ascii", "abc", 3},
		{"cjk", "日本語", 6},
		{"full-width punctuation", "、。（）", 8},
		{"half-width katakana", "ｶﾅ", 2},
		{"emoji", "🎉", 2},
		{"zwj sequence", "👨\u200d👩\u200d👧", 2},
		{"flag", "🇯🇵", 2},
		{"skin tone", "👍🏽", 2},
		{"text presentation", "☀", 1},
		{"emoji presentation by VS16", "☀\ufe0f", 2},
		{"combining mark", "cafe\u0301", 4},
		{"branch", "├─ ", 3},
		{"last branch", "└─ ", 3},
		{"indent", "│   ", 4},
		{"ellipsis", "…", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, width(tc.in), tc.want)
		})
	}
}

// wideSrc writes the characters a source editor may silently normalize or
// drop as placeholders, which wideSource replaces.
const wideSrc = `package p

//kama:fieldtree wide
type Wide struct {
	// 名前はユーザーの表示名です。
	Name string ` + "`json:\"name\"`" + `
	Mark string ` + "`json:\"mark\"`" + ` // 区切り：「全角」（括弧）
	Party bool ` + "`json:\"party\"`" + ` // パーティー 🎉 開始
	Family int ` + "`json:\"family\"`" + ` // 家族 👨{ZWJ}👩{ZWJ}👧 です
	Region string ` + "`json:\"region\"`" + ` // 地域 🇯🇵 です
	Cafe string ` + "`json:\"cafe\"`" + ` // cafe{ACUTE} au lait
	Sun bool ` + "`json:\"sun\"`" + ` // 晴れ ☀{VS16} です
	Grid [][]string ` + "`json:\"grid\"`" + ` // 格子
	Inner Inner ` + "`json:\"inner\"`" + `
}

type Inner struct {
	// 深い階層の説明。
	Deep string ` + "`json:\"deep\"`" + `
	Plain int ` + "`json:\"plain\"`" + ` // plain text
}
`

var wideSource = strings.NewReplacer(
	"{ZWJ}", "\u200d",
	"{ACUTE}", "\u0301",
	"{VS16}", "\ufe0f",
).Replace(wideSrc)

func TestWideCharacterTree(t *testing.T) {
	texts, warnings, err := gen(t, map[string]string{"p/p.go": wideSource}, nil)
	assert.NilError(t, err)
	assert.Equal(t, len(warnings), 0, "%v", warnings)

	tree := texts["wide"]
	golden.Assert(t, tree, "wide.golden")

	hashAt, keyAt := -1, -1

	for i, l := range strings.Split(strings.TrimSuffix(tree, "\n"), "\n")[1:] {
		h := strings.Index(l, "# ")
		// A key path follows two spaces, and an access expression may hold
		// parentheses of its own.
		k := strings.LastIndex(l, "  (")
		assert.Assert(t, h >= 0 && k > h, "line %d: %q", i+1, l)

		if hashAt < 0 {
			hashAt, keyAt = width(l[:h]), width(l[:k])
		}

		assert.Equal(t, width(l[:h]), hashAt, "line %d: %q", i+1, l)
		assert.Equal(t, width(l[:k]), keyAt, "line %d: %q", i+1, l)
	}
}
