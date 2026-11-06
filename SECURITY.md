# Security policy

Kupi handles kubeconfig credentials, Secret values and changes to real clusters, so security reports are taken seriously and answered quickly.

## Reporting a vulnerability

Please **don't open a public issue**. Report it privately instead:

- Use **[Report a vulnerability](https://github.com/ihebamiri/Kupi/security/advisories/new)** on the repository's Security tab.

Include what you found, how to reproduce it, the Kupi version and your OS, and the impact you expect. You'll get an answer within a few days. Once a fix is released, the advisory is published with credit to you, unless you'd rather stay anonymous.

## Supported versions

Kupi is in early development: security fixes go into the latest release only.

| Version | Supported |
|---|---|
| Latest release | Yes |
| Older releases | No |

## What's in scope

- Anything that lets cluster data (labels, logs, annotations, Helm notes…) run code or trigger actions in Kupi.
- Secret values reaching logs, errors, diffs, the audit log, or the UI without an explicit reveal.
- Ways around the Guard: changing a cluster without a preview, the profile's rules, or an audit entry.
- Kubeconfig exec plugins running without the user's trust.
- Files Kupi writes with permissions other users can read.
- Ways to switch on or drive the integrated terminal without the user.

How Kupi defends against these is described in [How Kupi works](docs/ARCHITECTURE.md#10-security-model).

## Out of scope

- Commands a user types in the integrated terminal: they run with the user's own permissions by design, and the terminal says so.
- Problems that need someone who already controls your user account or your kubeconfig files.
