// Copyright (C) 2018 Michael J. Fromberger. All Rights Reserved.

// Package tarsnap is a library interface to the tarsnap command-line tool.
package tarsnap

import (
	"errors"
	"fmt"
	"log"
	"os/exec"
	"sort"
	"strings"
	"time"
)

var std *Config

// Archives lists known archive names in the default config.
func Archives() ([]Archive, error) { return std.Archives() }

// Create creates a new archive in the default config.
func Create(name string, entries ...string) error { return std.Create(name, entries...) }

// Delete deletes an archive in the default config.
func Delete(archives ...string) error { return std.Delete(archives...) }

// Config carries configuration settings to a tarsnap execution.  A nil *Config
// is ready for use and provides default settings.
type Config struct {
	Tool    string
	Keyfile string
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
		when, err := time.Parse("2006-01-02 15:04:05", parts[1])
		if err != nil {
			log.Printf("WARNING: Invalid timestamp %q (skipped): %v", parts[1], err)
		}
		archs = append(archs, Archive{
			Name:    parts[0],
			Created: when,
		})
	}
	sort.Slice(archs, func(i, j int) bool {
		return archiveLess(archs[i], archs[j])
	})
	return archs, nil
}

// Create creates an archive with the specified name and entries.
// It is equivalent in effect to "tarsnap -c -f name entries ...".
func (c *Config) Create(name string, entries ...string) error {
	return c.run(append([]string{"-c", "-f", name}, entries...))
}

// Delete deletes the specified archives.
func (c *Config) Delete(archives ...string) error {
	args := []string{"-d"}
	for _, a := range archives {
		args = append(args, "-f", a)
	}
	return c.run(args)
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
	out, err := exec.Command(cmd, args...).Output()
	if err != nil {
		if e, ok := err.(*exec.ExitError); ok {
			err = errors.New(strings.SplitN(string(e.Stderr), "\n", 2)[0])
		}
		return nil, fmt.Errorf("failed: %v", err)
	}
	return out, err
}

// An Archive represents the name and metadata known about an archive.
type Archive struct {
	Name    string    `json:"archive"`
	Created time.Time `json:"created,omitempty"`
}

func archiveLess(a, b Archive) bool {
	if a.Created.Equal(b.Created) {
		return a.Name < b.Name
	}
	return a.Created.Before(b.Created)
}
