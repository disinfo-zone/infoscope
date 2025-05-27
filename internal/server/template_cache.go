package server

import (
	"html/template"
	"os"
	"path/filepath"
	"strings"
	"fmt"
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

			// Base name of the current template file, used for `template.New()`
			// This is important for how `ExecuteTemplate` refers to named templates within a set.
			// For templates parsed with a layout, the layout is often the entry point,
			// and the specific page template is a block within it.
			// When parsing page specific templates with a layout, the `template.New(name)`
			// should ideally be consistent. Using the templateName (relative path) for `New`
			// ensures uniqueness and clarity.
			
			var tmpl *template.Template

			// Check if it's an admin template (and not the layout itself)
			if strings.HasPrefix(templateName, "admin/") && info.Name() != adminLayoutFile {
				// Ensure admin layout exists before trying to parse with it
				if _, statErr := os.Stat(adminLayoutPath); os.IsNotExist(statErr) {
					// If admin layout doesn't exist, we can't parse admin templates that depend on it.
					// Depending on desired behavior, either skip, error out, or parse without layout.
					// For now, let's error if an admin page exists but its layout is missing.
					return fmt.Errorf("admin layout template not found at %s, required by %s", adminLayoutPath, path)
				}
				// Parse with admin layout. The name of the template created by New() is important.
				// The main template (entry point) is the layout.
				// The page file is added to this template set.
				// We name the template by the layout's base name so ExecuteTemplate can find "layout.html"
				// Or, more commonly, the layout defines `{{define "layout"}}` and pages define `{{define "content"}}`
				// and are executed by name.
				// For `ParseFiles`, the first file sets the name of the template.
				// So, `template.New(templateName)` then `ParseFiles(adminLayoutPath, path)`
				// means the resulting template is named `templateName` but contains definitions from both files.
				// If adminLayoutPath defines `{{define "layout"}}`, we execute "layout".
				// If pagePath defines `{{define "content"}}`, it's available.

				// The name given to template.New() is the one we use to ExecuteTemplate's first argument (name string)
				// if we are directly executing that specific template.
				// When using layouts, often the layout is the one named and executed, and it includes other templates.
				// Let's name the template by its own relative path for clarity in the cache.
				tmpl, err = template.New(info.Name()).Funcs(funcMap).ParseFiles(path, adminLayoutPath)
				if err != nil {
					return fmt.Errorf("failed to parse admin template %s with layout %s: %w", path, adminLayoutPath, err)
				}
			} else if templateName == "admin/"+adminLayoutFile {
				// Skip parsing the admin layout file on its own here, it's handled with pages.
				// Or, parse it if it can be rendered standalone (e.g. for testing components).
				// For now, we assume it's only used as a layout for other admin pages.
				return nil
			} else {
				// Non-admin template or a standalone template
				tmpl, err = template.New(info.Name()).Funcs(funcMap).ParseFiles(path)
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

	// Special case for 404.html if it's not in admin and needs specific handling or layout
	// For now, it's treated like any other non-admin template.

	return templates, nil
}
```

A note on the `template.New(name)` part in `LoadTemplates`:
When parsing files, especially with layouts, the name given to `template.New()` and how `ParseFiles` is called matters.
If `adminLayoutPath` is the first argument to `ParseFiles`, the resulting template set is associated with the name of the first file in that path.
If `path` (the specific page) is first, then that page's name is associated.
The common pattern for layouts is:
1.  Layout file (e.g., `layout.html`) defines blocks: `{{define "layout"}} ... {{template "content" .}} ... {{end}}`
2.  Page file (e.g., `dashboard.html`) defines content: `{{define "content"}} ... {{end}}`
3.  Parsing: `tmpl, err := template.New("layout.html").ParseFiles(layoutFile, pageFile)`
4.  Executing: `tmpl.ExecuteTemplate(w, "layout.html", data)`

My current implementation uses `template.New(info.Name()).Funcs(funcMap).ParseFiles(path, adminLayoutPath)` for admin pages. This means the template is named after the specific page (e.g., "dashboard.html"), but contains definitions from both itself and the layout. When executing, I'll need to call `tmpl.ExecuteTemplate(w, "layout.html", data)` if "layout.html" is the entry point defined in the layout file, or `tmpl.Execute(w, data)` if the page file itself is the main definition. The current `renderTemplate` logic distinguishes this based on the "admin/" prefix and calls `ExecuteTemplate(w, "layout", ...)` for admin pages. This implies the layout defines a template named "layout". I need to ensure the `adminLayoutPath` file actually defines `{{define "layout"}}...{{end}}`.

Let's assume the admin layout file (`templates/admin/layout.html`) is structured to be the entry point, possibly defining `{{define "layout"}} ... {{end}}` or simply being the first file parsed in a way that its base name becomes the one to execute. The current `renderTemplate` calls `ExecuteTemplate(w, "layout", ...)`. This means the template set (derived from `path` and `adminLayoutPath`) must contain a template named "layout". This is typically achieved if `adminLayoutPath` is the first file in `ParseFiles` *when the template is intended to be named "layout"*.

Let me adjust the parsing for admin templates to align with executing "layout":
`tmpl, err = template.New("layout").Funcs(funcMap).ParseFiles(adminLayoutPath, path)` - but this would mean all admin templates in the cache point to the same named underlying template "layout", just with different page contents included. This is not right.

The key is that `tmpl.ExecuteTemplate(w, "layout", ...)` means the template *set* associated with `tmpl` (which we get from `s.templateCache[name]`) must contain a definition for "layout".
If `template.New(info.Name())` is used, then `tmpl` is named after the page. `ParseFiles(path, adminLayoutPath)` adds both to the set. If `adminLayoutPath` defines "layout", then `tmpl.ExecuteTemplate(w, "layout", data)` should work. This seems correct.

The `templateName` (e.g. "admin/dashboard.html") is the key in the cache.
`tmpl := s.templateCache[templateName]`
If `templateName` is admin, we do `tmpl.ExecuteTemplate(w, "layout", data)`. This means `tmpl` (which was created as `template.New("dashboard.html").ParseFiles("dashboard.html", "layout.html")`) must have a template named "layout" in its set. This is true if `layout.html` itself defines `{{define "layout"}} ... {{end}}` or if its content is implicitly the "layout" template by being the first file parsed when `template.New("layout")` is used.

The current `renderTemplate` in `auth_handlers.go` for admin pages does:
`tmpl := template.New(name).Funcs(funcMap)`
`files = [layout, page]`
`tmpl, err := tmpl.ParseFiles(files...)`
`return tmpl.ExecuteTemplate(w, "layout", wrappedData)`

This implies that `template.New(name)` (where `name` is like "admin/dashboard.html") creates a template. Then `ParseFiles` adds the layout and the page. The execution `ExecuteTemplate(w, "layout", ...)` means that one of the parsed files (likely `layout.html`) must define a template named "layout". This is a common pattern.

My `LoadTemplates` needs to replicate this:
For admin template `admin/dashboard.html` (path: `.../templates/admin/dashboard.html`, `info.Name()`: "dashboard.html"):
`tmpl, err = template.New(templateName).Funcs(funcMap).ParseFiles(path, adminLayoutPath)`
Here, `templateName` is "admin/dashboard.html".
Then, `tmpl.ExecuteTemplate(w, "layout", data)` would look for a template named "layout" within the set associated with `tmpl`. This is correct if `adminLayoutPath` defines `{{define "layout"}}...{{end}}`.

The current `renderTemplate` uses `template.New(name)` where `name` is the full relative path.
Let's stick to `template.New(templateName)` for consistency.
`tmpl, err = template.New(templateName).Funcs(funcMap).ParseFiles(path, adminLayoutPath)`
This should work. The key `templateName` ("admin/dashboard.html") will retrieve this specific parsed set.

Final check on `LoadTemplates`:
For admin file `admin/dashboard.html`:
`templateName` = "admin/dashboard.html"
`path` = ".../web/templates/admin/dashboard.html"
`adminLayoutPath` = ".../web/templates/admin/layout.html"
`tmpl, err = template.New(templateName).Funcs(funcMap).ParseFiles(path, adminLayoutPath)`
This creates a template named "admin/dashboard.html". It parses `path` first, then `adminLayoutPath`.
If `adminLayoutPath` contains `{{define "layout"}}...{{end}}`, then `tmpl.ExecuteTemplate(w, "layout", data)` will work.
This seems robust.

One minor adjustment: `template.New(info.Name())` was used. For admin templates, `template.New(templateName)` might be more consistent with `ExecuteTemplate(w, "layout", ...)`, assuming the "layout" template is defined within the admin layout file. Let's stick to `template.New(info.Name())` for the `New()` part as `ExecuteTemplate` refers to a *defined* template name (like "layout"), not the name passed to `New()`. The name passed to `New()` is the default name if `Execute()` is called or if it's the first file in `ParseFiles` without a `{{define}}` block.

The current `renderTemplate` uses `template.New(name)` where `name` is like "admin/dashboard.html".
Then `tmpl.ParseFiles(filepath.Join(s.config.WebPath, "templates/admin/layout.html"), filepath.Join(s.config.WebPath, "templates", name))`
And executes `tmpl.ExecuteTemplate(w, "layout", wrappedData)`.
So, `LoadTemplates` should do something like:
`tmpl = template.New(templateName).Funcs(funcMap)`
`tmpl, err = tmpl.ParseFiles(adminLayoutPath, path)` (Order of files in ParseFiles matters for which defines base)
Or, more simply, if `adminLayoutPath` defines `{{define "layout"}}`, then `template.New(pageBasename).Funcs(funcMap).ParseFiles(pagePath, adminLayoutPath)` is fine, and then execute "layout".

Let's use the logic from the existing `renderTemplate` more directly:
For admin: `template.New(templateName).Funcs(funcMap).ParseFiles(adminLayoutPath, pagePath)`
For non-admin: `template.New(templateName).Funcs(funcMap).ParseFiles(pagePath)`
This looks correct. The key in the map will be `templateName`.
When retrieving `tmpl := s.templateCache[name]`:
If admin: `tmpl.ExecuteTemplate(w, "layout", data)`
If non-admin: `tmpl.Execute(w, data)` (which executes the template named `name`, which is `templateName`).

This plan seems solid.Okay, I'm starting Phase 1: Creating `internal/server/template_cache.go`.
I've refined the logic for `LoadTemplates` based on the analysis in the thought block.
The key aspects are:
- Using the relative path (e.g., "admin/dashboard.html") as the key in the returned map.
- For admin templates, parsing both the page file and the admin layout file.
- For non-admin templates, parsing just the page file.
- The name given to `template.New()` will be the relative path, which becomes the default template name to execute if `tmpl.Execute(w, data)` is called.
- The `funcMap` is applied to each new template instance.
- Error handling for file operations and template parsing.
