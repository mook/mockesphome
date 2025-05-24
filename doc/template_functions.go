package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var templateFunctions = map[string]any{
	"yearRange": func(startYear int) string {
		currentYear := time.Now().Year()
		if startYear > 0 && startYear < currentYear {
			return fmt.Sprintf("%d-%d", startYear, currentYear)
		}
		return fmt.Sprintf("%d", currentYear)
	},
	"licenceText": func(item licenseItem) (string, error) {
		if item.LicenseFile == "" {
			return "", fmt.Errorf("no license file found for %s", item.Name)
		}

		contents, err := os.ReadFile(filepath.Join(item.Directory, item.LicenseFile))
		if err != nil {
			return "", fmt.Errorf("failed to read %s license file %s: %w", item.Name, item.LicenseFile, err)
		}

		return string(contents), nil
	},
	"line": func(ch string) string {
		return strings.Repeat(ch, 80)
	},
}
