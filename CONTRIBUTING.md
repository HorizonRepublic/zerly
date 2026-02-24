# Contributing to Zerly

Thank you for your interest in contributing to Zerly! 🚀

## Getting Started

1.  **Fork the repository** on GitHub.
2.  **Clone your fork** locally:
    ```bash
    git clone https://github.com/YOUR_USERNAME/zerly.git
    cd zerly
    ```
3.  **Install dependencies**:
    ```bash
    pnpm install
    ```

## Development Workflow

We use [Nx](https://nx.dev) for monorepo management.

*   **Build**: `pnpm nx affected -t build`
*   **Lint**: `pnpm nx affected -t lint`
*   **Test**: `pnpm nx affected -t test`

## Commit Convention

We use [Conventional Commits](https://www.conventionalcommits.org/).
Please format your commit messages as follows:

*   `feat(core): add new kernel module`
*   `fix(logger): fix pino integration`
*   `chore: update dependencies`

This is important because we use automated semantic versioning based on commits.

## Pull Requests

*   Create a branch from `dev`.
*   Ensure all checks pass (Lint, Build).
*   Open a PR to `dev` branch.