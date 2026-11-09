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
