---
name: read-issue
description: Read a GitHub Issue as the authoritative task context by checking the issue body, all comments, explicit Finalized comments, and ADR Plan comments before acting.
---

# Read Issue Skill

Use this skill whenever you need to understand, review, implement, summarize, or update a GitHub Issue.

The Issue body alone is not sufficient. Always read the Issue body and the full comment thread before deciding what the Issue currently requires.

## Required Reading Order

1. Read the Issue title and body.
2. Read all comments in chronological order.
3. Check all comments for explicit control headings.
4. Identify whether exactly one comment contains a level 1 heading `Finalized`.
5. Identify whether one or more comments contain a level 1 heading `ADR Plan`.

## Control Headings

This skill uses explicit Markdown level 1 headings as control markers.

### Finalized comment

A comment is a **Finalized comment** only if it contains the following Markdown level 1 heading:

```md
# Finalized
```

Do not infer a Finalized comment from wording such as "final policy", "accepted", "OK", "approved", "確定", or "この方針で進める" unless the comment also contains the exact level 1 heading `# Finalized`.

### ADR Plan comment

A comment is an **ADR Plan comment** only if it contains the following Markdown level 1 heading:

```md
# ADR Plan
```

Do not infer an ADR Plan comment from wording such as "ADR案", "ADRとして記録する", or "ADR creation plan" unless the comment also contains the exact level 1 heading `# ADR Plan`.

## Authority Rules

### 1. Exactly one Finalized comment takes precedence

If exactly one Finalized comment exists, treat that comment as the authoritative final policy for the Issue.

The Finalized comment may override, narrow, or supersede the Issue body and earlier comments.

When acting on the Issue, follow the Finalized comment first, then use the Issue body and other comments only as supporting context.

### 2. Multiple Finalized comments are an error

If more than one Finalized comment exists, stop the task.

Do not choose the latest Finalized comment automatically.

Report that the Issue has multiple Finalized comments and request that the comments be revised so the Finalized comment can be identified unambiguously.

The user or maintainer should resolve this by leaving exactly one comment with the level 1 heading:

```md
# Finalized
```

### 3. If no Finalized comment exists, synthesize the body and all comments

If no Finalized comment exists, construct the working interpretation from:

* the Issue body,
* all comments,
* accepted suggestions,
* rejected suggestions,
* unresolved questions,
* and the latest state of the discussion.

Do not rely only on the Issue body.

Do not treat an early proposal as accepted unless a later comment accepts it or the surrounding discussion clearly implies acceptance.

Clearly distinguish explicit requirements from inferred conclusions.

### 4. ADR Plan comments guide ADR creation

If one or more ADR Plan comments exist, use them as the primary guide when creating or updating ADRs.

When multiple ADR Plan comments exist, integrate the contents of all ADR Plan comments.

Do not stop merely because multiple ADR Plan comments exist.

When integrating multiple ADR Plan comments:

* preserve each comment's intended scope,
* preserve each comment's proposed separation of concerns,
* preserve any stated file-splitting or heading-splitting policy,
* merge overlapping decisions only when they are substantively the same,
* keep distinct ADR topics separate when the comments distinguish them,
* do not add unrelated design decisions.

If ADR Plan comments conflict with each other, report the conflict and avoid silently choosing one side unless a Finalized comment resolves the conflict.

If an ADR Plan comment conflicts with the single Finalized comment, follow the Finalized comment and report the conflict.

## Interpretation Rules

Distinguish the following categories:

* **Directly stated requirement**: explicitly written in the Issue body or comments.
* **Finalized decision**: stated in the single Finalized comment.
* **Accepted decision**: a proposal later accepted by the user or maintainer.
* **Rejected decision**: a proposal explicitly rejected or scoped out.
* **Inference**: a conclusion derived from the body and comments but not explicitly stated.
* **Open question**: an unresolved ambiguity that still affects implementation, review, or ADR creation.

Do not present inferred content as if it were explicitly decided.

If an ambiguity blocks safe action, state the ambiguity clearly. If a reasonable best-effort action is possible, proceed with the conservative interpretation and note the assumption.

## Output Expectations

When reporting your understanding of an Issue, include:

1. Current effective scope.
2. Whether a Finalized comment exists.
3. Finalized decisions, if any.
4. Accepted decisions outside the Finalized comment, if relevant.
5. Rejected or superseded ideas.
6. ADR-relevant decisions.
7. Remaining ambiguities, if any.
8. Implementation or review implications.

When reviewing an Issue, check whether the Issue body reflects the latest effective understanding. If the body is outdated, recommend updating it.

When implementing an Issue, use the effective scope derived from the full Issue thread, not only the body.

When creating an ADR, check for ADR Plan comments first and follow them unless superseded by the single Finalized comment.

## Safety Checks

Before acting on an Issue, verify:

* The Issue body was read.
* Comments were checked through the latest available comment.
* All comments were checked for `# Finalized`.
* All comments were checked for `# ADR Plan`.
* There is not more than one Finalized comment.
* ADR Plan comments, if present, were identified and integrated.
* Accepted and rejected proposals were not mixed.
* The Issue body was not treated as authoritative when a Finalized comment changed the direction.
* Inferences were not presented as direct requirements.

## Required Stop Condition

Stop the task if more than one Finalized comment exists.

Use the following response shape:

```md
This Issue contains multiple `# Finalized` comments.

I cannot determine the authoritative final policy unambiguously.

Please revise the Issue comments so that exactly one comment contains the level 1 heading:

# Finalized
```

## Recommended Summary Format

```md
## Effective Issue Understanding

### Finalized Comment Status
...

### Current Scope
...

### Finalized Decisions
...

### Accepted Decisions Outside Finalized Comment
...

### Rejected / Superseded Ideas
...

### ADR-Relevant Decisions
...

### Remaining Ambiguities
...

### Action Implications
...
```
