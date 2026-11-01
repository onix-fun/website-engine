---
name: admin-design-system
description: Maintain or redesign the Website Engine admin's visual tokens and shared UI components; exclude the public runtime and one-off artwork.
metadata:
  short-description: Website Engine admin visual system
---

# Admin design system

Use this skill for visual changes shared across the CMS administration UI.

- Keep semantic HSL tokens in `frontend/src/index.css`, Tailwind mappings in `frontend/tailwind.config.ts`, and system/light/dark theme behavior in `frontend/src/app/theme.tsx` aligned.
- Keep the admin and public runtime separate: admin UI uses `index.css`; the public site uses `runtime/runtime.css` and its own theme data.
- Prefer shared primitives from `components/ui/` and the existing layout classes over page-specific copies. Keep buttons, fields, tables, dialogs, empty states, and focus treatment coherent across screens.
- Use color to communicate action, state, and hierarchy. Retain legibility in both themes and avoid decorative gradients, nested surfaces, and shadows without a functional reason.
- Preserve the CMS's dense editing workflows: keep content editors and route canvases spacious, and keep secondary metadata visually quieter.
- Maintain hover, active, focus-visible, disabled, loading, error, and success states where they apply. Respect reduced motion and touch input.
- Do not add fonts, icon packages, or UI dependencies merely for visual novelty. Use Lucide and existing local/system font fallbacks.
