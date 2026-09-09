# Windows CI must test the supported platform contract

**Date:** 2026-09-08
**Area:** Cross-platform tests, native hooks and executable approval
**Root Cause:** Tests assumed POSIX paths and hook support on Windows, while path normalization and executable hashing missed Windows-specific behavior.

## What happened

Both push and pull-request Windows jobs failed on the same commit. Both release-build jobs were skipped because their `needs: test` dependency failed.

## Why it happened — Root Cause Analysis

Installation and logging tests expected native hook support even though Windows deliberately rejects it. Ownership fixtures used Unix absolute paths, and the runner test compared short and long Windows directory names as strings. New tool paths containing forward slashes also broke recursive parent resolution: splitting accepted both separators, but trimming only removed the native separator.

The approval digest additionally needed the same executable lookup as the runner. Windows can resolve an extensionless command through PATHEXT; hashing the extensionless file can otherwise approve different bytes from the executable actually selected.

## What I probably didn't know

A common misconception is that a three-platform CI matrix makes every feature portable. Each platform must test its documented behavior, including intentional rejection. A path's spelling also does not uniquely identify a filesystem object.

## The fundamental knowledge

Use `os.SameFile` to compare directory identity, platform-absolute paths for fixtures, and `filepath.FromSlash` before native-separator operations. Preserve symlink traversal semantics; normalizing separators does not require cleaning away `..`.

Use `exec.LookPath` when binding approval to the selected executable. Keep portable tests on Windows, explicitly test unsupported capabilities, and scope skips to POSIX-specific installation, shell and log scenarios. A dependent build should remain gated on successful tests.

## Key takeaway — Prevention checklist

- Inspect failed job logs before changing workflow dependencies.
- Compare filesystem identity rather than display strings.
- Test both slash styles and nonexistent nested paths on Windows.
- Resolve executable paths with the runner's lookup rules.
- Keep Windows compilation, vet and runtime tests in CI.

## Related concepts to study

- Windows short paths, PATHEXT and symlink behavior
- Platform capability contracts and test fixtures
- GitHub Actions job dependencies
