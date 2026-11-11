package labels

import "testing"

func TestParseNumericLabels(t *testing.T) {
	got := Parse("关联单据类型：0-全部，1-日常收支，2-内部转账，3-债权债务", false)
	wantKeys := []string{"0", "1", "2", "3"}
	if got.Len() != len(wantKeys) {
		t.Fatalf("len = %d, keys = %#v", got.Len(), got.Keys)
	}
	for i, k := range wantKeys {
		if got.Keys[i] != k {
			t.Fatalf("key[%d] = %q, want %q", i, got.Keys[i], k)
		}
	}
	if got.Get("0") != "全部" || got.Get("1") != "日常收支" || got.Get("3") != "债权债务" {
		t.Fatalf("labels = %#v", got.M)
	}
}

func TestParsePlusForm(t *testing.T) {
	text := "关联单据类型\n+ 日常收支：1\n+ 内部转账：2\n+ 债权债务: 3"
	got := Parse(text, false)
	if got.Len() != 3 {
		t.Fatalf("len = %d, keys = %#v", got.Len(), got.Keys)
	}
	if got.Get("1") != "日常收支" || got.Get("2") != "内部转账" || got.Get("3") != "债权债务" {
		t.Fatalf("labels = %#v", got.M)
	}
}

func TestHanGateRejectsDatesAndAmounts(t *testing.T) {
	for _, text := range []string{"2026-11-06", "金额 100.00", "2026/11/06 12:30"} {
		if got := Parse(text, false); got.Len() != 0 {
			t.Errorf("Parse(%q) = %#v, want empty", text, got.M)
		}
	}
}

func TestFullwidthPunctuation(t *testing.T) {
	got := Parse("完成状态：1：已完成，2：部分完成，3：未完成", false)
	if got.Get("1") != "已完成" || got.Get("2") != "部分完成" || got.Get("3") != "未完成" {
		t.Fatalf("labels = %#v (keys %#v)", got.M, got.Keys)
	}
}

// 括号里的补充说明要么整段保留、要么整段丢掉，不能只剩半个括号。
// 之前 reAlt 的字符类排除了右括号 ）却放过了左括号 （，于是
// "2 读写（默认）" 被截成 "读写（默认"。
func TestBalancedParentheses(t *testing.T) {
	cases := []struct {
		name string
		text string
		want map[string]string
	}{
		{
			name: "全角括号整段保留",
			text: "受邀者初始权限：1 完整 / 2 读写（默认）/ 3 只读",
			want: map[string]string{"1": "完整", "2": "读写（默认）", "3": "只读"},
		},
		{
			name: "括号内的分隔符不断开标签",
			text: "权限：1 完全 / 2 读写 / 3 只读（0 = 不改；管理员永远是 1）",
			want: map[string]string{"1": "完全", "2": "读写", "3": "只读（0 = 不改；管理员永远是 1）"},
		},
		{
			name: "ASCII 括号同样处理",
			text: "状态：1 启用(默认), 2 停用",
			want: map[string]string{"1": "启用(默认)", "2": "停用"},
		},
		{
			name: "嵌套括号按层级配对",
			text: "状态：1 启用（含子项（二级））/ 2 停用",
			want: map[string]string{"1": "启用（含子项（二级））", "2": "停用"},
		},
		{
			name: "未闭合的左括号整段丢弃",
			text: "状态：1 启用（默认",
			want: map[string]string{"1": "启用"},
		},
		{
			name: "没有配对的右括号仍是终点",
			text: "状态：1 启用）后续",
			want: map[string]string{"1": "启用"},
		},
		{
			name: "括号后的句号不进标签",
			text: "权限：1 完全 / 2 读写 / 3 只读（管理员恒为 1）。",
			want: map[string]string{"1": "完全", "2": "读写", "3": "只读（管理员恒为 1）"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Parse(tc.text, false)
			for v, want := range tc.want {
				if got.Get(v) != want {
					t.Errorf("value %s = %q, want %q (all: %#v)", v, got.Get(v), want, got.M)
				}
			}
		})
	}
}

func TestStringEnumBalancedParentheses(t *testing.T) {
	got := Parse("排序方式：ASC-升序（默认），DESC-降序", true)
	if got.Get("ASC") != "升序（默认）" || got.Get("DESC") != "降序" {
		t.Fatalf("labels = %#v (keys %#v)", got.M, got.Keys)
	}
}

func TestStringEnumsOptIn(t *testing.T) {
	text := "排序方式：ASC-升序，DESC-降序；OEM：zm-众马"
	if got := Parse(text, false); got.Len() != 0 {
		t.Fatalf("string enums must be off by default: %#v", got.M)
	}
	got := Parse(text, true)
	if got.Get("ASC") != "升序" || got.Get("DESC") != "降序" || got.Get("zm") != "众马" {
		t.Fatalf("labels = %#v (keys %#v)", got.M, got.Keys)
	}
}

func TestParentDescriptionWindow(t *testing.T) {
	lines := []string{
		"      - description: 类型：0-全部，1-日常收支，2-内部转账",
		"        in: query",
		"        name: bus_type",
		"        schema:",
		"          enum:",
		"          - 0",
	}
	if got := ParentDescription(lines, 3); got != "类型：0-全部，1-日常收支，2-内部转账" {
		t.Fatalf("parent description = %q", got)
	}
	far := make([]string, 12)
	far[0] = "      - description: 0-全部，1-日常收支"
	for i := 1; i < len(far); i++ {
		far[i] = "        x: y"
	}
	if got := ParentDescription(far, 11); got != "" {
		t.Fatalf("description more than 8 lines away must be ignored: %q", got)
	}
}
