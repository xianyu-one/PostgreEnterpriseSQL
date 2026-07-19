package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/spf13/cobra"

	"pg_mgr/internal/config"
	"pg_mgr/internal/i18n"
	"pg_mgr/internal/process"
	"pg_mgr/internal/utils"
)

var (
	checkRoot       = func() bool { return os.Geteuid() == 0 }
	findPgProcesses = process.FindPgProcesses
)

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Scan running PostgreSQL processes and update the global registry configuration",
	Run: func(cmd *cobra.Command, args []string) {
		if !checkRoot() {
			fmt.Println(text.FgHiRed.Sprint(i18n.T("req_root")))
			os.Exit(1)
		}
		scanAndSyncInstances()
	},
}

func init() {
	RootCmd.AddCommand(syncCmd)
}

func scanAndSyncInstances() {
	if !checkRoot() {
		return
	}

	runningProcs := findPgProcesses()
	if len(runningProcs) == 0 {
		return
	}

	configChanged := false

	for _, proc := range runningProcs {
		procDataDir := filepath.Clean(proc.DataDir)
		procPort := proc.Port

		// Find if there is a registered instance with the same DataDir
		var matchByDirName string
		var matchByDirMeta config.InstanceMeta
		foundByDir := false

		for name, meta := range config.Global.Instances {
			if filepath.Clean(meta.DataDir) == procDataDir {
				matchByDirName = name
				matchByDirMeta = meta
				foundByDir = true
				break
			}
		}

		if foundByDir {
			// Case 1: Same DataDir, check if Port matches
			if matchByDirMeta.Port != procPort {
				fmt.Println()
				fmt.Println(text.FgHiYellow.Sprint(i18n.T("sync_port_mismatch", matchByDirName, procPort, matchByDirMeta.Port)))
				fmt.Println(i18n.T("sync_prompt_choice", proc.PID, procPort, procDataDir, matchByDirName))

				ans := ""
				for {
					ans = utils.PromptInput("Enter choice / 请输入选择 (1/2/3)", "3")
					if ans == "1" || ans == "2" || ans == "3" {
						break
					}
				}

				if ans == "1" {
					err := config.SaveInstanceToRegistry(matchByDirName, proc.OSUser, procDataDir, proc.BinPath, procPort)
					if err != nil {
						fmt.Printf("Error updating registry: %v\n", err)
					} else {
						fmt.Println(text.FgHiGreen.Sprint(i18n.T("sync_update_success", matchByDirName)))
						configChanged = true
					}
				} else if ans == "2" {
					registerAsNew(proc.OSUser, procDataDir, proc.BinPath, procPort)
					configChanged = true
				}
			}
		} else {
			// Case 2: DataDir doesn't match, check if Port matches any registered instance
			var matchByPortName string
			var matchByPortMeta config.InstanceMeta
			foundByPort := false

			for name, meta := range config.Global.Instances {
				if meta.Port == procPort {
					matchByPortName = name
					matchByPortMeta = meta
					foundByPort = true
					break
				}
			}

			if foundByPort {
				fmt.Println()
				fmt.Println(text.FgHiYellow.Sprint(i18n.T("sync_datadir_mismatch", procPort, procDataDir, matchByPortName, matchByPortMeta.DataDir)))
				fmt.Println(i18n.T("sync_prompt_choice", proc.PID, procPort, procDataDir, matchByPortName))

				ans := ""
				for {
					ans = utils.PromptInput("Enter choice / 请输入选择 (1/2/3)", "3")
					if ans == "1" || ans == "2" || ans == "3" {
						break
					}
				}

				if ans == "1" {
					err := config.SaveInstanceToRegistry(matchByPortName, proc.OSUser, procDataDir, proc.BinPath, procPort)
					if err != nil {
						fmt.Printf("Error updating registry: %v\n", err)
					} else {
						fmt.Println(text.FgHiGreen.Sprint(i18n.T("sync_update_success", matchByPortName)))
						configChanged = true
					}
				} else if ans == "2" {
					registerAsNew(proc.OSUser, procDataDir, proc.BinPath, procPort)
					configChanged = true
				}
			}
		}
	}

	if configChanged {
		config.InitConfig()
	}
}

func registerAsNew(osUser, dataDir, binPath, port string) {
	for {
		name := utils.PromptInput(i18n.T("sync_enter_name"), "new_instance_"+port)
		if _, exists := config.Global.Instances[name]; exists {
			fmt.Println(text.FgHiRed.Sprint(i18n.T("sync_err_exists", name)))
			continue
		}
		err := config.SaveInstanceToRegistry(name, osUser, dataDir, binPath, port)
		if err != nil {
			fmt.Printf("Error saving to registry: %v\n", err)
		} else {
			fmt.Println(text.FgHiGreen.Sprint(i18n.T("sync_register_success", name)))
		}
		break
	}
}
