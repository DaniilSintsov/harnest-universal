// Package learn stores reviewable rule candidates without activating them.
package learn

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/daniilsintsov/harnest-universal/internal/managedfile"
	"github.com/daniilsintsov/harnest-universal/internal/rules"
	goyaml "gopkg.in/yaml.v3"
)

func Propose(projectDir, id, statement string) (string, error) {
	rule := rules.Rule{ID: id, Severity: rules.Preference, Statement: statement, Source: rules.Source{Type: "learned-candidate"}}
	if err := rules.Validate(rule); err != nil {
		return "", err
	}
	if strings.ContainsAny(id, "/\\") {
		return "", fmt.Errorf("candidate id must not contain path separators")
	}
	path := filepath.Join(projectDir, ".harnest", "rules", "candidates", id+".yaml")
	if _, err := os.Stat(path); err == nil {
		return "", fmt.Errorf("candidate %q already exists", id)
	} else if !os.IsNotExist(err) {
		return "", err
	}
	data, err := goyaml.Marshal(rule)
	if err != nil {
		return "", err
	}
	if err := managedfile.WriteAtomic(path, data, 0600); err != nil {
		return "", err
	}
	return path, nil
}
