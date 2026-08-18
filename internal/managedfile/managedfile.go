package managedfile

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Upsert replaces one managed block while preserving all user-owned content.
func Upsert(path, id, body string) error {
	return UpsertWithMode(path, id, body, 0600)
}

// WriteAtomic replaces path without exposing a partially written file.
func WriteAtomic(path string, data []byte, defaultMode os.FileMode) error {
	mode := defaultMode
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	} else if !os.IsNotExist(err) {
		return err
	}
	return writeAtomic(path, data, mode)
}

// ValidateUpsert checks managed markers without changing path.
func ValidateUpsert(path, id string) error {
	if id == "" || strings.ContainsAny(id, "\r\n") {
		return fmt.Errorf("invalid managed block id %q", id)
	}
	content, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	start := fmt.Sprintf("<!-- %s-managed:start -->", id)
	end := fmt.Sprintf("<!-- %s-managed:end -->", id)
	startCount, endCount := strings.Count(string(content), start), strings.Count(string(content), end)
	if startCount != endCount || startCount > 1 || (startCount == 1 && strings.Index(string(content), end) < strings.Index(string(content), start)) {
		return fmt.Errorf("malformed %s managed markers in %s", id, path)
	}
	return nil
}

// UpsertWithMode uses defaultMode only when path does not exist.
func UpsertWithMode(path, id, body string, defaultMode os.FileMode) error {
	if id == "" || strings.ContainsAny(id, "\r\n") {
		return fmt.Errorf("invalid managed block id %q", id)
	}

	start := fmt.Sprintf("<!-- %s-managed:start -->", id)
	end := fmt.Sprintf("<!-- %s-managed:end -->", id)
	block := start + "\n" + strings.TrimSpace(body) + "\n" + end

	existing, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		return writeAtomic(path, []byte(block+"\n"), defaultMode)
	}

	content := string(existing)
	startCount := strings.Count(content, start)
	endCount := strings.Count(content, end)
	if startCount != endCount || startCount > 1 {
		return fmt.Errorf("malformed %s managed markers in %s", id, path)
	}

	updated := content
	if startCount == 1 {
		startAt := strings.Index(content, start)
		endAt := strings.Index(content, end)
		if endAt < startAt {
			return fmt.Errorf("malformed %s managed markers in %s", id, path)
		}
		updated = content[:startAt] + block + content[endAt+len(end):]
	} else {
		updated = strings.TrimRight(content, "\n") + "\n\n" + block + "\n"
	}
	if updated == content {
		return nil
	}

	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	mode := info.Mode().Perm()
	if err := writeAtomic(path+".bak", existing, mode); err != nil {
		return fmt.Errorf("creating backup: %w", err)
	}
	if err := writeAtomic(path, []byte(updated), mode); err != nil {
		return fmt.Errorf("updating managed file: %w", err)
	}
	return nil
}

func writeAtomic(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	tmp, err := os.CreateTemp(dir, ".harnest-*")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)

	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return replaceFile(tmpPath, path)
}
