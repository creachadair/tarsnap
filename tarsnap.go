// Copyright (C) 2018 Michael J. Fromberger. All Rights Reserved.

// Package tarsnap is a library interface to the tarsnap command-line tool.
package tarsnap

import (
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

var std *Config

// Archives lists known archive names in the default config.
func Archives() ([]Archive, error) { return std.Archives() }

// Create creates a new archive in the default config.
func Create(name string, opts CreateOptions) error { return std.Create(name, opts) }

// Extract extracts an archive with the default config.
func Extract(name string, opts ExtractOptions) error { return std.Extract(name, opts) }

// Delete deletes an archive in the default config.
func Delete(archives ...string) error { return std.Delete(archives...) }

// Size reports archive size statistics in the default config.
func Size(archives ...string) (*SizeInfo, error) { return std.Size(archives...) }

// Config carries configuration settings to a tarsnap execution.  A nil *Config
// is ready for use and provides default settings.
type Config struct {
	Tool    string `json:"tool"`
	Keyfile string `json:"keyFile"`
	WorkDir string `json:"workDir"`

	// If not nil, this function is called with each tarsnap command-line giving
	// the full argument list.
	CmdLog func(cmd string, args []string) `json:"-" yaml:"-"`
}

// Archives returns a list of the known archive names.  The resulting slice is
// ordered nondecreasing by creation time and by name.
func (c *Config) Archives() ([]Archive, error) {
	raw, err := c.runOutput([]string{"--list-archives", "-v"})
	if err != nil {
		return nil, err
	}
	cooked := strings.TrimSpace(string(raw))
	var archs []Archive
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
			log.Printf("WARNING: Invalid timestamp %q (skipped): %v", parts[1], err)
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
	sort.Slice(archs, func(i, j int) bool {
		return archiveLess(archs[i], archs[j])
	})
	return archs, nil
}

// CreateOptions control the creation of archives.
type CreateOptions struct {
	// Include these file or directories in the archive.
	Include []string `json:"include"`

	// Change to this directory before adding entries.
	WorkDir string `json:"workDir,omitempty"`

	// Modify names by these patterns, /old/new/[gps].
	Modify []string `json:"modify,omitempty"`

	// Exclude files or directories matching these patterns.
	Exclude []string `json:"exclude,omitempty"`

	// Follow symlinks, storing the target rather than the link.
	FollowSymlinks bool `json:"followSymlinks" yaml:"follow-symlinks"`

	// Store access times.
	StoreAccessTime bool `json:"storeAccessTime" yaml:"store-access-time"`

	// Preserve original pathnames.
	PreservePaths bool `json:"preservePaths" yaml:"preserve-paths"`

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
	if opts.DryRun {
		args = append(args, "--dry-run")
	}
	for _, mod := range opts.Modify {
		args = append(args, "-s", mod)
	}
	for _, exc := range opts.Exclude {
		args = append(args, "--exclude", exc)
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

	// TODO: Consider -k, --chroot, -m, -P, -q
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
	for _, exc := range opts.Exclude {
		args = append(args, "--exclude", exc)
	}
	if len(opts.Include) != 0 {
		args = append(args, "--")
	}
	return c.run(append(args, opts.Include...))
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
	InputBytes            int64 // total bytes of original input
	CompressedBytes       int64 // size after input compression
	UniqueBytes           int64 // size after deduplication
	CompressedUniqueBytes int64 // size after deduplication and compression
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
	base := []string{"--quiet", "--no-print-stats"}
	cmd := "tarsnap"
	if c != nil {
		if c.Tool != "" {
			cmd = c.Tool
		}
		if c.Keyfile != "" {
			base = append(base, "--keyfile", c.Keyfile)
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
	if err != nil {
		if e, ok := err.(*exec.ExitError); ok {
			err = errors.New(strings.SplitN(string(e.Stderr), "\n", 2)[0])
		}
		return nil, fmt.Errorf("failed: %v", err)
	}
	return out, err
}

func (c *Config) cmdLog(cmd string, args []string) {
	if c != nil && c.CmdLog != nil {
		c.CmdLog(cmd, args)
	}
}

// An Archive represents the name and metadata known about an archive.
type Archive struct {
	Name    string    `json:"archive"`           // base.tag
	Base    string    `json:"base,omitempty"`    // base alone
	Tag     string    `json:"tag,omitempty"`     // .tag alone
	Created time.Time `json:"created,omitempty"` // in UTC
}

func archiveLess(a, b Archive) bool {
	if a.Created.Equal(b.Created) {
		return a.Name < b.Name
	}
	return a.Created.Before(b.Created)
}
