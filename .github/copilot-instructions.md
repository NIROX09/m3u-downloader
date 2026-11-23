# Plex Media Manager – Project Instructions

## Project overview

This repository contains an existing Go-based IPTV downloader and media management application.

The application is being extended into a browser-based Plex Media Manager.

Development environment:
- Windows 11
- Visual Studio Code
- GitHub Copilot
- Go
- SQLite
- Synology DS1525+ as target NAS
- Plex Media Server running on the NAS

Repository location:
`C:\PlexMediaManager\tools\m3u-downloader`

## Fundamental rules

- Always extend the existing application.
- Never start a replacement project.
- Inspect existing source code before implementing anything.
- Preserve working functionality.
- Avoid unnecessary refactoring and dependencies.
- Keep SQLite as the existing database.
- Reuse existing database methods and services.
- Preserve existing CLI functionality.
- Keep the browser frontend and backend in the same Go project.
- Use context-aware database and HTTP operations.
- Handle errors explicitly.
- Never expose IPTV credentials or API keys.
- Never perform destructive database operations without explicit user approval.
- Never claim successful implementation without testing it.

## Existing architecture

The existing project is based on m3u-dl.

Existing components include:
- Go application entry point
- Xtream Codes integration
- Catalog synchronization
- SQLite storage
- Download queue
- Background worker
- Terminal user interface
- Browser-based management interface

Existing database tables include:
- categories
- vods
- series
- seasons
- episodes
- jobs
- subscriptions

The database file is `state.db`.

Do not create a second database.

## Existing catalog state

Last confirmed synchronization:
- 62 categories
- 27,226 VOD movies
- 5,348 series

Do not trigger unnecessary full catalog synchronizations.

## Existing download infrastructure

The backend already supports:
- Adding download jobs
- Claiming jobs
- Tracking progress
- Completing jobs
- Retrying failed jobs
- Cancelling jobs
- Deleting jobs
- Listing jobs
- Duplicate job prevention
- Retry/backoff handling
- Optional remuxing
- Sidecar file generation

Existing job states:
- pending
- active
- completed
- failed
- cancelled

The worker infrastructure must be reused rather than rebuilt.

## Browser interface

The browser interface is the primary management interface.

Existing implementation:
- Go net/http web server
- JSON API
- Browser dashboard
- Dark interface
- Tablet-friendly layout

Planned CLI command:
`m3u-dl web`

Development server:
`127.0.0.1:8765`

Existing dashboard and health endpoint have been implemented.

Last confirmed health response:

`{"database":"connected","status":"ok"}`

Last confirmed dashboard response:

`{"catalog":{"categories":62,"movies":27226,"series":5348},"jobs":{"active":0,"cancelled":0,"completed":0,"failed":0,"pending":1}}`

## Mandatory NSFW filter

Implement a persistent NSFW filter.

Requirements:
1. Hide categories whose names begin with `FOR ADULTS` by default.
2. Exclude movies belonging to those categories from All Categories results.
3. Add a small NSFW filter control next to the category dropdown.
4. The filter must be enabled by default.
5. The user can explicitly disable it.
6. Apply filtering consistently to catalog views and search.
7. Do not delete or modify database records.
8. Persist the user's filter preference.
9. If no preference exists, default to enabled.
10. Apply the same behavior after browser reloads.

## Development workflow

For every implementation task:

1. Inspect relevant source files.
2. Explain the proposed changes briefly.
3. Implement one coherent feature at a time.
4. Format Go code using gofmt.
5. Run relevant tests.
6. Build the application.
7. Test relevant HTTP endpoints.
8. Report the actual results.

Use PowerShell-compatible commands on Windows.

Do not assume source files have remained unchanged since the last session.

## Target deployment

The final application will run on a Synology DS1525+ NAS alongside Plex.

Keep deployment compatibility in mind:
- Avoid unnecessary Windows-specific dependencies.
- Keep paths configurable.
- Preserve portability of the Go backend.
- Do not assume the NAS has the same directory structure as the Windows development environment.

## Communication

Communicate with the user in German.

Keep explanations concise and technical.

Ask before destructive operations, database migrations that could risk existing data, or major architectural changes.

Prefer incremental, verifiable development over large speculative implementations.