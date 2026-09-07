package hooks

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	hookLogLimit = 1 << 20
	lockWait     = 500 * time.Millisecond
)

type logEntry struct {
	Time       time.Time `json:"time"`
	Platform   string    `json:"platform"`
	Event      string    `json:"event"`
	SessionID  string    `json:"session_id,omitempty"`
	TurnID     string    `json:"turn_id,omitempty"`
	ToolUseID  string    `json:"tool_use_id,omitempty"`
	RuleIDs    []string  `json:"rule_ids,omitempty"`
	CheckID    string    `json:"check_id,omitempty"`
	Base       string    `json:"base,omitempty"`
	Status     string    `json:"status"`
	Reason     string    `json:"reason,omitempty"`
	DurationMS int64     `json:"duration_ms"`
}

func appendLog(root string, options Options, event Event, records []Record, duration time.Duration) error {
	stateRoot, err := secureStateRoot(root)
	if err != nil {
		return err
	}
	defer stateRoot.Close()
	unlock, err := acquireLogLock(stateRoot, "hooks.lock")
	if err != nil {
		return err
	}
	defer unlock()

	const path = "hooks.jsonl"
	if err := rejectRootSymlink(stateRoot, path); err != nil {
		return err
	}
	lines := make([]byte, 0, len(records)*256)
	for _, record := range records {
		entry := logEntry{
			Time: time.Now().UTC(), Platform: safeText(options.Platform, 32),
			Event: safeText(options.Event, 32), SessionID: safeID(event.SessionID),
			TurnID: safeID(event.TurnID), ToolUseID: safeID(event.ToolUseID),
			RuleIDs: safeIDs(record.RuleIDs), CheckID: safeID(record.CheckID),
			Base:   safeText(record.Base, 128),
			Status: safeText(record.Status, 32), Reason: safeText(record.Reason, 256),
			DurationMS: duration.Milliseconds(),
		}
		line, err := json.Marshal(entry)
		if err != nil {
			return fmt.Errorf("encoding hook log: %w", err)
		}
		lines = append(lines, line...)
		lines = append(lines, '\n')
	}
	if len(lines) == 0 {
		return nil
	}
	if len(lines) > hookLogLimit {
		return fmt.Errorf("hook log entry exceeds size limit")
	}
	info, err := stateRoot.Stat(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("reading hook log metadata: %w", err)
	}
	if err == nil && info.Size()+int64(len(lines)) > hookLogLimit {
		previous := path + ".1"
		if err := rejectRootSymlink(stateRoot, previous); err != nil {
			return err
		}
		if err := stateRoot.Remove(previous); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("removing previous hook log: %w", err)
		}
		if err := stateRoot.Rename(path, previous); err != nil {
			return fmt.Errorf("rotating hook log: %w", err)
		}
	}
	file, err := openLogFile(stateRoot, path)
	if err != nil {
		return fmt.Errorf("opening hook log: %w", err)
	}
	defer file.Close()
	if info, err := file.Stat(); err != nil || !info.Mode().IsRegular() {
		return fmt.Errorf("hook log must be a regular file")
	}
	if err := file.Chmod(0o600); err != nil {
		return fmt.Errorf("securing hook log: %w", err)
	}
	if _, err := file.Write(lines); err != nil {
		return fmt.Errorf("writing hook log: %w", err)
	}
	return nil
}

func secureStateRoot(root string) (*os.Root, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolving log root: %w", err)
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return nil, fmt.Errorf("resolving log root: %w", err)
	}
	projectRoot, err := os.OpenRoot(root)
	if err != nil {
		return nil, fmt.Errorf("opening log root: %w", err)
	}
	defer projectRoot.Close()
	if err := rejectRootSymlink(projectRoot, ".harnest"); err != nil {
		return nil, err
	}
	if err := projectRoot.Mkdir(".harnest", 0o700); err != nil && !os.IsExist(err) {
		return nil, fmt.Errorf("creating hook log directory: %w", err)
	}
	harnestRoot, err := projectRoot.OpenRoot(".harnest")
	if err != nil {
		return nil, fmt.Errorf("opening hook log directory: %w", err)
	}
	defer harnestRoot.Close()
	if err := rejectRootSymlink(harnestRoot, "state"); err != nil {
		return nil, err
	}
	if err := harnestRoot.Mkdir("state", 0o700); err != nil && !os.IsExist(err) {
		return nil, fmt.Errorf("creating hook log directory: %w", err)
	}
	stateRoot, err := harnestRoot.OpenRoot("state")
	if err != nil {
		return nil, fmt.Errorf("opening hook log state: %w", err)
	}
	return stateRoot, nil
}

func rejectRootSymlink(root *os.Root, path string) error {
	info, err := root.Lstat(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspecting hook log path: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("hook log path must not be a symlink")
	}
	return nil
}

func acquireLogLock(root *os.Root, path string) (func(), error) {
	deadline := time.Now().Add(lockWait)
	for {
		err := root.Mkdir(path, 0o700)
		if err == nil {
			return func() { _ = root.Remove(path) }, nil
		}
		if !os.IsExist(err) {
			return nil, fmt.Errorf("creating hook log lock: %w", err)
		}
		if time.Now().After(deadline) {
			return nil, errors.New("hook log lock is busy or stale")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func safeID(value string) string {
	var out strings.Builder
	for _, r := range value {
		if out.Len() >= 128 {
			break
		}
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("._:-", r) {
			out.WriteRune(r)
		} else {
			out.WriteByte('_')
		}
	}
	return out.String()
}

func safeIDs(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	result := make([]string, len(values))
	for i, value := range values {
		result[i] = safeID(value)
	}
	return result
}

func safeText(value string, limit int) string {
	var out strings.Builder
	for _, r := range value {
		if out.Len() >= limit {
			break
		}
		if r >= 0x20 && r != 0x7f {
			out.WriteRune(r)
		} else {
			out.WriteByte(' ')
		}
	}
	return strings.TrimSpace(out.String())
}
