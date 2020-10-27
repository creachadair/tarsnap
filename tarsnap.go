// Copyright (C) 2018 Michael J. Fromberger. All Rights Reserved.

// Package tarsnap is a library interface to the tarsnap command-line tool.
package tarsnap // import "github.com/creachadair/tarsnap"

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Config carries configuration settings to a tarsnap execution.  A nil *Config
// is ready for use and provides default settings.
type Config struct {
	Tool     string `json:"tool"`
	Keyfile  string `json:"keyFile"`
	WorkDir  string `json:"workDir"`
	CacheDir string `json:"cacheDir"`

	// Optional settings flags to pass to the tarsnap command-line tool.
	Flags []Flag `json:"flags"`

	// If not nil, this function is called with each tarsnap command-line giving
	// the full argument list.
	CmdLog func(cmd string, args []string) `json:"-" yaml:"-"`
}

// List returns a list of the known archives.  The resulting slice is ordered
// nondecreasing by creation time and by name.
func (c *Config) List() (Archives, error) {
	raw, err := c.runOutput([]string{"--list-archives", "-v"})
	if err != nil {
		return nil, err
	}
	cooked := strings.TrimSpace(string(raw))
	if cooked == "" {
		return nil, nil // no archives exist
	}

	var archs Archives
	for _, line := range strings.Split(cooked, "\n") {
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			log.Printf("WARNING: Invalid archive spec %q (skipped)", line)
			continue
		}

		// N.B. Tarsnap prints times in the local timeszone by default, so we
		// need to parse them in the same way.
		when, err := time.ParseInLocation("2006-01-02 15:04:05", parts[1], time.Local)
		if err != nil {
			log.Printf("WARNING: Invalid timestamp %q (ignored): %v", parts[1], err)
		}
		i := strings.Index(parts[0], ".")
		if i < 0 {
			i = len(parts[0])
		}
		archs = append(archs, Archive{
			Name:    parts[0],
			Base:    parts[0][:i],
			Tag:     parts[0][i:],
			Created: when.In(time.UTC),
		})
	}
	sort.Sort(archs)
	return archs, nil
}

// CreateOptions control the creation of archives.
type CreateOptions struct {
	// Include these files or directories in the archive.
	// N.B. The tarsnap tool does not expand globs in include paths.
	Include []string `json:"include"`

	// Change to this directory before adding entries.
	WorkDir string `json:"workDir,omitempty"`

	// Modify names by these patterns, /old/new/[gps].
	Modify []string `json:"modify,omitempty"`

	// Exclude files or directories matching these glob patterns.
	Exclude []string `json:"exclude,omitempty"`

	// Follow symlinks (as tarsnap -H), storing the target rather than the link.
	FollowSymlinks bool `json:"followSymlinks" yaml:"follow-symlinks"`

	// Store access times (as tarsnap --store-atime). Not advised.
	StoreAccessTime bool `json:"storeAccessTime" yaml:"store-access-time"`

	// Preserve original pathnames (as tarsnap -P).
	PreservePaths bool `json:"preservePaths" yaml:"preserve-paths"`

	// If non-zero, set the creation time of the archive to this time.
	CreationTime time.Time `json:"creationTime,omitempty" yaml:"creation-time"`

	// Simulate creating archives rather than creating them.
	DryRun bool `json:"dryRun,omitempty" yaml:"dry-run"`
}

// Create creates an archive with the specified name and options.
// It is equivalent in effect to "tarsnap -c -f name opts...".
func (c *Config) Create(name string, opts CreateOptions) error {
	if name == "" {
		return errors.New("empty archive name")
	} else if len(opts.Include) == 0 {
		return errors.New("empty include list")
	}
	args := []string{"-c", "-f", name}
	if opts.WorkDir != "" {
		args = append(args, "-C", opts.WorkDir)
	} else if c != nil && c.WorkDir != "" {
		args = append(args, "-C", c.WorkDir)
	}
	if opts.FollowSymlinks {
		args = append(args, "-H")
	}
	if opts.StoreAccessTime {
		args = append(args, "--store-atime")
	}
	if opts.PreservePaths {
		args = append(args, "-P")
	}
	if !opts.CreationTime.IsZero() {
		args = append(args, "--creationtime", fmt.Sprint(opts.CreationTime.Unix()))
	}
	if opts.DryRun {
		args = append(args, "--dry-run")
	}
	for _, mod := range opts.Modify {
		args = append(args, "-s", mod)
	}
	for _, exc := range opts.Exclude {
		args = append(args, "--exclude", exc)
	}
	if len(opts.Include) != 0 {
		args = append(args, "--")
	}
	return c.run(append(args, opts.Include...))
}

