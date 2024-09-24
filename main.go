package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"strings"

	"golang.org/x/net/html"
)

func main() {
	targetDir := flag.String("d", "./concode", "Directory where the files are saved")
	flag.Parse()

	contractAddress := flag.Arg(0)
	if *targetDir == "" || contractAddress == "" {
		fmt.Fprintf(os.Stderr, "Usage: %s [-d TARGET_DIRECTORY] CONTRACT_ADDRESS\n", os.Args[0])
		os.Exit(1)
	}

	stat, err := os.Stat(*targetDir)
	if err != nil {
		if !os.IsNotExist(err) {
			panic("could not check if target directory exists: " + err.Error())
		}
		if err := os.Mkdir(*targetDir, 0755); err != nil {
			panic("could not create target directory: " + err.Error())
		}
	} else {
		if !stat.IsDir() {
			panic("target directory is not a directory")
		}
	}

	url := "https://etherscan.io/address/" + contractAddress
	resp, err := http.Get(url)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()

	tokenizer := html.NewTokenizer(resp.Body)
	fileName := ""
	for {
		tokenType := tokenizer.Next()
		if tokenType == html.ErrorToken {
			err := tokenizer.Err()
			if errors.Is(err, io.EOF) {
				return
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

				tokenType = tokenizer.Next()
				if tokenType != html.TextToken {
					panic("unexpected token type")
				}

				code := tokenizer.Text()
				filePath := path.Join(*targetDir, fileName)
				if err := os.WriteFile(filePath, code, 0640); err != nil {
					panic("could not save file: " + filePath + ": " + err.Error())
				}

				fileName = ""
				break
			}

			if !moreAttrs {
				break
			}
		}
	}
}
