package main

import (
	"fmt"
	"os"
	"path"
	"strings"
)

func writeAllFiles(files map[FileName]*SourceCodeFile, dstPath string) (int, error) {
	filesWritten := 0

	for _, f := range files {
		if len(f.PathFields) == 0 || f.PathFields[0] != rootDirName {
			return filesWritten, fmt.Errorf(
				"file %s does not have a complete path: %s",
				f.Name,
				strings.Join(f.PathFields, "/"))
		}

		dirPath := path.Join(dstPath, strings.Join(f.PathFields[1:], "/"))
		if err := os.MkdirAll(dirPath, 0750); err != nil {
			return filesWritten, fmt.Errorf("could not create directory '%s': %v", dirPath, err)
		}

		filePath := path.Join(dirPath, f.Name)
		if err := os.WriteFile(filePath, []byte(f.RawContent), 0640); err != nil {
			return filesWritten, fmt.Errorf("could not save file %s: %v", filePath, err)
		}

		filesWritten++
	}

	return filesWritten, nil
}
