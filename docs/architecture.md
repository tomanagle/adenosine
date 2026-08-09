# Architecture

The canonical architecture and implementation sequence currently live in [`../plan.md`](../plan.md).

The core dependency direction is transport adapters to domain services to narrow consumer-owned capability interfaces to infrastructure adapters. Concrete dependencies are assembled only in `cmd/adenosine` and `internal/di`.
