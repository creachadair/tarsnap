// Copyright (C) 2018 Michael J. Fromberger. All Rights Reserved.

package tarsnap

import (
	"flag"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
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
		Settings: map[string]interface{}{
			"aggressive-networking": false,
		},
		CmdLog: func(cmd string, args []string) {
			log.Printf("+ [%s] %s", cmd, strings.Join(args, " "))
		},
	}
	const testArchive = "test-archive"

	// Create a small archive containing some of the files in this repo.
	// Skip the .git directory to test exclusions.
	ts := time.Date(1996, 6, 9, 11, 37, 0, 0, time.Local)
	if err := cfg.Create(testArchive, CreateOptions{
		Include:      []string{"tarsnap"},
		Exclude:      []string{".git", "*~"},
		WorkDir:      "..",
		CreationTime: ts,
	}); err != nil {
		t.Fatalf("Create %s failed: %v", testArchive, err)
	}

	// Verify that the archive got created and has the correct timestamp.
	lst, err := cfg.List()
	if err != nil {
		t.Fatalf("Listing archives failed: %v", err)
	} else if arch, ok := lst.Latest(testArchive); !ok {
		t.Errorf("Did not find %q in the archive list", testArchive)
	} else if !arch.Created.Equal(ts) {
		t.Errorf("Creation time: got %v, want %v", arch.Created, ts)
	}

	// Verify that listing a non-existing archive provokes an error.
	if err := cfg.Entries("no-such-archive", func(e *Entry) error {
		t.Errorf("Unexpected entry: %v", e)
		return nil
	}); err != nil {
		t.Logf("Entries correctly failed for a non-existing archive: %v", err)
	} else {
		t.Error("Entries succeeded for non-existing archive")
	}

	// Log the contents of the test archive.
	if err := cfg.Entries(testArchive, func(e *Entry) error {
		t.Log(e)
		return nil
	}); err != nil {
		t.Fatalf("Entries failed: %v", err)
	}

	// Extract a file from the archive to make sure we can, and compare the
	// contents to the original.
	tmp, err := ioutil.TempDir("", "tarsnap-test")
	if err != nil {
		t.Fatalf("Creating temporary directory: %v", err)
	}
	defer os.RemoveAll(tmp) // best effort cleanup

	if err := cfg.Extract(testArchive, ExtractOptions{
		Include:  []string{"tarsnap/tarsnap.go"},
		WorkDir:  tmp,
		FastRead: true,
	}); err != nil {
		t.Fatalf("Extracting %s failed: %v", testArchive, err)
	}

	want, err := ioutil.ReadFile("tarsnap.go")
	if err != nil {
		t.Fatalf("Reading old source: %v", err)
	}
	got, err := ioutil.ReadFile(filepath.Join(tmp, "tarsnap/tarsnap.go"))
	if err != nil {
		t.Fatalf("Reading extracted source: %v", err)
	}

	if d := cmp.Diff(string(want), string(got)); d != "" {
		t.Errorf("Extracted file does not match, diff is: %s", d)
	}

	// Delete the test archive to verify that we can, and to clean up.
	if err := cfg.Delete(testArchive); err != nil {
		t.Errorf("Deleting %s failed: %v", testArchive, err)
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

func TestRC(t *testing.T) {
	const kf = "oh hi there"
	c := &Config{Keyfile: kf}

	rc, err := c.RC()
	if err != nil {
		t.Fatalf("Loading default RC: %v", err)
	}
	for key, val := range rc {
		exp, _ := rc.Path(key)
		t.Logf("Key %q | raw %q | expanded %q", key, val, exp)
	}
	if v, ok := rc["keyfile"]; !ok || v != kf {
		t.Errorf("RC(keyfile): got (%q, %v), want (%q, true)", v, ok, kf)
	}

	seq, err := c.CacheTag()
	if err != nil {
		t.Errorf("CacheTag failed: %v", err)
	} else {
		t.Logf("Cache tag is %q", seq)
	}
}
