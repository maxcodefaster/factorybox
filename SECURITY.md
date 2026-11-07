# Security policy

## Supported versions

Only the latest published release and `main` receive security fixes. Older releases do not.

## Reporting a vulnerability

Report privately through
[GitHub private vulnerability reporting](https://github.com/maxcodefaster/factorybox/security/advisories/new).
Do not open a public issue or pull request for a vulnerability.

Include the affected version or commit, the impact and steps to reproduce. Expect an
acknowledgement within a few days. Fixes ship in a new release, and the advisory credits the
reporter unless they ask otherwise.

## Scope

Factorybox runs guest code in Sysbox system containers on the node's kernel. It is not a virtual
machine boundary, and the worker is privileged by design. Reports about escaping a Sandbox, reaching
another Sandbox's files or memory, or abusing the worker's privileged paths are in scope. Findings
that need a node without the capability gate, or a worker shared across trust domains, are not.
