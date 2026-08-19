package cmd

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strconv"

	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/spf13/cobra"

	"pg_mgr/internal/config"
	"pg_mgr/internal/i18n"
	"pg_mgr/internal/interaction"
	"pg_mgr/internal/utils"
)

var installPkgCmd = &cobra.Command{
	Use:     "install",
	Aliases: []string{"install-pkg"},
	Short:   i18n.T("install_pkg_desc"),
	RunE:    func(cmd *cobra.Command, args []string) error { return runInstallPkg(cmd) },
}

var installPkgLegacyCmd = &cobra.Command{
	Use:   "install-pkg",
	Short: i18n.T("install_pkg_desc"),
	RunE:  func(cmd *cobra.Command, args []string) error { return runInstallPkg(cmd) },
}

func init() {
	installPkgCmd.Flags().StringVarP(&Config.TarPath, "tar", "t", "postgresql-16.9-x64-Ubuntu24.04.tar.gz", i18n.T("flag_tar"))
	installPkgCmd.Flags().StringVar(&Config.MajorVersion, "major", "16", i18n.T("flag_major"))
	installPkgCmd.Flags().StringVar(&Config.MinorVersion, "minor", "9", i18n.T("flag_minor"))
	installPkgCmd.Flags().BoolVarP(&Config.Silent, "silent", "s", false, i18n.T("flag_silent_deprecated"))
	_ = installPkgCmd.Flags().MarkDeprecated("silent", i18n.T("flag_silent_replacement"))

	installPkgLegacyCmd.Flags().StringVarP(&Config.TarPath, "tar", "t", "postgresql-16.9-x64-Ubuntu24.04.tar.gz", i18n.T("flag_tar"))
	installPkgLegacyCmd.Flags().StringVar(&Config.MajorVersion, "major", "16", i18n.T("flag_major"))
	installPkgLegacyCmd.Flags().StringVar(&Config.MinorVersion, "minor", "9", i18n.T("flag_minor"))
	installPkgLegacyCmd.Flags().BoolVarP(&Config.Silent, "silent", "s", false, i18n.T("flag_silent_deprecated"))
	_ = installPkgLegacyCmd.Flags().MarkDeprecated("silent", i18n.T("flag_silent_replacement"))

	PkgCmd.AddCommand(installPkgCmd)
	RootCmd.AddCommand(installPkgLegacyCmd)
}

func runInstallPkg(cmd *cobra.Command) error {
	if err := utils.CheckRoot(); err != nil {
		return err
	}
	checkRemoveIPC()

	if !Config.Silent {
		Config.TarPath = utils.PromptPath(i18n.T("prompt_tar"), Config.TarPath)

		detectedMajor, detectedMinor, detected, vErr := utils.DetectAndVerifyTarVersion(Config.TarPath)
		if vErr != nil && !detected {
			fmt.Fprintln(os.Stderr, text.FgHiYellow.Sprint(i18n.T("warn_version_inspection", vErr)))
		} else if vErr != nil && detected {
			fmt.Fprintln(os.Stderr, text.FgHiYellow.Sprint(i18n.T("warn_package_execution", vErr, detectedMajor, detectedMinor)))
		}

		if detected {
			fnMajor, fnMinor, fnOk := utils.DetectVersionFromTar(Config.TarPath)
			if fnOk && (fnMajor != detectedMajor || fnMinor != detectedMinor) {
				fmt.Fprintln(os.Stderr, text.FgHiYellow.Sprint(i18n.T("warn_version_mismatch", fnMajor, fnMinor, detectedMajor, detectedMinor)))
			}
			Config.MajorVersion = detectedMajor
			Config.MinorVersion = detectedMinor
			fmt.Fprintln(os.Stderr, i18n.T("version_verified", detectedMajor, detectedMinor))
		} else {
			baseDir := config.Global.BaseDir
			installed, err := utils.GetInstalledVersions(baseDir)
			if err == nil && len(installed) > 0 {
				selected, selectErr := promptInstalledVersion(i18n.T("prompt_select_version"), installed, len(installed)-1)
				if selectErr != nil {
					fmt.Println(text.FgHiRed.Sprint(selectErr))
					return selectErr
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

	mode := interaction.OutputTable
	if UI.Output == string(interaction.OutputJSON) {
		mode = interaction.OutputJSON
	}
	operation := interaction.NewOperation(os.Stderr, mode)
	executeStep := func(msg string, action func() error) error {
		return operation.Run(msg, action)
	}

	baseDir := config.Global.BaseDir
	versionPathFull := filepath.Join(baseDir, Config.MajorVersion, Config.MinorVersion)

	pgBin := filepath.Join(versionPathFull, "bin", "postgres")
	if _, err := os.Stat(pgBin); err == nil {
		if !Config.Silent {
			overwritePrompt := i18n.T("confirm_overwrite_version", Config.MajorVersion, Config.MinorVersion, versionPathFull)
			if !utils.PromptConfirm(overwritePrompt) {
				fmt.Println(i18n.T("abort"))
				return nil
			}
		}
	}

	var pgUserHome string
	if err := executeStep(i18n.T("step_user"), func() error {
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
	}); err != nil {
		return err
	}

	if err := executeStep(i18n.T("step_extract"), func() error {
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
	}); err != nil {
		return err
	}

	if UI.Output == string(interaction.OutputJSON) {
		return interaction.NewRenderer(os.Stdout, os.Stderr, interaction.OutputJSON, UI.Quiet).Success(map[string]any{"version": Config.MajorVersion + "." + Config.MinorVersion, "status": "installed", "operation": operation.Result()})
	}
	if !UI.Quiet {
		fmt.Printf("\n%s\n", text.FgHiGreen.Sprint(i18n.T("done")))
	}
	return nil
}

func checkRemoveIPC() {
	if UI.Output == string(interaction.OutputJSON) {
		return
	}
	setting, err := utils.DetectLogindRemoveIPC()
	if err != nil {
		fmt.Fprintln(os.Stderr, text.FgHiYellow.Sprint(i18n.T("removeipc_check_failed", err)))
		fmt.Fprintln(os.Stderr, i18n.T("removeipc_manual_check"))
		return
	}
	if setting == "no" {
		fmt.Fprintln(os.Stderr, text.FgHiGreen.Sprint(i18n.T("removeipc_check_ok")))
		return
	}

	fmt.Fprintln(os.Stderr, text.FgHiYellow.Sprint(i18n.T("removeipc_warning", setting)))
	fmt.Fprintln(os.Stderr, i18n.T("removeipc_recommendation"))
}
