#!/bin/bash

# --- 配置区 ---
# 确保脚本在任何命令失败时都会退出
set -e
# 确保如果使用了未定义的变量，脚本会退出
set -u

# 加载环境变量 (如果需要)
if [ -f ~/.bashrc ]; then
    source ~/.bashrc
fi

# pgBackRest 配置文件路径 (新增)
export PGBACKREST_CONFIG=${PGBACKREST_CONFIG:-/etc/pgbackrest/pgbackrest.conf}
# pgBackRest 实例名 (Stanza) (新增)
export STANZA_NAME=${STANZA_NAME:-my_stanza}

# 日志文件路径
export SHELL_LOG_PATH=${SHELL_LOG_PATH:-/app/postgresql/home/scripts/backup/logs/backup.log}
# 备份模式 (full, incr, or diff) - 注意 pgbackrest 增量模式简写为 incr
export BK_MODE=${BK_MODE:-incr}
# 日志保留天数
LOG_RETENTION_DAYS=60

# --- 日志功能函数 ---
# 功能: 向日志文件打印带有时间戳和日志级别的消息
# 用法: log "日志级别" "日志消息"
log() {
    local level="$1"
    local message="$2"
    # 格式: [YYYY-MM-DD HH:MM:SS] [级别] 消息
    # 使用 tee 同时输出到控制台和日志文件，方便调试
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] [$level] $message" | tee -a "$SHELL_LOG_PATH"
}

# --- 主程序 ---

# 确保日志目录存在
mkdir -p "$(dirname "$SHELL_LOG_PATH")"

# 1. 自动清理旧日志
# -----------------------------------------------------------------------------
# 使用 awk 进行清理，这是更安全、更可靠的方式
if [ -f "$SHELL_LOG_PATH" ]; then
    log "INFO" "开始清理旧日志，保留最近 $LOG_RETENTION_DAYS 天的记录..."
    
    # 计算截止日期
    cutoff_date=$(date -d "$LOG_RETENTION_DAYS days ago" '+%Y-%m-%d')
    tmp_log_file="${SHELL_LOG_PATH}.tmp"

    # 使用 awk 处理日志文件
    awk -v cutoff="$cutoff_date" '
    {
        if ($1 ~ /^\[[0-9]{4}-[0-9]{2}-[0-9]{2}/) {
            line_date = substr($1, 2, 10);
            if (line_date >= cutoff) {
                print;
            }
        } else {
            print;
        }
    }' "$SHELL_LOG_PATH" > "$tmp_log_file"

    # 用清理后的新日志文件覆盖旧文件
    mv "$tmp_log_file" "$SHELL_LOG_PATH"
    log "INFO" "旧日志清理完成。"
fi


# 2. 执行备份任务
# -----------------------------------------------------------------------------
log "INFO" "==================== 备份任务开始 ===================="
log "INFO" "配置文件: $PGBACKREST_CONFIG"
log "INFO" "实例名称(Stanza): $STANZA_NAME"
log "INFO" "备份模式(Type): $BK_MODE"

# 执行备份命令
log "INFO" "开始执行 pgbackrest backup 命令..."

# 临时关闭 set -e 以便手动捕获错误码
set +e
pgbackrest --config="$PGBACKREST_CONFIG" --stanza="$STANZA_NAME" --type="$BK_MODE" backup >> "$SHELL_LOG_PATH" 2>&1
exit_code=$?
# 恢复 set -e
set -e

if [ $exit_code -eq 0 ]; then
    log "SUCCESS" "pgbackrest backup 命令成功执行。"
else
    log "ERROR" "pgbackrest backup 命令执行失败！退出码: $exit_code"
    log "INFO" "==================== 备份任务异常结束 ===================="
    exit 1
fi

# 执行验证命令 (使用 info 命令查看备份状态)
log "INFO" "开始执行 pgbackrest info 命令验证备份结果..."

set +e
pgbackrest --config="$PGBACKREST_CONFIG" --stanza="$STANZA_NAME" info >> "$SHELL_LOG_PATH" 2>&1
exit_code=$?
set -e

if [ $exit_code -eq 0 ]; then
    log "SUCCESS" "pgbackrest info 验证命令成功执行。"
else
    log "ERROR" "pgbackrest info 验证命令执行失败！退出码: $exit_code"
    exit 1
fi

log "INFO" "==================== 备份任务圆满结束 ===================="

exit 0
