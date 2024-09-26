package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	targetDir := flag.String("d", "./concode", "Directory where the files are saved")
	importsBasePath := flag.String("b", "", "append base path to non relative imports")

	flag.Usage = func() {
		fmt.Println("Usage: concode [options] CONTRACT_ADDRESS")
		fmt.Println("Options:")
		flag.PrintDefaults()
	}

	flag.Parse()

	contractAddress := flag.Arg(0)
	if *targetDir == "" || contractAddress == "" {
		fmt.Fprintf(os.Stderr, "Usage: %s [-d TARGET_DIRECTORY] CONTRACT_ADDRESS\n", os.Args[0])
		os.Exit(1)
	}

	files, err := getFiles(contractAddress)
	if err != nil {
		panic(err)
	}

	if err := fillPaths(files); err != nil {
		panic(err)
	}

	if *importsBasePath != "" {
		addBasePathToImports(files, *importsBasePath)
	}

	writtenFiles, err := writeAllFiles(files, *targetDir)
	if err != nil {
		panic(err)
	}

	if writtenFiles != len(files) {
		panic(fmt.Sprintf("%d out of %d were written", writtenFiles, len(files)))
	}
}
