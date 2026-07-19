package cmd

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"strings"

	"github.com/jedib0t/go-pretty/v6/text"
	"github.com/spf13/cobra"

	"pg_mgr/internal/config"
	"pg_mgr/internal/i18n"
	"pg_mgr/internal/utils"
)

var initCmd = &cobra.Command{
	Use:   "init",
	Short: i18n.T("init_desc"),
	Run:   func(cmd *cobra.Command, args []string) { runInit() },
}

func init() {
	RootCmd.AddCommand(initCmd)
}

func runInit() {
	if os.Geteuid() != 0 {
		fmt.Println(text.FgHiRed.Sprint(i18n.T("req_root")))
		os.Exit(1)
	}

	baseDir := utils.PromptInput(i18n.T("prompt_global_base"), config.Global.BaseDir)
	if err := config.SaveGlobalConfig(baseDir); err != nil {
		fmt.Println(i18n.T("err_failed", err))
		return
	}

	if err := updateRootProfile(baseDir); err != nil {
		fmt.Printf("Warning: failed to update root profile: %v\n", err)
	}

	fmt.Println(text.FgHiGreen.Sprint(i18n.T("init_success")))
}

func updateRootProfile(baseDir string) error {
	u, err := user.Lookup("root")
	var rootHome string
	if err != nil {
		rootHome = "/root"
	} else {
		rootHome = u.HomeDir
	}

	bashrcPath := filepath.Join(rootHome, ".bashrc")
	return updateProfileFile(bashrcPath, baseDir)
}

func updateProfileFile(bashrcPath string, baseDir string) error {
	blockStart := "# >>> pg_mgr sbin path >>>"
	blockEnd := "# <<< pg_mgr sbin path <<<"

	blockContent := fmt.Sprintf(`%s
export PG_MGR_BASE_DIR="%s"
if [ -d "$PG_MGR_BASE_DIR" ]; then
    highest_sbin=""
    highest_ver=""
    for major_dir in "$PG_MGR_BASE_DIR"/*; do
        if [ -d "$major_dir" ]; then
            major_name="${major_dir##*/}"
            if [[ "$major_name" =~ ^[0-9]+$ ]]; then
                for minor_dir in "$major_dir"/*; do
                    if [ -d "$minor_dir/sbin" ] && [ -f "$minor_dir/sbin/pg_mgr" ]; then
                        minor_name="${minor_dir##*/}"
                        if [[ "$minor_name" =~ ^[0-9]+$ ]]; then
                            ver_str="${major_name}.${minor_name}"
                            if [ -z "$highest_ver" ]; then
                                highest_ver="$ver_str"
                                highest_sbin="$minor_dir/sbin"
                            else
                                h_major="${highest_ver%%.*}"
                                h_minor="${highest_ver#*.}"
                                c_major="${ver_str%%.*}"
                                c_minor="${ver_str#*.}"
                                is_newer=0
                                if [ "$c_major" -gt "$h_major" ]; then
                                    is_newer=1
                                elif [ "$c_major" -eq "$h_major" ] && [ "$c_minor" -gt "$h_minor" ]; then
                                    is_newer=1
                                fi
                                if [ "$is_newer" -eq 1 ]; then
                                    highest_ver="$ver_str"
                                    highest_sbin="$minor_dir/sbin"
                                fi
                            fi
                        fi
                    fi
                done
            fi
        fi
    done
    if [ -n "$highest_sbin" ]; then
        if [[ ":$PATH:" != *":$highest_sbin:"* ]]; then
            export PATH="$highest_sbin:$PATH"
        fi
    fi
fi
%s
`, blockStart, baseDir, blockEnd)

	// Read existing content
	content := ""
	if data, err := os.ReadFile(bashrcPath); err == nil {
		content = string(data)
	}

	var newContent string
	if strings.Contains(content, blockStart) && strings.Contains(content, blockEnd) {
		startIndex := strings.Index(content, blockStart)
		endIndex := strings.Index(content, blockEnd) + len(blockEnd)
		newContent = content[:startIndex] + blockContent + content[endIndex:]
	} else {
		if content != "" && !strings.HasSuffix(content, "\n") {
			content += "\n"
		}
		newContent = content + blockContent
	}

	return os.WriteFile(bashrcPath, []byte(newContent), 0644)
}
