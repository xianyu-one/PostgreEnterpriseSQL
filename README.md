# PostgreSQL Enterprise SQL (PESQL)

PostgreSQL Enterprise SQL (PESQL) is an enterprise-grade distribution of PostgreSQL. It is designed to extend PostgreSQL with enterprise-level features and modular add-ons while preserving the purity and compatibility of the PostgreSQL mainline.

PESQL bundles PostgreSQL with vital database management utilities, cluster tools, and advanced physical/logical backup capabilities, managed seamlessly under a unified interface.

---

## Key Features

1. **Pure Mainline Core**: Preserves 100% of the standard PostgreSQL database kernel and behavior.
2. **Unified Enterprise Manager (`pg_mgr`)**: A pure Go tool providing simplified installation, instance creation, environment switching, upgrade paths, and systemd service orchestration.
3. **Ecosystem Bundles**:
   - **PostGIS**: Spatial data types, functions, and indexes.
   - **pgvector**: Vector similarity search with exact and approximate indexes.
   - **`pgBackRest`**: Powerful, enterprise backup and restore solution.
   - **`repmgr`**: Easy replication and failover management.
   - **`pg_rman`**: Online physical backup and restore tool.
4. **Custom Go-based Sidecars**:
   - **`pg_archiver`**: Fast WAL archiving tool with strict CRC64 verification.
   - **`pg_checker`**: Scans the system to detect and catalog active PostgreSQL instances.
   - **`pg_wal_cleaner`**: Portable WAL cleanup utility supporting retention settings by size and age.

---

## Installation & Quick Start

PESQL is delivered as a precompiled tarball matching your target OS and CPU architecture.

### 1. Download and Extract
Obtain the appropriate release tarball:
```bash
# Example for RockyLinux 9
tar -zxvf postgresql-16.14-x64-RockyLinux9-PESQL-v1.0.0.tar.gz -C /usr/local/pesql
```

### 2. Add PESQL to PATH
Add the PESQL binaries to your shell profile:
```bash
export PATH="/usr/local/pesql/bin:/usr/local/pesql/sbin:$PATH"
```

---

## Unified Instance Management via `pg_mgr`

`pg_mgr` is the primary interface for managing your database instances. It automates common DBA tasks like service configuration, environment path switching, instance registration, and major/minor upgrades.

> [!IMPORTANT]
> Operations that write to system services (`/etc/systemd/system`) or require root paths must be run with `sudo` or as the `root` user.

### 1. Initialize the Environment
Before creating instances, initialize the global configuration of `pg_mgr`. This sets the base directory where PostgreSQL software versions and instances are located.
```bash
sudo pg_mgr init
```
*You will be prompted to confirm the base management directory (default: `/usr/local/pesql`).*

### 2. Register standard software & Create an Instance
Install PostgreSQL from the release package, register the version, and initialize a new instance (e.g. named `default` on port `5432`):
```bash
sudo pg_mgr install \
  --tar /path/to/postgresql-16.14-x64-RockyLinux9-PESQL-v1.0.0.tar.gz \
  --instance default \
  --port 5432 \
  --password "YourStrongPassword"
```

### 3. Create Additional Instances
Once a software version is registered, you can spin up additional instances without re-extracting the tarball:
```bash
sudo pg_mgr create \
  --instance sales-db \
  --major 16 \
  --minor 14 \
  --port 5433 \
  --password "AnotherSecurePassword"
```

### 4. Control Database Services (systemd Integration)
`pg_mgr` automatically generates systemd units for registered instances. Manage database services with simple commands:
```bash
# Start an instance
sudo pg_mgr start sales-db

# Stop an instance
sudo pg_mgr stop sales-db

# Enable auto-start on system boot
sudo pg_mgr enable sales-db

# Disable auto-start on boot
sudo pg_mgr disable sales-db
```

### 5. List Managed Instances & Software Versions
Track all registered database instances and software versions:
```bash
# List all database instances and their operational status
pg_mgr list

# List all installed/registered PostgreSQL versions
pg_mgr list versions
```

### 6. Adopt Existing Instances
If you have an existing PostgreSQL instance running on your system (not created by `pg_mgr`), you can bring it under `pg_mgr` control using `adopt`:
```bash
sudo pg_mgr adopt \
  --data-dir /var/lib/pgsql/data \
  --instance legacy-db \
  --os-user postgres \
  --bin-path /usr/bin/postgres \
  --port 5432
```

