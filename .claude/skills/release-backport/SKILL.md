---
name: release-backport
description: Use when asked to compare upstream main with a release branch, backport main changes, or create or update a release backport PR for Crane. Accepts an optional target branch and target build.
---

# Release backport

Create or update a focused backport PR from `upstream/main` to a Crane release branch.

## Inputs

Read the user's request for these values:

- `target branch`: an explicit branch such as `release-0.11`. If absent, select the `upstream/release-X.Y` branch with the largest numeric `X.Y` version. Compare version components numerically, not lexically.
- `target build`: an explicit build such as `v0.11.2`. If absent, inspect the milestones of candidate linked issues and use the newest relevant `vX.Y.Z` milestone for the target release line. State the inferred build in the PR title and description.
- `scope`: include only the commits or PRs the user names. If none are named, select source-only changes that are appropriate for the maintenance release. Prefer bug fixes, security fixes, regression tests, and issues targeting the chosen build. Exclude unrelated CI churn, documentation-only changes, and speculative E2E additions unless the user asks to include them.

Ask one concise question only when the selection cannot be determined from the branch, linked issues, milestones, and user request.

## Inspect

1. Fetch current remote refs with `git fetch upstream --prune`.
2. Confirm `upstream/main` and the target branch exist. Do not assume local remote-tracking refs are current.
3. Preserve all user changes. Inspect `git status --short`; never reset, checkout away, clean, or alter unrelated files.
4. Compare branches using:
   - `git merge-base upstream/main upstream/<target>`
   - `git rev-list --left-right --count upstream/<target>...upstream/main`
   - `git log --left-right --cherry-mark --oneline upstream/<target>...upstream/main`
   - `git log --cherry-pick --right-only --no-merges --format='%h %s' upstream/<target>...upstream/main`
   - `git diff --stat upstream/<target>...upstream/main`
5. Treat commits marked `=` by `--cherry-mark` as already backported. Do not duplicate them.
6. For every selected source PR, retrieve its full title and linked issues with `gh pr view <number> --repo migtools/crane --json number,title,url,closingIssuesReferences`. Retrieve every linked issue's milestone with `gh issue view <number> --repo migtools/crane --json number,title,url,milestone`.

## Select and apply

1. Report the divergence and selected PRs before making substantive edits.
2. Search for an existing open backport PR against the target branch. If one owns the same backport scope, update its source branch instead of creating another PR.
3. Otherwise, create a branch from the current target branch. Use a descriptive name such as `backport/<target>-main-changes`.
4. Cherry-pick whole merged PR commits with `git cherry-pick -m 1 <merge-commit>`. Keep dependent PRs in chronological order.
5. Resolve conflicts minimally. Retain target-branch-specific behavior where it differs intentionally. Do not drop tests or source-only behavior merely to make the cherry-pick apply.
6. Add a focused conventional commit if a conflict resolution cannot remain part of the cherry-pick. Do not amend existing commits unless the user explicitly asks.
7. Run `gofmt` on modified Go files.

## Verify

1. Run `git diff --check` and inspect the complete diff and commit list against `upstream/<target>`.
2. Run unit tests for every changed production package and its directly affected test package.
3. Do not run E2E tests unless the user explicitly asks. State this clearly in the PR test plan.
4. Do not proceed past a failed unit test. Fix the backport or report the blocker.

## Pull request

1. Push the branch to `origin` and create or update a PR in `migtools/crane` with base `<target>`.
2. Use this exact title format: `:seedling: Backport selected main changes for <target-build>`.
3. Write the PR body in English. It must contain these sections:
   - `## Summary`
   - `## Included upstream PRs`
   - `## Impact`
   - `## Test Plan`
4. In `Included upstream PRs`, include a Markdown table with columns `Upstream PR`, `Related issue`, and `Issue target`. The `Upstream PR` cell must link to the upstream PR and include its complete title, for example `[#1074: :sparkles: Report transform plugins origin and run mode](...)`. The issue cell must link every related issue. Write `None linked` or `N/A` when applicable. State `No milestone` for linked issues without a milestone.
5. Describe release-specific conflict resolutions in the summary.
6. Do not add GitHub comments unless the user explicitly asks.
7. Return the PR URL, included PR numbers, selected target branch and build, and exact tests run.

## Constraints

- A fast-forward is only possible when the target branch is an ancestor of `main`. Release branches normally diverge, so use selective cherry-picks rather than merging all of `main`.
- Use `gh` for GitHub reads and writes. Pass all `gh` text payloads in single quotes, or put longer payloads in a temporary file and use `--body-file`.
- Never include untracked user files in commits.
