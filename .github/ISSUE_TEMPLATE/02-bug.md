name: Bug report
description: Report incorrect behavior, unexpected behavior, or a regression in git-kura
title: "[bug]"
labels: ["bug"]
body:
  - type: markdown
    attributes:
      value: |
        Use this form to report a bug in git-kura.
        Please separate reproduction steps, expected behavior, and actual behavior.

  - type: textarea
    id: summary
    attributes:
      label: Summary
      description: Briefly describe what is wrong.
      placeholder: |
        Example:
        `git kura review` selects the wrong worktree under specific conditions.
    validations:
      required: true

  - type: textarea
    id: steps
    attributes:
      label: Steps to reproduce
      description: Provide the smallest practical set of steps that reproduces the issue.
      placeholder: |
        1. ...
        2. ...
        3. ...
    validations:
      required: true

  - type: textarea
    id: expected
    attributes:
      label: Expected behavior
      description: Describe what should have happened.
    validations:
      required: true

  - type: textarea
    id: actual
    attributes:
      label: Actual behavior
      description: Describe what actually happened.
    validations:
      required: true

  - type: input
    id: version
    attributes:
      label: git-kura version
      placeholder: "Example: v0.0.6 / main branch commit SHA / unknown"
    validations:
      required: false

  - type: input
    id: os
    attributes:
      label: OS / environment
      placeholder: "Example: Windows 11, Ubuntu on WSL2, macOS"
    validations:
      required: false

  - type: input
    id: install-method
    attributes:
      label: Installation method
      placeholder: "Example: curl installer, GitHub Release archive, scoop"
    validations:
      required: false

  - type: textarea
    id: command
    attributes:
      label: Command
      description: Paste the exact command you ran.
      render: shell
    validations:
      required: false

  - type: textarea
    id: logs
    attributes:
      label: Logs and error messages
      description: Paste stdout, stderr, stack traces, or GitHub Actions logs if available.
      render: shell
    validations:
      required: false

  - type: dropdown
    id: regression
    attributes:
      label: Regression status
      description: If this used to work, mark it as a regression.
      options:
        - Unknown
        - Not a regression
        - Possible regression
        - Confirmed regression
    validations:
      required: false

  - type: textarea
    id: impact
    attributes:
      label: Impact
      description: Describe the impact, such as unusable commands, false positives, output inconsistency, or package manager impact.
    validations:
      required: false

  - type: textarea
    id: workaround
    attributes:
      label: Workaround
      description: Describe any known workaround.
    validations:
      required: false

  - type: textarea
    id: acceptance-criteria
    attributes:
      label: Acceptance criteria for the fix
      description: List the conditions required to consider the bug fixed. Leave blank if unknown.
      placeholder: |
        - [ ] The reproduction case no longer fails
        - [ ] A regression test is added
        - [ ] Documentation is updated if needed
    validations:
      required: false
