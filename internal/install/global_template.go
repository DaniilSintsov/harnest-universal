package install

import (
	"fmt"
	"path/filepath"
)

type templateTarget struct {
	name        string
	projectFile string
}

func globalTemplateFor(harnessName, globalDir string) string {
	target := templateTarget{name: harnessName, projectFile: "AGENTS.md"}
	switch harnessName {
	case "claude-code":
		target = templateTarget{name: "Claude Code", projectFile: "CLAUDE.md"}
	case "codex":
		target = templateTarget{name: "Codex", projectFile: "AGENTS.md"}
	}

	return fmt.Sprintf(`# Harnest workflow for %s

## Общие настройки

- Язык общения по умолчанию: русский. Явная настройка проекта или пользователя имеет приоритет.
- Harnest workflow используется по умолчанию, но масштаб процесса соответствует задаче.
- Нельзя выполнять production deploy без явного запроса пользователя или project policy.

## Выбор workflow

1. Перед каждой новой задачей и до Research выбрать workflow-профиль интерактивно.
2. Если пользователь явно назвал профиль, считать выбор сделанным. Иначе сопоставить запрос с Meta профилей и попросить выбрать лучший match (recommended) либо одну из максимум двух альтернатив. Не начинать Research до выбора.
3. Project-local workflow.default_profile, либо business-feature при его отсутствии, использовать как рекомендацию только когда Meta не даёт точного match.
4. Прочитать полностью только выбранный профиль; для выбора читать только Meta остальных профилей.
5. Для любой code-changing задачи сначала выполнить Research, затем Plan, затем Executing. Стадии не пропускать; для мелкой задачи сокращать их глубину.
6. Значение агента auto означает выбор совместимого доступного агента текущей платформы; если такого нет, стадию выполняет основной агент с явным fallback.

Профили: %s

## Проектный контекст

1. Прочитать %s и project-local Harnest routing.
2. Если существует harnest.yaml, загружать только релевантные architecture docs и scoped rules.
3. Если глубокий bootstrap не выполнен, не запускать его автоматически; предложить skill harnest-bootstrap.

## Роли

- Primary roles определять автоматически по запросу и выбранному профилю; пользователя не спрашивать.
- Если профиль назначает роль и совместимый агент доступен, использовать роль обязательно.
- Если роль недоступна, выполнить стадию главным агентом и явно сообщить fallback.
- Останавливать workflow только когда стадия помечена isolation: required.
- Не использовать агента другой платформы без portable declaration.

## Validation

- Проверять сначала локально или в явно разрешённом test/staging environment.
- После изменения кода запускать harnest verify --changed.
- Не создавать swarm-report по умолчанию. Артефакт нужен только по требованию профиля, для долгой задачи или при ошибке validation.
`, target.name, filepath.Join(globalDir, "profiles"), target.projectFile)
}
