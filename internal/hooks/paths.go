package hooks

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/daniilsintsov/harnest-universal/internal/rules"
)

func inside(root, name string) bool {
	rel, err := filepath.Rel(root, name)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// canonical resolves the nearest existing parent, including for new files.
func canonical(name string) (string, error) {
	name = filepath.FromSlash(name)
	resolved, err := filepath.EvalSymlinks(name)
	if err == nil {
		absolute, err := filepath.Abs(resolved)
		if err != nil {
			return "", err
		}
		return filesystemNames(absolute)
	}
	if !os.IsNotExist(err) {
		return "", err
	}
	// A dangling symlink must not become an ordinary, nonexistent file.
	if info, statErr := os.Lstat(name); statErr == nil && info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("dangling symlink")
	}
	parent, base := filepath.Split(name)
	if parent != string(filepath.Separator) && parent != filepath.VolumeName(parent)+string(filepath.Separator) {
		parent = strings.TrimSuffix(parent, string(filepath.Separator))
	}
	if parent == name {
		return "", err
	}
	resolved, err = canonical(parent)
	if err != nil {
		return "", err
	}
	return filepath.Join(resolved, base), nil
}

// filesystemNames preserves symlink aliases while spelling existing components
// as directory entries do. SameFile uses the filesystem's identity rules rather
// than folding names that may identify different files on a case-sensitive volume.
func filesystemNames(name string) (string, error) {
	parent, base := filepath.Dir(name), filepath.Base(name)
	if parent == name {
		return name, nil
	}
	parent, err := filesystemNames(parent)
	if err != nil {
		return "", err
	}
	name = filepath.Join(parent, base)
	info, err := os.Lstat(name)
	if os.IsNotExist(err) {
		return name, nil
	}
	if err != nil {
		return "", err
	}
	entries, err := os.ReadDir(parent)
	if err != nil {
		return "", err
	}
	for _, entry := range entries {
		if entry.Name() == base {
			return name, nil
		}
	}
	var matches []string
	for _, entry := range entries {
		candidate, err := entry.Info()
		if err != nil {
			return "", err
		}
		if os.SameFile(info, candidate) {
			// Prefer the matching spelling over unrelated hard-link names.
			if strings.EqualFold(entry.Name(), base) {
				return filepath.Join(parent, entry.Name()), nil
			}
			matches = append(matches, entry.Name())
		}
	}
	if len(matches) == 1 {
		return filepath.Join(parent, matches[0]), nil
	}
	return "", fmt.Errorf("cannot determine filesystem path spelling")
}

func normalizeChanges(root, cwd string, changes []rules.Change) ([]rules.Change, error) {
	var normalized []rules.Change
	for _, change := range changes {
		name := change.Path
		if name == "" || strings.ContainsRune(name, 0) {
			return nil, fmt.Errorf("invalid tool path")
		}
		if runtime.GOOS != "windows" && (strings.HasPrefix(name, `\\`) || (len(name) > 1 && name[1] == ':')) {
			return nil, fmt.Errorf("path uses another platform's syntax")
		}
		if !filepath.IsAbs(name) {
			// Preserve .. until symlinks have been resolved: link/../file follows
			// the link's target before walking to its parent on the filesystem.
			name = cwd + string(filepath.Separator) + name
		}
		resolved, err := canonical(name)
		if err != nil {
			return nil, fmt.Errorf("cannot safely resolve tool path inside project")
		}
		name, err = filesystemNames(filepath.Clean(name))
		if err != nil {
			return nil, fmt.Errorf("cannot safely resolve tool path spelling")
		}
		if !inside(root, resolved) {
			if !inside(root, name) {
				continue
			}
			return nil, fmt.Errorf("tool path escapes project through a symlink")
		}
		for _, candidate := range []string{name, resolved} {
			if !inside(root, candidate) {
				continue
			}
			rel, err := filepath.Rel(root, candidate)
			if err != nil {
				return nil, fmt.Errorf("cannot normalize tool path")
			}
			normalized = append(normalized, rules.Change{Path: filepath.ToSlash(rel), Operation: change.Operation})
			if name == resolved {
				break
			}
		}
	}
	return normalized, nil
}

