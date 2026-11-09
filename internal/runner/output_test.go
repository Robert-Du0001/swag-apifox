package runner

import (
	"bytes"
	"strings"
	"testing"

	"github.com/Robert-Du0001/swag-apifox/internal/transform"
)

func TestShouldColor(t *testing.T) {
	t.Run("always", func(t *testing.T) {
		if !shouldColor(ColorAlways) {
			t.Fatal("always must enable colour regardless of the environment")
		}
	})
	t.Run("never", func(t *testing.T) {
		t.Setenv("CLICOLOR_FORCE", "1")
		if shouldColor(ColorNever) {
			t.Fatal("never must win over CLICOLOR_FORCE")
		}
	})
	t.Run("no color", func(t *testing.T) {
		t.Setenv("NO_COLOR", "1")
		t.Setenv("CLICOLOR_FORCE", "")
		if shouldColor(ColorAuto) {
			t.Fatal("NO_COLOR must disable auto colour")
		}
	})
	t.Run("dumb terminal", func(t *testing.T) {
		t.Setenv("NO_COLOR", "")
		t.Setenv("CLICOLOR_FORCE", "")
		t.Setenv("TERM", "dumb")
		if shouldColor(ColorAuto) {
			t.Fatal("TERM=dumb must disable auto colour")
		}
	})
	t.Run("forced", func(t *testing.T) {
		t.Setenv("NO_COLOR", "")
		t.Setenv("CLICOLOR_FORCE", "1")
		if !shouldColor(ColorAuto) {
			t.Fatal("CLICOLOR_FORCE must enable auto colour")
		}
	})
}

func TestClassify(t *testing.T) {
	cases := []struct {
		line string
		want string
	}{
		{"2026/11/09 19:49:22 Generating dto.AccountStatus", ansiCyan},
		{"2026/11/09 19:49:22 Create swagger.yaml", ansiCyan},
		{"Override detected for carbon.Date: using string instead", ansiYellow},
		{"found schema with no Type, returning any", ansiYellow},
		{"2026/11/09 19:49:22 warn: skipped field", ansiYellow},
		{"Error parsing type definition 'models.X': cannot find type definition: orm.Model", ansiRed},
		{"ParseComment error in file main.go", ansiRed},
		{"2026/11/09 19:49:22 swag init failed", ansiRed},
		{"some plain informational line", ""},
	}
	for _, tc := range cases {
		if got := classify(tc.line); got != tc.want {
			t.Errorf("classify(%q) = %q, want %q", tc.line, got, tc.want)
		}
	}
}

func TestSwagLogColourised(t *testing.T) {
	var buf bytes.Buffer
	l := newSwagLog(&buf, true)
	_, _ = l.Write([]byte("Generating dto.Foo\nOverride detected for carbon.Date\n"))
	_, _ = l.Write([]byte("Error parsing type definition\nplain line\n"))
	l.Flush()
	got := buf.String()
	for _, want := range []string{
		ansiCyan + "Generating dto.Foo" + ansiReset,
		ansiYellow + "Override detected for carbon.Date" + ansiReset,
		ansiRed + "Error parsing type definition" + ansiReset,
		"\nplain line\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output %q does not contain %q", got, want)
		}
	}
	if strings.Count(got, "\n") != 4 {
		t.Errorf("want 4 lines, got %d: %q", strings.Count(got, "\n"), got)
	}
}

func TestSwagLogNoColour(t *testing.T) {
	var buf bytes.Buffer
	l := newSwagLog(&buf, false)
	_, _ = l.Write([]byte("Error parsing type definition\nGenerating dto.Foo\n"))
	l.Flush()
	got := buf.String()
	if strings.Contains(got, "\x1b") {
		t.Fatalf("colour disabled but escapes were emitted: %q", got)
	}
	if got != "Error parsing type definition\nGenerating dto.Foo\n" {
		t.Fatalf("unexpected output: %q", got)
	}
}

func TestSwagLogChunkedAndFlush(t *testing.T) {
	var buf bytes.Buffer
	l := newSwagLog(&buf, true)
	_, _ = l.Write([]byte("Gener"))
	_, _ = l.Write([]byte("ating dto.Foo\n"))
	if !strings.Contains(buf.String(), ansiCyan+"Generating dto.Foo"+ansiReset) {
		t.Fatalf("split line was not reassembled: %q", buf.String())
	}
	_, _ = l.Write([]byte("trailing fragment"))
	if strings.Contains(buf.String(), "trailing fragment") {
		t.Fatal("unterminated line must stay buffered until Flush")
	}
	l.Flush()
	if !strings.Contains(buf.String(), "trailing fragment\n") {
		t.Fatalf("Flush did not emit the trailing fragment: %q", buf.String())
	}
}

func TestSwagLogBlankLine(t *testing.T) {
	var buf bytes.Buffer
	l := newSwagLog(&buf, true)
	_, _ = l.Write([]byte("\n\r\n"))
	l.Flush()
	if buf.String() != "\n\n" {
		t.Fatalf("blank lines changed: %q", buf.String())
	}
}

func TestPrintStatsColour(t *testing.T) {
	st := &transform.Stats{SchemaEnumCount: 2}
	plain, coloured := &bytes.Buffer{}, &bytes.Buffer{}
	PrintStats(plain, "in.yaml", st, "out.yaml", 10, false)
	PrintStats(coloured, "in.yaml", st, "out.yaml", 10, true)
	if strings.Contains(plain.String(), "\x1b") {
		t.Fatal("plain stats must not contain escapes")
	}
	if !strings.Contains(coloured.String(), ansiGreen+"done."+ansiReset) {
		t.Fatalf("done. was not tinted: %q", coloured.String())
	}
	got := strings.ReplaceAll(coloured.String(), ansiGreen, "")
	got = strings.ReplaceAll(got, ansiReset, "")
	if got != plain.String() {
		t.Fatalf("colour must be the only difference: %q vs %q", got, plain.String())
	}
}

func TestConfigColorValidation(t *testing.T) {
	for _, mode := range []string{"", ColorAuto, ColorAlways, ColorNever} {
		c := Config{EnumLabelField: "description", Color: mode}
		c.Defaults()
		if err := c.Validate(); err != nil {
			t.Errorf("mode %q rejected: %v", mode, err)
		}
	}
	c := Config{EnumLabelField: "description", Color: "rainbow"}
	c.Defaults()
	if err := c.Validate(); err == nil {
		t.Fatal("invalid --color must be rejected")
	}
}
