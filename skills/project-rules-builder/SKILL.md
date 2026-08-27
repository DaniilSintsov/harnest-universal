---
name: project-rules-builder
description: Анализирует репозиторий и предлагает доказуемые scoped rules Harnest в `.harnest/rules/*.yaml`. Использовать при bootstrap, создании или пересмотре project rules, классификации ограничений как hard/required/preference, проектировании mechanical enforcement либо разборе повторяющихся review-требований.
---

# Project Rules Builder

Предлагай минимальный набор правил, который подтверждён кодом, документацией, тестами или ответом оператора.

## Workflow

1. Прочитай `harnest.yaml`, architecture index, root instructions и только релевантные исходники.
2. Собери кандидатов с evidence: путь, символ, тест, config или operator-confirmed ответ.
3. Удали дубли и общие советы, которые модель уже знает.
4. Для каждого кандидата задай scope: `paths`, `domains`, `operations`. Пустой scope означает весь проект и требует сильного обоснования.
5. Классифицируй:
   - `hard` — нарушение должно механически блокироваться;
   - `required` — обязательно для агента, но может требовать semantic review;
   - `preference` — локальный стиль или желательное решение.
6. Сначала покажи таблицу кандидатов: id, severity, scope, evidence, enforcement. Активируй только подтверждённые пользователем.
7. Не создавай `hard`, если v1 не умеет механически его обеспечить. Проверь через `harnest doctor`.
8. Для наблюдения без решения создай inactive candidate через `harnest learn --id ... --statement ...`.

## Формат active rule

```yaml
id: protect-production
title: Production changes require approval
severity: hard
statement: Не изменять production-конфигурацию без явного разрешения пользователя.
scope:
  paths: [deploy/**]
  operations: [change]
enforcement:
  - type: protect-path
    paths: [deploy/**]
source:
  type: operator-confirmed
  evidence: ["2026-08-07: user decision"]
```

Допустимые v1 enforcement: `protect-path`, `require-check`. `deny-command` не поддерживается и отклоняется при validation для любой severity.

Custom check хранить в `.harnest/checks/<id>.yaml` как executable + args без shell-интерпретации. Устанавливать `approved: true` только после явного одобрения пользователя.