// ExtractOptions control the extraction of archives.
type ExtractOptions struct {
	// Include files matching these globs in the output.  If this is empty, the
	// whole archive is extracted except for any exclusions. If not, only the
	// files or directories specified are extracted, modulo exclusions.
	Include []string `json:"include"`

	// Exclude files or directories matching these patterns.
	Exclude []string `json:"exclude,omitempty"`

	// Change to this directory before extracting entries.
	WorkDir string `json:"workDir,omitempty"`

	// Restore permissions, owner, flags, and ACL.
	RestorePermissions bool `json:"restorePerms" yaml:"restore-perms"`

	// Ignore owner and group settings from the archive.
	IgnoreOwners bool `json:"ignoreOwners" yaml:"ignore-owners"`

	// Stop reading after the first match for each included path.
	FastRead bool `json:"fastRead" yaml:"fast-read"`

	// TODO: Consider -k, --chroot, -m, -P
}

// Extract extracts from an archive with the specified name and options.
// It is equivalent in effect to "tarsnap -x -f name opts...".
func (c *Config) Extract(name string, opts ExtractOptions) error {
	if name == "" {
		return errors.New("empty archive name")
	}

	args := []string{"-x", "-f", name}
	var dir string
	if opts.WorkDir != "" {
		args = append(args, "-C", opts.WorkDir)
		dir = opts.WorkDir
	} else if c != nil && c.WorkDir != "" {
		args = append(args, "-C", c.WorkDir)
		dir = c.WorkDir
	}

	// Make sure the output directory exists, since tarsnap will not.
	if dir != "" {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return fmt.Errorf("creating workdir: %v", err)
		}
	}

	if opts.RestorePermissions {
		args = append(args, "-p")
		if opts.IgnoreOwners {
			args = append(args, "-o")
		}
	}
	if opts.FastRead {
		args = append(args, "--fast-read")
	}
	for _, exc := range opts.Exclude {
		args = append(args, "--exclude", exc)
	}
	if len(opts.Include) != 0 {
		args = append(args, "--")
	}
	return c.run(append(args, opts.Include...))
}

// Entries calls f with each entry stored in the specified archive.
// If f reports an error, scanning stops and that error is returned to the
// caller of contents.
func (c *Config) Entries(name string, f func(*Entry) error) (err error) {
	if name == "" {
		return errors.New("empty archive name")
	}

	// The -v flag is needed to ensure the output contains stat.
	// The --numeric-owner flag ensures owner/group are not converted to names.
	// The --iso-dates flag ensures we get seconds precision on timestamps.
	cmd, args := c.base("-v", "--iso-dates", "--numeric-owner", "-t", "-f", name)
	c.cmdLog(cmd, args)

	// Ensure the subprocess is terminated on return, since the caller may not
	// fully consume the output.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	proc := exec.CommandContext(ctx, cmd, args...)
	ebuf := bytes.NewBuffer(nil)
	proc.Stderr = ebuf
	out, err := proc.StdoutPipe()
	if err != nil {
		return err
	}
	defer out.Close()

	if err := proc.Start(); err != nil {
		return err
	}
	defer func() {
		cancel() // the deferred cancel above happens after this
		werr := proc.Wait()
		if werr != nil && err == nil {
			err = errors.New(strings.SplitN(ebuf.String(), "\n", 2)[0])
		}
	}()

	s := bufio.NewScanner(out)
	for s.Scan() {
		e, err := parseEntry(s.Text())
		if err != nil {
			return err
		} else if err := f(e); err != nil {
			return err
		}
	}
	if err := s.Err(); err != io.EOF {
		return err
	}
	return nil
}

// An Entry describes a single file or directory entry stored in an archive.
type Entry struct {
	Mode         os.FileMode
	Owner, Group int
	Size         int64     // in bytes
	ModTime      time.Time // in UTC
	Name         string
}

func (e *Entry) String() string {
	return fmt.Sprintf("%v uid=%d gid=%d size=%d %v %q",
		e.Mode, e.Owner, e.Group, e.Size, e.ModTime, e.Name)
}

var spaces = regexp.MustCompile(" +")

