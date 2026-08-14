# Harnest Universal

Универсальный локальный workflow для работы AI coding agents с любым репозиторием.

CLI остаётся детерминированным: обнаруживает проект, валидирует YAML, строит vendor-neutral IR, компилирует инструкции и запускает проверки. Исследование кода, архитектурные решения и формирование правил выполняют portable Agent Skills.

## Поддержка v1

- Claude Code и Codex: активные adapters.
- Cursor, Windsurf, OpenCode и Qwen Code: не входят в поддерживаемую v1 matrix. Legacy generators остаются в source без public compatibility guarantees.
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

`make release` собирает Darwin, Linux и Windows binaries для x64/arm64 и создаёт `dist/checksums.txt`.

`harnest install`:

- обнаруживает установленные Claude Code и Codex; на чистой машине ставит оба target-а;
- `--harness claude-code|codex` ограничивает установку одним target-ом;
- добавляет/обновляет только managed block в `~/.claude/CLAUDE.md` и `~/.codex/AGENTS.md`;
- сохраняет внешний текст, права файла и `.bak`;
- ставит profiles и portable skills в каталоги выбранных платформ;
- обновляет неизменённые builtin profiles по checksum state, а пользовательские сохраняет;
- переносит retired upstream profiles/agents в hash-named recoverable backup только при точном известном checksum;
- останавливается при повреждённых или дублированных managed markers.

`harnest init` создаёт schema v2 и запускает agent wizard: для каждой consilium-роли и exec scope можно принять предложенного агента, пропустить назначение, найти установленного агента или указать имя вручную. `harnest init --non-interactive` принимает предложения автоматически для CI и scripts. Пути architecture/rules/skills резервируются в config, но используются только если artifacts существуют. Harnest artifacts локальны по умолчанию через `.git/info/exclude`; tracked `.gitignore` не меняется. Глубокий архитектурный onboarding запускается отдельно: `/harnest-bootstrap`.

Portable agents из `.agents/agents/*.md` копируются под реальным `name` только в каталоги выбранных adapters, например `.claude/agents/` и `.codex/agents/`. При конфликте существующий target-файл сохраняется как `.bak`; последующие обновления распознаются по managed ownership marker.

`harnest generate --dry-run` показывает только adapter outputs. Materialization и cleanup portable agents в dry-run не previewed.

## Distribution

Поддерживаемые каналы: Go install и binaries из GitHub Releases. Npm и Homebrew packages не публикуются.

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
