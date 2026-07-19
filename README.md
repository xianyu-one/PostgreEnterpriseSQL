# PostgreSQL Enterprise SQL (PESQL)

PostgreSQL Enterprise SQL (PESQL) is an enterprise-grade distribution of PostgreSQL. It is designed to extend PostgreSQL with enterprise-level features and modular add-ons while preserving the purity and compatibility of the PostgreSQL mainline.

PESQL bundles PostgreSQL with vital database management utilities, cluster tools, and advanced physical/logical backup capabilities, managed seamlessly under a unified interface.

---

## Key Features

1. **Pure Mainline Core**: Preserves 100% of the standard PostgreSQL database kernel and behavior.
2. **Unified Enterprise Manager (`pg_mgr`)**: A pure Go tool providing simplified installation, instance creation, environment switching, upgrade paths, and systemd service orchestration.
3. **Ecosystem Bundles**:
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
