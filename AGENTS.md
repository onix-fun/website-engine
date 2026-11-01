# Website Engine agent guidance

## Application map

- The admin is a React 18 + TypeScript SPA in `frontend/src`, built with Vite and served below `/admin/` by the Go engine.
- Routes and role boundaries live in `frontend/src/app/router.tsx`; the shared shell is `frontend/src/app/layouts/AppShell.tsx`.
- API request code is centralized in `frontend/src/api.ts`. Frontend types in `frontend/src/types.ts` mirror the Go contracts. The handlers and role gates in `presentation/http/routes.go` are authoritative.
- `frontend/src/domains/` holds reusable domain logic; `frontend/src/pages/` holds route screens; `frontend/src/components/ui/` holds shared Radix-based controls.
- `frontend/src/runtime/` is the public website runtime. Keep its CSS and behavior separate from admin-only redesigns.

## Product invariants

- Keep all existing CMS capabilities, backend contracts, user data, and role checks. Do not bypass or weaken route guards or the `can*` permission helpers.
- Keep destructive confirmations and editor unsaved-change protection.
- Preserve revision checks, autosave behavior, environment distinctions, and publication/rollback safeguards.
- Do not send writes to a running API during visual QA unless it is an isolated test environment with disposable data.
- Do not add a UI, state, or test dependency without a concrete need and user approval.

## Frontend conventions

- Use the existing React Router, TanStack Query, React Hook Form, Zod, Radix, Tailwind, and Lucide patterns.
- Use semantic HSL tokens from `frontend/src/index.css` for admin surfaces; define matching light and dark values. Avoid new one-off colors and spacing values where a token or utility already fits.
- Keep request behavior in `api.ts`/domain hooks rather than duplicating fetch logic in pages. Handle loading, empty, error, and saving states where a workflow needs them.
- Keep React 18 compatibility. Do not apply React 19-only composition guidance.
- Make controls keyboard operable, label fields, provide visible focus, respect reduced motion, and keep mobile targets usable.

## Local checks

From `frontend/`, the supported scripts are `pnpm typecheck`, `pnpm lint`, `pnpm test`, and `pnpm build`. `go test ./...` and `go vet ./...` cover the backend. Browser QA should use the admin entry, confirm that its API target is this checkout before interacting, and record any environment limitation instead of treating it as an application pass.

## Project skills

- `admin-design-system`: admin visual language and token consistency.
- `admin-ux-architect`: route structure and CMS workflows.
- `admin-frontend-refactor`: React and TypeScript architecture.
- `admin-visual-qa`: responsive browser verification and screenshots.
- `admin-production-audit`: release checks and issue reporting.
