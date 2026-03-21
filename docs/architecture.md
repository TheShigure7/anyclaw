# AnyClaw 0.1 Architecture

## Goal

AnyClaw 0.1 provides the future-state framework of the platform instead of a feature-complete implementation.

## Core layers

- Control plane: assistant management, workspace management, task management, audit management.
- Orchestration plane: planning, execution routing, retries, approvals, background jobs.
- Runtime core: prompt assembly, model routing, skill composition, tool invocation.
- Security boundary: authorization, sandbox policies, dangerous operation control.
- Local data plane: assistant config, memory, task records, audit events, workspace state.

## Main domain entities

- Assistant
- Workspace
- Task
- Audit Event
- Memory Item

## Build direction

1. Implement repositories for all domain entities.
2. Add application services for assistant/workspace/task flows.
3. Add HTTP APIs and Web UI backend.
4. Add runtime execution contracts for tools and skills.
5. Add approval and audit replay support.
