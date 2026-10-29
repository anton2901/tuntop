# Security Policy

tuntop runs a privileged daemon and routes all of your traffic, so security
bugs matter more than usual. Examples: traffic leaking outside the tunnel,
DNS leaks, a local user gaining root through the daemon socket, credentials
from subscriptions exposed in logs or files.

## Supported versions

Only the latest release receives security fixes while the project is in
0.x.

## Reporting a vulnerability

**Please do not open a public issue.** Use GitHub's
[private vulnerability reporting](https://docs.github.com/en/code-security/security-advisories/guidance-on-reporting-and-writing-information-about-vulnerabilities/privately-reporting-a-security-vulnerability)
on this repository (Security → Report a vulnerability).

Include the tuntop version (`tuntop version`), your OS, steps to reproduce, and
the impact you observed. You will get an acknowledgement within 7 days.
