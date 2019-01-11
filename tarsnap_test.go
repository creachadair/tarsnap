package tarsnap

import (
	"flag"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kylelemons/godebug/diff"
)

var doManual = flag.Bool("manual", false, "Set true to enable manual tests")

// Test that an archive can be round-tripped by the library.
// To run this test you need a real tarsnap account and a working config.
// Running this test costs you money -- very little, but it is not free.
func TestRoundTrip(t *testing.T) {
	if !*doManual {
		t.Skip("Skipping manual test because -manual=false")
	}

	cfg := &Config{
		CmdLog: func(cmd string, args []string) {
			log.Printf("+ [%s] %s", cmd, strings.Join(args, " "))
		},
	}

	if err := cfg.Create("test-archive", CreateOptions{
		Include: []string{"tarsnap"},
		Exclude: []string{".git", "*~"},
		WorkDir: "..",
	}); err != nil {
		t.Fatalf("Create test-archive failed: %v", err)
	}

	tmp, err := ioutil.TempDir("", "tarsnap-test")
	if err != nil {
		t.Fatalf("Creating temporary directory: %v", err)
	}
	defer os.RemoveAll(tmp) // best effort cleanup

	if err := cfg.Extract("test-archive", ExtractOptions{
		Include:  []string{"tarsnap/tarsnap.go"},
		WorkDir:  tmp,
		FastRead: true,
	}); err != nil {
		t.Fatalf("Extracting test-archive failed: %v", err)
	}

	want, err := ioutil.ReadFile("tarsnap.go")
	if err != nil {
		t.Fatalf("Reading old source: %v", err)
	}
	got, err := ioutil.ReadFile(filepath.Join(tmp, "tarsnap/tarsnap.go"))
	if err != nil {
		t.Fatalf("Reading extracted source: %v", err)
	}

	if d := diff.Diff(string(want), string(got)); d != "" {
		t.Errorf("Extracted file does not match, diff is: %s", d)
	}

	if err := cfg.Delete("test-archive"); err != nil {
		t.Errorf("Deleting test-archive failed: %v", err)
	}
}

func TestBasicRE(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		// Base cases
		{"", ""},
		{"abc", "abc"},

		// Unquoted operators retain their meaning.
		{`a.*`, `a.*`},
		{`^cherry$`, `^cherry$`},
		{`abc[0-9]*def`, `abc[0-9]*def`},

		// Unsupported regexp operators are quoted.
		{`smoke|eh?`, `smoke\|eh\?`},
		{`this+that`, `this\+that`},

		// Escaped operators have their escapes propagated.
		{`\^a\.b\*c \\ \$`, `\^a\.b\*c \\ \$`},

		// Unescaped parentheses remain intact.
		{`(abc)`, `\(abc\)`},
		{`(you|shall|not|pass)+?`, `\(you\|shall\|not\|pass\)\+\?`},

		// Escaped parentheses become groups.
		{`(a\(bc\)d)`, `\(a(bc)d\)`},
	}

	for _, test := range tests {
		got := basicToRE(test.in)
		if got != test.want {
			t.Errorf("BRE %#q: got %#q, want %#q", test.in, got, test.want)
		}
	}
}

func TestRule(t *testing.T) {
	tests := []struct {
		pattern string
		in, out string
		ok      bool
	}{
		{`/^\.//`, "nothing", "nothing", false},
		{`/^\.//`, ".dot", "dot", true},
		{`/a\(b*c\).txt/\1.md/`, "abbbc.txt", "bbbc.md", true},
	}
	for _, test := range tests {
		r, err := ParseRule(test.pattern)
		if err != nil {
			t.Errorf("ParseRule(%q): unexpected error: %v", test.pattern, err)
			continue
		}
		t.Logf("Rule: %#q → %#q :: %#q", r.lhs, r.rhs, test.in)
		got, ok := r.Apply(test.in)
		if ok != test.ok || got != test.out {
			t.Errorf("(%q).Apply(%q): got (%q, %v), want (%q, %v)",
				test.pattern, test.in, got, ok, test.out, test.ok)
		}
	}
}
