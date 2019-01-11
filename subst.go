package tarsnap

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Cf. https://github.com/Tarsnap/tarsnap/blob/master/tar/subst.c

// A Rule is a path substitution rule, as defined for the "-s" flag of the
// tarsnap command-line tool.
//
// See: https://www.tarsnap.com/man-tarsnap.1.html
type Rule struct {
	lhs     *regexp.Regexp
	rhs     string
	global  bool // apply to all matches
	print   bool // print result (not used, saved for rendering)
	symlink bool // apply to symlinks (not used)
}

// Apply reports whether s matches the left-hand side of the rule and, if so,
// returns the result from applying the rule to the string.
func (r *Rule) Apply(s string) (string, bool) {
	// TODO: Handle r.global.
	m := r.lhs.FindStringSubmatchIndex(s)
	if m == nil {
		return s, false
	}
	t := string(r.lhs.ExpandString(nil, r.rhs, s, m))
	return s[:m[0]] + t + s[m[1]:], true
}

// ParseRule parses a substitution rule from a string argument.  The input must
// have the form "/old/new/prg".
func ParseRule(s string) (*Rule, error) {
	parts := strings.SplitN(s, "/", 4)
	if len(parts) != 4 || parts[0] != "" {
		return nil, errors.New("invalid rule format")
	}

	lhs, err := regexp.Compile(basicToRE(parts[1]))
	if err != nil {
		return nil, fmt.Errorf("invalid match pattern: %v", err)
	}
	rule := &Rule{
		lhs: lhs,
		rhs: fixSubs(parts[2]),
	}
	for _, ch := range parts[3] {
		switch ch {
		case 'g', 'G':
			rule.global = true
		case 'p', 'P':
			rule.print = true
		case 's', 'S':
			rule.symlink = true
		default:
			return nil, fmt.Errorf("unknown flag: %c", ch)
		}
	}
	return rule, err
}

func fixSubs(s string) string {
	var re strings.Builder
	esc := false // pending escape sequence
	for _, ch := range s {
		switch ch {
		case '\\':
			if !esc {
				esc = true
				continue
			}
		case '$': // literal in pattern outputs
			if !esc {
				re.WriteString("$$")
				continue
			}
		case '~': // denotes the match entire
			if !esc {
				re.WriteString("${0}")
				continue
			}
		}
		if esc {
			esc = false
			if ch >= '1' && ch <= '9' {
				re.WriteString("${")
				re.WriteRune(ch)
				re.WriteRune('}')
				continue
			}
			re.WriteRune('\\')
		}
		re.WriteRune(ch)
	}
	return re.String()
}

func basicToRE(s string) string {
	// Convert s from a POSIX BRE to RE2 syntax.  Broadly this requires
	// swizzling the weird escape sequences.
	var re strings.Builder
	esc := false // pending escape sequence
	for _, ch := range s {
		switch ch {
		case '(', ')', '{', '}':
			// In BRE, an unescaped parenthesis is matched literally and an
			// escaped one induces grouping.
			if !esc {
				re.WriteRune('\\')
			}
			re.WriteRune(ch)
			esc = false
			continue

		case '^', '[', ']', '$', '.', '*':
			// These regexp operators have their usual meanings when unescaped and
			// are not handed to regexp.QuoteMeta.
			if !esc {
				re.WriteRune(ch)
				continue
			}
			// fall through to the default below

		case '\\':
			// Handle explicit escape signals.
			if esc {
				re.WriteString(`\\`)
				esc = false
			} else {
				esc = true
			}
			continue
		}

		// Anything that reaches here needs quoting of some kind.
		if esc {
			re.WriteRune('\\')
			re.WriteRune(ch)
			esc = false
		} else {
			re.WriteString(regexp.QuoteMeta(string(ch)))
		}
	}
	return re.String()
}
