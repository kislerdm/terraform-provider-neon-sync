# Neon Functions API contract notes

Captured for task 1.1 of `add-neon-functions`. Source pages cited inline.

## Status

- Beta service. Schema and behavior may shift before GA.
- Region constraint during beta: `aws-us-east-2` only. Projects for Functions must be created there.
- Runtime during beta: `nodejs24` only.

## Base URL and auth

- Base URL: `https://console.neon.tech/api/v2`
- We use the existing project `Authorization: Bearer $NEON_API_KEY` header. Documented schemes: `BearerAuth`, `CookieAuth`, `TokenCookieAuth`. The Go SDK uses Bearer; we keep that.
- All endpoints require auth. Body and query content types vary (see below).

## Scope for `neon_function` resource

The `neon_function` Terraform resource covers the Function lifecycle: list, get, update, delete. The deploy endpoint is used to **create** (first deploy to a slug creates the function) and to push new code on subsequent deploys. Deploys are a separate concern; they belong to either the same resource (with optional code fields) or a sibling `neon_function_deployment` resource. Decision recorded as Open Question 5 in `openspec/changes/add-neon-functions/design.md`.

Buckets, triggers, function logs, invocations, and metrics are not in scope for this resource. They live behind the other 8 of the 13 Functions endpoints.

## Endpoints (the five that matter for the Function resource)

### `GET /projects/{project_id}/branches/{branch_id}/functions`
List functions on a branch. Cursor pagination, `limit` 1 to 1000. Response shape: `{ items: [...], pagination: { next: <cursor> | null } }`.

### `GET /projects/{project_id}/branches/{branch_id}/functions/{slug}`
Get function details. Returns the full `NeonFunctionResponse` object.

### `PATCH /projects/{project_id}/branches/{branch_id}/functions/{slug}`
Update the function. Body shape is one field:
```json
{ "name": "My function" }
```
`name` can be `null` to clear the display name; it then falls back to the slug. Whitespace is trimmed; whitespace-only names are rejected. `name` is the only mutable field on the Function.

### `DELETE /projects/{project_id}/branches/{branch_id}/functions/{slug}`
Delete the function and all its deployments. 200 on success (or 204; the docs note it returns void in the SDK).

### `POST /projects/{project_id}/branches/{branch_id}/functions/{slug}/deployments`
Deploy code to the function. Also implicitly creates the function on first deploy.
- Content-Type: `multipart/form-data`
- Form fields:
  - `zip` (file, optional except on first deploy)
  - `runtime` (string enum, currently `nodejs24`)
  - `environment` (JSON-encoded `Record<string,string>`; values are write-only at rest, never returned)
- Response 201 with the created `NeonFunctionDeployment`.

## Path param patterns

| Param | Pattern | Notes |
|---|---|---|
| `project_id` | `^[a-z0-9-]{1,60}$` | The existing project ID. |
| `branch_id` | `^[a-z0-9-]{1,60}$` | Functions are branch-scoped, not project-scoped. |
| `slug` | `^[a-z0-9]{1,20}$` | Lowercase letters and digits, 1 to 20, no hyphens. Assigned on first deploy; immutable afterward (it appears in the public invocation URL). |

## Object schemas (inferred; full OpenAPI in source pages)

### `NeonFunctionResponse`
Returned by get/list. Used to populate state.

| Field | Type | Mutable | Notes |
|---|---|---|---|
| `slug` | string | no | Identifier. Pattern as above. |
| `name` | string | yes | Display name. Falls back to slug when empty. |
| `runtime` | string | (via deploy) | Current runtime of the active deployment. |
| `environment` | array of strings | (via deploy) | Names of env vars only. Values are write-only. |
| `current_deployment` | object \| null | no | Subset of the latest deployment: id (int), status (string), memory_mib (int), runtime (string), created_at (timestamp). Null before first deploy. |

