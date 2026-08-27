// Package checks loads explicitly approved verification commands.
package checks

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	goyaml "gopkg.in/yaml.v3"
)

type Check struct {
	ID       string   `yaml:"id"`
	Command  string   `yaml:"command"`
	Args     []string `yaml:"args,omitempty"`
	Approved bool     `yaml:"approved"`
}

func Load(projectDir, root, id string) (Check, error) {
	if id == "" || strings.ContainsAny(id, "/\\") {
		return Check{}, fmt.Errorf("invalid check id %q", id)
	}
	dir := root
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(projectDir, root)
	}
	path := filepath.Join(dir, id+".yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return Check{}, fmt.Errorf("reading check %q: %w", id, err)
	}
	var check Check
	if err := goyaml.Unmarshal(data, &check); err != nil {
		return Check{}, fmt.Errorf("parsing check %q: %w", id, err)
	}
	if check.ID != id || check.Command == "" {
		return Check{}, fmt.Errorf("invalid check %q", id)
	}
	return check, nil
}

func Run(projectDir string, check Check, changed []string) error {
	if !check.Approved {
		return fmt.Errorf("check %q is not approved", check.ID)
	}
	cmd := exec.Command(check.Command, check.Args...)
	cmd.Dir = projectDir
	cmd.Env = append(os.Environ(), "HARNEST_CHANGED_FILES="+strings.Join(changed, "\n"))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("check %q failed: %w", check.ID, err)
	}
	return nil
}
