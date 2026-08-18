# Harnest Universal

Универсальный локальный workflow для работы AI coding agents с любым репозиторием.

CLI остаётся детерминированным: обнаруживает проект, валидирует YAML, строит vendor-neutral IR, компилирует инструкции и запускает проверки. Исследование кода, архитектурные решения и формирование правил выполняют portable Agent Skills.

## Поддержка v1

- Claude Code и Codex: активные adapters.
- Cursor, Windsurf, OpenCode и Qwen Code: не входят в поддерживаемую v1 matrix; generators удалены.
- Русский язык по умолчанию; `settings.language: en` переключает создаваемые workflow-инструкции.
- Production deploy не входит в default workflow.

## Локальный запуск

Нужен Go 1.25+.

```bash
make build
./bin/harnest install
cd /path/to/project
/path/to/harnest/bin/harnest init
```

После публикации module можно установить через Go:

```bash
go install github.com/daniilsintsov/harnest-universal/cmd/harnest@latest
```

`make release` собирает Darwin, Linux и Windows binaries для x64/arm64 и создаёт `dist/checksums.txt`. Push tag-а вида `v<version>` запускает GitHub Release workflow; tag обязан совпасть с `make version`.

`harnest install`:

- без флагов всегда ставит оба target-а — Claude Code и Codex, независимо от уже существующих каталогов;
- берёт 10 builtin profiles и bundled skills из самого binary, поэтому на чистом компьютере никакие заранее созданные профили не нужны;
- `--harness claude-code|codex` ограничивает установку одним target-ом;
- учитывает `CLAUDE_CONFIG_DIR` и `CODEX_HOME`; без них использует `~/.claude` и `~/.codex`;
- добавляет/обновляет только managed block в `<CLAUDE_CONFIG_DIR>/CLAUDE.md` и `<CODEX_HOME>/AGENTS.md`;
- сохраняет внешний текст, права файла и `.bak`;
- ставит profiles и portable skills в каталоги выбранных платформ;
- обновляет неизменённые builtin profiles по checksum state, а пользовательские сохраняет;
- переносит retired upstream profiles/agents в hash-named recoverable backup только при точном известном checksum;
- останавливается при повреждённых или дублированных managed markers.

`harnest init` без `--harness` всегда создаёт проект для Claude Code и Codex. Команда создаёт schema v2 и запускает agent wizard: для каждой consilium-роли и exec scope можно принять предложенного агента, пропустить назначение, найти установленного агента или указать имя вручную. `harnest init --non-interactive` принимает предложения автоматически для CI и scripts. Пути architecture/rules/skills используются только если artifacts существуют. В Git-репозитории Harnest artifacts локальны по умолчанию через `.git/info/exclude`; tracked `.gitignore` не меняется. Глубокий архитектурный onboarding запускается отдельно: `/harnest-bootstrap`.

Portable agents из Harnest-source `.agents/agents/*.md` материализуются в нативный формат выбранного adapter: `.claude/agents/<name>.md` для Claude Code и `.codex/agents/<name>.toml` для Codex. Codex TOML получает обязательные `name`, `description`, `developer_instructions`; при конфликте существующий target-файл сохраняется как `.bak`, последующие обновления распознаются по managed ownership marker.

Project skills имеют единый редактируемый source в `.agents/skills/<name>/SKILL.md`. Codex читает source напрямую. `harnest generate` зеркалирует каждый skill в `.claude/skills/<name>/` для Claude Code без symlink; target-каталоги помечаются как managed, пользовательские каталоги не перезаписываются. Global bundled skills ставятся в `<CLAUDE_CONFIG_DIR>/skills` для Claude Code и `~/.agents/skills` для Codex; `CODEX_HOME` не меняет официальный global skills path.

`harnest generate --dry-run` валидирует targets и conflicts, показывает adapter outputs, portable agents, project skills и cleanup, но не пишет файлы.

### Кастомный профиль для обеих платформ

Создай профиль в одном source target-е, затем синхронизируй во второй:

```bash
harnest profiles add my-workflow --harness claude-code
harnest profiles sync my-workflow --from claude-code
```

Обратное направление тоже поддерживается: `harnest profiles sync my-workflow --from codex`. Sync адаптирует известные имена инструкций, файлов и моделей (`opus` ↔ `gpt-5.6-sol`, `sonnet` ↔ `gpt-5.6-terra`, `haiku` ↔ `gpt-5.6-luna`); существующая отличающаяся копия сохраняется рядом как hash-named `.bak.<hash>`. Builtin profiles синхронизировать вручную нельзя — их platform-specific версии поддерживает `harnest install`.

`harnest convert --from claude-code --to codex` переключает target в существующем `harnest.yaml`; без него сохраняется legacy-конвертация из `CLAUDE.md`.

## Distribution

Поддерживаемые каналы: Go install и binaries из GitHub Releases. Npm и Homebrew packages не публикуются.

## Обновление

Harnest не обновляет себя автоматически. Если CLI установлен через Go, установи последнюю версию поверх текущей и повторно примени встроенные profiles, global config и skills:

```bash
go install github.com/daniilsintsov/harnest-universal/cmd/harnest@latest
harnest version
harnest install
```

