# pg_mgr CLI Naming Conventions & Permission Specification
# pg_mgr 命令行命名风格与权限控制规范

为了提升 `pg_mgr` 命令行工具的开发一致性与用户使用体验，消除历史命令中的命名歧义（如 `install` 既安装软件又初始化实例、`uninstall` 仅删除实例、`list` 与 `show` 混用等），特制定本设计规范。后续所有新增功能与修改均需遵循本规范。

---

## 1. 核心概念与资源分类 (Resource Domains)

`pg_mgr` 中的主要操作对象划分为以下 6 个核心资源域：

1. **`pkg` (软件包/软件版本)**：PostgreSQL 官方或企业版二进制安装包与版本目录。
2. **`instance` (数据库实例)**：基于特定软件版本创建的数据库集群（数据目录、端口、用户环境变量、系统服务等）。
3. **`service` (服务控制)**：实例生命周期管理（启动、停止、重启、重载、自启等）。
4. **`backup` (备份与恢复)**：基于 `pg_rman` 的实例备份策略配置、定时任务与手工备份管理。
5. **`archive` (日志归档)**：WAL 归档配置与状态管理。
6. **`daemon` (系统守护进程)**：`pg_mgr` 全局后台守护进程管理。

---

## 2. 动词统一与子命令规范 (Verb Standardization)

子命令中的动词必须保持统一含义与明确分工：

| 动词 Verb | 统一定义与使用场景 | 规范示例 | 禁忌与替代方案 |
|---|---|---|---|
| **`list`** | 列出集合/资源列表（多项概述）。统一使用 `list` 及其别名 `ls`。 | `instance list`<br>`pkg list`<br>`backup list` | 禁止混用 `show` 来展示多项列表。 |
| **`show`** | 查看单项资源的详细信息、配置或状态。 | `backup show`<br>`archive show` | 展示列表时不要使用 `show`。 |
| **`create`** | 创建新资源（如基于已安装版本创建实例）。 | `instance create` | 不要在非一键部署场景中使用 `install`。 |
| **`remove`** | 移除/删除资源（如删除实例、卸载守护进程）。别名保持 `uninstall`。 | `instance remove`<br>`daemon remove` | 避免仅写 `uninstall` 导致语义不清（究竟是删实例还是卸软件）。 |
| **`deploy`** | 一键部署（安装软件包 binaries 并初始化/启动实例）。别名保持 `install`。 | `deploy` | 区分单步创建 `instance create` 与一键部署 `deploy`。 |
| **`modify`** | 修改已存在资源的配置属性。别名支持 `edit`, `set`, `configure`。 | `instance modify`<br>`backup modify`<br>`archive set` | 避免各自发明 `edit`, `configure`, `set` 等不同主主动词。 |
| **`init` / `uninit`** | 系统的初始化与反初始化（如全局配置、备份目录初始化）。 | `init`<br>`backup init`<br>`backup remove` | - |

---

## 3. 命令矩阵与别名参考表 (Command Matrix & Aliases)

为了保持向下兼容性，老旧子命令（如 `install`, `uninstall`, `create-instance`, `install-pkg`, `ls` 等）继续作为别名 (Aliases) 保留，新规范推荐使用主命令。

