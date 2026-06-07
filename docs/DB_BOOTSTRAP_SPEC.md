# DB Bootstrap Specification

## Goal

Provide one shared, code-first database bootstrap system that can be reused by new apps without creating a custom setup service for each app.

The app should only register its entity types and optional seed providers. The shared bootstrap engine should handle database existence checks, schema creation, and initial data seeding.

## Core Principle

Entities are the source of truth.

The bootstrap engine reflects over app entity structs and uses metadata tags to build tables and constraints.

## Standard Responsibilities

### Shared bootstrap engine in infra

The shared engine should own:

- database existence checks
- schema creation from entity metadata
- table creation
- primary key generation
- index generation
- foreign key creation where metadata is available
- seed execution
- bootstrap status reporting
- idempotent setup execution

### App layer responsibilities

Each app should only provide:

- entity registration
- seed registration
- config flags for bootstrap behavior
- optional app name or namespace for setup UI and logging

The app should not implement its own bootstrap service unless it needs custom behavior beyond the shared standard.

## Proposed Folder Shape

Suggested layout:

- `infra/db/bootstrap`
  - shared bootstrap engine
  - entity scanner
  - schema builder
  - seed runner
  - setup status APIs
- `apps/<app>/entities`
  - schema source of truth
- `apps/<app>/config.json`
  - bootstrap and seeding flags
- `apps/<app>/main.go`
  - registers entities with the shared bootstrap engine

## Startup Flow

1. App loads config.
2. App builds entity registry.
3. Shared bootstrap engine checks database connectivity.
4. If database is missing and auto-create is enabled, the engine creates it.
5. Engine opens the target database and ensures the bootstrap state table exists.
6. Engine creates schema from entities and adds missing columns when safe additive migration is enabled.
7. Engine seeds initial rows if enabled.
8. Engine stores the applied manifest hash.
9. App transitions into normal runtime mode.

## Bootstrap Modes

### Dev mode

Recommended defaults:

- auto-create database: true
- auto-create schema: true
- auto-seed: true
- auto-migrate: true

### Production mode

Recommended defaults:

- auto-create database: false
- auto-create schema: false or controlled
- auto-migrate: controlled
- auto-seed: false unless explicitly enabled

## Entity Metadata Contract

Entities should carry explicit tags that describe database behavior.

Recommended tags:

- table name
- column name
- primary key
- unique key
- nullable
- default value
- index hint
- foreign key hint
- seed hint when needed

Example intent:

- struct field names remain Go-friendly
- tags describe DB behavior
- reflection reads tags during bootstrap

## Seed Contract

Seeding should be separate from schema generation.

Recommended seed model:

- register named seed providers per app
- each provider returns rows to insert
- seed execution is idempotent where possible
- seed profiles can be enabled by config

## Setup Page Behavior

The setup page should be a thin UI over server-side provisioning.

It should show:

- DB reachability status
- database existence status
- schema readiness status
- seed status
- provisioning action button

It should not execute SQL directly from the browser.

## Recommended HTTP Surface

Shared endpoints can be standardized as:

- `GET /setup/status`
- `POST /setup/provision`
- `POST /setup/seed`
- `POST /setup/reset` only when explicitly allowed in dev

Current implementation also exposes:

- `GET <setupPath>` for the bootstrap status page
- `GET <setupPath>/status` for the JSON status payload

The app can redirect to the setup page when bootstrap mode is active.

## Safety Rules

- Never auto-drop production databases.
- Never auto-reset schema unless a dev-only flag permits it.
- Never expose raw SQL execution from the browser.
- Never infer schema from entities without an explicit registry boundary.

## Config Proposal

Suggested config keys in `config.json`:

- `db.bootstrap.enabled`
- `db.bootstrap.autoCreateDatabase`
- `db.bootstrap.autoCreateSchema`
- `db.bootstrap.autoSeed`
- `db.bootstrap.allowReset`
- `db.bootstrap.seedProfile`
- `db.bootstrap.setupPath`
- `db.bootstrap.seedStatements`

## Minimal App Integration Contract

Every new app should only need to do this:

1. Import the shared bootstrap package.
2. Register entity types.
3. Register optional seed providers.
4. Pass bootstrap config.
5. Start server.

That is the standard I recommend for reuse across new apps.

## Recommended Next Implementation Step

Build the shared bootstrap engine first, then add a thin setup API and setup page on top of it.

## Current Implementation Note

The first implementation in this repository uses startup bootstrap plus additive schema reconciliation. It does not drop tables or columns automatically.

When `autoSeed` is enabled, the engine can execute config-defined SQL seed statements through the shared seeder helper.

The current app also seeds a minimal core identity dataset on first run:

- a `system` user group
- a `superadmin` user role associated with that group
- a default `superadmin` login account (`superadmin` / `superadmin123`, stored as bcrypt) linked to that role
- wildcard-host `api_endpoint` rows with `accessTier` metadata and `api_endpoint_rbac` rows for protected modules, so the default access rules are portable across hosts
