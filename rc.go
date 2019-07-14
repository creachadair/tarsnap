package tarsnap

import (
	"bufio"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// An RC represents a collection of tarsnap configuration settings.
type RC map[string]string

// Merge updates rc with the keys and values from other.
func (rc RC) Merge(other RC) {
	for key, val := range other {
		rc[key] = val
	}
}

// Path expands the value of the specified config key as a path, and reports
// whether it was set. Note that this expansion occurs even if the value for
// that key is not intended to be a path.
func (rc RC) Path(key string) (string, bool) {
	v, ok := rc[key]
	if !ok {
		return "", false
	} else if t := strings.TrimPrefix(v, "~"); t != v && (t == "" || t[0] == '/') {
		return os.Getenv("HOME") + t, true
	}
	return v, true
}

// ParseRC parses tarsnap configuration settings from r.
func ParseRC(r io.Reader) (RC, error) {
	rc := make(RC)
	s := bufio.NewScanner(r)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue // skip comments, blanks
		}
		i := strings.IndexAny(line, " \t")
		if i < 0 {
			rc[line] = ""
		} else {
			rc[strings.TrimSpace(line[:i])] = strings.TrimSpace(line[i+1:])
		}
	}
	if err := s.Err(); err != nil {
		return nil, err
	}
	return rc, nil
}

// LoadRC reads the contents of the specified RC files, parses and merges them
// in the order specified. If one of the paths is not found, it is skipped
// without error. If no paths are specified, an empty RC is returned without
// error.
func LoadRC(paths ...string) (RC, error) {
	rc := make(RC)
	for _, path := range paths {
		f, err := os.Open(path)
		if os.IsNotExist(err) {
			continue
		} else if err != nil {
			return nil, err
		}
		next, err := ParseRC(f)
		f.Close()
		if err != nil {
			return nil, err
		}
		rc.Merge(next)
	}
	return rc, nil
}

// RC loads and returns the resource configuration for c. If no configurations
// are found, an empty RC is returned without error.
func (c *Config) RC() (RC, error) {
	rc, err := LoadRC("/usr/local/etc/tarsnap.conf", os.ExpandEnv("$HOME/.tarsnaprc"))
	if err != nil {
		return nil, err
	} else if c != nil && c.Keyfile != "" {
		rc["keyfile"] = c.Keyfile
	}
	return rc, nil
}

// CacheTag loads and returns the current cache sequence tag.
// If no cache directory is found, it returns "", nil.
func (c *Config) CacheTag() (string, error) {
	rc, err := c.RC()
	if err != nil {
		return "", err
	}
	cdir, ok := rc.Path("cachedir")
	if !ok {
		return "", nil
	}
	return os.Readlink(filepath.Join(cdir, "cseq"))
}