| 资源域 | 主命令 (Primary Command) | 兼容别名 (Aliases) | 功能说明 |
|---|---|---|---|
| **全局配置** | `pg_mgr init` | `pg_mgr config init` | 初始化 `pg_mgr` 全局配置文件 `/etc/pg_mgr/conf.yaml` |
| **软件包** | `pg_mgr pkg install` | `pg_mgr install-pkg` | 仅解压/安装 PostgreSQL 软件包二进制文件 |
| | `pg_mgr pkg list` | `pg_mgr list versions`, `versions` | 列出系统中已安装的所有 PostgreSQL 软件版本 |
| **实例管理** | `pg_mgr deploy` | `pg_mgr install` | 一键安装软件包并创建/初始化/启动新实例 |
| | `pg_mgr instance create` | `pg_mgr create-instance`, `create` | 使用已安装的软件版本创建新的数据库实例 |
| | `pg_mgr instance remove` | `pg_mgr remove-instance`, `uninstall` | 停止并删除数据库实例及其数据目录 |
| | `pg_mgr instance list` | `pg_mgr list`, `ls` | 列出当前受控及游离的所有数据库实例信息 |
| | `pg_mgr instance modify` | `pg_mgr modify`, `configure`, `edit` | 修改数据库实例配置（端口、数据目录、运行用户等） |
| | `pg_mgr instance upgrade` | `pg_mgr upgrade` | 将数据库实例升级至更高版本的 PostgreSQL |
| | `pg_mgr instance adopt` | `pg_mgr adopt` | 检测并纳管游离的 PostgreSQL 实例 |
| | `pg_mgr instance sync` | `pg_mgr sync` | 扫描运行中的数据库进程并同步至注册表 |
| | `pg_mgr instance use` | `pg_mgr use`, `switch` | 切换当前 shell 环境变量以使用指定实例 |
| **服务控制** | `pg_mgr start [inst]` | `pg_mgr instance start` | 启动数据库实例服务 |
| | `pg_mgr stop [inst]` | `pg_mgr instance stop` | 停止数据库实例服务 |
| | `pg_mgr restart [inst]`| `pg_mgr instance restart` | 重启数据库实例服务 |
| | `pg_mgr reload [inst]` | `pg_mgr instance reload` | 重载数据库实例配置文件 |
| | `pg_mgr status [inst]` | `pg_mgr instance status` | 查看数据库实例服务运行状态 |
| | `pg_mgr enable [inst]` | `pg_mgr instance enable` | 设置数据库实例开机自启 |
| | `pg_mgr disable [inst]`| `pg_mgr instance disable` | 取消数据库实例开机自启 |
| **备份管理** | `pg_mgr backup list` | `pg_mgr backup ls` | 列出所有实例的备份配置与定时任务 |
| | `pg_mgr backup init` | `pg_mgr backup pgrman init` | 交互式初始化实例的 `pg_rman` 备份配置 |
| | `pg_mgr backup modify` | `pg_mgr backup edit`, `set` | 修改实例的备份参数与定时任务 |
| | `pg_mgr backup remove` | `pg_mgr backup uninit`, `clean` | 清除/取消实例的备份配置 |
| | `pg_mgr backup show` | `pg_mgr backup pgrman show` | 查看备份目录中的备份集详细信息 |
| | `pg_mgr backup run` | `pg_mgr backup pgrman run` | 手动触发全量或增量备份 |
| | `pg_mgr backup pgrman delete DATE` | - | 删除指定开始时间及其之前、不再被后续增量备份依赖的备份集；`pg_rman` 会保留恢复必需的最新全量备份，`DATE` 格式为 `YYYY-MM-DD HH:MM:SS` |
| **归档管理** | `pg_mgr archive show` | `pg_mgr archive status` | 查看实例的 WAL 归档配置与状态 |
| | `pg_mgr archive enable`| - | 开启实例的 WAL 归档功能 |
| | `pg_mgr archive disable`| - | 关闭实例的 `pg_mgr` WAL 归档设置 |
| | `pg_mgr archive set` | `pg_mgr archive modify` | 修改实例的 WAL 归档目录或命令 |
| **守护进程** | `pg_mgr daemon install`| - | 安装 `pg_mgr` 为 systemd 系统服务 |
| | `pg_mgr daemon remove` | `pg_mgr daemon uninstall` | 卸载 `pg_mgr` systemd 系统服务 |
| | `pg_mgr daemon start/stop/restart/reload/status/run` | - | 管理守护进程服务状态 |
| **自动补全** | `pg_mgr completion install [bash\|zsh]` | - | 安装 Shell 自动补全脚本 |
| | `pg_mgr completion remove [bash\|zsh]` | `completion uninstall` | 卸载 Shell 自动补全脚本 |
| **工具自身** | `pg_mgr self-update --binary PATH [--target PATH]` | `pg_mgr update` | 使用本地新版二进制原子更新当前安装；`--target` 可引导升级尚无此命令的旧安装；运行中的 daemon 会安全重启，失败时恢复旧版本 |

---

## 4. 权限与运行用户控制规范 (Role-Based Access Control)

为了兼顾系统安全性与操作便捷性，程序根据使用场景与运行用户自动判断是否允许执行命令：

### 4.1 权限等级划分

1. **Root 专属权限 (Root-Only Scope)**
   - **涉及场景**：修改全局配置文件（`/etc/pg_mgr/conf.yaml`）、创建/修改系统用户和组（`postgres`）、管理全局 systemd 系统服务（`/etc/systemd/system/pg_mgr.service`）、扫描全局 `/proc` 进程。
   - **涵盖子命令**：`init`, `pkg install`, `deploy`, `daemon *`, `adopt`, `sync`, `completion *`。
   - **控制逻辑**：若当前运行用户非 root (EUID != 0)，程序提示用户提权并退出：
     ```text
     This program must be run as root (sudo). / 此程序必须以 root 权限运行 (sudo)。
     ```

