---
name: harnest-rules-builder
description: Создаёт scoped rules, executable checks и native hook bindings через Harnest. Использовать при явном выборе Harnest для правил проекта, пересмотре `.harnest/rules/*.yaml` или из harnest-bootstrap; для самостоятельных правил Codex/Claude Code использовать project-rules-builder.
---

# Harnest Rules Builder

Адаптер `project-rules-builder` для Harnest: правила и checks хранятся в YAML, native hooks создаёт `harnest generate`.

## Выбор режима

1. Используй Harnest, только когда пользователь выбрал его или запросил Harnest bootstrap. Само наличие `harnest.yaml` не означает согласия на этот режим.
2. Сохрани уже указанный выбор провайдера: `codex`, `claude-code` или оба. Если выбора нет, спроси пользователя; не выводи target из текущего агента. Перед генерацией сверь выбранные платформы с `harnest.yaml`; расхождение согласуй до изменения config или записи native-файлов.
3. Нужны Harnest CLI, `harnest.yaml` и skill `project-rules-builder`. Если config отсутствует, сообщи об этом; не запускай `harnest init` автоматически. Для самостоятельного режима используй `project-rules-builder` по выбору пользователя.
4. Найди [project-rules-builder](../project-rules-builder/SKILL.md) рядом с этим skill либо в установленных skills целевой платформы. Если он отсутствует, сообщи недостающую зависимость. Не подменяй её собственной копией анализа и не устанавливай без запроса.

## Workflow

1. Прочитай `harnest.yaml`, root instructions, architecture index при наличии и только релевантные исходники.
2. Выполни только раздел **«Анализ правил»** из `project-rules-builder`: собери кандидатов с evidence и убери дубли. Не запускай его этап записи нативных правил/hooks.
3. Для каждого кандидата задай Harnest scope: `paths`, `domains`, `operations`. Пустой scope означает весь проект и требует сильного обоснования.
4. Назначь severity: `hard` — нарушение должно механически блокироваться; `required` — обязательно для агента, но может требовать semantic review; `preference` — локальный стиль или желательное решение.
5. Покажи таблицу: id, severity, scope, evidence, enforcement. Активируй только подтверждённые пользователем правила; уже данное разрешение на согласованный набор сохраняется.
6. Не создавай `hard`, если v1 не умеет механически его обеспечить. Для наблюдения без решения создай inactive candidate через `harnest learn --id ... --statement ...`.
7. Запиши одобренные rules в `.harnest/rules/<id>.yaml`, custom checks — в `.harnest/checks/<id>.yaml`; при настроенных `rules.root`/`checks.root` используй их. Check хранится как executable + args без shell-интерпретации. Устанавливай `approved: true` только после явного одобрения именно этой версии check.
8. Для native hooks прочитай [references/native-hooks.md](references/native-hooks.md). Сверь целевую версию и официальный контракт по provider references основного скилла, не запуская его standalone-запись. Несовместимость генератора сообщи; не исправляй её ручной подменой generated handler. Подключай только выбранные IDs через `hooks.rules`; обычное создание правила не включает binding. Существующие Harnest handlers остаются под управлением генератора: не дублируй их standalone hooks и не редактируй команды вручную. Чужие hooks сохраняй.
9. Выполни `harnest generate <project> --dry-run`; после согласованной активации — `harnest generate <project>`, затем `harnest doctor <project>`. Для изменений проекта выполни `harnest verify --changed`. Не выдавай generation/doctor за реальный host smoke.

## Формат active rule

```yaml
id: protect-production
title: Production configuration is immutable
severity: hard
statement: Агенту запрещено изменять production-конфигурацию.
scope:
  paths: [deploy/**]
  operations: [change]
enforcement:
  - type: protect-path
    paths: [deploy/**]
source:
  type: operator-confirmed
  evidence: ["Решение владельца проекта о безусловном запрете изменений агентом"]
```

V1 не поддерживает переносимое разовое разрешение для `protect-path`. Требование «не изменять без разрешения» не превращай в безусловный запрет без решения пользователя; оставь approval штатным механизмам платформы либо согласуй безусловную формулировку.

Допустимые v1 enforcement: `protect-path`, `require-check`. `deny-command` отклоняется при validation для любой severity. Ограничения bindings, approvals, digest и native trust/reload определены в reference.
