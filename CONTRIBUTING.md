# Contributing to git-kura

Thank you for your interest in git-kura.

git-kura is currently maintained as an owner-led project. At this stage, the project does **not** accept unsolicited pull requests from non-collaborators.

The preferred way to contribute is to open an issue with a question, bug report, feature request, or specification change proposal. The owner will review the issue, decide whether and how it should be addressed, and make the corresponding changes when appropriate.

## Contribution model

git-kura currently follows this contribution model:

1. Users and interested parties open issues.
2. The owner reviews questions, reports, and proposals.
3. The owner decides whether the change fits the project scope.
4. The owner implements accepted changes or creates follow-up issues as needed.

This policy is intended to keep the project direction coherent while git-kura is still evolving.

## What kinds of issues are welcome?

The following types of issues are welcome:

* **Questions**: ask about current behavior, intended usage, design direction, or implementation assumptions.
* **Bug reports**: report behavior that appears incorrect, unsafe, inconsistent, or different from the documented behavior.
* **Feature requests**: propose new commands, options, output formats, integrations, or workflows.
* **Specification change proposals**: propose changes to existing behavior, documented semantics, compatibility policy, or design direction.

Please use the appropriate issue template when opening an issue.

## Before opening an issue

Before opening an issue, please check:

* whether the behavior is already described in the README or documentation;
* whether a related issue already exists;
* whether the question is about git-kura itself rather than Git, GitHub, an AI coding agent, or a package manager integration.

It is fine to open a question issue when the current behavior or design intent is unclear.

## Pull requests

Unsolicited pull requests from non-collaborators are not currently accepted.

Please open an issue first instead of opening a pull request. If the proposal is accepted, the owner will decide how to implement it. In some cases, the owner may invite a contributor to prepare a pull request, but this should not be assumed by default.

Pull requests from collaborators should normally be linked to an issue and should preserve the project’s documented design direction.

## Project scope

git-kura is a conflict-aware keyed worktree coordinator for Git.

It focuses on:

* deterministic worktree and branch resolution from stable keys;
* repository-local worktree metadata;
* cooperative path seals for early conflict detection;
* conservative safety behavior;
* script-friendly and agent-friendly output.

git-kura should remain intentionally small. It is not intended to become:

* a general Git client;
* an AI session manager;
* a pull request management tool;
* an issue tracker client;
* a project management system;
* a general-purpose conflict resolver.

Issues that would substantially expand git-kura beyond this scope may be closed as not planned.

## Documentation

Documentation updates are normally handled by the owner as part of the accepted change.

If an issue identifies unclear or inconsistent documentation, please describe:

* which document or section is unclear;
* what interpretation seems possible;
* why the ambiguity matters;
* what behavior or policy should be clarified.

## License

git-kura is licensed under the Apache License 2.0.

By contributing issue text, comments, suggestions, or invited code changes, you agree that your contribution may be used in the project under the same license, unless explicitly stated otherwise.
