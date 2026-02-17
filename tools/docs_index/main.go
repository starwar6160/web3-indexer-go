package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type DocFileInfo struct {
	Path        string
	Title       string
	Description string
	Category    string
}

func main() {
	docsDir := "./docs"
	summaryFile := filepath.Join(docsDir, "SUMMARY.md")

	categories := []string{"01-Architecture", "02-Database", "03-Web3-RPC", "04-Observability", "99-Operations"}
	docsByCat := make(map[string][]DocFileInfo)

	for _, cat := range categories {
		catPath := filepath.Join(docsDir, cat)
		files, err := os.ReadDir(catPath)
		if err != nil {
			continue
		}

		for _, file := range files {
			if file.IsDir() || !strings.HasSuffix(file.Name(), ".md") {
				continue
			}

			filePath := filepath.Join(cat, file.Name())
			absPath := filepath.Join(docsDir, filePath)
			info := extractDocInfo(absPath, filePath)
			info.Category = cat
			docsByCat[cat] = append(docsByCat[cat], info)
		}
	}

	generateSummary(summaryFile, categories, docsByCat)
}

func extractDocInfo(absPath, relPath string) DocFileInfo {
	info := DocFileInfo{Path: relPath, Title: relPath}
	file, err := os.Open(absPath)
	if err != nil {
		return info
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	inFrontMatter := false
	frontMatterContent := []string{}

	for scanner.Scan() {
		line := scanner.Text()
		if line == "---" {
			if !inFrontMatter {
				inFrontMatter = true
				continue
			} else {
				inFrontMatter = false
				break
			}
		}

		if inFrontMatter {
			frontMatterContent = append(frontMatterContent, line)
		} else if strings.HasPrefix(line, "# ") && info.Title == relPath {
			info.Title = strings.TrimPrefix(line, "# ")
		}
	}

	for _, line := range frontMatterContent {
		if strings.HasPrefix(line, "title:") {
			info.Title = strings.TrimSpace(strings.TrimPrefix(line, "title:"))
			info.Title = strings.Trim(info.Title, "\"")
		}
		if strings.HasPrefix(line, "ai_context:") {
			info.Description = strings.TrimSpace(strings.TrimPrefix(line, "ai_context:"))
			info.Description = strings.Trim(info.Description, "\"")
		}
	}

	return info
}

func generateSummary(summaryFile string, categories []string, docsByCat map[string][]DocFileInfo) {
	f, err := os.Create(summaryFile)
	if err != nil {
		fmt.Printf("Error creating summary file: %v\n", err)
		return
	}
	defer f.Close()

	f.WriteString("# Documentation Index\n\n")
	f.WriteString("Welcome to the Web3 Indexer documentation. This index is automatically generated.\n\n")

	for _, cat := range categories {
		docs := docsByCat[cat]
		if len(docs) == 0 {
			continue
		}

		f.WriteString(fmt.Sprintf("## %s\n", cat))
		for _, doc := range docs {
			desc := ""
			if doc.Description != "" {
				desc = fmt.Sprintf(" - %s", doc.Description)
			}
			f.WriteString(fmt.Sprintf("- [%s](%s)%s\n", doc.Title, doc.Path, desc))
		}
		f.WriteString("\n")
	}

	f.WriteString("---\n")
	f.WriteString("*Last Updated: Sunday, February 15, 2026*\n")
}
