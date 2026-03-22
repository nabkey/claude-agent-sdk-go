package main

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

var skillsDir = filepath.Join(agentDir, "skills")

// parseSkillFrontmatter extracts name and description from YAML frontmatter.
func parseSkillFrontmatter(content string) (name, description string) {
	if !strings.HasPrefix(content, "---") {
		return "", ""
	}
	end := strings.Index(content[3:], "---")
	if end == -1 {
		return "", ""
	}
	frontmatter := content[3 : 3+end]
	for _, line := range strings.Split(frontmatter, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "name:") {
			name = strings.TrimSpace(strings.TrimPrefix(line, "name:"))
		} else if strings.HasPrefix(line, "description:") {
			description = strings.TrimSpace(strings.TrimPrefix(line, "description:"))
		}
	}
	return name, description
}

func handleListSkills(w http.ResponseWriter, r *http.Request) {
	entries, err := os.ReadDir(skillsDir)
	if err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, map[string]any{"skills": []any{}})
			return
		}
		writeError(w, 500, "failed to read skills directory: "+err.Error())
		return
	}

	type skillInfo struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Path        string `json:"path"`
	}

	var skills []skillInfo
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		skillPath := filepath.Join(skillsDir, e.Name(), "SKILL.md")
		data, err := os.ReadFile(skillPath)
		if err != nil {
			continue
		}
		name, desc := parseSkillFrontmatter(string(data))
		if name == "" {
			name = e.Name()
		}
		skills = append(skills, skillInfo{
			Name:        name,
			Description: desc,
			Path:        filepath.Join("skills", e.Name()),
		})
	}

	if skills == nil {
		skills = []skillInfo{}
	}
	writeJSON(w, map[string]any{"skills": skills})
}

func handleReadSkill(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if err := decodeBody(r, &req); err != nil {
		writeError(w, 400, "invalid request: "+err.Error())
		return
	}

	skillPath := filepath.Join(skillsDir, req.Name, "SKILL.md")
	// Validate path stays under skills dir
	abs, err := filepath.Abs(skillPath)
	if err != nil || !strings.HasPrefix(abs, skillsDir) {
		writeError(w, 403, "invalid skill name")
		return
	}

	data, err := os.ReadFile(skillPath)
	if err != nil {
		writeError(w, 404, "skill not found: "+req.Name)
		return
	}

	// Also collect any reference/example files
	skillDir := filepath.Join(skillsDir, req.Name)
	extras := make(map[string]string)
	for _, subdir := range []string{"references", "examples"} {
		subpath := filepath.Join(skillDir, subdir)
		files, _ := os.ReadDir(subpath)
		for _, f := range files {
			if f.IsDir() {
				continue
			}
			content, err := os.ReadFile(filepath.Join(subpath, f.Name()))
			if err == nil {
				extras[subdir+"/"+f.Name()] = string(content)
			}
		}
	}

	result := map[string]any{"content": string(data)}
	if len(extras) > 0 {
		result["files"] = extras
	}
	writeJSON(w, result)
}

func handleWriteSkill(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name    string `json:"name"`
		Content string `json:"content"`
	}
	if err := decodeBody(r, &req); err != nil {
		writeError(w, 400, "invalid request: "+err.Error())
		return
	}
	if req.Name == "" {
		writeError(w, 400, "name is required")
		return
	}

	skillDir := filepath.Join(skillsDir, req.Name)
	abs, err := filepath.Abs(skillDir)
	if err != nil || !strings.HasPrefix(abs, skillsDir) {
		writeError(w, 403, "invalid skill name")
		return
	}

	if err := os.MkdirAll(skillDir, 0755); err != nil {
		writeError(w, 500, "failed to create skill directory: "+err.Error())
		return
	}

	skillPath := filepath.Join(skillDir, "SKILL.md")
	if err := os.WriteFile(skillPath, []byte(req.Content), 0644); err != nil {
		writeError(w, 500, "failed to write skill: "+err.Error())
		return
	}

	writeJSON(w, map[string]any{"ok": true, "path": fmt.Sprintf("skills/%s/SKILL.md", req.Name)})
}

func handleDeleteSkill(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if err := decodeBody(r, &req); err != nil {
		writeError(w, 400, "invalid request: "+err.Error())
		return
	}

	skillDir := filepath.Join(skillsDir, req.Name)
	abs, err := filepath.Abs(skillDir)
	if err != nil || !strings.HasPrefix(abs, skillsDir) {
		writeError(w, 403, "invalid skill name")
		return
	}

	if _, err := os.Stat(skillDir); os.IsNotExist(err) {
		writeError(w, 404, "skill not found: "+req.Name)
		return
	}

	if err := os.RemoveAll(skillDir); err != nil {
		writeError(w, 500, "failed to delete skill: "+err.Error())
		return
	}

	writeJSON(w, map[string]bool{"ok": true})
}

func handleGetPrompt(w http.ResponseWriter, r *http.Request) {
	data, err := os.ReadFile(filepath.Join(agentDir, "prompt.md"))
	if err != nil {
		writeError(w, 404, "prompt.md not found")
		return
	}
	writeJSON(w, map[string]string{"content": string(data)})
}

func handleSetPrompt(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Content string `json:"content"`
	}
	if err := decodeBody(r, &req); err != nil {
		writeError(w, 400, "invalid request: "+err.Error())
		return
	}
	if err := os.WriteFile(filepath.Join(agentDir, "prompt.md"), []byte(req.Content), 0644); err != nil {
		writeError(w, 500, "failed to write prompt: "+err.Error())
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}