// patchChanges reads apply_patch's file headers, not shell commands or diffs.
func patchChanges(cwd, patch string) ([]rules.Change, error) {
	lines := strings.Split(strings.TrimSuffix(strings.ReplaceAll(patch, "\r\n", "\n"), "\n"), "\n")
	if len(lines) < 3 || lines[0] != "*** Begin Patch" || lines[len(lines)-1] != "*** End Patch" {
		return nil, fmt.Errorf("invalid apply_patch envelope")
	}
	var changes []rules.Change
	exists := map[string]bool{}
	pathKey := func(name string, unlink bool) (string, error) {
		if !filepath.IsAbs(name) {
			name = cwd + string(filepath.Separator) + name
		}
		parent, base := filepath.Split(name)
		parent, err := canonical(parent)
		if err != nil {
			return "", err
		}
		entry, err := filesystemNames(filepath.Join(parent, base))
		if err != nil {
			return "", err
		}
		if _, known := exists[entry]; known {
			return entry, nil
		}
		// Delete and move remove a symlink itself, leaving its target intact.
		if info, err := os.Lstat(name); unlink && err == nil && info.Mode()&os.ModeSymlink != 0 {
			return entry, nil
		}
		return canonical(name)
	}
	for i := 1; i < len(lines)-1; {
		line := lines[i]
		var operation, name string
		switch {
		case strings.HasPrefix(line, "*** Add File: "):
			operation, name = "create", strings.TrimPrefix(line, "*** Add File: ")
		case strings.HasPrefix(line, "*** Delete File: "):
			operation, name = "delete", strings.TrimPrefix(line, "*** Delete File: ")
		case strings.HasPrefix(line, "*** Update File: "):
			operation, name = "update", strings.TrimPrefix(line, "*** Update File: ")
		default:
			return nil, fmt.Errorf("invalid apply_patch file header")
		}
		if name == "" {
			return nil, fmt.Errorf("empty apply_patch path")
		}
		moving := operation == "update" && i+1 < len(lines)-1 && strings.HasPrefix(lines[i+1], "*** Move to: ")
		key, err := pathKey(name, operation == "delete" || moving)
		if err != nil {
			return nil, err
		}
		i++
		if moving {
			operation = "move"
			destination := strings.TrimPrefix(lines[i], "*** Move to: ")
			if destination == "" {
				return nil, fmt.Errorf("empty apply_patch move destination")
			}
			destinationKey, err := pathKey(destination, false)
			if err != nil {
				return nil, err
			}
			exists[destinationKey] = true
			changes = append(changes, rules.Change{Path: destination, Operation: "move"})
			i++
		}
		body := 0
		for i < len(lines)-1 {
			line = lines[i]
			if strings.HasPrefix(line, "*** Add File: ") || strings.HasPrefix(line, "*** Update File: ") || strings.HasPrefix(line, "*** Delete File: ") {
				break
			}
			valid := operation != "delete" && len(line) > 0 && line[0] == '+'
			if operation == "update" || operation == "move" {
				valid = line == "" || line == "*** End of File" || line == "@@" || strings.HasPrefix(line, "@@ ") || (len(line) > 0 && strings.ContainsRune(" +-", rune(line[0])))
			}
			if !valid {
				return nil, fmt.Errorf("invalid apply_patch body")
			}
			body++
			i++
		}
		if (operation == "update" || operation == "move") && body == 0 {
			return nil, fmt.Errorf("empty apply_patch update")
		}
		if operation == "create" {
			present, known := exists[key]
			if !known {
				_, err := os.Stat(key)
				if err != nil && !os.IsNotExist(err) {
					return nil, err
				}
				present = err == nil
				if !present {
					for previous, written := range exists {
						if written && strings.EqualFold(previous, key) {
							if _, err := os.Lstat(previous); os.IsNotExist(err) {
								// ponytail: new aliases lack identity; require one spelling until volume case detection is available.
								return nil, fmt.Errorf("ambiguous new apply_patch path casing")
							}
						}
					}
				}
			}
			if present {
				operation = "update"
			}
		}
		exists[key] = operation != "delete" && operation != "move"
		changes = append(changes, rules.Change{Path: name, Operation: operation})
	}
	if len(changes) == 0 {
		return nil, fmt.Errorf("empty apply_patch")
	}
	return changes, nil
}
