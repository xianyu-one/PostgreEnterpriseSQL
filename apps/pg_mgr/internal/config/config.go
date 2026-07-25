package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

type PgrmanConfig struct {
	Tool            string `mapstructure:"tool" yaml:"tool,omitempty"`
	BackupDir       string `mapstructure:"backup_dir" yaml:"backup_dir,omitempty"`
	SrvLogPath      string `mapstructure:"srv_log_path" yaml:"srv_log_path,omitempty"`
	ArcLogPath      string `mapstructure:"arc_log_path" yaml:"arc_log_path,omitempty"`
	CompressData    string `mapstructure:"compress_data" yaml:"compress_data,omitempty"`
	KeepArcLogDays  int    `mapstructure:"keep_arclog_days" yaml:"keep_arclog_days,omitempty"`
	KeepSrvLogDays  int    `mapstructure:"keep_srvlog_days" yaml:"keep_srvlog_days,omitempty"`
	KeepDataDays    int    `mapstructure:"keep_data_days" yaml:"keep_data_days,omitempty"`
	FullBackupCron  string `mapstructure:"full_backup_cron" yaml:"full_backup_cron,omitempty"`
	IncrBackupCron  string `mapstructure:"incr_backup_cron" yaml:"incr_backup_cron,omitempty"`
	ScheduleEnabled *bool  `mapstructure:"schedule_enabled" yaml:"schedule_enabled,omitempty"`
	FullBackupDay   int    `mapstructure:"full_backup_day" yaml:"full_backup_day,omitempty"` // Legacy: 0-6 (0=Sunday)
	FullBackupHour  int    `mapstructure:"full_backup_hour" yaml:"full_backup_hour,omitempty"`
	FullBackupMin   int    `mapstructure:"full_backup_min" yaml:"full_backup_min,omitempty"`
	IncrBackupHour  int    `mapstructure:"incr_backup_hour" yaml:"incr_backup_hour,omitempty"`
	IncrBackupMin   int    `mapstructure:"incr_backup_min" yaml:"incr_backup_min,omitempty"`
}

type InstanceMeta struct {
	User         string        `mapstructure:"user" yaml:"user"`
	DataDir      string        `mapstructure:"data_dir" yaml:"data_dir"`
	OldDataDir   string        `mapstructure:"datadir" yaml:"datadir,omitempty"`
	BinPath      string        `mapstructure:"bin_path" yaml:"bin_path"`
	OldBinPath   string        `mapstructure:"binpath" yaml:"binpath,omitempty"`
	Port         string        `mapstructure:"port" yaml:"port"`
	DatabaseUser string        `mapstructure:"database_user" yaml:"database_user,omitempty"`
	DatabaseName string        `mapstructure:"database_name" yaml:"database_name,omitempty"`
	Pgrman       *PgrmanConfig `mapstructure:"pgrman" yaml:"pgrman,omitempty"`
	OldBackup    *PgrmanConfig `mapstructure:"backup" yaml:"backup,omitempty"`
}

type GlobalConfig struct {
	BaseDir    string                  `mapstructure:"base_dir" yaml:"base_dir"`
	OldBaseDir string                  `mapstructure:"basedir" yaml:"basedir,omitempty"`
	LogDir     string                  `mapstructure:"log_dir" yaml:"log_dir"`
	LogLevel   string                  `mapstructure:"log_level" yaml:"log_level"`
	Instances  map[string]InstanceMeta `mapstructure:"instances" yaml:"instances"`
}

var Global GlobalConfig
var ConfigFilePath = "/etc/pg_mgr/conf.yaml"

