package tools

// ProductionRegistry is the registry exposed to users. It recognizes the
// three component IDs the framework ships with.
func ProductionRegistry() (*Registry, error) {
	return NewRegistry(
		PreCommitComponent{},
		NewClaudeSkillComponent(),
		NewCodexSkillComponent(),
	)
}