The OpenAPI components for `NeonFunctionResponse` are not visible in the indexed markdown excerpts; this is the inferred shape consistent with the SDK type `NeonFunction` and the writable fields described. Re-verify against the live OpenAPI before finalizing the schema in `resource_function.go`.

### `NeonFunctionDeployment`
Returned by deploy and indirectly nested in the function response.

Required fields: `id` (int32), `status` (string), `memory_mib` (int), `runtime` (string), `created_at` (timestamp).
`id` is the platform version, monotonic per function. `status` is an enum (likely `pending`, `building`, `active`, `superseded`, `completed`, `error_failed`; values inferred, confirm against OpenAPI).

## Slug implications for the Terraform schema

- `slug` is the resource identifier (used in URLs and the public invocation URL). In HCL it belongs as `slug` (or the conventional `name` field for human use, with `slug` derived). Per consistency with the rest of the provider (`neon_api_key.name` is the identifier), `slug` is the right convention here.
- `slug` is immutable after creation. The schema must mark it `RequiresReplace` if a future update to the spec lets users change it (current API forbids it).
- `slug` is a value the user picks. It is `Required`, `ForceNew` (effectively), and validated client-side against `^[a-z0-9]{1,20}$`.

## Error model

Standard `GeneralError`:
```json
{
  "request_id": "string",
  "code": "machine_readable",
  "message": "human readable"
}
```
- Idempotency: GET/HEAD/OPTIONS safe to retry; POST/PATCH/DELETE/PUT generally not.
- `503 Service Unavailable`: always safe to retry.
- `423 Locked`: always safe to retry (resource temporarily locked).

This lines up with `internal/provider/retry.go` (handles 429, 500, 423). The FSM helper is a fit; we extend the same retry policy to Functions.

Rate limiting: 429 returns standard `GeneralError` plus a `Retry-After` header (per Neon API convention). The existing FSM helper handles 429 but the `Retry-After` value is not currently honored; the 1-second fixed delay is policy. Document as Future Work, not blocker.

## Response shapes needed

- 200 list response: `{ items: NeonFunctionResponse[], pagination: { next: string|null } }`
- 200 get response: `NeonFunctionResponse`
- 200 patch response: `NeonFunctionResponse` (echo back updated)
- 200/204 delete response: empty
- 201 deploy response: `NeonFunctionDeployment`

## What's NOT in scope here

- Deployment list, deployment get, deployment cancel, deployment retry (covered by other 4 of the 13 Functions endpoints). Belongs to a future `neon_function_deployment` resource if we ship it.
- Function logs, invocations, metrics. Deferred per proposal Out of Scope section.
- Auth schemes other than Bearer. Out of scope.

## Open questions remaining after this recon

These will move into `design.md` once resolved:

1. SDK path (Dmitry's call).
2. Branch-scoped means `branch_id` is a required attribute on the resource. Need to confirm with Andre whether users compose this from `neon_branch.id` lookup or expect a flat `branch_id` string.
3. Whether the deployment endpoint belongs in this resource (with optional code fields) or behind a sibling `neon_function_deployment` resource.
4. Whether `slug` should be required in HCL or derivable from `name` via `ReplaceChars` style.
5. Whether `environment` (env var values) belongs in this resource at all, given that values are write-only and never returned. Likely no — users set values via deploy, and the resource only surfaces the names.

## Sources

- https://api-docs.neon.tech/reference/createprojectbranchfunctiondeployment
- https://api-docs.neon.tech/reference/getprojectbranchfunction
- https://api-docs.neon.tech/reference/getprojectbranchfunction.md
- https://api-docs.neon.tech/reference/listprojectbranchfunctions
- https://api-docs.neon.tech/reference/listprojectbranchfunctions.md
- https://api-docs.neon.tech/reference/updateprojectbranchfunction
- https://neon.com/docs/compute/functions/deploy.md
- https://neon.com/docs/compute/functions/overview
- https://neon.com/docs/reference/api/functions
- https://neon.com/docs/reference/api/reference
- https://neon.com/docs/reference/typescript-sdk (canonical field names via `NeonFunction` type)
