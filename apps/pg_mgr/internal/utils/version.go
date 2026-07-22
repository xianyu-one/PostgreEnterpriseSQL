package utils

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

type PGVersion struct {
	Major int
	Minor int
	Raw   string
}

func ParseVersion(vStr string) (PGVersion, error) {
	parts := strings.Split(vStr, ".")
	if len(parts) != 2 {
		return PGVersion{}, fmt.Errorf("invalid version format: %s", vStr)
	}
	major, err1 := strconv.Atoi(parts[0])
	minor, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return PGVersion{}, fmt.Errorf("invalid version numbers: %s", vStr)
	}
	return PGVersion{Major: major, Minor: minor, Raw: vStr}, nil
}

func CompareVersions(v1, v2 PGVersion) int {
	if v1.Major != v2.Major {
		return v1.Major - v2.Major
	}
	return v1.Minor - v2.Minor
}

func GetInstalledVersions(baseDir string) ([]PGVersion, error) {
	var versions []PGVersion
	majorEntries, err := os.ReadDir(baseDir)
	if err != nil {
		return nil, err
	}
	for _, majorEntry := range majorEntries {
		if !majorEntry.IsDir() {
			continue
		}
		major, err := strconv.Atoi(majorEntry.Name())
		if err != nil {
			continue
		}
		majorPath := filepath.Join(baseDir, majorEntry.Name())
		minorEntries, err := os.ReadDir(majorPath)
		if err != nil {
			continue
		}
		for _, minorEntry := range minorEntries {
			if !minorEntry.IsDir() {
				continue
			}
			minor, err := strconv.Atoi(minorEntry.Name())
			if err != nil {
				continue
			}
			postgresPath := filepath.Join(majorPath, minorEntry.Name(), "bin", "postgres")
			if _, err := os.Stat(postgresPath); err == nil {
				versions = append(versions, PGVersion{
					Major: major,
					Minor: minor,
					Raw:   fmt.Sprintf("%d.%d", major, minor),
				})
			}
		}
	}

	sort.Slice(versions, func(i, j int) bool {
		return CompareVersions(versions[i], versions[j]) < 0
	})

	return versions, nil
}

func GetPGVersion(binPath, dataDir, osUser string) string {
	if binPath != "" {
		var out string
		var err error
		if osUser != "" && osUser != "root" && os.Geteuid() == 0 {
			out, err = RunAsUserWithOutput(osUser, binPath+" -V")
		} else {
			cmdOut, cmdErr := exec.Command(binPath, "-V").Output()
			out = string(cmdOut)
			err = cmdErr
		}

		if err == nil && out != "" {
			re := regexp.MustCompile(`PostgreSQL\)?\s+([0-9]+\.[0-9]+)`)
			matches := re.FindStringSubmatch(out)
			if len(matches) >= 2 {
				return matches[1]
			}
			reNum := regexp.MustCompile(`([0-9]+\.[0-9]+)`)
			numMatch := reNum.FindStringSubmatch(out)
			if len(numMatch) >= 2 {
				return numMatch[1]
			}
		}

		dir := filepath.Dir(filepath.Dir(binPath))
		parts := strings.Split(filepath.ToSlash(dir), "/")
		if len(parts) >= 2 {
			majorStr := parts[len(parts)-2]
			minorStr := parts[len(parts)-1]
			if _, err1 := strconv.Atoi(majorStr); err1 == nil {
				if _, err2 := strconv.Atoi(minorStr); err2 == nil {
					return fmt.Sprintf("%s.%s", majorStr, minorStr)
				}
			}
		}
	}

	if dataDir != "" {
		verFile := filepath.Join(dataDir, "PG_VERSION")
		content, err := os.ReadFile(verFile)
		if err == nil {
			verStr := strings.TrimSpace(string(content))
			if verStr != "" {
				return verStr
			}
		}
	}

	return "Unknown"
}

func DetectAndVerifyTarVersion(tarPath string) (major string, minor string, ok bool, err error) {
	filenameMajor, filenameMinor, filenameDetected := DetectVersionFromTar(tarPath)

	file, oErr := os.Open(tarPath)
	if oErr != nil {
		if filenameDetected {
			return filenameMajor, filenameMinor, true, nil
		}
		return "", "", false, oErr
	}
	defer file.Close()

	gzReader, gErr := gzip.NewReader(file)
	if gErr != nil {
		if filenameDetected {
			return filenameMajor, filenameMinor, true, nil
		}
		return "", "", false, gErr
	}
	defer gzReader.Close()

	tarReader := tar.NewReader(gzReader)
	var foundPgContent []byte

	for {
		header, tErr := tarReader.Next()
		if tErr == io.EOF {
			break
		}
		if tErr != nil {
			break
		}

		cleanName := filepath.Clean(header.Name)
		if header.Typeflag == tar.TypeReg && (cleanName == "postgres" || strings.HasSuffix(cleanName, "/bin/postgres") || strings.HasSuffix(cleanName, "/postgres")) {
			content, readErr := io.ReadAll(tarReader)
			if readErr == nil && len(content) > 0 {
				foundPgContent = content
				break
			}
		}
	}

	if len(foundPgContent) == 0 {
		if filenameDetected {
			return filenameMajor, filenameMinor, true, nil
		}
		return "", "", false, fmt.Errorf("postgres binary not found in archive %s", tarPath)
	}

	tmpDir, tmpErr := os.MkdirTemp("", "pg_ver_check_*")
	if tmpErr != nil {
		if filenameDetected {
			return filenameMajor, filenameMinor, true, nil
		}
		return "", "", false, tmpErr
	}
	defer os.RemoveAll(tmpDir)

	tmpBin := filepath.Join(tmpDir, "postgres")
	if wErr := os.WriteFile(tmpBin, foundPgContent, 0755); wErr != nil {
		if filenameDetected {
			return filenameMajor, filenameMinor, true, nil
		}
		return "", "", false, wErr
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, tmpBin, "-V")
	outBytes, execErr := cmd.Output()
	if execErr != nil {
		return filenameMajor, filenameMinor, false, fmt.Errorf("binary 'postgres' in archive cannot run on current system: %v", execErr)
	}

	outStr := string(outBytes)
	re := regexp.MustCompile(`PostgreSQL\)?\s+([0-9]+)\.([0-9]+)`)
	matches := re.FindStringSubmatch(outStr)
	if len(matches) >= 3 {
		return matches[1], matches[2], true, nil
	}

	reNum := regexp.MustCompile(`([0-9]+)\.([0-9]+)`)
	numMatches := reNum.FindStringSubmatch(outStr)
	if len(numMatches) >= 3 {
		return numMatches[1], numMatches[2], true, nil
	}

	if filenameDetected {
		return filenameMajor, filenameMinor, true, nil
	}
	return "", "", false, fmt.Errorf("could not parse version output from binary: %s", outStr)
}

