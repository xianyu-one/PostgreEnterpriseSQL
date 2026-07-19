package config

import (
	"os"
	"path/filepath"

	"github.com/spf13/viper"
)

type InstanceMeta struct {
	User       string `mapstructure:"user" yaml:"user"`
	DataDir    string `mapstructure:"data_dir" yaml:"data_dir"`
	OldDataDir string `mapstructure:"datadir" yaml:"datadir,omitempty"`
	BinPath    string `mapstructure:"bin_path" yaml:"bin_path"`
	OldBinPath string `mapstructure:"binpath" yaml:"binpath,omitempty"`
	Port       string `mapstructure:"port" yaml:"port"`
}

type GlobalConfig struct {
	BaseDir    string                  `mapstructure:"base_dir" yaml:"base_dir"`
	OldBaseDir string                  `mapstructure:"basedir" yaml:"basedir,omitempty"`
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
	viper.Set("instances", Global.Instances)
	return viper.WriteConfigAs(ConfigFilePath)
}

func SaveGlobalConfig(baseDir string) error {
	os.MkdirAll(filepath.Dir(ConfigFilePath), 0755)
	Global.BaseDir = baseDir
	return writeConfig()
}

func SaveInstanceToRegistry(name, user, dataDir, binPath, port string) error {
	os.MkdirAll(filepath.Dir(ConfigFilePath), 0755)
	if Global.Instances == nil {
		Global.Instances = make(map[string]InstanceMeta)
	}
	Global.Instances[name] = InstanceMeta{
		User:    user,
		DataDir: dataDir,
		BinPath: binPath,
		Port:    port,
	}
	return writeConfig()
}

func RemoveInstanceFromRegistry(name string) error {
	if Global.Instances != nil {
		delete(Global.Instances, name)
		return writeConfig()
	}
	return nil
}
