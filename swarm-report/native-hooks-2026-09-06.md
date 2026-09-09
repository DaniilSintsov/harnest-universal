# Native hooks — 2026-09-06

Статус: код реализован; полная приёмка по спецификации остаётся частичной из-за непроверенного Codex host integration и непройденного полного набора skill evals в обоих хостах.

Ветка: `feature/native-hooks`, создана от актуальной `origin/main`. Спецификация: [native-hooks.md](../.specs/native-hooks.md). Workflow: Research → Plan → Executing → Validation → Report. Назначенные profile-роли с недоступной моделью заменены совместимыми агентами.

## Решение

В `harnest.yaml` появился `hooks.rules` — явный список активируемых правил. Существующие rule/check loaders и общий matcher переиспользуются. `harnest hook evaluate` принимает native JSON и возвращает протокол Claude Code/Codex; shared check runner не пишет в его stdout.

`PreToolUse` обрабатывает Claude Edit/Write/NotebookEdit и Codex apply_patch, включая multi-file patch и обе стороны move. Пути учитывают event cwd, symlinks, новые файлы, traversal и границы checkout. `Stop` проверяет committed/staged/unstaged/untracked изменения Git, запускает каждый применимый approved check один раз и допускает одно автоматическое продолжение.

Checks имеют точные argv, timeout (60 секунд по умолчанию), предел combined output 64 КиБ и ограничение завершения процессов. Общий deadline evaluator — 110 секунд, handler — 120. CLI verification сохраняет доступ к check output; native output и журнал его не содержат.

`generate`/`--dry-run` планируют native configs и локальные ignore-правила до записи. Установка сохраняет чужие handlers, проверяет ownership, tracked/inline conflicts и изменения config после чтения. Native-записи атомарны с rollback; ошибки остальных генераторов явно сообщают о частичном результате. Повторная генерация идемпотентна, удаление binding сохраняет rule/check. Локальный `hooks.enabled: false` оставляет wiring.

Журнал `.harnest/state/hooks.jsonl` ограничен 1 МиБ и одной предыдущей копией, защищён межпроцессным lock и confined filesystem operations. `doctor` различает selection, definition/check validity, установленный config, отключение и неподтверждённый native trust/запуск.

`project-rules-builder` расширен self-contained reference с выбором правил, точным предпросмотром executable/check, approval, сопровождением bindings и воспроизводимыми eval/smoke сценариями.

## Изменённые области и impact

| Область | Изменение | Зависимые пути и проверка |
|---|---|---|
| `internal/rules`, `internal/verify` | Selection validation, общий scope/operation matcher, NUL-safe Git discovery с subproject/rename | CLI verify и evaluator используют один matcher; `--allow` сохранён; unit и реальные Git fixtures |
| `internal/checks` | Timeout, capped capture, context, process-tree cleanup | CLI выводит captured output; native runner остаётся тихим; helper-process tests и race |
| `internal/harness/native_hooks.go` | Native planning, merge, ownership, ignores, atomic writes, inspection | Generate/DryRun и doctor; preservation, rollback, symlink, tracked config, inline TOML, worktree tests |
| `internal/yaml`, `internal/ir` | Additive hooks config и local switch | BuildIR/Generate/CLI/doctor/verify; существующие consumers и полный suite проходят |
| `internal/hooks` | Evaluator, paths/patch, protocol, JSONL | CLI internal hook entrypoint; positive/negative fixtures, malformed events, nested worktree, repeats, concurrency |
| `cmd/harnest`, `internal/doctor` | CLI wiring, exact native preview, local set/unset/show, диагностика | CLI round-trip и inspection tests; hard-rule removal даёт warning, не ошибку |
| `skills/project-rules-builder`, README, smoke docs | Пользовательский сценарий и evidence | Skill validator и forward eval во временном проекте |

CodeGraph проверил callers изменённых shared APIs. Новых внешних зависимостей нет. Изменение поведения CLI verify намеренное: частичный Git failure, ambiguous newline filenames для check env и invalid globs больше не превращаются в успешную проверку.

## Проверки

- `GOCACHE=/tmp/harnest-go-cache go test ./...` — passed.
- `GOCACHE=/tmp/harnest-go-cache go test -race ./...` — passed после финальных изменений.
- Сборки `GOOS=linux GOARCH=amd64` и `GOOS=windows GOARCH=amd64` — passed; это компиляция, не native host validation.
- `harnest verify --changed` — passed. В этом репозитории нет активных project-local rule/check файлов; функциональность отдельно проверена fixture-тестами.
- `harnest doctor` — healthy; существующее предупреждение об отсутствующем `docs/architecture/INDEX.md` сохранено.
- Skill validator — passed. `git diff --check` — passed.
- Forward eval: исходные два bindings, dry-run без записи, согласованная генерация, удаление одного binding, сохранение rule/check и чужих hooks, повторная генерация — passed. Новая executable-проверка остаётся pending approval; semantic requirement не превращается в hook; неизвестный trust не выдаётся за verified.

## Реальные хосты и ограничения

Актуальные версии, SHA и наблюдения хранятся в [smoke evidence](../docs/native-hooks-smoke.md).

Claude Code 2.1.226: реальные allowed/protected Edit и Stop/repeated Stop подтверждены host lifecycle events; защищённый файл не изменился. Проверен compiled evaluator, а не только прямой вызов через stdin.

Codex 0.146.0: unit/contract и generation/eval проверки пройдены, реальный запуск hook не подтверждён. Интерактивный trust TUI оказался ненадёжно управляемым из PTY; обход trust и ручная запись hashes не применялись. Случайно запущенное при попытке smoke обновление CLI отменено: восстановлена версия 0.146.0, пользовательский config сравнен с резервной копией и совпал байт-в-байт. Дальнейшие интерактивные попытки прекращены.

Windows native installation явно отклоняется до подтверждения совместимости. Linux host smoke и полный набор агентных evals в обоих хостах остаются невыполненными. Shell/MCP до исполнения, ignored/external изменения и защита от недоверенной подмены самого репозитория не входят в гарантию v1; обязательный acceptance gate остаётся в CI.
