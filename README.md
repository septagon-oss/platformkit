# PlatformKit

The public front door for PlatformKit OSS. Clone and run:

```bash
go run .
```

This boots the OSS monolith (tenant, user, audit, health, auth, api_key,
content, notification, admin) against a single SQLite database on `:8080`.

All application logic lives in
[`github.com/septagon-oss/pk-apps/pkg/starterapp`](https://github.com/septagon-oss/pk-apps);
this module is a thin wrapper over `starterapp.Run(ctx, starterapp.DefaultConfig())`.

> Stub README — the full front-door README is drafted separately.
