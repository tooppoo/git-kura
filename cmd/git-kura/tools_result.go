package main

// toolsAction is the common action enum shared by the status, install, and
// uninstall commands. The enum is shared, but each command only ever emits a
// subset of the actions (see actionValidFor): install never reports
// not-installed or installed, status never reports created/updated/removed,
// and uninstall never reports created/updated/installed.
type toolsAction string

const (
	actionInstalled    toolsAction = "installed"
	actionCreated      toolsAction = "created"
	actionUpdated      toolsAction = "updated"
	actionSkipped      toolsAction = "skipped"
	actionRemoved      toolsAction = "removed"
	actionFailed       toolsAction = "failed"
	actionNotInstalled toolsAction = "not-installed"
)

// toolsCommand identifies which user-facing command produced a result, so the
// orchestrator can verify that a component only emits actions valid for that
// command.
type toolsCommand string

const (
	toolsCmdStatus    toolsCommand = "status"
	toolsCmdInstall   toolsCommand = "install"
	toolsCmdUninstall toolsCommand = "uninstall"
)

// actionsByCommand limits which actions may appear for each command, matching
// the command-specific action semantics in the framework spec. Any other
// action returned by a component is an internal contract violation.
var actionsByCommand = map[toolsCommand]map[toolsAction]bool{
	toolsCmdInstall: {
		actionCreated: true,
		actionUpdated: true,
		actionSkipped: true,
		actionFailed:  true,
	},
	toolsCmdUninstall: {
		actionRemoved:      true,
		actionNotInstalled: true,
		actionSkipped:      true,
		actionFailed:       true,
	},
	toolsCmdStatus: {
		actionInstalled:    true,
		actionNotInstalled: true,
		actionSkipped:      true,
		actionFailed:       true,
	},
}

// actionValidFor reports whether action is one a component may emit for cmd.
func actionValidFor(cmd toolsCommand, action toolsAction) bool {
	return actionsByCommand[cmd][action]
}

// toolsResult is the common result model returned by every component operation
// and rendered by every command. The same shape is reused across status,
// install, and uninstall so output and (future) machine formats stay uniform.
type toolsResult struct {
	Component      string
	ReleaseVersion string
	SourceAsset    string
	Destination    string
	Action         toolsAction
	Managed        bool
	Reason         string
}

// isFailure reports whether the result counts toward a non-zero command exit.
// skipped and not-installed are explicitly not failures.
func (r toolsResult) isFailure() bool {
	return r.Action == actionFailed
}
