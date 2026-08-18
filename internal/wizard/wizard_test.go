package wizard

import (
	"bytes"
	"strings"
	"testing"

	"github.com/daniilsintsov/harnest-universal/internal/mapping"
)

func TestRun(t *testing.T) {
	structure := mapping.AgentStructure{
		Roles: []string{"architect", "security"},
		ExecScopes: []mapping.ExecScope{{
			StackName: "go",
			Scope:     "**/*.go",
		}},
	}
	suggestions := mapping.Suggestions{
		Consilium:  map[string]string{"architect": "arch-agent", "security": "sec-agent"},
		Exec:       map[string]string{"go": "go-agent"},
		ModelTiers: map[string]string{"architect": "high"},
	}
	input := strings.NewReader("\n?\nsec\ns\ncustom\nn\ncustom\ny\n")
	var output bytes.Buffer

	got, err := Run(input, &output, structure, suggestions, []string{"sec-agent", "arch-agent", "go-agent"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Consilium) != 1 || got.Consilium[0].Role != "architect" || got.Consilium[0].Agent != "arch-agent" {
		t.Fatalf("consilium = %#v", got.Consilium)
	}
	if len(got.Exec) != 1 || got.Exec[0].Agent != "custom" || got.Exec[0].Scope != "**/*.go" {
		t.Fatalf("exec = %#v", got.Exec)
	}
	if !strings.Contains(output.String(), "- sec-agent") || !strings.Contains(output.String(), `Agent "custom" not found locally`) {
		t.Fatalf("wizard output missing search or confirmation:\n%s", output.String())
	}
}
