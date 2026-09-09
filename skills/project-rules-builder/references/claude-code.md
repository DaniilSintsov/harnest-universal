# Claude Code: правила и hooks

При каждом запуске запиши `claude --version`, ОС и дату; сверь нужные поля с актуальными официальными [memory](https://code.claude.com/docs/en/memory), [hooks reference](https://code.claude.com/docs/en/hooks) и [hooks guide](https://code.claude.com/docs/en/hooks-guide). Эта памятка проверена 2026-09-08; она не подтверждает возможности установленной версии. Общий порядок генерации и проверок: [native-hooks.md](native-hooks.md).

## Instructions

Общие инструкции — `CLAUDE.md` или `.claude/CLAUDE.md`; тематические — `.claude/rules/**/*.md`. Например, `.claude/rules/api.md`:

```markdown
---
paths:
  - "src/api/**/*.ts"
---

# API

Проверяй входные данные на границе API существующим валидатором проекта.
```

Без `paths` правило загружается всегда; с `paths` — при чтении совпавшего файла. Это контекст, не executable enforcement. Для общего с Codex текста допустим `@AGENTS.md` внутри `CLAUDE.md`; Claude сам `AGENTS.md` не загружает. Сохраняй существующие инструкции, не копируй общий текст вторично. [Источник](https://code.claude.com/docs/en/memory#organize-rules-with-clauderules)

## Регистрация

Добавляй только свои handlers в `.claude/settings.json`; сохраняй остальные keys/hooks, повторный запуск не создаёт дубли. Читай `.claude/settings.local.json`: обычные настройки имеют приоритет над project settings, hooks объединяются. Не меняй local/user/managed settings ради активации. Сохраняй установленный `disableAllHooks`. [Settings precedence](https://code.claude.com/docs/en/settings#settings-precedence)

Ниже wiring для **уже написанных и проверенных** Python-скриптов. Выбери реально доступный runtime, создай скрипты в указанных местах. Exec form используй только после подтверждения поддержки `args` целевой версией:

```json
{
  "hooks": {
    "PreToolUse": [{
      "matcher": "Edit|Write",
      "hooks": [{
        "type": "command",
        "command": "python3",
        "args": ["${CLAUDE_PROJECT_DIR}/.claude/hooks/project-rules-pre.py"],
        "timeout": 10
      }]
    }],
    "Stop": [{
      "hooks": [{
        "type": "command",
        "command": "python3",
        "args": ["${CLAUDE_PROJECT_DIR}/.claude/hooks/project-rules-stop.py"],
        "timeout": 120
      }]
    }]
  }
}
```

Exec form передаёт путь одним аргументом. Для версии без `args`, на POSIX удали `args` и используй shell form:

```json
{"type":"command","command":"python3 \"$CLAUDE_PROJECT_DIR/.claude/hooks/project-rules-pre.py\"","timeout":10}
```

Здесь `$CLAUDE_PROJECT_DIR` раскрывает shell из environment внутри кавычек; не подставляй путь строковой конкатенацией. Для Stop аналогично замени имя скрипта. На Windows проверяй фактический shell/runtime, POSIX-строку не переноси автоматически. Скрипт находится относительно root старта; `cwd` payload может указывать другой worktree. Явно выбирай cwd проверки. [Источник](https://code.claude.com/docs/en/hooks#command-hook-fields)

## Контракт handlers

Stdin — JSON. Общие поля: `session_id`, `cwd`, `hook_event_name`. PreToolUse также получает `tool_name`, `tool_input`, `tool_use_id`; Edit/Write используют `tool_input.file_path`, shell tools — `tool_input.command`.

При нарушении PreToolUse возвращай один JSON в stdout, exit 0:

```json
{"hookSpecificOutput":{"hookEventName":"PreToolUse","permissionDecision":"deny","permissionDecisionReason":"Rule ID: конкретное нарушение"}}
```

При отсутствии нарушения — exit 0 без вывода: обычные approvals сохраняются. Не возвращай `allow` для успешной проверки. `Edit|Write` не покрывает shell/MCP-запись.

Stop при неуспешной проверке, stdout + exit 0:

```json
{"decision":"block","reason":"Rule ID: проверка завершилась ошибкой; исправь указанную причину"}
```

Альтернатива для обоих событий — stderr + exit 2. Exit 1, missing executable и timeout сами обычно не блокируют: сбой runtime не доказывает enforcement. [Источник](https://code.claude.com/docs/en/hooks#hook-input-and-output)

Stop сначала проверяет `stop_hook_active`: если `true`, разрешает остановку без повторного block. Stop возникает после ответа, не только при завершении задачи. Общий check budget должен помещаться в timeout handler. [Источник](https://code.claude.com/docs/en/hooks-guide#stop-hook-hits-the-block-cap)

## Загрузка и smoke

Interactive Claude требует workspace trust; `-p`/SDK считает каталог trusted и может выполнить project hooks без диалога. Не запускай такой smoke автоматически в непроверенном проекте. [Источник](https://code.claude.com/docs/en/hooks#workspace-trust)

Изменения settings обычно подхватываются автоматически. Проверь `/hooks`; перезапускай при пропущенном watcher-изменении или требовании целевой версии. Прямой запуск скрипта проверяет протокол, `/hooks` — регистрацию; реальное событие в Claude с наблюдаемым результатом — host smoke. Эти статусы фиксируй отдельно; примеры выше не заменяют проверку. Используй общий сценарий [native-hooks.md](native-hooks.md). [Источник](https://code.claude.com/docs/en/hooks-guide#hooks-shows-no-hooks-configured)
