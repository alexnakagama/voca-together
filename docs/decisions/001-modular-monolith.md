# 001: Modular monolith: Go + PostgreSQL + REST

> **Status:** in force.
>
> **Map of what was built:** `docs/architecture.md`.

One deployable Go service, one database. Feature packages under `backend/internal/`.
Microservices would add operational cost with no benefit at this stage.
