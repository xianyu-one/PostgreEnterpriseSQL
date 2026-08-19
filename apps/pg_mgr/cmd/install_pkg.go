package cmd

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"

	"github.com/jedib0t/go-pretty/v6/progress"
	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/spf13/cobra"

	"pg_mgr/internal/config"
	"pg_mgr/internal/i18n"
	"pg_mgr/internal/utils"
)

var installPkgCmd = &cobra.Command{
	Use:     "install",
	Aliases: []string{"install-pkg"},
	Short:   i18n.T("install_pkg_desc"),
	Run:     func(cmd *cobra.Command, args []string) { runInstallPkg(cmd) },
}

var installPkgLegacyCmd = &cobra.Command{
	Use:   "install-pkg",
	Short: i18n.T("install_pkg_desc"),
	Run:   func(cmd *cobra.Command, args []string) { runInstallPkg(cmd) },
}

func init() {
	installPkgCmd.Flags().StringVarP(&Config.TarPath, "tar", "t", "postgresql-16.9-x64-Ubuntu24.04.tar.gz", "Path to the tar.gz package")
	installPkgCmd.Flags().StringVar(&Config.MajorVersion, "major", "16", "Major version path structure")
	installPkgCmd.Flags().StringVar(&Config.MinorVersion, "minor", "9", "Minor version path structure")
	installPkgCmd.Flags().BoolVarP(&Config.Silent, "silent", "s", false, "Run in silent mode without prompts")

	installPkgLegacyCmd.Flags().StringVarP(&Config.TarPath, "tar", "t", "postgresql-16.9-x64-Ubuntu24.04.tar.gz", "Path to the tar.gz package")
	installPkgLegacyCmd.Flags().StringVar(&Config.MajorVersion, "major", "16", "Major version path structure")
	installPkgLegacyCmd.Flags().StringVar(&Config.MinorVersion, "minor", "9", "Minor version path structure")
	installPkgLegacyCmd.Flags().BoolVarP(&Config.Silent, "silent", "s", false, "Run in silent mode without prompts")

	PkgCmd.AddCommand(installPkgCmd)
	RootCmd.AddCommand(installPkgLegacyCmd)
}

