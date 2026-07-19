# Global arguments defined before the first FROM
ARG OS_IMAGE=ubuntu:22.04
ARG PG_VERSION=16.9
ARG PGBACKREST_VERSION=2.54.2
ARG REPMGR_VERSION=v5.4.1
ARG PGRMAN_BRANCH=REL_16_STABLE
ARG ARCHIVE_NAME=postgresql-16.9-x64-ubuntu22.04.tar.gz
ARG USE_CHINA_MIRROR=false

# ==========================================
# Stage 1: apps_builder (Compile Go utilities)
# ==========================================
FROM golang:1.24 AS apps_builder
ARG USE_CHINA_MIRROR=false
ARG PESQL_VERSION=unknown
WORKDIR /app
COPY apps/ ./apps/
COPY scripts/build_apps.sh ./scripts/
RUN if [ "$USE_CHINA_MIRROR" = "true" ]; then \
        go env -w GOPROXY=https://goproxy.cn,direct; \
    fi && \
    chmod +x scripts/build_apps.sh && \
    PESQL_VERSION="${PESQL_VERSION}" ./scripts/build_apps.sh ./apps ./output

# ==========================================
# Stage 2: base_os (Setup OS dependencies)
# ==========================================
FROM ${OS_IMAGE} AS base_os
ARG USE_CHINA_MIRROR=false

# Copy system dependency installer scripts
COPY scripts/ /tmp/scripts/

# Dynamically run the appropriate script based on OS release info
RUN if [ -f /etc/os-release ]; then \
        . /etc/os-release; \
        if [ "$ID" = "ubuntu" ] || [ "$ID" = "debian" ]; then \
            USE_CHINA_MIRROR=${USE_CHINA_MIRROR} bash /tmp/scripts/deps-debian.sh; \
        elif [ "$ID" = "rocky" ] || [ "$ID" = "rhel" ] || [ "$ID" = "centos" ]; then \
            USE_CHINA_MIRROR=${USE_CHINA_MIRROR} bash /tmp/scripts/deps-rhel.sh; \
        else \
            echo "Unsupported OS: $ID" && exit 1; \
        fi; \
    else \
        echo "/etc/os-release not found!" && exit 1; \
    fi && \
    rm -rf /tmp/scripts

# ==========================================
# Stage 3: builder (Core Build Layer)
# ==========================================
FROM base_os AS builder

# Re-declare arguments for use within this stage
ARG PG_VERSION
ARG PGBACKREST_VERSION
ARG REPMGR_VERSION
ARG PGRMAN_BRANCH
ARG ARCHIVE_NAME
ARG USE_CHINA_MIRROR=false

# Create build and release directories
RUN mkdir /build && mkdir /release

# Compile PostgreSQL
RUN cd /build && \
    if [ "$USE_CHINA_MIRROR" = "true" ]; then \
        PG_URL="https://mirrors.aliyun.com/postgresql/source/v${PG_VERSION}/postgresql-${PG_VERSION}.tar.gz"; \
    else \
        PG_URL="https://ftp.postgresql.org/pub/source/v${PG_VERSION}/postgresql-${PG_VERSION}.tar.gz"; \
    fi && \
    wget -q -O - "$PG_URL" | \
        tar zx -C /build && \
    cd /build/postgresql-${PG_VERSION} && \
    export CPU_CORES=$(nproc) && \
    ./configure \
        --prefix=/build/postgresql-release \
        --with-ssl=openssl \
        --with-perl \
        --with-python \
        --with-tcl \
        --with-systemd \
        --with-lz4 && \
    make -j $CPU_CORES world && \
    make install-world

# Configure environment path for PG tools and compilation
ENV PKG_CONFIG_PATH="/build/postgresql-release/lib/pkgconfig"
ENV PATH="/build/postgresql-release/bin:${PATH}"

# Compile pgBackRest
RUN cd /build && \
    PGBACKREST_URL="https://github.com/pgbackrest/pgbackrest/archive/release/${PGBACKREST_VERSION}.tar.gz"; \
    wget -q -O - "$PGBACKREST_URL" | \
        tar zx -C /build && \
    cd /build/pgbackrest-release-${PGBACKREST_VERSION} && \
    meson setup build && \
    ninja -C build && \
    cp build/src/pgbackrest /build/postgresql-release/bin/

# Compile pg_rman
RUN cd /build && \
    git clone --branch ${PGRMAN_BRANCH} --depth 1 https://github.com/ossc-db/pg_rman.git && \
    cd /build/pg_rman && \
    make && \
    make install

# Compile repmgr
RUN cd /build && \
    git clone --branch ${REPMGR_VERSION} --depth 1 https://github.com/EnterpriseDB/repmgr.git && \
    cd /build/repmgr && \
    ./configure && \
    make install

# Copy custom Go utilities compiled in Stage 1
COPY --from=apps_builder --chmod=755 /app/output/ /build/postgresql-release/

# Copy custom backup scripts to example_scripts
COPY --chmod=755 apps/backup/ /build/postgresql-release/example_scripts/

# Package the final build into a tarball
RUN cd /build/postgresql-release && \
    tar -czvf /release/${ARCHIVE_NAME} ./*

# ==========================================
# Stage 4: scratch (Final Output Layer)
# ==========================================
FROM scratch

ARG ARCHIVE_NAME
COPY --from=builder /release/${ARCHIVE_NAME} /${ARCHIVE_NAME}