func InitConfig() {
	viper.SetConfigFile(ConfigFilePath)
	viper.SetEnvPrefix("PG_MGR")
	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err == nil {
		viper.Unmarshal(&Global)
	}

	// Migrate fields if loaded from older format
	migrated := false
	if Global.BaseDir == "" && Global.OldBaseDir != "" {
		Global.BaseDir = Global.OldBaseDir
		Global.OldBaseDir = ""
		migrated = true
	}
	for name, meta := range Global.Instances {
		needsUpdate := false
		if meta.DataDir == "" && meta.OldDataDir != "" {
			meta.DataDir = meta.OldDataDir
			needsUpdate = true
		}
		if meta.BinPath == "" && meta.OldBinPath != "" {
			meta.BinPath = meta.OldBinPath
			needsUpdate = true
		}
		if meta.Pgrman == nil && meta.OldBackup != nil {
			meta.Pgrman = meta.OldBackup
			meta.OldBackup = nil
			needsUpdate = true
		}
		if needsUpdate {
			meta.OldDataDir = ""
			meta.OldBinPath = ""
			Global.Instances[name] = meta
			migrated = true
		}
	}

	if envBase := os.Getenv("PG_MGR_BASE_DIR"); envBase != "" {
		Global.BaseDir = envBase
	}
	if Global.BaseDir == "" {
		Global.BaseDir = "/app/postgresql"
	}
	if Global.LogDir == "" {
		Global.LogDir = "/var/log/pg_mgr"
	}
	if Global.LogLevel == "" {
		Global.LogLevel = "error"
	}
	if Global.Instances == nil {
		Global.Instances = make(map[string]InstanceMeta)
	}

	if migrated {
		_ = writeConfig()
	}
}

func writeConfig() error {
	viper.Reset()
	viper.SetConfigFile(ConfigFilePath)
	viper.SetEnvPrefix("PG_MGR")
	viper.AutomaticEnv()
	viper.Set("base_dir", Global.BaseDir)
	viper.Set("log_dir", Global.LogDir)
	viper.Set("log_level", Global.LogLevel)
	viper.Set("instances", Global.Instances)
	return viper.WriteConfigAs(ConfigFilePath)
}

func SaveGlobalConfig(baseDir, logDir, logLevel string) error {
	os.MkdirAll(filepath.Dir(ConfigFilePath), 0755)
	Global.BaseDir = baseDir
	Global.LogDir = logDir
	Global.LogLevel = logLevel
	return writeConfig()
}

func SaveInstanceToRegistry(name, user, dataDir, binPath, port string) error {
	return SaveInstanceToRegistryWithDatabaseUser(name, user, dataDir, binPath, port, "")
}

func SaveInstanceToRegistryWithDatabaseUser(name, user, dataDir, binPath, port, databaseUser string) error {
	return SaveInstanceToRegistryWithDatabaseConnection(name, user, dataDir, binPath, port, databaseUser, "")
}

func SaveInstanceToRegistryWithDatabaseConnection(name, user, dataDir, binPath, port, databaseUser, databaseName string) error {
	os.MkdirAll(filepath.Dir(ConfigFilePath), 0755)
	if Global.Instances == nil {
		Global.Instances = make(map[string]InstanceMeta)
	}
	// Preserve Pgrman configuration if it exists
	var pgrman *PgrmanConfig
	if exist, ok := Global.Instances[name]; ok {
		pgrman = exist.Pgrman
		if databaseUser == "" {
			databaseUser = exist.DatabaseUser
		}
		if databaseName == "" {
			databaseName = exist.DatabaseName
		}
	}
	Global.Instances[name] = InstanceMeta{
		User:         user,
		DataDir:      dataDir,
		BinPath:      binPath,
		Port:         port,
		DatabaseUser: databaseUser,
		DatabaseName: databaseName,
		Pgrman:       pgrman,
	}
	return writeConfig()
}

func SaveInstanceDatabaseUser(name, databaseUser string) error {
	return SaveInstanceDatabaseConnection(name, databaseUser, "")
}

func SaveInstanceDatabaseConnection(name, databaseUser, databaseName string) error {
	if Global.Instances == nil {
		return fmt.Errorf("no instances registered")
	}
	meta, ok := Global.Instances[name]
	if !ok {
		return fmt.Errorf("instance %s not found", name)
	}
	meta.DatabaseUser = databaseUser
	if databaseName != "" {
		meta.DatabaseName = databaseName
	}
	Global.Instances[name] = meta
	return writeConfig()
}

func SaveInstancePgrmanConfig(name string, pgrman *PgrmanConfig) error {
	if Global.Instances == nil {
		return fmt.Errorf("no instances registered")
	}
	if meta, ok := Global.Instances[name]; ok {
		meta.Pgrman = pgrman
		Global.Instances[name] = meta
		return writeConfig()
	}
	return fmt.Errorf("instance %s not found", name)
}

func RemoveInstanceFromRegistry(name string) error {
	if Global.Instances != nil {
		delete(Global.Instances, name)
		return writeConfig()
	}
	return nil
}