func parseEntry(s string) (*Entry, error) {
	// 0           1 2      3       4     5          6        7 ...
	// -rw-r--r--  0 501    20      26628 2019-08-26 18:30:46 Documents/.DS_Store
	parts := spaces.Split(s, 8)
	if len(parts) != 8 {
		return nil, errors.New("invalid entry format")
	}
	mode, err := parseMode(parts[0])
	if err != nil {
		return nil, fmt.Errorf("entry %q: invalid mode: %v", s, err)
	}
	ts := parts[5] + "T" + parts[6]
	mtime, err := time.ParseInLocation("2006-01-02T15:04:05", ts, time.Local)
	if err != nil {
		return nil, fmt.Errorf("entry %q: invalid mtime: %v", s, err)
	}

	// Directory names are stored with a trailing "/"; remove this for the entry.
	e := &Entry{Mode: mode, Name: strings.TrimSuffix(parts[7], "/")}
	e.Owner, _ = strconv.Atoi(parts[2])
	e.Group, _ = strconv.Atoi(parts[3])
	e.Size, _ = strconv.ParseInt(parts[4], 10, 64)
	e.ModTime = mtime.In(time.UTC)
	return e, nil
}

// parseMode parses the file mode from a 10-character string of the form
// trwxrwxrwx.
func parseMode(s string) (os.FileMode, error) {
	if len(s) != 10 {
		return 0, errors.New("invalid mode string")
	}
	var mode os.FileMode
	switch s[0] {
	case '-':
		// do nothing; this is the default mode
	case 'd':
		mode |= os.ModeDir
	case 'L':
		mode |= os.ModeSymlink
	default:
		return 0, fmt.Errorf("unknown mode type %q", s)
	}
	mode |= parseRWX(s[1:], 6, os.ModeSetuid) |
		parseRWX(s[4:], 3, os.ModeSetgid) |
		parseRWX(s[7:], 0, 0)
	return mode, nil
}

func parseRWX(s string, shift, setBit os.FileMode) (rwx os.FileMode) {
	const modeRead = 4
	const modeWrite = 2
	const modeExec = 1

	if s[0] == 'r' {
		rwx |= modeRead << shift
	}
	if s[1] == 'w' {
		rwx |= modeWrite << shift
	}
	if s[2] == 'x' || s[2] == 's' {
		rwx |= modeExec << shift
	}
	if s[2] == 's' || s[2] == 'S' {
		rwx |= setBit
	}
	return
}

// Delete deletes the specified archives.
func (c *Config) Delete(archives ...string) error {
	args := []string{"-d"}
	for _, a := range archives {
		args = append(args, "-f", a)
	}
	return c.run(args)
}

// Size reports storage sizes for the specified archives.  If no archives are
// specified only global stats are reported.
func (c *Config) Size(archives ...string) (*SizeInfo, error) {
	args := []string{"--print-stats", "--no-humanize-numbers"}
	for _, arch := range archives {
		args = append(args, "-f", arch)
	}
	return maybeParseSizeInfo(c.runOutput(args))
}

// Sizes represents storage size values.
type Sizes struct {
	// Total bytes of original input
	InputBytes int64 `json:"inputBytes"`
	// Size after input compression
	CompressedBytes int64 `json:"compressedBytes"`
	// Size after deduplication
	UniqueBytes int64 `json:"uniqueBytes"`
	// Size after deduplication and compression
	CompressedUniqueBytes int64 `json:"compressedUniqueBytes"`
}

func (s *Sizes) String() string {
	return fmt.Sprintf("#<size: input=%d, compressed=%d, unique=%d, cunique=%d>",
		s.InputBytes, s.CompressedBytes, s.UniqueBytes, s.CompressedUniqueBytes)
}

// SizeInfo records storage size information for archives.
type SizeInfo struct {
	All     *Sizes            // sizes for all archives known
	Archive map[string]*Sizes // sizes for individual archives
}

var sizes = regexp.MustCompile(`^\s*(.*?)\s+(\d+)\s+(\d+)$`)

func maybeParseSizeInfo(data []byte, err error) (*SizeInfo, error) {
	if err != nil {
		return nil, err
	}
	info := &SizeInfo{Archive: make(map[string]*Sizes)}
	var cur *Sizes
	for i, line := range strings.Split(string(data), "\n") {
		m := sizes.FindStringSubmatch(line)
		if m == nil {
			continue // skip header row
		}

		// Syntax:  <name>    <total-size> <compressed-size>
		//
		// The name may contain spaces, so the regexp specifically globs
		// everything up to the final two numeric fields.
		total, err := strconv.ParseInt(m[2], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("line %d: invalid total size: %v", i+1, err)
		}
		comp, err := strconv.ParseInt(m[3], 10, 64)
		if err != nil {
			return nil, fmt.Errorf("line %d: invalid compressed size: %v", i+1, err)
		}

		// If the name is "All archives", this is a summary stats block.
		// If the name is "(unique data)", this is a continuation block.
		// Otherwise, this is an archive-specific block.
		switch tag := m[1]; tag {
		case "All archives":
			cur = &Sizes{
				InputBytes:      total,
				CompressedBytes: comp,
			}
			info.All = cur

		case "(unique data)":
			if cur == nil {
				return nil, fmt.Errorf("line %d: unexpected continuation line", i+1)
			}
			cur.UniqueBytes = total
			cur.CompressedUniqueBytes = comp
			cur = nil

		default:
			cur = &Sizes{InputBytes: total, CompressedBytes: comp}
			info.Archive[tag] = cur
		}
	}
	return info, nil
}

