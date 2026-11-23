---
name: continue-plex
description: Resume development of the existing Plex Media Manager from its last confirmed implementation state. Use when continuing development, analyzing the repository, implementing catalog features, download queue functionality or NSFW filtering.
---

# Continue Plex Media Manager Development

## Objective

Continue development of the existing Plex Media Manager in the current repository.

Never create a replacement application.

## Repository

`C:\PlexMediaManager\tools\m3u-downloader`

## Last confirmed implementation state

The following was confirmed during the previous development session:

- Go build successful.
- SQLite database connected.
- Browser dashboard working.
- Health endpoint working.
- Catalog synchronization completed.
- 62 categories.
- 27,226 VOD movies.
- 5,348 series.
- A movie was successfully added to the backend download queue.

Health response:

`{"database":"connected","status":"ok"}`

Dashboard response:

`{"catalog":{"categories":62,"movies":27226,"series":5348},"jobs":{"active":0,"cancelled":0,"completed":0,"failed":0,"pending":1}}`

## Exact stopping point

The backend accepts download jobs successfully.

The browser frontend does not yet show:
- A download button for movies.
- A download queue navigation button.
- A complete queue management interface.

The next task is to connect the existing backend functionality to the frontend.

## Mandatory implementation order

### 1. Inspect

Inspect the actual repository before making changes.

Check:
- `cmd/m3u-dl/main.go`
- `internal/store/`
- `internal/catalog/`
- `internal/downloader/`
- `internal/worker/`
- `internal/ui/`
- Existing web server implementation
- Existing frontend files
- Existing SQL schema

Discover the real file structure instead of assuming it matches the previous session.

### 2. Download integration

Add frontend download actions.

Reuse existing backend queue methods and endpoints.

Do not implement a second queue system.

### 3. Queue interface

Implement a browser view for existing download jobs.

Display:
- Name
- Status
- Progress
- Errors
- Relevant download information

Provide actions for:
- Cancelling
- Retrying failed jobs
- Removing jobs

Respect existing job states and backend rules.

### 4. NSFW filter

Implement the persistent NSFW filter.

Requirements:
- Hide categories beginning with `FOR ADULTS`.
- Hide movies from these categories in All Categories.
- Apply the filter to relevant searches and catalog views.
- Provide a small NSFW control next to the category dropdown.
- Default state: enabled.
- Allow the user to disable the filter.
- Persist the user's choice.
- Never delete database records.

### 5. Verification

After implementation:
- Run gofmt.
- Run relevant Go tests.
- Build the application.
- Test relevant API endpoints.
- Verify frontend functionality.
- Report actual results.

## Important constraints

Do not:
- Replace SQLite.
- Rebuild the existing worker.
- Rewrite the CLI.
- Delete existing catalog entries.
- Modify credentials.
- Perform destructive database operations without approval.
- Start unrelated disc-management features before the download workflow is complete.

## Initial execution behavior

When invoked for the first time:

1. Read the project instructions.
2. Inspect the repository.
3. Compare the current implementation with this handover.
4. Report the discovered architecture and current state.
5. Identify the smallest next implementation step.
6. Wait for approval before modifying source code.

Communicate in German.

Preserve existing working functionality and prioritize incremental, testable development.