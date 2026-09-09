# Native hooks Claude Code и Codex

Используй этот сценарий только для выбранных правил с механическим enforcement: `hard + protect-path` на `PreToolUse`, `required + protect-path` или `required + require-check` на `Stop`. Semantic rules и preferences остаются instruction/review. `hard + require-check` и `deny-command` в v1 не поддерживаются.

`hard + protect-path` дополнительно обнаруживает видимые изменения на `Stop` через Git. Это не preflight-защита произвольного shell/MCP.

## Выбор и предпросмотр

1. Прочитай `harnest.yaml`, выбранные rules/checks и только нужный evidence. Переиспользуй существующие rule/check.
2. Покажи таблицу до записи. Ниже пример формы: ID, пути и команды выбирай из правил и средств проверки целевого проекта; эти примеры не задают обязательный стек или структуру каталогов.

| Rule ID | Trigger | Scope и действие | Check | Claude Code | Codex | Ограничение |
|---|---|---|---|---|---|---|
| `protect-production` | `PreToolUse`: deny | `deploy/production/**`, change | `protect-path` | файловые tools | `apply_patch` | shell/MCP не блокируются до исполнения |
| `check-changed-files` | `Stop`: block при сбое | изменённые файлы выбранного scope | согласованный check проекта | требует native smoke | требует native smoke | CI остаётся quality gate |

3. Получи подтверждение выбранных ID. Уже данное явное разрешение действует для согласованного набора; повторно его не запрашивай. Для нового или изменённого executable-check отдельно покажи точные `command`, `args`, полный исходник вызываемого скрипта, `timeout_seconds`, cwd и побочные эффекты. Ставь `approved: true` только после явного одобрения именно этой версии. Не добавляй shell-интерпретацию.
4. Сохрани выбранные ID единым списком:

```yaml
hooks:
  rules:
    - protect-production
    - check-changed-files
```

Отсутствующий/пустой список ничего не активирует. Неизвестные ID, дубли и неподдерживаемая комбинация — ошибка; не активируй остальные правила автоматически.

## Генерация и evaluator

Сначала запусти `harnest generate <project> --dry-run`. Предпросмотр обязан показать точные native-файлы, события, project root, executable и команды handlers, ничего не записывая. После согласованной активации запускай `harnest generate <project>`.

Native handlers вызывают evaluator через stdin:

```text
harnest hook evaluate --platform <claude-code|codex> --event <pre-tool-use|stop> --project <absolute-root>
```

Для Stop с `require-check` генератор добавляет `--checks-digest <sha256>` в native command. Digest закрепляет определения всех выбранных checks, project cwd, разрешённые пути и содержимое executable, файлов из отдельных `args` и списка `sources`. Stop сравнивает digest до запуска; отсутствующее или устаревшее значение даёт `evaluation-error`. Не обновляй digest вручную: после одобрения изменённой версии выполни generation и повторный native trust/reload.

Файлы, переданные отдельными аргументами, считаются входными зависимостями: их изменение требует нового approval. Для косвенных зависимостей (например, script, вызываемый внутри `sh -c`, или импортируемый helper) перечисли пути явно в check YAML: `sources: [scripts/check.sh, scripts/helper.sh]`. Пути разрешаются от project cwd; отсутствующий explicit source блокирует генерацию и выполнение. Harnest не разбирает shell-код и дерево импортов.

Используй только `pre-tool-use` и `stop`. Не добавляй `--allow`: native hook не должен обходить разрешения хоста. Для одного project/platform/event нужен один Harnest handler; повторная генерация не создаёт дубли и сохраняет чужие hooks.

`protect-path` — безусловный запрет поддерживаемой операции. Формулировку «запрещено без разрешения» нельзя реализовать как portable per-operation approval в v1. Предложи пользователю либо безусловный запрет, либо штатный approval хоста. Выключение hooks не является разрешением одной операции.

## Scope и limits

Файл применим, только если он совпал одновременно с `scope.paths` и `enforcement.paths`, когда заданы оба набора. Не объединяй совпадения разных файлов. Пустые operations или `[change]` означают любое изменение; допустимы также `create`, `update`, `delete`, `move`. Для move проверяй обе стороны. Неизвестная operation отклоняет binding. `scope.domains` служит evidence/review и не является вычисляемым фильтром; domain-only rule к hook не подключай.

Check запускается точными argv из project cwd. `timeout_seconds` — положительное число, default 60 секунд. Общий evaluator budget — 110 секунд внутри 120-секундного native handler; combined check output ограничен 64 KiB. Timeout, output overflow, missing/unapproved check и неоднозначный `HARNEST_CHANGED_FILES` дают `evaluation-error`, не успех.

## Сопровождение bindings

