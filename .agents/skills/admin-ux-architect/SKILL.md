---
name: admin-ux-architect
description: Rework navigation, information hierarchy, or CMS workflows in the Website Engine admin while preserving existing business capabilities.
metadata:
  short-description: Website Engine CMS navigation and workflows
---

# Admin UX architect

Use this skill when reorganizing admin navigation, page hierarchy, or multi-step workflows.

- Build the screen map from `frontend/src/app/router.tsx`, `AppShell.tsx`, the page components, and the matching handlers in `presentation/http/routes.go`.
- Treat user-facing content, feature builds, routes/pages, design tokens, snapshots/releases, media, submissions, tokens/users/roles, and settings as distinct real capabilities. Do not infer that similar labels mean duplicate business functions.
- Preserve deep links, route guards, permission-aware actions, destructive confirmations, and unsaved-change warnings when moving interface elements.
- Use progressive disclosure for advanced configuration. Keep frequent work close to its data and show state changes next to the relevant action.
- Keep the existing Russian product language plain and consistent. Label actions by their result and errors by the next useful step.
- Do not change backend rules or remove a workflow to simplify the interface. If code and API contracts do not reveal the intended behavior, record the exact product question before implementing a behavior change.
