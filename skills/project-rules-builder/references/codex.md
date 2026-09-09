# Codex: инструкции и hooks

Перед записью открой актуальные официальные страницы и сопоставь с установленной версией `codex --version` и целевым хостом. CLI, desktop и другие клиенты могут обновляться независимо:

- [AGENTS.md](https://learn.chatgpt.com/docs/agent-configuration/agents-md)
- [Hooks](https://learn.chatgpt.com/docs/hooks)
- [Config reference](https://learn.chatgpt.com/docs/config-file/config-reference)
- [Command rules](https://learn.chatgpt.com/docs/agent-configuration/rules) — только если нужны ограничения запуска команд.

Ниже отправная точка, не обещание совместимости любой версии. При расхождении используй подтверждённый контракт целевой версии. Схема из ветки `main` может опережать release.

## Правила

Пиши проектные инструкции в `AGENTS.md`. Учитывай `AGENTS.override.md`: в одном каталоге он заменяет обычный файл, а не дополняет его. Не создавай override только ради нового правила, скрывая существующие инструкции.

Цепочка инструкций строится от project root до startup cwd; не полагайся на загрузку произвольного вложенного `AGENTS.md` при каждом редактировании. Для правила, нужного и при запуске из корня, добавь туда короткую формулировку с явным scope, например: «При изменении `src/api/**` запускай `…`». Проверяй фактические источники после нового запуска из корня и нужного подкаталога. Claude `paths` frontmatter здесь не задаёт native scope.

`.codex/rules/*.rules` — отдельные Starlark `prefix_rule`, регулирующие запуск команд вне sandbox, не Markdown-инструкции и не защита файлов. Добавляй только под такой запрос, задавай `decision` явно и проверяй match/non-match через `codex execpolicy check --rules <file> -- <argv>`. Не используй `allow` для обхода существующего approval.

## Регистрация hooks

Обычный project target — `.codex/hooks.json`; если проект уже использует inline `[hooks]` в `.codex/config.toml`, обновляй существующее представление. Источники складываются: более приоритетный слой не заменяет все предыдущие hooks. Не меняй глобальный конфиг без запроса.

Пример формы для **Git-проекта с доступным python3**, после создания обоих указанных scripts:

```json
{
  "hooks": {
    "PreToolUse": [{
      "matcher": "^apply_patch$",
      "hooks": [{
        "type": "command",
        "command": "python3 \"$(git rev-parse --show-toplevel)/.codex/hooks/protect_paths.py\"",
        "timeout": 10
      }]
    }],
    "Stop": [{
      "hooks": [{
        "type": "command",
        "command": "python3 \"$(git rev-parse --show-toplevel)/.codex/hooks/check_project.py\"",
        "timeout": 30
      }]
    }]
  }
}
```

Это форма, не готовые handlers: создай и проверь только запрошенные scripts, удали ненужные events. Для non-Git проекта используй безопасно экранированные абсолютные пути. Команда запускается из session cwd; корень для самой проверки тоже разреши явно. Не переноси Claude `args` или `CLAUDE_PROJECT_DIR` в Codex без документированного подтверждения.

Текущий контракт использует `type: "command"`; `prompt`/`agent` handlers могут распознаваться, но пропускаться. Hooks включены по умолчанию; `features.hooks` — канонический ключ, `features.codex_hooks` — устаревший alias. Если hook отключён конфигом или managed policy, сообщи причину, не переопределяй молча.

## PreToolUse

Stdin JSON содержит `hook_event_name`, `cwd`, `session_id`, `tool_name`, `tool_input`, `tool_use_id`; могут быть дополнительные поля. `Bash` соответствует также unified exec. `apply_patch` можно матчить как `apply_patch`, `Edit` или `Write`, но входной `tool_name` остаётся `apply_patch`.

```json
{
  "session_id": "fixture",
  "cwd": "/tmp/example-project",
  "hook_event_name": "PreToolUse",
  "tool_name": "apply_patch",
  "tool_use_id": "fixture-call",
  "tool_input": {"command": "*** Begin Patch\n*** Update File: src/main.go\n@@\n-old\n+new\n*** End Patch"}
}
```

`Bash` и `apply_patch` передают `tool_input.command`; это shell command и patch соответственно. Для file guard разбирай операции patch, включая add/delete/update/move, а не ищи имя файла во всём тексте. MCP/local tools имеют собственные аргументы; не считай их patch.

Блокировка: stdout JSON и exit 0:

```json
{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"Нарушено правило protect-production"}}
```

Разрешённый вызов: `{}` и exit 0, без `permissionDecision: "allow"`, чтобы оставить штатные permissions в силе. Для блокировки также документирован exit 2 с причиной в stderr. Не используй `permissionDecision: "ask"`, `continue: false`, `stopReason` или `suppressOutput` для PreToolUse: текущий контракт их не поддерживает и может продолжить tool call после ошибки hook.

Hook не является полной границей enforcement: hosted tools и некоторые специальные пути не проходят через него; `write_stdin` не запускает PreToolUse повторно. Матчер `apply_patch` не защищает от записи через shell/MCP.

## Stop и проверка хостом

`Stop` получает `stop_hook_active` и `last_assistant_message`; matcher для него не используется. Для успеха возвращай `{}` с exit 0; для одного продолжения — `{"decision":"block","reason":"Конкретная причина исправления"}` с exit 0. Plain text не заменяет JSON. Защиту от циклов и обработку ошибок реализуй по [общему workflow](native-hooks.md).

Project `.codex/` должен быть trusted. Кроме того, каждый non-managed hook требует review/trust текущего definition; изменённая definition снова требует review. Используй `/hooks` в CLI для проверки источника, enabled-состояния и trust; затем наблюдай реальное срабатывание. Не применяй `--dangerously-bypass-hook-trust` и не меняй trust-хранилище вручную.

Проверь прямые payload-тесты и безопасный host smoke отдельно. Если доступен только CLI, не называй desktop проверенным. Если host trust/reload нельзя завершить здесь, результат — «handler проверен; вызов хостом не подтверждён».
