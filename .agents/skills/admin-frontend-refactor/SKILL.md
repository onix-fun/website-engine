---
name: admin-frontend-refactor
description: Refactor React, TypeScript, routing, or data flow in the Website Engine admin without changing its Go API contract.
metadata:
  short-description: Website Engine admin React architecture
---

# Admin frontend refactor

Use this skill when changing frontend architecture or extracting shared behavior.

- The stack is React 18, Vite, TypeScript, React Router 6, TanStack Query, React Hook Form, Zod, Radix, and Tailwind. Keep React 18 semantics.
- Keep API shapes aligned with `frontend/src/api.ts`, `frontend/src/types.ts`, and the Go handlers. Do not introduce a parallel client or alter the contract as part of UI work.
- Put reusable domain rules in `domains/` or focused hooks; keep route composition in `pages/`; keep generic accessible controls in `components/ui/`.
- Extract a component when it gives a screen a clear boundary or makes behavior reusable. Avoid boolean-prop matrices, speculative abstractions, oversized context providers, and moving code only to shorten a file.
- Preserve query keys, abort signals, optimistic revision checks, autosave, explicit save state, and unsaved-change guards when touching data flows.
- Prefer derived state and event-driven updates. Keep types precise; do not silence errors with `any`, `@ts-ignore`, disabled lint rules, or broad casts.
- Treat architecture changes as frontend-only unless a separately approved requirement explicitly changes the API.