func runInstallPkg(cmd *cobra.Command) {
	utils.EnsureRoot()
	checkRemoveIPC()

	if !Config.Silent {
		Config.TarPath = utils.PromptPath(i18n.T("prompt_tar"), Config.TarPath)

		detectedMajor, detectedMinor, detected, vErr := utils.DetectAndVerifyTarVersion(Config.TarPath)
		if vErr != nil && !detected {
			fmt.Println(text.FgHiYellow.Sprintf("Warning: Version inspection failed: %v", vErr))
		} else if vErr != nil && detected {
			fmt.Println(text.FgHiYellow.Sprintf("Warning: Package binary execution check failed: %v (Using filename version: %s.%s)", vErr, detectedMajor, detectedMinor))
		}

		if detected {
			fnMajor, fnMinor, fnOk := utils.DetectVersionFromTar(Config.TarPath)
			if fnOk && (fnMajor != detectedMajor || fnMinor != detectedMinor) {
				fmt.Println(text.FgHiYellow.Sprintf("Warning: Version mismatch between tarball filename (%s.%s) and binary output (%s.%s). Using binary version.", fnMajor, fnMinor, detectedMajor, detectedMinor))
			}
			Config.MajorVersion = detectedMajor
			Config.MinorVersion = detectedMinor
			fmt.Printf("Auto-detected and verified version from tarball: %s.%s\n", detectedMajor, detectedMinor)
		} else {
			baseDir := config.Global.BaseDir
			installed, err := utils.GetInstalledVersions(baseDir)
			if err == nil && len(installed) > 0 {
				selected, selectErr := promptInstalledVersion(i18n.T("prompt_select_version"), installed, len(installed)-1)
				if selectErr != nil {
					fmt.Println(text.FgHiRed.Sprint(selectErr))
					return
				}
				Config.MajorVersion = strconv.Itoa(selected.Major)
				Config.MinorVersion = strconv.Itoa(selected.Minor)
			} else {
				Config.MajorVersion = utils.PromptInput(i18n.T("prompt_major"), Config.MajorVersion)
				Config.MinorVersion = utils.PromptInput(i18n.T("prompt_minor"), Config.MinorVersion)
			}
		}
	} else {
		detectedMajor, detectedMinor, detected, _ := utils.DetectAndVerifyTarVersion(Config.TarPath)
		if detected {
			if !cmd.Flags().Changed("major") {
				Config.MajorVersion = detectedMajor
			}
			if !cmd.Flags().Changed("minor") {
				Config.MinorVersion = detectedMinor
			}
		}
	}

	pw := progress.NewWriter()
	pw.SetAutoStop(false)
	pw.SetTrackerLength(25)
	pw.SetMessageWidth(40)
	pw.Style().Colors = progress.StyleColorsExample
	pw.Style().Options.DoneString = "✓"
	pw.Style().Options.ErrorString = "✗"
	go pw.Render()

	executeStep := func(msg string, action func() error) {
		tracker := progress.Tracker{Message: msg, Total: 1, Units: progress.UnitsDefault}
		pw.AppendTracker(&tracker)
		if err := action(); err != nil {
			tracker.MarkAsErrored()
			pw.Stop()
			fmt.Printf("\n%s\n", text.FgHiRed.Sprint(i18n.T("err_failed", err)))
			os.Exit(1)
		}
		tracker.MarkAsDone()
	}

	baseDir := config.Global.BaseDir
	versionPathFull := filepath.Join(baseDir, Config.MajorVersion, Config.MinorVersion)

	pgBin := filepath.Join(versionPathFull, "bin", "postgres")
	if _, err := os.Stat(pgBin); err == nil {
		if !Config.Silent {
			overwritePrompt := i18n.T("confirm_overwrite_version", Config.MajorVersion, Config.MinorVersion, versionPathFull)
			if !utils.PromptConfirm(overwritePrompt) {
				fmt.Println(i18n.T("abort"))
				return
			}
		}
	}

	var pgUserHome string
	executeStep(i18n.T("step_user"), func() error {
		u, err := user.Lookup("postgres")
		if err != nil {
			pgUserHome = filepath.Join(baseDir, "home")
			_ = utils.RunCmd("groupadd", "-g", "5432", "postgres")
			_ = utils.RunCmd("useradd", "-g", "postgres", "-u", "5432", "-d", pgUserHome, "postgres")
			u, _ = user.Lookup("postgres")
		} else {
			pgUserHome = u.HomeDir
		}

		uid, _ := strconv.Atoi(u.Uid)
		gid, _ := strconv.Atoi(u.Gid)

		dirs := []string{versionPathFull, pgUserHome}
		for _, d := range dirs {
			os.MkdirAll(d, 0755)
			os.Chown(d, uid, gid)
		}

		return utils.RunCmd("loginctl", "enable-linger", "postgres")
	})

	executeStep(i18n.T("step_extract"), func() error {
		file, err := os.Open(Config.TarPath)
		if err != nil {
			return err
		}
		defer file.Close()
		u, _ := user.Lookup("postgres")
		uid, _ := strconv.Atoi(u.Uid)
		gid, _ := strconv.Atoi(u.Gid)
		if err := utils.UntarGz(file, versionPathFull, uid, gid); err != nil {
			return err
		}
		return utils.EnsurePkgPermissions(versionPathFull)
	})

	pw.Stop()
	fmt.Printf("\n%s\n", text.FgHiGreen.Sprint(i18n.T("done")))
}

func checkRemoveIPC() {
	setting, err := utils.DetectLogindRemoveIPC()
	if err != nil {
		fmt.Println(text.FgHiYellow.Sprint(i18n.T("removeipc_check_failed", err)))
		fmt.Println(i18n.T("removeipc_manual_check"))
		return
	}
	if setting == "no" {
		fmt.Println(text.FgHiGreen.Sprint(i18n.T("removeipc_check_ok")))
		return
	}

	fmt.Println(text.FgHiYellow.Sprint(i18n.T("removeipc_warning", setting)))
	fmt.Println(i18n.T("removeipc_recommendation"))
}
