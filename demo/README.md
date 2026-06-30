# Conflict Observation Demo

<video src="https://github.com/user-attachments/assets/be7a25b4-ac29-45c8-a250-3453bd543875"></video>


This demo is designed to help you observe how merge conflicts are detected after parallel work by multiple agents using git-kura.

This directory is not a demo execution tool.
Users should clone this repository and, using the materials and instructions below, have two agents carry out work on their own local environment.

## Objectives

- To confirm that merge conflicts do not disappear even when using a Git worktree
- To observe conflicts that arise when multiple agents edit the same file or the same section of code
- To confirm that git-kura assists in detecting this state

## Overview of Procedure

1. Clone this repository
2. Use `git-kura` to set up two working trees or branches
3. Provide Agent A with the instructions in `prompts/agent-a.md`
4. Provide Agent B with the instructions in `prompts/agent-b.md`
5. Check the changes made by each agent
6. Check the conflict detection results from `git-kura`

## Observations

- Agent A and Agent B edit the same file, `sample.md`
- Both make changes to the same `Archive Policy` section
- During integration or a git-kura check, a conflict is detected over the same area of changes

## What this demo demonstrates

git worktree prevents the problem of multiple agents simultaneously overwriting the same working directory.
However, even when working on separate branches or worktrees, merge conflicts will still occur if the same target is edited.

git-kura observes the conflict state before and after merging, making it easier to manage the risk of conflicts in multi-agent development.

## Note

The output of AI agents is non-deterministic.
As this demo is designed to reproduce conflicts with a high degree of certainty, both sets of instructions are designed to edit the same section of `sample.md`.
However, depending on the agents used or how the prompts are interpreted, conflicts may not occur.
