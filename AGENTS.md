# AGENTS.md (project-local)

Supplements the global AGENTS.md with rules specific to terraform-provider-neon.

## Project Context

**Contract**: Justin is contracted for Neon (Aug 2026 onward) to migrate terraform-provider-neon off the deprecated SDK v2 onto the Plugin Framework and to add three new resources: `neon_bucket`, `neon_functions`, `neon_triggers`. The repo is a fork of `neondatabase/terraform-provider-neon` at `randoneering/terraform-provider-neon`.

**Upstream tracking**: migration progress tracked in `neondatabase/terraform-provider-neon` issue #28.

## Team and Timezones

- Justin (contractor, owns neon_functions): Boise, UTC-7
- Dmitry (peer reviewer, previous provider maintainer): Berlin, UTC+1/+2
- Andre (gates merges, Neon): San Francisco, UTC-8

## Operating Model

- **Trunk-based development.** Short-lived feature branches, frequent merges to main.
- **Async.** Real-time overlap across the three timezones is minimal. Status updates go in openspec change artifacts and PR descriptions, not chat.
- **Peer review.** Dmitry reviews Justin's PRs and vice versa.
- **Merge gate.** Ping Andre when a PR is ready to merge. He has the final say.

## SDK Constraint

`github.com/kislerdm/neon-sdk-go` is the only Go SDK for Neon. New resources may require SDK additions. Coordinate with Dmitry (he is the upstream SDK maintainer) before adding raw HTTP calls in `internal/provider/`.

## Current Deadline

Two weeks from 2026-08-31 for `neon_functions`, `neon_bucket`, possibly `neon_triggers` go-live. Scope and cutting decisions belong to the team, not to assumptions.
