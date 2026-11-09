package document

import (
	"regexp"
	"strings"
)

var (
	reSingleQuoted = regexp.MustCompile(`^'(.*)'$`)
	reDoubleQuoted = regexp.MustCompile(`^"(.*)"$`)
	reNumber       = regexp.MustCompile(`^-?[0-9]+(\.[0-9]+)?$`)
	reLabelColon   = regexp.MustCompile(`:\s`)
	reLabelSpecial = regexp.MustCompile("^[-?:,\\[\\]{}&*!|>@`]")
)

// FormatValue formats an enum value as a YAML scalar, mirroring
// Format-YamlValue: numbers/booleans/null pass through, quoted input is
// unquoted and forced to a string, everything else is single-quoted.
func FormatValue(v string) string {
	s := strings.TrimSpace(v)
	forceString := false
	if m := reSingleQuoted.FindStringSubmatch(s); m != nil {
		s, forceString = m[1], true
	} else if m := reDoubleQuoted.FindStringSubmatch(s); m != nil {
		s, forceString = m[1], true
	}
	if !forceString {
		if reNumber.MatchString(s) {
			return s
		}
		// YAML boolean/null literals are matched case-insensitively.
		if strings.EqualFold(s, "true") || strings.EqualFold(s, "false") || strings.EqualFold(s, "null") {
			return s
		}
	}
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// FormatLabel formats a Chinese enum label as a YAML scalar.
func FormatLabel(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, `\n`, " ")
	s = strings.ReplaceAll(s, `\t`, " ")
	if s == "" {
		return "''"
	}
	quote := strings.ContainsAny(s, "'\"#") ||
		strings.Contains(s, "\n") ||
		reLabelColon.MatchString(s) ||
		reLabelSpecial.MatchString(s)
	if quote {
		return "'" + strings.ReplaceAll(s, "'", "''") + "'"
	}
	return s
}

var reBlockScalar = regexp.MustCompile(`^[|>]`)

// DescriptionText extracts and decodes a node's description property.
// It supports |/> block scalars, double-quoted strings (with \n, \t and \")
// and single-quoted strings (with ” escapes).
func DescriptionText(lines []string, n Node, info *NodeInfo) string {
	p, ok := info.Props["description"]
	if !ok {
		return ""
	}
	raw := p.Value
	if reBlockScalar.MatchString(raw) {
		di := Indent(lines[p.Index])
		var buf []string
		for j := p.Index + 1; j <= n.End && j < len(lines); j++ {
			if Indent(lines[j]) <= di && !IsBlank(lines[j]) {
				break
			}
			buf = append(buf, strings.TrimSpace(lines[j]))
		}
		raw = strings.Join(buf, "\n")
	}
	if m := reDoubleQuoted.FindStringSubmatch(raw); m != nil {
		r := m[1]
		r = strings.ReplaceAll(r, `\n`, "\n")
		r = strings.ReplaceAll(r, `\t`, " ")
		r = strings.ReplaceAll(r, `\"`, `"`)
		return r
	}
	if m := reSingleQuoted.FindStringSubmatch(raw); m != nil {
		return strings.ReplaceAll(m[1], "''", "'")
	}
	return raw
}
