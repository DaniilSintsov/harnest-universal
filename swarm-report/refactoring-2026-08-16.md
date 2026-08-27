# Refactoring report: universal release hardening

**Дата:** 2026-08-16  
**Профиль:** refactoring  
**Версия:** 0.12.0-universal.3  
**Статус:** Ready for release candidate

## Результат

- Чистая установка по умолчанию ставит 10 встроенных профилей, global config и bundled skills для Claude Code и Codex.
- `init` на пустом компьютере генерирует оба target-а: `CLAUDE.md` и `AGENTS.md`.
- `CLAUDE_CONFIG_DIR` и `CODEX_HOME` учитываются при установке и внутри профилей; Codex user skills остаются в нативном `$HOME/.agents/skills`.
- Проектные skills имеют один source root и управляемое зеркало для Claude Code без symlink.
- Portable agents материализуются как Markdown для Claude Code и TOML для Codex.
- Custom profiles синхронизируются в обе стороны с адаптацией инструкций, путей и моделей; конфликтующий target получает hash-named backup.
- `generate --dry-run` показывает adapters, skills, agents и stale cleanup без записи.
- Неиспользуемые public generators и no-op schema fields удалены или переведены в явную ошибку.
- Запись managed-файлов атомарна; Unix дополнительно синхронизирует каталог, Windows использует replacement semantics `os.Rename`.
- Команда `harnest update` отсутствует; README описывает обновление через `go install` и повторный `harnest install`.
- Добавлены Windows/macOS/Linux CI matrix и tag-based release workflow.

## Capability matrix

| Возможность | Claude Code | Codex |
|---|---|---|
| Project/global instructions | native | native |
| Project/global skills | native | native |
| Project/global agents | native Markdown | native TOML |
| Workflow profiles | Harnest global profiles | Harnest global profiles |
| Rules | fallback через generated instructions | fallback через generated instructions |
| Permissions | fallback | fallback |
| Pre/post hooks | unsupported | unsupported |
| `harnest verify` | Harnest fallback | Harnest fallback |

Итог: одинаковый Harnest workflow поддерживается для portable subset. Полной идентичности платформ нет: нативные orchestration/tool APIs и hooks различаются.

## Validation

- `go test ./...` — passed.
- `go test -race ./...` — passed.
- `go vet ./...` — passed.
- `go mod verify` — passed.
- Linux/Windows cross-build — passed.
- Release build: Darwin/Linux/Windows, amd64/arm64 — passed.
- SHA-256 verification всех шести release artifacts — passed.
- Clean-home smoke с пробелами, Unicode и custom config roots — passed.
- Custom profile sync Claude Code → Codex — passed; root, instruction filename, question tool и model ID адаптированы.
- `harnest doctor .` — healthy; только warning об отсутствующем `docs/architecture/INDEX.md`.
- `harnest verify --changed .` — passed, 88 changed files текущего worktree.
- Scoped `.harnest/rules` отсутствуют.
- `govulncheck` локально не установлен; dependency surface состоит из stdlib и `gopkg.in/yaml.v3`.

## Impact review

Core dependents для global paths, materialization, dry-run, portable agents, profile sync и schema validation покрыты package tests. Основные остаточные риски:

1. Windows runtime проверен cross-compile и CI-конфигурацией, но локально не исполнялся на Windows.
2. Публикация требует существующего GitHub repository, push кода и tag `v0.12.0-universal.3`; эти внешние действия не выполнялись.
3. Лицензия остаётся non-commercial; README сохраняет attribution и описание отличий fork-а.
