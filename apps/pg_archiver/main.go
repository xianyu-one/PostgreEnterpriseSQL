package main

import (
	"fmt"
	"hash/crc64"
	"io"
	"os"
	"path/filepath"
	"time"
)

// 环境变量配置键名
const (
	EnvArchiveDir = "PG_ARCHIVE_DIR"
	EnvLogFile    = "PG_ARCHIVE_LOG_FILE"
)

// Config 存储程序配置
type Config struct {
	SourcePath string
	FileName   string
	ArchiveDir string
	LogFile    string
}

// bufferSize 定义拷贝时的缓冲区大小 (1MB)，对于16MB的WAL文件来说比较合适
const bufferSize = 1024 * 1024

// checkSumAlgo 定义使用的校验算法，这里使用 CRC64 ISO 标准，速度极快且适合检错
var crcTable = crc64.MakeTable(crc64.ISO)

var Version = "dev"

func main() {
	// Parse version command
	if len(os.Args) >= 2 && (os.Args[1] == "version" || os.Args[1] == "--version" || os.Args[1] == "-v") {
		fmt.Printf("pg_archiver version %s\n", Version)
		os.Exit(0)
	}

	// 1. 解析参数和环境变量
	if len(os.Args) < 3 {
		fmt.Fprintf(os.Stderr, "Usage: %s <source_path> <filename>\n", os.Args[0])
		os.Exit(1)
	}

	cfg := Config{
		SourcePath: os.Args[1],
		FileName:   os.Args[2],
		ArchiveDir: os.Getenv(EnvArchiveDir),
		LogFile:    os.Getenv(EnvLogFile),
	}

	if cfg.ArchiveDir == "" {
		handleError(cfg, fmt.Errorf("environment variable %s is required", EnvArchiveDir))
	}

	targetPath := filepath.Join(cfg.ArchiveDir, cfg.FileName)

	// 2. 核心逻辑
	err := processArchive(cfg, targetPath)
	if err != nil {
		handleError(cfg, err)
	}

	// 成功归档，退出
	os.Exit(0)
}

// processArchive 执行归档的主要流程
func processArchive(cfg Config, targetPath string) error {
	// 检查目标文件是否存在
	if fileExists(targetPath) {
		return checkExistingFile(cfg.SourcePath, targetPath, cfg.FileName)
	}

	// 目标不存在，执行拷贝并同时计算Hash
	return copyAndVerify(cfg.SourcePath, targetPath)
}

// checkExistingFile 处理目标文件已存在的情况
func checkExistingFile(sourcePath, targetPath, fileName string) error {
	// 计算源文件 Hash
	sourceHash, err := calculateHash(sourcePath)
	if err != nil {
		return fmt.Errorf("failed to calculate source hash: %v", err)
	}

	// 计算目标文件 Hash
	targetHash, err := calculateHash(targetPath)
	if err != nil {
		return fmt.Errorf("failed to calculate target hash: %v", err)
	}

	// 对比 Hash
	if sourceHash == targetHash {
		// Hash一致，视为成功，打印日志（标准输出给Postgres看）
		fmt.Printf("File %s integrity check passed (Existing). Skipping.\n", fileName)
		return nil
	}

	// Hash不一致，这是严重错误
	return fmt.Errorf("file %s exists but Hash mismatch! Src:%s Dst:%s", fileName, sourceHash, targetHash)
}

// copyAndVerify 执行流式拷贝和校验
func copyAndVerify(sourcePath, targetPath string) error {
	srcFile, err := os.Open(sourcePath)
	if err != nil {
		return fmt.Errorf("failed to open source: %v", err)
	}
	defer srcFile.Close()

	// 确保目标目录存在
	targetDir := filepath.Dir(targetPath)
	if !fileExists(targetDir) {
		// 尝试创建目录 (0750)
		if err := os.MkdirAll(targetDir, 0750); err != nil {
			return fmt.Errorf("failed to create target directory: %v", err)
		}
	}

	dstFile, err := os.Create(targetPath)
	if err != nil {
		return fmt.Errorf("failed to create target: %v", err)
	}

	defer func() {
		dstFile.Close()
	}()

	hasherSrc := crc64.New(crcTable)

	// 创建一个 MultiWriter，写入 dstFile 的同时写入 hasherSrc
	writer := io.MultiWriter(dstFile, hasherSrc)

	// 使用较大的缓冲区进行拷贝
	buf := make([]byte, bufferSize)
	if _, err := io.CopyBuffer(writer, srcFile, buf); err != nil {
		return fmt.Errorf("copy failed: %v", err)
	}

	// 确保写入磁盘
	if err := dstFile.Sync(); err != nil {
		return fmt.Errorf("failed to sync file: %v", err)
	}

	// 获取源文件的计算结果
	srcSum := fmt.Sprintf("%x", hasherSrc.Sum(nil))

	// 步骤 B: 重新读取目标文件进行校验
	dstFileRead, err := os.Open(targetPath)
	if err != nil {
		return fmt.Errorf("failed to re-open target for verification: %v", err)
	}
	defer dstFileRead.Close()

	hasherDst := crc64.New(crcTable)
	if _, err := io.CopyBuffer(hasherDst, dstFileRead, buf); err != nil {
		return fmt.Errorf("verification read failed: %v", err)
	}

	dstSum := fmt.Sprintf("%x", hasherDst.Sum(nil))

	if srcSum != dstSum {
		return fmt.Errorf("copy verification failed! Src:%s Dst:%s", srcSum, dstSum)
	}

	return nil
}

// calculateHash 计算文件的 CRC64 Hash
func calculateHash(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	hasher := crc64.New(crcTable)
	buf := make([]byte, bufferSize)
	if _, err := io.CopyBuffer(hasher, f, buf); err != nil {
		return "", err
	}

	return fmt.Sprintf("%x", hasher.Sum(nil)), nil
}

// fileExists 检查文件是否存在
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return !os.IsNotExist(err)
}

// handleError 处理错误：打印到Stderr，写入日志，退出程序
func handleError(cfg Config, err error) {
	errMsg := fmt.Sprintf("ERROR: %v", err)
	fmt.Fprintln(os.Stderr, errMsg) // 必须输出到 Stderr 这样 PostgreSQL 才能捕获到错误

	// 如果配置了日志文件，则写入
	if cfg.LogFile != "" {
		writeLog(cfg.LogFile, errMsg)
	}

	os.Exit(1)
}

// writeLog 写入日志
func writeLog(logPath, message string) {
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err == nil {
		timestamp := time.Now().Format("2006-01-02 15:04:05")
		// 格式: [Time] [PID] Message
		logLine := fmt.Sprintf("[%s] [%d] %s\n", timestamp, os.Getpid(), message)
		f.WriteString(logLine)
		f.Close()
	}
}
