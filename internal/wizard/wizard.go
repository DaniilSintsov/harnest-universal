package wizard

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	agents_pkg "github.com/daniilsintsov/harnest-universal/internal/agents"
	"github.com/daniilsintsov/harnest-universal/internal/mapping"
)

// Run lets the user accept, skip, search, or replace each suggested agent.
func Run(in io.Reader, out io.Writer, structure mapping.AgentStructure, suggestions mapping.Suggestions, discovered []string) (mapping.AgentConfig, error) {
	reader := bufio.NewReader(in)
	agents, known := uniqueAgents(discovered)
	config := mapping.AgentConfig{Models: copyMap(suggestions.ModelTiers)}

	fmt.Fprintf(out, "\n── Agent Wizard ──\nFound %d compatible agents\nEnter = accept suggestion, s = skip, ? = search\n", len(agents))

	for _, role := range structure.Roles {
		agent, err := promptAgent(reader, out, "Consilium: "+role, suggestions.Consilium[role], agents, known)
		if err != nil {
			return mapping.AgentConfig{}, err
		}
		if agent != "" {
			config.Consilium = append(config.Consilium, mapping.ConsiliumRole{Role: role, Agent: agent})
		}
	}

	for _, scope := range structure.ExecScopes {
		agent, err := promptAgent(reader, out, "Exec: "+scope.Scope, suggestions.Exec[scope.StackName], agents, known)
		if err != nil {
			return mapping.AgentConfig{}, err
		}
		if agent != "" {
			config.Exec = append(config.Exec, mapping.ExecAgent{Agent: agent, Scope: scope.Scope})
		}
	}

	return config, nil
}

func promptAgent(reader *bufio.Reader, out io.Writer, label, suggestion string, agents []string, known map[string]bool) (string, error) {
	display := suggestion
	if display == "" {
		display = "(none)"
	}
	fmt.Fprintf(out, "\n[%s]\n  Suggestion: %s\n", label, display)

	for {
		fmt.Fprint(out, "  Enter=accept, s=skip, ?=search: ")
		answer, err := readLine(reader)
		if err != nil {
			return "", err
		}

		switch strings.ToLower(answer) {
		case "":
			return suggestion, nil
		case "s":
			return "", nil
		case "?":
			fmt.Fprint(out, "  Search: ")
			query, err := readLine(reader)
			if err != nil {
				return "", err
			}
			matches := agents_pkg.Search(agents, query)
			if len(matches) == 0 {
				fmt.Fprintln(out, "  No matching agents.")
			} else {
				for _, match := range matches {
					fmt.Fprintln(out, "  - "+match)
				}
			}
			continue
		}

		if known[answer] || answer == suggestion {
			return answer, nil
		}
		fmt.Fprintf(out, "  Agent %q not found locally. Use anyway? [y/N]: ", answer)
		confirm, err := readLine(reader)
		if err != nil {
			return "", err
		}
		if strings.EqualFold(confirm, "y") || strings.EqualFold(confirm, "yes") {
			return answer, nil
		}
	}
}

func readLine(reader *bufio.Reader) (string, error) {
	line, err := reader.ReadString('\n')
	if err != nil && !(errors.Is(err, io.EOF) && line != "") {
		return "", fmt.Errorf("read interactive input: %w", err)
	}
	return strings.TrimSpace(line), nil
}

func uniqueAgents(discovered []string) ([]string, map[string]bool) {
	known := make(map[string]bool, len(discovered))
	for _, agent := range discovered {
		if agent != "" {
			known[agent] = true
		}
	}
	agents := make([]string, 0, len(known))
	for agent := range known {
		agents = append(agents, agent)
	}
	sort.Strings(agents)
	return agents, known
}

func copyMap(source map[string]string) map[string]string {
	result := make(map[string]string, len(source))
	for key, value := range source {
		result[key] = value
	}
	return result
}