- Добавление/удаление ID меняет только `hooks.rules` и производный native wiring; rule/check файлы сохраняются.
- Изменение executable, args, timeout, `sources` или исходника check делает сохранённый digest недействительным даже при `approved: true`. Снова покажи точные argv, полный source, timeout, cwd и side effects; после явного одобрения запусти `generate --dry-run`, generation и native trust/reload.
- Изменение scope или statement выбранного rule читается evaluator на следующем событии и не требует regeneration.
- Изменение выбранных IDs, событий, executable path или native command требует `generate --dry-run`, затем regeneration.
- Повторная генерация сохраняет чужие hooks и не создаёт дубли. Последний удалённый binding убирает только Harnest handlers.

## Диагностика и отключение

Запусти `harnest doctor <project>` после генерации. Различай четыре независимых факта:

- config сгенерирован и handler найден;
- rule/check валиден и approved;
- local hooks включены;
- native host загрузил/trust-нул definition и реально вызвал handler.

Config не доказывает trust, а trust не доказывает успешный host smoke. `doctor` сообщает `installed, execution not verified`, `requires trust/reload`, `disabled`, `not supported` или неизвестное состояние; не повышай неизвестное до verified.

Аварийный переключатель не удаляет wiring:

```text
harnest local set hooks.enabled false --dir <project>
harnest local show --dir <project>
harnest local unset hooks.enabled --dir <project>
```

Его эквивалент в `.harnest-local.yaml`:

```yaml
hooks:
  enabled: false
```

## Проверка результата

Работай во временном Git-проекте; не подключай тестовые hooks к рабочему репозиторию. Подготовь разрешённый и защищённый файлы, два правила (`hard + protect-path` и `required + require-check`), согласованный check с воспроизводимыми успешным и неуспешным результатами и чужой no-op native hook для проверки preservation. Выбирай файлы, scope и check под стек целевого проекта, используя уже доступные инструменты.

Минимальные evaluator checks:

1. Разрешённое изменение файла на `pre-tool-use` проходит.
2. Изменение защищённого файла блокируется до записи с ID выбранного правила.
3. Codex multi-file patch блокируется целиком, если один путь защищён.
4. Stop после изменения файла в scope правила запускает назначенный check; сбой возвращает rule ID, повторный Stop не создаёт бесконечное продолжение.
5. `hooks.enabled: false` даёт `disabled`, не `passed`.
6. Missing/unapproved check, timeout, output overflow и повреждённый input дают `evaluation-error`.

### Пять forward eval задач

Фиксируй созданные файлы, dry-run, approvals и doctor statuses; сравнивай observable artifacts, не формулировки ответа.

| № | Запрос | Ожидаемые IDs/артефакты | Approval | Итог |
|---|---|---|---|---|
| 1 | Подключить два существующих правила | `hooks.rules` содержит ровно оба ID; PreToolUse/Stop; чужие hooks сохранены | bindings уже выбраны | installed; execution `not-verified` до smoke |
| 2 | Создать check после изменения файлов выбранного scope | новый required rule/check, точные argv/source/timeout | check остаётся unapproved до явного решения | unapproved = `evaluation-error`; после approval pass/fail по check |
| 3 | Гарантировать «хорошую архитектуру» | binding/check не создаются | нет | semantic review, без mechanical `passed` |
| 4 | Добавить, затем убрать один ID | оставшийся ID один; rule/check удалённого binding сохранены; без дублей | новый approval только при изменении executable | doctor описывает только выбранный hook set |
| 5 | Проверить при недоступном trust | config path, executable, event/root диагностированы | trust/reload решает host | `installed, execution not verified` либо `requires trust/reload` |

### Реальный host smoke

Для каждого заявленного хоста:

1. Запиши точную host version и абсолютный путь fixture.
2. Выполни `generate`, затем `doctor`.
3. Открой fixture в host, выполни требуемые trust/reload.
4. Через поддерживаемый файловый tool создай разрешённый файл; проверь факт выполнения handler.
5. Тем же tool запроси защищённое изменение; проверь deny и неизменность файла.
6. Измени файл в scope правила с check и инициируй Stop; проверь check, максимум один feedback cycle и отсутствие loop.
7. Запиши отдельно config status, trust/reload и наблюдавшийся host invocation.

Прямой evaluator, config или журнал не считаются host smoke. Версии хостов, ОС, дату, проверенные executable/config и результаты записывай в отчёт текущего прогона проекта, не в reusable-инструкции. Совместимость подтверждай только для реально проверенной конфигурации; после её изменения или смены версии нужен новый smoke.

Ограничение текущей реализации Harnest: native installation на Windows явно отклоняется как unverified; не обещай `commandWindows` или Windows support.
