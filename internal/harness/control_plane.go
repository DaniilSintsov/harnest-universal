package harness

import (
	"fmt"
	"strings"

	"github.com/daniilsintsov/harnest-universal/internal/ir"
	"github.com/daniilsintsov/harnest-universal/internal/mapping"
)

func hasAssignedConsilium(agents mapping.AgentConfig) bool {
	for _, role := range agents.Consilium {
		if role.Agent != "" {
			return true
		}
	}
	return false
}

func hasAssignedExec(agents mapping.AgentConfig) bool {
	for _, agent := range agents.Exec {
		if agent.Agent != "" {
			return true
		}
	}
	return false
}

func renderControlPlane(project ir.Project) string {
	var b strings.Builder
	b.WriteString("## Harnest workflow\n\n")
	profile := project.Workflow.DefaultProfile
	if profile == "" {
		profile = "business-feature"
	}
	if project.Language == "en" {
		if project.Workflow.Adaptive {
			b.WriteString("- Adaptive workflow is explicitly enabled: trivial tasks may go directly; substantive tasks select the smallest relevant profile.\n")
		} else {
			b.WriteString("- Strict workflow: before any project change, complete Research -> Plan -> Executing in order.\n")
			b.WriteString("- Scale stage depth to the task, but never skip Research or Plan before execution.\n")
		}
		renderProfileSelection(&b, profile, project.Language)
		b.WriteString("- Agent value `auto` means select a compatible available agent for that role; if unavailable, continue in the main agent and state the fallback.\n")
		renderAutomaticRoleSelection(&b, project.Language)
		b.WriteString("- Never deploy to production unless the user explicitly requests it and project policy permits it.\n")
	} else {
		if project.Workflow.Adaptive {
			b.WriteString("- Адаптивный workflow явно включён: тривиальные задачи можно выполнять напрямую; для существенных выбирай минимальный подходящий профиль.\n")
		} else {
			b.WriteString("- Строгий workflow: перед любым изменением проекта выполни Research -> Plan -> Executing по порядку.\n")
			b.WriteString("- Масштаб стадий зависит от задачи, но Research и Plan перед выполнением не пропускай.\n")
		}
		renderProfileSelection(&b, profile, project.Language)
		b.WriteString("- Значение агента `auto` означает: выбери совместимого доступного агента для роли; если такого нет, продолжай основным агентом и явно укажи fallback.\n")
		renderAutomaticRoleSelection(&b, project.Language)
		b.WriteString("- Не деплой production без явного запроса пользователя и разрешения правил проекта.\n")
	}
	if project.Architecture.Index != "" {
		if project.Language == "en" {
			b.WriteString(fmt.Sprintf("- If architecture entrypoint `%s` exists, load only task-relevant documents.\n", project.Architecture.Index))
		} else {
			b.WriteString(fmt.Sprintf("- Если существует точка входа архитектуры `%s`, загружай только документы, релевантные задаче.\n", project.Architecture.Index))
		}
	}
	if project.Skills.Root != "" {
		if project.Language == "en" {
			b.WriteString(fmt.Sprintf("- If skills root `%s` exists, follow matching skill instructions.\n", project.Skills.Root))
		} else {
			b.WriteString(fmt.Sprintf("- Если существует каталог skills `%s`, следуй инструкциям подходящего skill.\n", project.Skills.Root))
		}
	}
	if project.Rules.Root != "" {
		if project.Language == "en" {
			b.WriteString(fmt.Sprintf("- If rules root `%s` exists, apply only rules matching current scope.\n", project.Rules.Root))
		} else {
			b.WriteString(fmt.Sprintf("- Если существует каталог rules `%s`, применяй только правила текущего scope.\n", project.Rules.Root))
		}
	}
	if project.Workflow.VerifyChanged {
		if project.Language == "en" {
			b.WriteString("- Before finishing a code-changing task, run `harnest verify --changed`. Read-only research does not require it.\n")
		} else {
			b.WriteString("- Перед завершением code-changing задачи запусти `harnest verify --changed`. Для read-only research проверка не нужна.\n")
		}
	}
	if len(project.PolicyRules) > 0 {
		b.WriteString("\n### Active project rules\n")
		for _, rule := range project.PolicyRules {
			var scopeParts []string
			if len(rule.Scope.Paths) > 0 {
				scopeParts = append(scopeParts, "paths: "+strings.Join(rule.Scope.Paths, ", "))
			}
			if len(rule.Scope.Domains) > 0 {
				scopeParts = append(scopeParts, "domains: "+strings.Join(rule.Scope.Domains, ", "))
			}
			if len(rule.Scope.Operations) > 0 {
				scopeParts = append(scopeParts, "operations: "+strings.Join(rule.Scope.Operations, ", "))
			}
			scope := ""
			if len(scopeParts) > 0 {
				scope = " (" + strings.Join(scopeParts, "; ") + ")"
			}
			b.WriteString(fmt.Sprintf("- [%s] `%s`%s: %s\n", rule.Severity, rule.ID, scope, rule.Statement))
		}
	}
	b.WriteString("\n")
	return b.String()
}

func renderProfileSelection(b *strings.Builder, profile, language string) {
	if language == "en" {
		b.WriteString("- Before every new task and before Research, select a workflow profile interactively. If the user explicitly names one, treat it as selected. Otherwise match the request against profile Meta and ask the user to choose the best match (recommended) or up to two alternatives. Do not start Research before the choice.\n")
		b.WriteString(fmt.Sprintf("- Use `%s` as the recommended fallback only when profile Meta has no clear match. Load the full text of the selected profile only.\n", profile))
		return
	}
	b.WriteString("- Перед каждой новой задачей и до Research выбери workflow-профиль интерактивно. Если пользователь явно назвал профиль, считай выбор сделанным. Иначе сопоставь запрос с Meta профилей и попроси выбрать лучший match (recommended) либо одну из максимум двух альтернатив. Не начинай Research до выбора.\n")
	b.WriteString(fmt.Sprintf("- `%s` используй как рекомендуемый fallback только когда Meta не даёт точного match. Полностью загружай только выбранный профиль.\n", profile))
}

func renderAutomaticRoleSelection(b *strings.Builder, language string) {
	if language == "en" {
		b.WriteString("- Determine primary roles automatically from the request and selected profile; do not ask the user to choose roles.\n")
		return
	}
	b.WriteString("- Primary roles определяй автоматически по запросу и выбранному профилю; пользователя не спрашивай.\n")
}