2. **实例守护用户 / Root 共享权限 (Instance Owner Scope)**
   - **涉及场景**：针对某特定实例的日常管理（启动/停止服务、修改该实例配置、升级版本、备份、归档等）。
   - **涵盖子命令**：`start`, `stop`, `restart`, `reload`, `status`, `enable`, `disable`, `instance remove`, `instance modify`, `instance upgrade`, `backup *`, `archive *`。
   - **控制逻辑**：
     - 若当前用户为 `root`：**允许执行**。
     - 若当前用户为该实例在注册表中记录的守护进程 OS 用户（`meta.User`，如 `postgres`）：**允许执行**（直接以当前用户操作 systemd user 服务或修改文件，无需 `sudo` 或密码提权）。
     - 若当前用户既不是 `root` 也不是 `meta.User`：**拒绝执行**，并明确提示提权或切换至该实例用户：
       ```text
       This command requires root privileges (sudo) or running as instance user '<user>'.
       此命令必须以 root 权限 (sudo) 或实例运行用户 '<user>' 身份运行。
       ```

3. **完全只读 / 环境变量查询 (Read-Only Scope)**
   - **涵盖子命令**：`instance list`, `pkg list`, `instance use`。
   - **控制逻辑**：任何用户均可直接执行。

---

## 5. 开发者新增命令指南 (Developer Workflow)

当开发新增子命令时，请遵循以下步骤：

1. **确定所属资源域**：判断新功能属于 `pkg`, `instance`, `backup`, `archive`, `daemon` 还是全局工具。
2. **选择规范动词**：列表使用 `list`，看详情使用 `show`，修改配置使用 `modify`/`set`，创建使用 `create`，删除使用 `remove`。
3. **注册命令与别名**：
   - 将主命令挂载至对应的资源组命令（如 `InstanceCmd.AddCommand(newCmd)`）。
   - 如果需要保持向下兼容，可同时向 `RootCmd` 挂载别名。
4. **校验权限**：
   - Root 专属命令：在 `Run` 函数起始位置调用 `utils.EnsureRoot()`。
   - 实例相关命令：在 `Run` 函数起始位置调用 `utils.EnsureInstancePermission(instanceName)`。
5. **添加 i18n 多语言**：在 `internal/i18n/i18n.go` 中同步添加 `en` 和 `zh-CN` 字符串。

---

## 6. 优先使用纯 Go 实现规范 (Pure Go First Implementation Guideline)

为了降低对外部系统 Shell 命令（如 `ps`、`kill`、`cp` 等）的依赖，消除进程派生 (fork/exec) 的开销并提升极简系统环境下的健壮性与运行效率，开发中必须遵循**优先使用纯 Golang 原生实现**的原则：

1. **进程检测与资源统计**：
   - 严禁调用外部 `ps` 等 Shell 命令。
   - 统一通过直接读取 Linux `/proc` 虚拟文件系统（如 `/proc/<pid>/cmdline`、`/proc/<pid>/status`、`/proc/<pid>/stat`）获取进程列表、运行用户、CPU 占比及 RSS 内存占用。
2. **进程信号与状态控制**：
   - 严禁使用 `exec.Command("kill", ...)` 检查或终止进程。
   - 统一使用 Go 原生 `syscall.Kill(pid, sig)` 发送信号，或通过 `/proc/<pid>` 判断进程存活状态。
3. **文件与目录迁移/复制**：
   - 严禁使用 `cp -a` 或 shell 脚本复制目录。
   - 统一使用 Go 原生递归复制（如 `utils.CopyDir` / `utils.CopyFile`），精确控制文件权限 (`os.Chmod`)、所有权 (`os.Chown`) 及时间戳 (`os.Chtimes`)。
4. **外部命令使用边界**：
   - 仅在必须调用 PostgreSQL 核心二进制工具（如 `initdb`, `pg_ctl`, `pg_rman`, `psql`）或 Linux 系统服务管理工具（如 `systemctl`）时允许使用 `exec.Command`。
   - 所有通用系统操作（文件/目录处理、进程探测、信号传递、用户状态判断）必须采用纯 Go 原生 API 实现。