`harnest install` обновляет неизменённые builtin profiles, сохраняет custom profiles и создаёт backup перед миграцией изменённых managed-файлов.

Если Harnest установлен из GitHub Releases, скачай binary для своей OS/architecture и `checksums.txt`, проверь SHA-256, замени текущий executable, затем запусти `harnest install`.

После обновления CLI обнови каждый существующий проект:

```bash
harnest migrate /path/to/project
harnest generate /path/to/project
harnest doctor /path/to/project
```

`migrate` создаёт backup при переходе на новую schema и ничего не меняет, если проект уже использует текущую версию. `generate` заново применяет project config к Claude Code и Codex.

## Workflow

1. Перед каждой новой задачей интерактивно выбирается workflow-профиль. Явно названный профиль считается выбранным; иначе Harnest предлагает лучший match по Meta и до двух альтернатив.
2. Primary roles определяются автоматически по запросу и выбранному профилю; отдельного вопроса пользователю нет.
3. Любая code-changing задача проходит `Research → Plan → Executing`; мелкая задача сокращает глубину стадий, но не пропускает их.
4. `business-feature` используется как рекомендуемый fallback только когда Meta не даёт точного match.
5. Назначенная доступная роль используется обязательно. Значение `auto` выбирает совместимого агента текущей платформы; при отсутствии агент явно сообщает main-agent fallback.
6. Если configured paths существуют, загружаются только релевантные architecture docs, skills и scoped rules.
7. Code-changing задача завершается `harnest verify --changed`; read-only research проверки не требует. Для нестандартной mainline укажи `--base <ref>`.

## Schema v2

```yaml
version: 2
project:
  name: example
context:
  architecture:
    index: docs/architecture/INDEX.md
    state: docs/architecture/.context-state.json
rules:
  root: .harnest/rules
  index: .harnest/rules/INDEX.yaml
skills:
  root: .agents/skills
checks:
  root: .harnest/checks
workflow:
  default_profile: business-feature
  role_selection: auto # interactive поддерживается как legacy alias для auto
  require_available_roles: true
  verify_changed: true
agents:
  consilium:
    architect: auto
    security: auto
  executing: []
harnesses: [claude-code, codex]
settings:
  local_default: true
  language: ru
```

Legacy `version: 1` читается. `harnest migrate` создаёт `harnest.yaml.v1.bak` и атомарно записывает v2.

Поля `design_system`, `profiles`, `settings.lock_file` и `adapters.<name>.models` пока не реализованы. Вместо тихого no-op генерация возвращает явную ошибку с заменяющей командой или полем.

## Rules и checks

Active rules лежат в `.harnest/rules/*.yaml`:

```yaml
id: protect-production
severity: required
statement: Не изменять production-конфигурацию без явного разрешения.
scope:
  paths: [deploy/**]
enforcement:
  - type: protect-path
    paths: [deploy/**]
```

Severity:

- `hard` — только с механическим enforcement;
- `required` — обязательное semantic требование;
- `preference` — локальное предпочтение.

`harnest doctor` возвращает ошибку, если hard rule нельзя обеспечить выбранным adapter. Claude Code и Codex сейчас имеют `verification=fallback`, поэтому instruction-driven `harnest verify --changed` не делает hard rule adapter-native. `deny-command` не поддерживается и отклоняется при validation для любой severity; доступны `protect-path` и `require-check`. Custom executable-check запускается без shell-интерпретации и только при `approved: true`; список изменённых файлов доступен ему в `HARNEST_CHANGED_FILES`, по одному пути на строку. `harnest learn` создаёт inactive candidate в `.harnest/rules/candidates/`; автоматической активации нет.

## Команды

```text
harnest install [--harness claude-code|codex]
harnest init [dir] [--harness claude-code|codex] [--non-interactive]
harnest migrate [dir]
harnest generate [dir] [--dry-run]
harnest doctor [dir]
harnest verify --changed [dir] [--base <ref>] [--allow <rule-id>]
harnest profiles list|add|edit|remove [name] [--harness claude-code|codex]
harnest profiles sync <name> --from claude-code|codex
harnest learn [dir] --id <id> --statement <text>
harnest detect [dir]
harnest drift [dir] # legacy schema v1 only
harnest export [dir]
harnest convert --from claude-code --to claude-code|codex [dir]
```

`convert` читает только exact `CLAUDE.md` source и переносит legacy agent mapping. Он не переносит schema v2 control-plane fields; для v2 используй `harnest generate`.

`drift` анализирует только legacy schema v1 configs. При наличии schema v2 команда возвращает явную unsupported error до чтения соседних legacy-файлов.

## Portable skills

- `harnest-bootstrap`
- `architecture-context-builder`
- `project-rules-builder`
- `compliance-review`

Installer также сохраняет включённые license/source notices для bundled reference materials.

## Лицензия и происхождение

Проект распространяется по [CC BY-NC 4.0](LICENSE). Коммерческое использование лицензией не разрешено.

Это modified fork [AlexGladkov/harnest](https://github.com/AlexGladkov/harnest). Third-party components и их отдельные лицензии перечислены в [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).
