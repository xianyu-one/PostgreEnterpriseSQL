#!/bin/bash

# --- 配置区 ---
# 确保脚本在任何命令失败时都会退出
set -e
# 确保如果使用了未定义的变量，脚本会退出
set -u

# 加载环境变量 (如果需要)
source ~/.bashrc

# PostgreSQL 备份路径
export BK_PATH=${BK_PATH:-/app/postgresql/backup/}
# 日志文件路径
export SHELL_LOG_PATH=${SHELL_LOG_PATH:-/app/postgresql/home/scripts/backup/logs/backup.log}
# 备份模式 (full, incremental, or archive)
export BK_MODE=${BK_MODE:-incremental}
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
    # -v cutoff="$cutoff_date"  : 将shell变量 cutoff_date 传递给 awk
    # substr($1, 2, 10)         : 从每行的第一个字段（如 "[2025-09-01"）中提取日期部分 "2025-09-01"
    # if (line_date >= cutoff)  : 如果日志日期大于或等于截止日期，则打印该行
    # else                      : 如果不是日期开头的行（例如分隔符），也打印，予以保留
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
log "INFO" "备份模式: $BK_MODE"
# log "INFO" "备份目录: $BK_PATH" # 这行信息有点重复，可以酌情保留

# 执行备份命令
log "INFO" "开始执行 pg_rman backup 命令..."
# 将命令的 stdout 和 stderr 都附加到日志文件中
# 注意：pg_rman 的输出会由 log 函数处理，这里直接重定向即可
pg_rman backup -p 51721 --backup-mode="$BK_MODE" --with-serverlog -B "$BK_PATH" >> "$SHELL_LOG_PATH" 2>&1
# 捕获并检查上一条命令的退出码
exit_code=$?

if [ $exit_code -eq 0 ]; then
    log "SUCCESS" "pg_rman backup 命令成功执行。"
else
    log "ERROR" "pg_rman backup 命令执行失败！退出码: $exit_code"
    log "INFO" "==================== 备份任务异常结束 ===================="
    # 如果备份失败，脚本会因为 set -e 自动退出
    exit 1
fi

# 执行验证命令
log "INFO" "开始执行 pg_rman validate 命令..."
pg_rman validate -B "$BK_PATH" >> "$SHELL_LOG_PATH" 2>&1
exit_code=$?

if [ $exit_code -eq 0 ]; then
    log "SUCCESS" "pg_rman validate 命令成功执行。"
else
    # 验证失败也应该被视为一个错误
    log "ERROR" "pg_rman validate 命令执行失败！退出码: $exit_code"
    exit 1
fi

log "INFO" "==================== 备份任务圆满结束 ===================="

exit 0

