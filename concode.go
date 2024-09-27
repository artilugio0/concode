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
			if err := fillPathForFile(file, dependents, map[FileName]bool{}, files); err != nil {
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

func fillPathForFile(file *SourceCodeFile, dependents map[FileName][]*SourceCodeFile, callstack map[FileName]bool, files map[string]*SourceCodeFile) error {
	if callstack[file.Name] {
		return nil
	}

	callstack[file.Name] = true
	defer func() { callstack[file.Name] = false }()

	if len(file.PathFields) > 0 && file.PathFields[0] == rootDirName {
		return nil
	}

	// if no other file imports this file, try determining this file's path
	// using its imports
	if len(dependents[file.Name]) == 0 {
		// check if the path can be determined with the siblings in the import list
		for _, imp := range file.Imports {
			impFields := strings.Split(imp, "/")
			f, ok := files[impFields[len(impFields)-1]]
			if !ok {
				panic("unexpected file")
			}
			err := fillPathForFile(f, dependents, callstack, files)
			if err != nil {
				return err
			}

			if len(f.PathFields) > 0 {
				file.PathFields = append([]string{}, f.PathFields...)
			}

			if impFields[0] == ".." {
				for i := 0; i < len(impFields) && impFields[i] == ".."; i++ {
					file.PathFields = append(file.PathFields, "dummy")
				}
			}

			if len(file.PathFields) > 0 {
				return nil
			}
		}

		// if the path could not be determined with the siblings,
		// find out how many dirs deep this file should be located
		// and add that amount of dummy dirs to the path
		file.PathFields = append(file.PathFields, rootDirName)
		parentsCount := countParentDirsFromImports(file, files, map[string]bool{})
		count := 0
		if parentsCount != nil {
			count = *parentsCount
		}
		for i := 0; i < count; i++ {
			file.PathFields = append(file.PathFields, "dummy")
		}

		return nil
	}

	for _, dependentFile := range dependents[file.Name] {
		err := fillPathForFile(dependentFile, dependents, callstack, files)
		if err != nil {
			return err
		}

		if len(dependentFile.PathFields) == 0 {
			continue
		}

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

		newPathFields := []string{}
		if importPathFields[0] == ".." {
			parentsCount := 0
			for i := 0; i < len(importPathFields) && importPathFields[i] == ".."; i++ {
				parentsCount++
			}

			if parentsCount >= len(dependentFile.PathFields) {
				continue
			}

			newPathFields = append([]string{}, dependentFile.PathFields[:len(dependentFile.PathFields)-parentsCount]...)
			newPathFields = append(newPathFields, importPathFields[parentsCount:]...)
		} else if importPathFields[0] == "." {
			newPathFields = append([]string{}, dependentFile.PathFields...)
			newPathFields = append(newPathFields, importPathFields[1:]...)
		} else {
			newPathFields = append([]string{rootDirName}, importPathFields...)
		}

		// always keep the longer path
		if len(newPathFields) >= len(file.PathFields) {
			file.PathFields = newPathFields
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

func countParentDirsFromImports(file *SourceCodeFile, files map[string]*SourceCodeFile, callstack map[string]bool) *int {
	if callstack[file.Name] {
		return nil
	}

	callstack[file.Name] = true
	defer func() { callstack[file.Name] = false }()

	// find out how many parentDirs deep this file should be located
	parentDirs := 0
	for _, imp := range file.Imports {
		parentsCount := 0
		importFields := strings.Split(imp, "/")

		if importFields[0] == ".." {
			for _, f := range importFields {
				if f == ".." {
					parentsCount++
				}
			}
		} else if importFields[0] == "." && strings.HasSuffix(importFields[1], ".sol") {
			f, ok := files[importFields[1]]
			if !ok {
				panic("unexpected file")
			}

			c := countParentDirsFromImports(f, files, callstack)
			if c != nil {
				parentsCount = *c
			}
		}

		parentDirs = max(parentsCount, parentDirs)
	}

	return &parentDirs
}