### 7. Switch Environments (`use` / `switch`)
Easily configure your shell's environment variables (such as `PATH`, `PGDATA`, `LD_LIBRARY_PATH`, and `PG_RMAN_BACK_PATH`) to target a specific database instance:
```bash
# Load environmental variables for 'sales-db'
pg_mgr use sales-db
```
This command writes the instance's environmental configurations to the user's `~/.pgrc`. Source it to apply:
```bash
source ~/.pgrc
```
Now, standard PostgreSQL commands like `psql`, `pg_ctl`, and `pg_rman` will automatically point to `sales-db`.

### 8. Upgrade Instances
Safely upgrade a database instance to a newer registered version:
```bash
sudo pg_mgr upgrade \
  --instance sales-db \
  --target-version 16.15
```

If the instance has a `pg_mgr`-managed pg_rman configuration, `upgrade` first creates and validates a fresh full backup while the old instance is still running. The database service is stopped only after that backup succeeds.

For major upgrades, `pg_mgr` reads the old cluster's actual data-checksum state with its `pg_controldata`, probes the target `initdb` for supported checksum options, and verifies the initialized target cluster again with the target `pg_controldata`. The upgrade stops before `pg_upgrade` if the states do not match; no PostgreSQL-version assumption is used.

To deliberately skip this protection in an interactive terminal, pass `--skip-backup` and confirm the displayed recovery risk. Automation requires all three explicit acknowledgements:
```bash
pg_mgr upgrade \
  --instance sales-db \
  --target-version 18.6 \
  --skip-backup \
  --accept-no-backup-risk \
  --non-interactive \
  --yes
```

### 9. Update `pg_mgr`
After building or downloading a new `pg_mgr` binary, update the currently installed executable without manually stopping and copying the daemon binary:
```bash
sudo pg_mgr self-update --binary /path/to/new/pg_mgr
```
The command validates the candidate, stops the `pg_mgr` daemon only when it is running, replaces the executable atomically, and restarts the daemon. If the new daemon cannot start, the previous executable is restored automatically. For automation, add `--non-interactive --yes`.

To bootstrap this command from an older installation that does not yet provide `self-update`, run the newly built binary and identify the installed target explicitly:
```bash
sudo ./output/sbin/pg_mgr self-update \
  --binary ./output/sbin/pg_mgr \
  --target "$(command -v pg_mgr)"
```

---

## Standalone Utilities Guide

PESQL includes secondary utility tools to automate administrative tasks:

### `pg_checker`
Used to list all running PostgreSQL database instances across the host by inspecting processes:
```bash
pg_checker
```

### `pg_wal_cleaner`
Runs automated cleanups of WAL archiver directories:
```bash
# Clean immediately with 7-day retention and 10GB size limit
pg_wal_cleaner clean --dir /var/lib/pesql/archive --keep-duration 7d --keep-size 10G

# Run as a background daemon
pg_wal_cleaner daemon --dir /var/lib/pesql/archive --keep-duration 14d --interval 1h
```

---

## Compiling from Source

To compile the Go-based management tools manually:
```bash
# Execute the apps build script
./scripts/build_apps.sh ./apps ./output
```
To set a specific version at build time, specify the `PESQL_VERSION` environment variable:
```bash
PESQL_VERSION=v1.0.0 ./scripts/build_apps.sh ./apps ./output
```
This compiles the tools and places the output binaries under the `output/bin` or `output/sbin` directory depending on their root execution requirements.

The release Docker build also compiles PostGIS and pgvector against the bundled
PostgreSQL. Their versions can be selected with build arguments:

```bash
docker build \
  --build-arg POSTGIS_VERSION=3.6.4 \
  --build-arg PGVECTOR_VERSION=0.8.6 \
  .
```

After installing a release package, enable the extensions in each database that
needs them:

```sql
CREATE EXTENSION postgis;
CREATE EXTENSION vector;
```

When the release workflow is started manually, pgBackRest, repmgr, PostGIS, and
pgvector default to `latest`. The workflow resolves the newest stable upstream
tag and records the resolved version in the release notes. Enter an explicit
version in any component field to build that version instead.

---

## License

This project is licensed under the Apache License 2.0. See the [LICENSE](LICENSE) file for details.

### Third-Party Licenses

This project packages, compiles, or integrates third-party components under their respective open-source licenses:
- **PostgreSQL**: PostgreSQL License
- **pgBackRest**: MIT License
- **pg_rman**: BSD 3-Clause License
- **repmgr**: GNU General Public License v3 (GPLv3)
- **PostGIS**: GNU General Public License v2 or later
- **pgvector**: PostgreSQL License

For detailed licensing information on third-party components, see [LICENSE-3RD-PARTY.md](LICENSE-3RD-PARTY.md).
