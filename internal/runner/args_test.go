package runner

import (
	"reflect"
	"testing"
)

func TestSplitArgs(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"--parseDependency --parseDepth 1", []string{"--parseDependency", "--parseDepth", "1"}},
		{"", nil},
		{"   ", nil},
		{`--exclude "./vendor dir" --parseDepth 1`, []string{"--exclude", "./vendor dir", "--parseDepth", "1"}},
		{`--tag 'a b' --ot yaml`, []string{"--tag", "a b", "--ot", "yaml"}},
		{`--name foo\ bar`, []string{"--name", "foo bar"}},
		{`--empty ''`, []string{"--empty", ""}},
	}
	for _, tc := range cases {
		got, err := splitArgs(tc.in)
		if err != nil {
			t.Fatalf("splitArgs(%q): %v", tc.in, err)
		}
		if len(got) == 0 && len(tc.want) == 0 {
			continue
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("splitArgs(%q) = %#v, want %#v", tc.in, got, tc.want)
		}
	}
}

func TestSplitArgsUnterminatedQuote(t *testing.T) {
	if _, err := splitArgs(`--exclude "oops`); err == nil {
		t.Fatal("expected an error for an unterminated quote")
	}
}

func TestPassthroughArgs(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want []string
	}{
		{"no args", nil, []string{}},
		{"individual args", []string{"--parseDependency", "--parseDepth", "1"},
			[]string{"--parseDependency", "--parseDepth", "1"}},
		{"one quoted list", []string{"--parseDependency --parseDepth 1"},
			[]string{"--parseDependency", "--parseDepth", "1"}},
		{"quoted list with a spaced value", []string{`--parseDependency --exclude "./vendor dir" --parseDepth 1`},
			[]string{"--parseDependency", "--exclude", "./vendor dir", "--parseDepth", "1"}},
		{"spaced value stays intact", []string{"--exclude", "my dir", "--parseDepth", "1"},
			[]string{"--exclude", "my dir", "--parseDepth", "1"}},
	}
	for _, tc := range cases {
		got, err := PassthroughArgs(tc.in)
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: got %#v, want %#v", tc.name, got, tc.want)
		}
	}
}

func TestPassthroughArgsUnterminatedQuote(t *testing.T) {
	if _, err := PassthroughArgs([]string{`--exclude "oops`}); err == nil {
		t.Fatal("expected an error for an unterminated quote")
	}
}

func TestSwagCommandArgs(t *testing.T) {
	got := swagCommandArgs(Config{
		SwagArgs: []string{"--parseDependency", "--parseDepth", "1", "--parseInternal"},
	}, "/tmp/out")
	want := []string{
		"init", "--v3.1", "--ot", "yaml",
		"--parseDependency", "--parseDepth", "1", "--parseInternal",
		"-o", "/tmp/out",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}

	// The zero Config is what the CLI uses when nothing follows "--": only
	// the base command plus the tool-owned -o remain.
	got = swagCommandArgs(Config{}, "/tmp/out")
	want = []string{"init", "--v3.1", "--ot", "yaml", "-o", "/tmp/out"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}
