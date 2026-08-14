// Package yaml defines the schema for harnest.yaml — the declarative
// configuration file that serves as the source of truth for the Harnest CLI.
package yaml

// HarnestConfig is the top-level structure of a harnest.yaml file.
type HarnestConfig struct {
	Version      int                        `yaml:"version"`
	Project      ProjectInfo                `yaml:"project,omitempty"`
	Stacks       []StackEntry               `yaml:"stacks,omitempty"`
	Context      ContextBlock               `yaml:"context,omitempty"`
	Rules        ResourceBlock              `yaml:"rules,omitempty"`
	Skills       ResourceBlock              `yaml:"skills,omitempty"`
	Checks       ResourceBlock              `yaml:"checks,omitempty"`
	Workflow     WorkflowBlock              `yaml:"workflow,omitempty"`
	Agents       AgentsBlock                `yaml:"agents"`
	Adapters     map[string]AdapterSettings `yaml:"adapters,omitempty"`
	Harnesses    []string                   `yaml:"harnesses"`
	DesignSystem string                     `yaml:"design_system,omitempty"`
	Profiles     ProfilesBlock              `yaml:"profiles,omitempty"`
	Settings     SettingsBlock              `yaml:"settings,omitempty"`
}

// ProjectInfo holds optional human-readable metadata about the project.
type ProjectInfo struct {
	Name        string `yaml:"name,omitempty"`
	Description string `yaml:"description,omitempty"`
}

// StackEntry represents a single detected or manually specified technology stack.
type StackEntry struct {
	Name     string `yaml:"name"`
	Lang     string `yaml:"lang"`
	Category string `yaml:"category"`
	Path     string `yaml:"path"`
}

type ContextBlock struct {
	Architecture ArchitectureBlock `yaml:"architecture,omitempty"`
}

type ArchitectureBlock struct {
	Index string `yaml:"index,omitempty"`
	State string `yaml:"state,omitempty"`
}

type ResourceBlock struct {
	Root  string `yaml:"root,omitempty"`
	Index string `yaml:"index,omitempty"`
}

type WorkflowBlock struct {
	Adaptive       bool   `yaml:"adaptive,omitempty"`
	DefaultProfile string `yaml:"default_profile,omitempty"`
	// RoleSelection is retained for schema v2 compatibility. Interactive is a deprecated alias for auto.
	RoleSelection         string `yaml:"role_selection,omitempty"`
	RequireAvailableRoles bool   `yaml:"require_available_roles,omitempty"`
	VerifyChanged         bool   `yaml:"verify_changed,omitempty"`
}

type AdapterSettings struct {
	Models map[string]string `yaml:"models,omitempty"`
}

// AgentsBlock configures both consilium (advisory) and executing agents.
type AgentsBlock struct {
	Consilium map[string]string `yaml:"consilium"` // role -> agent name
	Executing []ExecEntry       `yaml:"executing"`
	Models    map[string]string `yaml:"models,omitempty"` // role -> capability tier (high/medium/low)
}

// ExecEntry maps a specific agent to a file glob scope for executing tasks.
type ExecEntry struct {
	Agent string `yaml:"agent"`
	Scope string `yaml:"scope"`
}

// ProfilesBlock controls which profiles are active and allows custom profile definitions.
type ProfilesBlock struct {
	Enabled []string        `yaml:"enabled,omitempty"`
	Custom  []CustomProfile `yaml:"custom,omitempty"`
}

// CustomProfile references a user-defined profile by name and file path.
type CustomProfile struct {
	Name string `yaml:"name"`
	File string `yaml:"file"`
}

// SettingsBlock holds global behavioral settings for the Harnest CLI.
type SettingsBlock struct {
	// AutoDetect enables automatic stack detection when stacks are empty or unset.
	AutoDetect bool `yaml:"auto_detect,omitempty"`
	// StackStrategy controls how detected stacks are merged with stacks declared in config.
	// Valid values: "replace" (default) or "merge".
	StackStrategy string `yaml:"stack_strategy,omitempty"`
	// LockFile enables writing a harnest.lock file after generation.
	LockFile bool `yaml:"lock_file,omitempty"`
	// LocalDefault keeps generated harness artifacts out of version control unless overridden.
	LocalDefault bool `yaml:"local_default,omitempty"`
	// Language is the default response language used by generated instructions.
	Language string `yaml:"language,omitempty"`
}
