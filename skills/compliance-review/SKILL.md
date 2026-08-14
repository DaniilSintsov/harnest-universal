---
name: compliance-review
description: Проверяет изменённые файлы и решение задачи против активных `.harnest/rules/*.yaml`, capability matrix и обязательных checks. Использовать для финального compliance-review, проверки hard/required/preference rules, подготовки evidence перед завершением code-changing задачи или разбора провала `harnest verify --changed`.
---

# Compliance Review

Дай доказуемый verdict без изменения кода, если исправление явно не запрошено.

## Workflow

1. Прочитай `harnest.yaml`, применимые scoped rules и релевантный architecture context.
2. Запусти `harnest doctor`. Не называй policy обеспеченной, если capability — fallback/unsupported или hard enforcement отсутствует.
3. Определи changed files и запусти `harnest verify --changed`.
4. Для semantic `required` и `preference` проверь diff вручную только в matching scope.
5. Если для scope назначена доступная роль, обязательно используй её. При отсутствии укажи main-agent fallback.
6. Отсортируй findings: hard failure, required violation, preference divergence. Для каждого укажи rule id, точный путь/строку, evidence и минимальное исправление.
7. Если findings нет, сообщи проверки и ограничения evidence. Не создавай отчётный каталог по умолчанию.

Не активируй новые rules во время review. Наблюдение можно сохранить только как inactive candidate после согласия пользователя.
