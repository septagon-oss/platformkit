# Working in this repository

Read ARCHITECTURE.md for the current boundaries and CONTRIBUTING.md for the
readability and review criteria. docs/adr/ records the decisions.

## Rules

1. **Make changes easy to understand and maintain.** Use domain language,
   explicit control flow and visible effects. Source size is a reviewed cost,
   not the objective. Keep make check-loc and make check-packages green;
   raising a ceiling remains a separate owner commit with a reason.
2. **No new channel.** No new registry type, config key namespace or generated
   document. Extend the existing composition and verification paths.
3. **One task, one commit, green build.** make check passes before you commit.
4. **Remove replaced implementations in the same change.** This rebuild has
   no legacy API or data compatibility requirement. Start from a fresh schema;
   keep subsequent changes testable through the new upgrade path.
5. **Keep scope bounded.** Record an unrelated defect separately.
6. **Verify before claiming.** Paste real command output into the commit body.
7. **Never commit secrets**, config.yaml, binaries or generated assets.
8. **One implementation per business rule.** Real and in-memory services may
   share pure decisions; their conformance cases need independent expectations.
   A fake proves a testing seam. Reuse requires evidence from different products.
9. **Contracts before implementations.** Define the module's public behavior and
   conformance cases before filling its implementation.

## Commands

    make up               # Postgres and NATS, waiting for both
    make check            # build, vet, formatting, tests, budgets and boundaries
    make run              # the reference app, on config.yaml
