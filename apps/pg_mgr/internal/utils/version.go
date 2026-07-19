package utils

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
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
