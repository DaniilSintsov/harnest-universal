// Package ir defines the vendor-neutral input compiled by harness adapters.
package ir

import (
	"github.com/daniilsintsov/harnest-universal/internal/detector"
	"github.com/daniilsintsov/harnest-universal/internal/mapping"
	"github.com/daniilsintsov/harnest-universal/internal/rules"
)

type Project struct {
	Version      int
	Name         string
	Description  string
	Stacks       []detector.Stack
	Agents       mapping.AgentConfig
	Architecture Architecture
	Rules        ResourceIndex
	PolicyRules  []rules.Rule
	Skills       ResourceIndex
	Checks       ResourceIndex
	Workflow     Workflow
	Targets      []string
	Language     string
	Adapters     map[string]AdapterSettings
}

type Architecture struct {
	Index string
	State string
}

type ResourceIndex struct {
	Root  string
	Index string
}

type Workflow struct {
	Adaptive              bool
	DefaultProfile        string
	RoleSelection         string
	RequireAvailableRoles bool
	VerifyChanged         bool
}

const (
	RoleSelectionInteractive = "interactive"
	RoleSelectionAuto        = "auto"
)

type AdapterSettings struct {
	Models map[string]string
}

type Support string

const (
	Native      Support = "native"
	Fallback    Support = "fallback"
	Unsupported Support = "unsupported"
)

type Capabilities struct {
	Instructions Support
	ScopedRules  Support
	Skills       Support
	Agents       Support
	PreToolHook  Support
	PostToolHook Support
	Permissions  Support
	Verification Support
}