func (c *Config) base(rest ...string) (string, []string) {
	base := c.addFlags([]string{"--quiet", "--no-print-stats"}, rest)

	cmd := "tarsnap"
	if c != nil {
		if c.Tool != "" {
			cmd = c.Tool
		}
		if c.Keyfile != "" {
			base = append(base, "--keyfile", c.Keyfile)
		}
		if c.CacheDir != "" {
			base = append(base, "--cachedir", c.CacheDir)
		}
	}
	return cmd, append(base, rest...)
}

func (c *Config) run(args []string) error {
	_, err := c.runOutput(args)
	return err
}

func (c *Config) runOutput(extra []string) ([]byte, error) {
	cmd, args := c.base(extra...)
	c.cmdLog(cmd, args)
	out, err := exec.Command(cmd, args...).Output()
	if err == nil {
		return out, nil
	} else if e, ok := err.(*exec.ExitError); ok {
		return nil, errors.New(strings.SplitN(string(e.Stderr), "\n", 2)[0])
	}
	return nil, fmt.Errorf("failed: %v", err)
}

func (c *Config) cmdLog(cmd string, args []string) {
	if c != nil && c.CmdLog != nil {
		c.CmdLog(cmd, args)
	}
}

// An Archive represents the name and metadata known about an archive.
type Archive struct {
	Name    string    `json:"name"`              // base.tag
	Base    string    `json:"base,omitempty"`    // base alone
	Tag     string    `json:"tag,omitempty"`     // .tag alone
	Created time.Time `json:"created,omitempty"` // in UTC
}

// Archives is a sortable slice of Archive values, ordered non-decreasing by
// creation time with ties broken by name.
type Archives []Archive

func (a Archives) Len() int      { return len(a) }
func (a Archives) Swap(i, j int) { a[i], a[j] = a[j], a[i] }

func (a Archives) Less(i, j int) bool {
	if a[i].Created.Equal(a[j].Created) {
		return a[i].Name < a[j].Name
	}
	return a[i].Created.Before(a[j].Created)
}

// Latest returns the most recently-created archive with the given base.  It is
// shorthand for LatestAsOf(base, time.Now()).
func (a Archives) Latest(base string) (Archive, bool) { return a.LatestAsOf(base, time.Now()) }

// LatestAsOf returns the most recently-created archive with the given base at
// or before the specified time.
func (a Archives) LatestAsOf(base string, when time.Time) (Archive, bool) {
	for i := len(a) - 1; i >= 0; i-- {
		if a[i].Base == base && !a[i].Created.After(when) {
			return a[i], true
		}
	}
	return Archive{}, false
}

// A Flag describes a command-line flag for a tarsnap execution.
type Flag struct {
	Match string // if non-empty, only add if this argument is present
	Flag  string // the name of the flag, passed as --name

	// If not nil, the value of a flag must be a string, float64, or bool.
	// A string value will have environment variables ($VAR) expanded.
	//
	// If value is nil or true, the flag has no argument: --name
	// If value is false, the flag is sent as: --no-name
	// Otherwise the flag is sent as: --name value
	Value interface{}
}

func flagAppliesTo(c *Config, f Flag, args []string) bool {
	if f.Match == "" {
		return true
	}
	if (f.Flag == "keyfile" && c.Keyfile != "") || (f.Flag == "cachedir" && c.CacheDir != "") {
		return false
	}
	for _, arg := range args {
		if arg == f.Match {
			return true
		}
	}
	return false
}

func (c *Config) addFlags(base, extra []string) []string {
	for _, f := range c.Flags {
		if !flagAppliesTo(c, f, extra) {
			continue
		}
		key := "--" + f.Flag
		switch v := f.Value.(type) {
		case nil:
			base = append(base, key)
		case bool:
			if v {
				base = append(base, key)
			} else {
				base = append(base, "--no-"+f.Flag)
			}
		case string:
			base = append(base, key, os.ExpandEnv(v))
		case float64:
			base = append(base, key, strconv.FormatFloat(v, 'g', -1, 64))
		default: // e.g., arrays, objects, null
			log.Printf("WARNING: Ignored invalid value for flag %q: %v", f.Flag, f.Value)
		}
	}
	return base
}
