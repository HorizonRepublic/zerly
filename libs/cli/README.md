# @zerly/cli

The official command-line interface for the **Zerly** ecosystem.

`@zerly/cli` is designed to supercharge your development workflow by automating routine tasks, scaffolding infrastructure, and enforcing architectural best practices. It allows you to spin up a production-ready environment in seconds, not hours.

## Installation

Install the CLI globally to access the `zerly` command from anywhere:

```bash
npm install -g @zerly/cli
```

Verify the installation:

```bash
zerly --help
```

## Commands

### Generate (`g`)

The `generate` command allows you to scaffold various application resources.

#### Infrastructure (`infra`)

Instantly create a `docker-compose.yaml` file tailored for Zerly applications. This setup includes a pre-configured stack:

- **PostgreSQL**: The robust SQL database.
- **NATS (JetStream)**: High-performance messaging system optimized for microservices.
- **Garnet**: Next-gen cache store (Redis compatible) from Microsoft.

**Usage:**

```bash
# Generate docker-compose.yaml in the current directory
zerly generate infra:docker

# Alias (shorter version)
zerly g docker
```

**Options:**

| Option      | Alias | Description                                         | Default |
|:------------|:------|:----------------------------------------------------|:--------|
| `--dry-run` | `-d`  | Output file content to console without creating it. | `false` |
| `--force`   | `-f`  | Overwrite existing files if they already exist.     | `false` |
| `--help`    | `-h`  | Display help for the command.                       |         |

**Example:**

```bash
# Preview what will be generated
zerly g docker --dry-run

# Force overwrite existing configuration
zerly g docker --force
```

## Roadmap

- [ ] **Module Generator:** Scaffold new feature modules (`zerly g module user`).
- [ ] **Microservice Generator:** Create standalone microservice apps.
- [ ] **Migration Tools:** Database migration helpers.

## License

This project is part of the **Zerly** ecosystem.
MIT Licensed.
