package checks

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

type approvalSource struct {
	Path   string
	Digest string
}

type approvalCheck struct {
	Definition Check
	Executable approvalSource
	Sources    []approvalSource
}

// Digest binds native-hook approval to the check definitions and executable sources.
// The caller must persist the result in the host-trusted hook command.
func Digest(projectDir string, definitions []Check) (string, error) {
	if len(definitions) == 0 {
		return "", nil
	}
	root, err := filepath.Abs(projectDir)
	if err != nil {
		return "", err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	definitions = append([]Check(nil), definitions...)
	sort.Slice(definitions, func(i, j int) bool { return definitions[i].ID < definitions[j].ID })
	approved := make([]approvalCheck, 0, len(definitions))
	for _, check := range definitions {
		command := check.Command
		if !filepath.IsAbs(command) && strings.ContainsAny(command, `/\`) {
			command = filepath.Join(root, command)
		}
		command, err = exec.LookPath(command)
		if err != nil {
			return "", fmt.Errorf("check %q executable: %w", check.ID, err)
		}
		executable, err := digestSource(command)
		if err != nil {
			return "", fmt.Errorf("check %q executable: %w", check.ID, err)
		}
		entry := approvalCheck{Definition: check, Executable: executable}
		// ponytail: regular argv files are inputs; declare indirect dependencies in
		// sources until a narrower source contract is needed. No shell parsing.
		for _, arg := range check.Args {
			path := arg
			if !filepath.IsAbs(path) {
				path = filepath.Join(root, path)
			}
			info, err := os.Stat(path)
			if os.IsNotExist(err) || (err == nil && !info.Mode().IsRegular()) {
				continue
			}
			if err != nil {
				return "", fmt.Errorf("check %q argument source: %w", check.ID, err)
			}
			source, err := digestSource(path)
			if err != nil {
				return "", fmt.Errorf("check %q argument source: %w", check.ID, err)
			}
			entry.Sources = append(entry.Sources, source)
		}
		for _, name := range check.Sources {
			path := name
			if !filepath.IsAbs(path) {
				path = filepath.Join(root, path)
			}
			source, err := digestSource(path)
			if err != nil {
				return "", fmt.Errorf("check %q declared source: %w", check.ID, err)
			}
			entry.Sources = append(entry.Sources, source)
		}
		approved = append(approved, entry)
	}
	data, err := json.Marshal(struct {
		Root   string
		Checks []approvalCheck
	}{Root: root, Checks: approved})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(data)), nil
}

func digestSource(path string) (approvalSource, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return approvalSource{}, err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return approvalSource{}, err
	}
	if !info.Mode().IsRegular() {
		return approvalSource{}, fmt.Errorf("source %q is not a regular file", path)
	}
	file, err := os.Open(resolved)
	if err != nil {
		return approvalSource{}, err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return approvalSource{}, err
	}
	return approvalSource{Path: resolved, Digest: fmt.Sprintf("%x", hash.Sum(nil))}, nil
}
