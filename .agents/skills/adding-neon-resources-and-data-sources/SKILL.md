---
name: adding-neon-resources-and-data-sources
description: Use when adding a new `neon_*` resource or data source to the Neon Terraform provider or porting an SDK v2 resource to framework.
---

# Adding Neon resources and data sources

Follow the approved plan and repository conventions when adding provider features.
For SDK v2 migrations, also use the `migrating-neon-sdkv2-to-framework` skill.

## Workflow

1. Use a feature branch based on current `main` from [neondatabase/terraform-provider-neon](https://github.com/neondatabase/terraform-provider-neon).
   Contributors work on a branch in their fork and propose changes to upstream `main`.
   Maintainers with the necessary rights may use a branch in the upstream repository instead.
   For approved review follow-ups, continue on the existing PR branch.
   Record the checked-out branch and HEAD, live PR head when applicable, upstream reference, and operator-approved base separately.
   Preserve dirty work and other worktrees; use an approved isolated worktree when needed.
   Do not commit, push, create a PR, or force-add ignored files; the operator owns those actions.
2. Before code, document the approved scope, design, requirements, and implementation tasks.
   Include the API and pinned SDK methods, identity and import semantics, drift and error handling, test coverage, changelog ownership, generated documentation command, and manual-test path and scenarios.
   Record unresolved API, SDK, permission, and compatibility questions before implementation.
3. Check the pinned SDK and OpenAPI for signatures, nullability, and error behavior.
   Do not bump the SDK without a separate method-level audit and version-gate decision.
   Prefer existing SDK operations and repository retry helpers; document status-specific fallbacks.
4. Implement the resource or data source using current repository conventions.
   Register it in `provider/provider.go` and keep the change focused.
   Reuse canonical resource schemas, models, mappers, and existing read-only converters for returned data-source entities.
   Keep lookup and filter inputs at the data-source boundary and returned fields computed-only.
5. Add tests appropriate to the type, as described below.
   Add an independent operator-run manual Terraform scenario under `../../neon/testing/manual-<resource-name>/`, with a README and self-contained scenario directories.
   Cover prerequisites, commands, expected state, live API checks, and cleanup; data-source scenarios include unset and configured inputs.
   Keep credentials, state, caches, plans, and provider overrides out of version control.
6. Hand-write examples and short-named templates under `examples/` and `templates/`.
   Generate registry docs with `make docu` if its directive supplies the explicit provider name; otherwise run `go run github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs generate --provider-name terraform-provider-neon` from the repository root.
   Never edit rendered docs by hand.
   Follow the recorded changelog ownership rather than assuming agents should edit `CHANGELOG.md`.
7. Run the approved verification commands and record the tested revision, results, and any pending live or manual gates.
   Leave working-tree edits for operator review.

## Schema and API rules

- Use Framework types for schema-facing model fields; use Go types for internal helpers.
  Typed slices are appropriate for computed lists when Framework reflection can map them and null or unknown values need not be preserved.
- Give computed fields stable API semantics and a user-facing purpose.
  Omit transient or constant fields with no actionable value; compose identity from real scope fields or omit `id`.
  Preserve field types, sensitivity, descriptions, and deprecation metadata when converting resource schemas to read-only data-source schemas.
- Prefer flat attributes for single-field blocks.
  Use Terraform types and required fields for configuration structure; the server validates API values, including enum, regex, length, and numeric bounds.
- Keep the provider thin and preserve SDK diagnostics without rewriting server errors.
  Treat HTTP 404 as not found without classifying its cause.
  Document evidence and approval for any provider-side decision beyond configured behavior, applicable contracts, or reproduced failures.
- For list data sources, fetch all cursor pages and preserve configured filters and required sorting across pages and retries.
  Treat cursors as opaque, retry the same page without duplicates, and publish no partial successful state on failure.
  Preserve missing versus present cursors, encode them exactly once as required by the SDK, and document any approved ordering contract.
- Add no source comments without operator approval, except comments produced by the documentation generation pipeline.

## Test requirements

| Type | Required coverage |
|---|---|
| Resource | Create/read, update or forced replacement, import, out-of-band deletion, invalid inputs and relevant boundaries, cleanup and partial failures where applicable. Compare Terraform state with independently observed Neon API state. |
| Data source | Default to SDK-stub or recorded-fixture tests in `provider/data_source_<name>_test.go`, exercising `Configure`, inputs, identity, mapping, and SDK errors. Document the reason for any live acceptance test in the approved plan. |
| List data source | Cover missing project and branch separately, and zero, one, and multiple results when applicable. For pagination, cover multiple pages, retry of a later page without duplicates, and later-page failure without partial success. |

Read nearby tests for naming and layout.
Use `newProviderFactories` and file-scope HCL helpers.
Use Terraform state for created IDs and the SDK for independent remote assertions and cleanup.
Use `ORG_ID` consistently in acceptance tests; when unset, use `org-twilight-cake-44366159` in approved test command environments, not in test code.
For manual Terraform, set `TF_VAR_org_id` from the selected `ORG_ID`.
Keep fixtures, lifecycle steps, and assertions readable, then run `gofmt` and review the diff.
Do not duplicate shared retry or converter matrices in each data-source suite.

## Common mistakes

- Treating resources and data sources as having the same lifecycle, file layout, or live-test requirements.
- Adding client-side validators for API input rules or inventing provider defaults and fallbacks.
- Copying a data-source entity field map instead of deriving it from the canonical resource schema.
- Assuming one API page is the full collection or treating an opaque cursor as a boolean.
- Using SDK lookups to rediscover Terraform-created IDs rather than asserting remote state independently.
- Skipping the external manual scenario, recording fixture tests as live-test evidence, or accepting historical results as proof for the current binary.
- Generating docs without the explicit provider name, editing generated pages, or assuming changelog ownership.
