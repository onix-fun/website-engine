---
name: admin-visual-qa
description: Verify Website Engine admin screens in a real browser across responsive sizes, themes, and interaction states; exclude public-runtime-only checks.
metadata:
  short-description: Website Engine admin browser QA
---

# Admin visual QA

Use this skill when the task changes the admin interface or claims a browser-verified result.

- Start from the actual Vite admin entry (`frontend/admin.html`, available in dev at `/admin/admin.html`) or the Go-served `/admin/` bundle. Do not mistake `frontend/index.html` for the admin; it is the public runtime entry.
- Check the API target and response shape before login or interaction. A reachable local server may belong to another checkout. Do not write to shared or unknown data during visual QA.
- Inspect desktop and mobile layouts, then check the required widths when practical: 360, 390, 768, 1024, 1280, 1440, and 1920 CSS pixels. Check system, light, and dark themes where available.
- Exercise navigation, tabs, open/close behavior, validation, filters, sorting, search, keyboard focus, and unsaved-state warnings. Use isolated disposable data for any persistent CRUD scenario.
- Review screenshots at full size for clipping, overflow, contrast, focus visibility, and editor workspace width. Save QA screenshots outside tracked source files; existing `artifacts/ui-audit/` captures are reference material and must not be overwritten.
- Record blocked checks (authentication, incompatible backend, or missing test data) explicitly. A rendered page or successful build alone is not browser QA.
