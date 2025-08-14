package main

import (
	"fmt"
	"io/ioutil"
	"path/filepath"
	"regexp"
	"strings"
)

// Simple CSS minifier
func minifyCSS(content string) string {
	// Remove comments
	content = regexp.MustCompile(`/\*.*?\*/`).ReplaceAllString(content, "")
	
	// Remove extra whitespace
	content = regexp.MustCompile(`\s+`).ReplaceAllString(content, " ")
	
	// Remove spaces around certain characters
	content = regexp.MustCompile(`\s*([{}:;,>+~])\s*`).ReplaceAllString(content, "$1")
	
	// Remove trailing semicolons before }
	content = regexp.MustCompile(`;}`).ReplaceAllString(content, "}")
	
	// Remove leading/trailing whitespace
	content = strings.TrimSpace(content)
	
	return content
}

// Simple JS minifier (basic)
func minifyJS(content string) string {
	// Remove single-line comments (but be careful with URLs)
	content = regexp.MustCompile(`^\s*//.*$`).ReplaceAllString(content, "")
	
	// Remove multi-line comments
	content = regexp.MustCompile(`/\*.*?\*/`).ReplaceAllString(content, "")
	
	// Remove extra whitespace (but preserve strings)
	lines := strings.Split(content, "\n")
	var result []string
	
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			result = append(result, line)
		}
	}
	
	return strings.Join(result, "\n")
}

func minifyFile(inputPath, outputPath string, isCSS bool) error {
	content, err := ioutil.ReadFile(inputPath)
	if err != nil {
		return fmt.Errorf("failed to read %s: %v", inputPath, err)
	}
	
	var minified string
	if isCSS {
		minified = minifyCSS(string(content))
	} else {
		minified = minifyJS(string(content))
	}
	
	err = ioutil.WriteFile(outputPath, []byte(minified), 0644)
	if err != nil {
		return fmt.Errorf("failed to write %s: %v", outputPath, err)
	}
	
	originalSize := len(content)
	minifiedSize := len(minified)
	reduction := float64(originalSize-minifiedSize) / float64(originalSize) * 100
	
	fmt.Printf("Minified %s: %d bytes → %d bytes (%.1f%% reduction)\n", 
		filepath.Base(inputPath), originalSize, minifiedSize, reduction)
	
	return nil
}

func main() {
	// Minify CSS files
	cssDir := "static/css"
	cssFiles := []string{"admin.css", "common.css", "home.css", "login.css", "moderator.css", "super-admin.css"}
	
	fmt.Println("Minifying CSS files...")
	for _, file := range cssFiles {
		inputPath := filepath.Join(cssDir, file)
		outputPath := filepath.Join(cssDir, strings.TrimSuffix(file, ".css")+".min.css")
		
		if err := minifyFile(inputPath, outputPath, true); err != nil {
			fmt.Printf("Error minifying %s: %v\n", file, err)
		}
	}
	
	// Minify JS files
	jsDir := "static/js"
	jsFiles := []string{"admin.js", "common.js", "home.js", "moderator.js", "super-admin.js"}
	
	fmt.Println("\nMinifying JS files...")
	for _, file := range jsFiles {
		inputPath := filepath.Join(jsDir, file)
		outputPath := filepath.Join(jsDir, strings.TrimSuffix(file, ".js")+".min.js")
		
		if err := minifyFile(inputPath, outputPath, false); err != nil {
			fmt.Printf("Error minifying %s: %v\n", file, err)
		}
	}
	
	fmt.Println("\nMinification complete!")
}