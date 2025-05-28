package server

import (
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"strings"
)

// LoadTemplates parses all HTML templates from the specified webPath and returns a map
// of parsed templates, keyed by their relative path from the "templates" directory.
// Admin templates are parsed with the admin layout.
func LoadTemplates(webPath string, funcMap template.FuncMap) (map[string]*template.Template, error) {
	templates := make(map[string]*template.Template)
	templatesDir := filepath.Join(webPath, "templates")
	adminLayoutFile := "layout.html" // Base name of the admin layout file
	adminLayoutPath := filepath.Join(templatesDir, "admin", adminLayoutFile)

	err := filepath.Walk(templatesDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip directories
		if info.IsDir() {
			return nil
		}

		// Process only .html files
		if strings.HasSuffix(info.Name(), ".html") {
			// Get relative path to use as template name (e.g., "admin/dashboard.html", "login.html")
			relPath, err := filepath.Rel(templatesDir, path)
			if err != nil {
				return fmt.Errorf("failed to get relative path for %s: %w", path, err)
			}
			// Normalize to use forward slashes for map keys, consistent with URL paths
			templateName := filepath.ToSlash(relPath)

			var tmpl *template.Template

			// Check if it's an admin template (and not the layout itself)
			if strings.HasPrefix(templateName, "admin/") && info.Name() != adminLayoutFile {
				if _, statErr := os.Stat(adminLayoutPath); os.IsNotExist(statErr) {
					return fmt.Errorf("admin layout template not found at %s, required by %s", adminLayoutPath, path)
				}
				// Admin templates are named by their own path (e.g., "admin/dashboard.html")
				// and include both their own content and the admin layout.
				// renderTemplate will later call tmpl.ExecuteTemplate(w, "layout.html", data)
				// This requires "layout.html" to be a defined template name, typically from adminLayoutPath.
				tmpl, err = template.New(templateName).Funcs(funcMap).ParseFiles(path, adminLayoutPath)
				if err != nil {
					return fmt.Errorf("failed to parse admin template %s with layout %s: %w", path, adminLayoutPath, err)
				}
			} else if templateName == "admin/"+adminLayoutFile {
				// Skip parsing the admin layout file on its own; it's handled with pages.
				return nil
			} else {
				// Non-admin template.
				// The template is named by its own path (e.g., "login.html").
				// renderTemplate will later call tmpl.Execute(w, data).
				tmpl, err = template.New(templateName).Funcs(funcMap).ParseFiles(path)
				if err != nil {
					return fmt.Errorf("failed to parse template %s: %w", path, err)
				}
			}
			templates[templateName] = tmpl
		}
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("error walking templates directory %s: %w", templatesDir, err)
	}

	return templates, nil
}
