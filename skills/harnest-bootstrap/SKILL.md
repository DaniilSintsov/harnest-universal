---
name: harnest-bootstrap
description: "Глубоко настраивает Harnest для существующего репозитория после быстрого harnest init: строит архитектурный контекст, обнаруживает portable и platform-specific роли, предлагает scoped rules и проверки, затем компилирует Claude Code/Codex-конфигурацию. Использовать только по явному запросу пользователя на /harnest-bootstrap, глубокий onboarding или bootstrap Harnest; не запускать автоматически после init."
---

# Harnest Bootstrap

Построй доказуемый локальный control plane проекта. Не вызывай LLM API из CLI и не деплой production.

## Workflow

1. Прочитай root `AGENTS.md` / `CLAUDE.md`, `harnest.yaml`, манифесты и существующую документацию. Сохрани пользовательский текст.
2. Если config v1, запусти `harnest migrate`. Затем запусти `harnest doctor`; ошибки hard-enforcement блокируют завершение.
3. Запусти `architecture-context-builder`, если `docs/architecture/INDEX.md` отсутствует или устарел. Загружай только релевантные документы.
4. Обнаружь роли отдельно для каждой целевой платформы. Общими считай только роли, исходник которых лежит в `.agents/agents/*.md`; Harnest materialize их в target-каталоги под реальным callable `name` без synthetic prefix. Не назначай Claude-only роль Codex и наоборот.
5. Если роль назначена и доступна, обязательно используй её для соответствующего шага. Если недоступна, продолжай основным агентом и явно запиши fallback. Не имитируй изоляцию, когда она обязательна.
6. Запусти `project-rules-builder`. Покажи кандидатов пользователю до активации. Никогда не создавай `hard` автоматически.
7. Для custom executable-check потребуй явное одобрение и зафиксируй `approved: true`. Не генерируй произвольные shell hooks.
8. Запусти `harnest generate`, затем `harnest doctor` и `harnest verify --changed`.
9. Сообщи созданные artifacts, выбранные adapters/roles, одобренные rules, fallback-и и неизвестные факты.

## Ограничения

- По умолчанию сохраняй Harnest artifacts локальными через `.git/info/exclude`.
- Используй targeted Git history только для спорного решения или происхождения правила; не сканируй всю историю.
- `swarm-report/` создавай только для выбранного профиля, долгой задачи или сбоя.
- Не изменяй production и не активируй кандидатов без подтверждения пользователя.
