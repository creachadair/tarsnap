// Package tarsnap is a library interface to the tarsnap command-line tool.
package tarsnap

import (
	"errors"
	"fmt"
	"os/exec"
	"sort"
	"strings"
)

var std *Config

// Archives lists known archive names in the default config.
func Archives() ([]string, error) { return std.Archives() }

// Create creates a new archive in the default config.
func Create(name string, entries ...string) error { return std.Create(name, entries...) }

// Delete deletes an archive in the default config.
func Delete(archives ...string) error { return std.Delete(archives...) }

// Config carries configuration settings to a tarsnap execution.  A nil *Config
// is ready for use and provides default settings.
type Config struct {
	Tool    string
	KeyFile string
}

func (c *Config) args(rest ...string) (cmd string, args []string) {
	args = append(args, "--no-print-stats")
	cmd = "tarsnap"
	if c != nil {
		if c.Tool != "" {
			cmd = c.Tool
		}
		if c.KeyFile != "" {
			args = append(args, "--keyfile", c.KeyFile)
		}
	}
	return cmd, append(args, rest...)
}

// Archives returns a list of the known archive names.  The resulting list is
// ordered lexicographically.
func (c *Config) Archives() ([]string, error) {
	out, err := c.runOutput([]string{"--list-archives"})
	if err != nil {
		return nil, err
	}
	archives := strings.Split(strings.TrimSpace(string(out)), "\n")
	sort.Strings(archives)
	return archives, nil
}

func (c *Config) runOutput(args []string) ([]byte, error) {
	cmd, base := c.args(args...)
	out, err := exec.Command(cmd, base...).Output()
	if err != nil {
		if e, ok := err.(*exec.ExitError); ok {
			err = errors.New(strings.SplitN(string(e.Stderr), "\n", 2)[0])
		}
		return nil, fmt.Errorf("failed: %v", err)
	}
	return out, err
}

// Create creates an archive with the specified name and entries.
// It is equivalent in effect to "tarsnap -c -f name entries ...".
func (c *Config) Create(name string, entries ...string) error {
	return c.run(append([]string{"-c", "-f", name}, entries...))
}

// Delete deletes the specified entries.
func (c *Config) Delete(archives ...string) error {
	args := []string{"-d"}
	for _, a := range archives {
		args = append(args, "-f", a)
	}
	return c.run(args)
}

func (c *Config) run(args []string) error {
	_, err := c.runOutput(args)
	return err
}