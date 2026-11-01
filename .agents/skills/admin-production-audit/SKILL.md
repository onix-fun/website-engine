---
name: admin-production-audit
description: Assess release readiness of the Website Engine admin through its available static, type, test, build, accessibility, and runtime checks.
metadata:
  short-description: Website Engine admin production checks
---

# Admin production audit

Use this skill when asked to harden, release, or make a broad claim about the admin's production readiness.

- Run only checks supported by this checkout: frontend `typecheck`, `lint`, `test`, and `build`; Go `test` and `vet`; and browser checks when the correct application and API are available.
- Review the route tree and role guards against `presentation/http/routes.go`; verify that administrative actions remain consistent with server permissions.
- Check keyboard semantics, labels, focus visibility, contrast, reduced motion, responsive overflow, errors, loading, empty, and saving states.
- Inspect build and runtime output for actual errors. Separate findings by severity and include the command, route, or file that supports each result.
- Never call the admin production-ready when a required integration or browser check is unavailable or failing. State the boundary and the evidence needed to close it.
- Do not run migrations, publish, promote, roll back, upload, revoke, archive, or delete against a shared backend during QA.
