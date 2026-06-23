package tools

// Action is the outcome of a component operation.
type Action string

const (
	ActionInstalled    Action = "installed"
	ActionCreated      Action = "created"
	ActionUpdated      Action = "updated"
	ActionSkipped      Action = "skipped"
	ActionRemoved      Action = "removed"
	ActionFailed       Action = "failed"
	ActionNotInstalled Action = "not-installed"
)

// Command identifies which user-facing command produced a result.
type Command string

const (
	CmdStatus    Command = "status"
	CmdInstall   Command = "install"
	CmdUninstall Command = "uninstall"
)

// actionsByCommand limits which actions may appear for each command.
var actionsByCommand = map[Command]map[Action]bool{
	CmdInstall: {
		ActionCreated: true,
		ActionUpdated: true,
		ActionSkipped: true,
		ActionFailed:  true,
	},
	CmdUninstall: {
		ActionRemoved:      true,
		ActionNotInstalled: true,
		ActionSkipped:      true,
		ActionFailed:       true,
	},
	CmdStatus: {
		ActionInstalled:    true,
		ActionNotInstalled: true,
		ActionSkipped:      true,
		ActionFailed:       true,
	},
}

func actionValidFor(cmd Command, action Action) bool {
	return actionsByCommand[cmd][action]
}

// Result is the common result model returned by every component operation.
type Result struct {
	Component      string
	ReleaseVersion string
	SourceAsset    string
	Destination    string
	Action         Action
	Managed        bool
	Reason         string
}

// IsFailure reports whether the result counts toward a non-zero command exit.
func (r Result) IsFailure() bool {
	return r.Action == ActionFailed
}
