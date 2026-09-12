# Security Policy

Media Gateway is a security boundary for private media. Please do not report suspected vulnerabilities through a public issue.

## Reporting

- Preferred: open a private GitHub security advisory for this repository when available.
- Otherwise: contact the maintainer privately with the affected version/commit, impact, and reproduction details.

## High-impact areas

Reports are especially valuable when they involve:

- serving an asset outside configured publication policy;
- bypassing allowed-root or exact path-segment checks;
- exposing provider credentials or private provider metadata;
- arbitrary URL fetching or SSRF;
- filesystem/path traversal;
- turning an arbitrary provider asset ID into public bytes without policy evaluation;
- accidentally exposing private/control endpoints through the public listener;
- request smuggling, header confusion, or cache behavior that could bypass authorization;
- denial-of-service behavior from unbounded provider/public requests.

## Disclosure

Please allow reasonable time for investigation and a fix before public disclosure.
