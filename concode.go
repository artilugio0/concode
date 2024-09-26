package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"

	"golang.org/x/net/html"
)

const baseUrl string = "https://etherscan.io/address/"
const rootDirName string = "<ROOT>"

type FileName = string

type SourceCodeFile struct {
	Name         FileName
	RawContent   string
	Dependencies []FileName
	PathFields   []string
	Imports      []string
}

func getFiles(contractAddress string) (map[FileName]*SourceCodeFile, error) {
	url := baseUrl + contractAddress
	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("get request failed: %v", err)
	}
	defer resp.Body.Close()

	files := map[string]*SourceCodeFile{}

	tokenizer := html.NewTokenizer(resp.Body)
	fileName := ""
	for {
		tokenType := tokenizer.Next()
		if tokenType == html.ErrorToken {
			err := tokenizer.Err()
			if errors.Is(err, io.EOF) {
				break
			}
			panic(err)
		}

		if tokenType == html.TextToken {
			text := string(tokenizer.Text())
			if strings.Contains(text, "File ") {
				fields := strings.Fields(text)
				fileName = fields[len(fields)-1]
			}
			continue
		}

		for {
			k, v, moreAttrs := tokenizer.TagAttr()
			if string(k) == "class" && bytes.Contains(v, []byte("js-sourcecopyarea")) {
				if fileName == "" {
					// not a contract code file
					break
				}

				rawContent := ""
				thisTokenType := tokenizer.Next()
				for {
					if thisTokenType == html.TextToken {
						rawContent += string(tokenizer.Text())
					}

					thisTokenType = tokenizer.Next()
					tagName, _ := tokenizer.TagName()

					if string(tagName) == "pre" && thisTokenType == html.EndTagToken {
						break
					}
				}

				file := &SourceCodeFile{
					Name:       fileName,
					RawContent: rawContent,
				}
				fillDependenciesAndImports(file)
				files[fileName] = file

				fileName = ""
				break
			}

			if !moreAttrs {
				break
			}
		}
	}

	return files, nil
}

func fillDependenciesAndImports(file *SourceCodeFile) {
	for _, line := range strings.Split(file.RawContent, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "import ") {
			fields := strings.Fields(line)
			importedFilePath := strings.Trim(fields[len(fields)-1], `'";`)
			importedFilePathFields := strings.Split(importedFilePath, "/")
			importedFilePathName := importedFilePathFields[len(importedFilePathFields)-1]

			file.Imports = append(file.Imports, importedFilePath)
			file.Dependencies = append(file.Dependencies, importedFilePathName)
		}
	}
}

func fillPaths(files map[FileName]*SourceCodeFile) error {
	// Create a mapping to determine which files depend on a specific file
	dependents := map[FileName][]*SourceCodeFile{}

	for _, file := range files {
		for _, dependency := range file.Dependencies {
			dependents[dependency] = append(dependents[dependency], file)
		}
	}

	totalDone := 0
	for {
		done := 0
		for _, file := range files {
			if err := fillPathForFile(file, dependents, map[FileName]bool{}); err != nil {
				return err
			}

			if len(file.PathFields) > 0 && file.PathFields[0] != rootDirName {
				done++
			}
		}

		// track if new paths where found. If not, there is nothing left to do
		if done == totalDone {
			break
		}
		totalDone = done
	}

	return nil
}

func fillPathForFile(file *SourceCodeFile, dependents map[FileName][]*SourceCodeFile, callstack map[FileName]bool) error {
	if callstack[file.Name] {
		return nil
	}

	callstack[file.Name] = true
	defer func() { callstack[file.Name] = false }()

	if len(file.PathFields) > 0 && file.PathFields[0] == rootDirName {
		return nil
	}

	if len(dependents[file.Name]) == 0 {
		file.PathFields = append(file.PathFields, rootDirName)
		return nil
	}

	for _, dependentFile := range dependents[file.Name] {
		fillPathForFile(dependentFile, dependents, callstack)
		// find out how the dependent is importing this file
		// get the index in the import array
		found := false
		importIndex := 0
		for i, dependency := range dependentFile.Dependencies {
			if dependency == file.Name {
				importIndex = i
				found = true
			}
		}

		if !found {
			return fmt.Errorf(
				"could not find file '%s' in dependent's array of dependencies (Dependent: %s)",
				file.Name,
				dependentFile.Name)
		}

		importPath := dependentFile.Imports[importIndex]
		importPathFields := strings.Split(importPath, "/")
		importPathFields = importPathFields[:len(importPathFields)-1]

		if importPathFields[0] == ".." {
			parentsCount := 0
			for i := 0; i < len(importPathFields) && importPathFields[i] == ".."; i++ {
				parentsCount++
			}
			file.PathFields = append([]string{}, dependentFile.PathFields[:len(dependentFile.PathFields)-parentsCount]...)
			file.PathFields = append(file.PathFields, importPathFields[parentsCount:]...)
		} else if importPathFields[0] == "." {
			file.PathFields = append([]string{}, dependentFile.PathFields...)
			file.PathFields = append(file.PathFields, importPathFields[1:]...)
		} else {
			file.PathFields = append([]string{rootDirName}, importPathFields...)
		}
	}

	return nil
}

func addBasePathToImports(files map[FileName]*SourceCodeFile, basePath string) {
	for _, file := range files {
		newRawLines := []string{}
		for _, line := range strings.Split(file.RawContent, "\n") {
			// only interested in import lines
			if !strings.HasPrefix(strings.TrimSpace(line), "import") {
				newRawLines = append(newRawLines, line)
				continue
			}

			importLineFields := strings.Fields(strings.TrimSpace(line))
			importPath := strings.Trim(importLineFields[len(importLineFields)-1], `'";`)

			// relative imports do not have to be added the basePath
			if strings.HasPrefix(importPath, ".") {
				newRawLines = append(newRawLines, line)
				continue
			}

			newImportPath := path.Join(basePath, importPath)
			newLine := strings.Replace(line, importPath, newImportPath, 1)

			newRawLines = append(newRawLines, newLine)
		}
		file.RawContent = strings.Join(newRawLines, "\n")
	}
}
